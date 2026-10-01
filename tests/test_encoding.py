"""Files people edit by hand on Windows, and output an agent captures through a
pipe. Windows PowerShell 5.1 (`Set-Content -Encoding UTF8`) saves with a UTF-8
BOM; a Windows pipe gets the ANSI code page; text files get CRLF unless told
otherwise."""

import json
import os
import subprocess
import sys
import tempfile
import unittest
from pathlib import Path
from unittest.mock import patch

from ix_ssh.domain import UsageError
from ix_ssh.infrastructure import load_ssh_config, read_config_file, read_inventory, write_backup

BOM = b"\xef\xbb\xbf"
NOTE = "café — 本社"  # outside cp932 (é, em dash) and inside it (本社)


class HandEditedFileTest(unittest.TestCase):
    def setUp(self):
        self.tmp = tempfile.TemporaryDirectory()
        self.addCleanup(self.tmp.cleanup)
        self.root = Path(self.tmp.name)

    def test_inventory_with_bom(self):
        path = self.root / "devices.json"
        path.write_bytes(BOM + json.dumps({"devices": {"r1": {"host": "h"}}}).encode())
        devices, _ = read_inventory(str(path), {})
        self.assertEqual(devices["r1"]["host"], "h")

    def test_config_file_with_bom_sends_no_bom(self):
        path = self.root / "changes.ix"
        path.write_bytes(BOM + b"logging buffered 100\r\nip route default Null0\r\n")
        self.assertEqual(
            read_config_file(str(path)), ["logging buffered 100", "ip route default Null0"]
        )

    def test_ssh_config_and_included_file_with_bom(self):
        (self.root / "lab.conf").write_bytes(BOM + b"Host lab\r\n  HostName 192.0.2.9\r\n")
        config = self.root / "config"
        config.write_bytes(BOM + b"Include lab.conf\r\nHost core\r\n  Port 2222\r\n")
        cfg = load_ssh_config(config)
        self.assertEqual(cfg.lookup("lab")["hostname"], "192.0.2.9")
        self.assertEqual(cfg.lookup("core")["port"], "2222")

    def test_backup_is_lf_on_every_os(self):
        path = self.root / "backup.conf"
        write_backup(path, "hostname r1\nip route default Null0\n")
        self.assertEqual(path.read_bytes(), b"hostname r1\nip route default Null0\n")

    def test_include_paths_with_spaces_quotes_comments_and_native_separators(self):
        directory = self.root / "space dir"
        directory.mkdir()
        included = directory / "lab.conf"
        included.write_text("Host lab\n  HostName 192.0.2.9\n", encoding="utf-8")
        config = self.root / "config"
        for directive in (
            'Include "space dir/lab.conf"',
            'Include\t"space dir/*.conf" # ignored comment',
            'Include="space dir/lab.conf"',
            f'Include "{included}"',
        ):
            with self.subTest(directive=directive):
                config.write_text(directive + "\n", encoding="utf-8")
                self.assertEqual(load_ssh_config(config).lookup("lab")["hostname"], "192.0.2.9")

    def test_invalid_include_quotes_are_reported(self):
        config = self.root / "config"
        config.write_text('Include "unterminated\n', encoding="utf-8")
        with self.assertRaisesRegex(UsageError, "cannot read ssh_config"):
            load_ssh_config(config)

    def test_include_with_unresolvable_home_is_reported(self):
        config = self.root / "config"
        config.write_text("Include ~missing-user/config\n", encoding="utf-8")
        expanduser = Path.expanduser

        def resolve_home(path):
            if str(path).startswith("~missing-user"):
                raise RuntimeError("Could not determine home directory")
            return expanduser(path)

        # Named-user home lookup differs on POSIX and Windows. Exercise the
        # documented pathlib failure without depending on local accounts.
        with (
            patch.object(Path, "expanduser", autospec=True, side_effect=resolve_home),
            self.assertRaisesRegex(UsageError, "cannot read ssh_config.*home directory"),
        ):
            load_ssh_config(config)


class PipeOutputTest(unittest.TestCase):
    def test_output_is_utf8_whatever_the_locale_encoding(self):
        # A Windows pipe gets cp932 on Japanese systems; ascii stands in for such a
        # narrow code page on every OS (PYTHONIOENCODING is what Python would use).
        with tempfile.TemporaryDirectory() as tmp:
            inv = Path(tmp) / "devices.json"
            inv.write_text(
                json.dumps({"devices": {"r1": {"host": "192.0.2.1", "note": NOTE}}}),
                encoding="utf-8",
            )
            env = {k: v for k, v in os.environ.items() if k != "PYTHONUTF8"}
            env["PYTHONIOENCODING"] = "ascii"
            src = str(Path(__file__).resolve().parents[1] / "src")
            env["PYTHONPATH"] = os.pathsep.join(filter(None, [src, env.get("PYTHONPATH")]))
            proc = subprocess.run(
                [sys.executable, "-m", "ix_ssh", "--list", "--inventory", str(inv)],
                capture_output=True,
                env=env,
                check=False,
            )
        self.assertEqual(proc.returncode, 0, proc.stderr.decode("utf-8", "replace"))
        self.assertIn(f"note: {NOTE}", proc.stdout.decode("utf-8"))


if __name__ == "__main__":
    unittest.main()
