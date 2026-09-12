"""ix-ssh end to end against a fake IX on a real SSH server (tests/fakeix.py):
inventory -> ssh_config alias (from an Include'd file) -> ProxyJump through the
same server -> netmiko session -> show / backup / config / save. Needs netmiko
and paramiko, so it is skipped where they are absent (the dev venv has them)."""

import contextlib
import io
import json
import os
import tempfile
import unittest
from pathlib import Path
from unittest.mock import patch

try:
    import netmiko  # noqa: F401
    import paramiko  # noqa: F401
except ImportError:  # pragma: no cover - --list/--help still work without them
    HAVE_SSH = False
else:
    HAVE_SSH = True
    from .fakeix import HOSTNAME, RUNNING_CONFIG, SHOW_VERSION, FakeIX

from ix_ssh.cli import main


@unittest.skipUnless(HAVE_SSH, "netmiko/paramiko not installed")
class IntegrationTest(unittest.TestCase):
    @classmethod
    def setUpClass(cls):
        cls.device = FakeIX(password="s3cret").start()
        cls.tmp = tempfile.TemporaryDirectory()
        root = Path(cls.tmp.name)
        key = root / "jump_key"
        cls.device.jump_key.write_private_key_file(str(key))
        (root / "conf.d").mkdir()
        # aliases live in an Include'd file: paramiko alone would not see them
        (root / "conf.d" / "lab.conf").write_text(
            f"Host fakejump\n  HostName 127.0.0.1\n  Port {cls.device.port}\n"
            f"  User jump\n  IdentityFile {key.as_posix()}\n  ProxyJump none\n"
            f"Host fakeix\n  HostName 127.0.0.1\n  Port {cls.device.port}\n  ProxyJump fakejump\n"
            f"Host direct\n  HostName 127.0.0.1\n  Port {cls.device.port}\n  ProxyJump none\n",
            encoding="utf-8",
        )
        cls.ssh_config = root / "config"
        cls.ssh_config.write_text("Include conf.d/*.conf\n", encoding="utf-8")
        cls.inventory = root / "devices.json"
        cls.inventory.write_text(
            json.dumps(
                {
                    "devices": {
                        "viajump": {
                            "host": "fakeix",
                            "username": "admin",
                            "password": "s3cret",
                            "ssh_config_file": str(cls.ssh_config),
                            "model": "IX2215",
                        },
                        "direct": {
                            "host": "direct",
                            "username": "admin",
                            "password_env": "FAKEIX_PASS",
                            "ssh_config_file": str(cls.ssh_config),
                        },
                    }
                }
            ),
            encoding="utf-8",
        )
        cls.cwd = os.getcwd()
        os.chdir(root)

    @classmethod
    def tearDownClass(cls):
        os.chdir(cls.cwd)
        cls.device.stop()
        cls.tmp.cleanup()

    def ix_ssh(self, *args: str) -> tuple[int, str, str]:
        out, err = io.StringIO(), io.StringIO()
        with contextlib.redirect_stdout(out), contextlib.redirect_stderr(err):
            code = main(["--inventory", str(self.inventory), *args])
        return code, out.getvalue(), err.getvalue()

    def test_list_shows_resolved_route_and_jump(self):
        code, out, _ = self.ix_ssh("--list")
        self.assertEqual(code, 0)
        self.assertIn(
            f"viajump  admin@fakeix:22 -> 127.0.0.1:{self.device.port} via fakejump"
            "  auth=inventory-password  model=IX2215",
            out,
        )
        self.assertIn(
            f"direct   admin@direct:22 -> 127.0.0.1:{self.device.port}  auth=env:$FAKEIX_PASS", out
        )
        self.assertNotIn("s3cret", out)

    def test_show_backup_config_save_through_proxyjump(self):
        before = (self.device.sessions, self.device.saved, list(self.device.applied))
        code, out, err = self.ix_ssh(
            "-d",
            "viajump",
            "show version",
            "--backup",
            "--config",
            "logging buffered 100",
            "--save",
        )
        self.assertEqual(code, 0, err)
        self.assertEqual(
            err.splitlines()[0],
            f"# target: viajump (admin@127.0.0.1:{self.device.port} [fakeix] via fakejump, model IX2215)",
        )
        self.assertIn("===== show version =====\n" + "\n".join(SHOW_VERSION), out)
        self.assertIn("===== config =====", out)
        self.assertIn("===== write memory =====", out)
        # exactly one device session, through the jump; the config line landed; saved once
        self.assertEqual(self.device.sessions, before[0] + 1)
        self.assertEqual(self.device.applied, [*before[2], "logging buffered 100"])
        self.assertEqual(self.device.saved, before[1] + 1)
        # the backup holds the whole running-config: the `hostname fakeix` line did
        # not end the read early (the prompt is pinned to `fakeix(config)#`)
        backups = sorted(Path("backups").glob("viajump-*.conf"))
        self.assertEqual(len(backups), 1, backups)
        self.assertEqual(backups[0].read_text(encoding="utf-8"), "\n".join(RUNNING_CONFIG) + "\n")
        self.assertIn(
            f"[OK] running-config of viajump saved to {backups[0]} ({len(RUNNING_CONFIG)} lines)",
            out,
        )

    def test_raw_show_direct_with_password_env(self):
        os.environ["FAKEIX_PASS"] = "s3cret"
        try:
            code, out, err = self.ix_ssh("-d", "direct", "--raw", "show running-config")
        finally:
            del os.environ["FAKEIX_PASS"]
        self.assertEqual(code, 0, err)
        self.assertEqual(out, "\n".join(RUNNING_CONFIG) + "\n")
        self.assertEqual(err, "")  # --raw: no banner either
        self.assertIn(f"hostname {HOSTNAME}", out)

    def test_config_error_stops_remaining_lines_and_save(self):
        before = (list(self.device.applied), self.device.saved)
        with patch.dict(
            self.device.responses, {"bad-command": ["% bad-command -- Invalid command."]}
        ):
            code, out, err = self.ix_ssh(
                "-d",
                "viajump",
                "--config",
                "logging buffered 200",
                "--config",
                "bad-command",
                "--config",
                "logging buffered 300",
                "--save",
            )
        self.assertEqual(code, 1, err)
        self.assertIn("configuration stopped", err)
        self.assertIn("bad-command", err)
        self.assertNotIn("Traceback", err)
        self.assertEqual(self.device.applied, [*before[0], "logging buffered 200"])
        self.assertEqual(self.device.saved, before[1])
        self.assertNotIn("===== write memory", out)

    def test_show_error_stops_following_operations(self):
        before = (list(self.device.applied), self.device.saved)
        code, _, err = self.ix_ssh(
            "-d", "viajump", "show invalid-command", "--config", "hostname x", "--save"
        )
        self.assertEqual(code, 1, err)
        self.assertIn("Invalid input", err)
        self.assertEqual((self.device.applied, self.device.saved), before)

    def test_backup_command_error_preserves_file_and_stops_config(self):
        dest = Path(self.tmp.name) / "preserved.conf"
        dest.write_text("previous backup\n", encoding="utf-8")
        before = (list(self.device.applied), self.device.saved)
        with patch.dict(self.device.responses, {"show running-config": ["% Permission denied."]}):
            code, out, err = self.ix_ssh(
                "-d", "viajump", "--backup", str(dest), "--config", "hostname x", "--save"
            )
        self.assertEqual(code, 1, err)
        self.assertIn("Permission denied", err)
        self.assertNotIn("[OK]", out)
        self.assertEqual(dest.read_text(encoding="utf-8"), "previous backup\n")
        self.assertEqual((self.device.applied, self.device.saved), before)

    def test_save_error_is_reported_to_cli(self):
        before = self.device.saved
        with patch.dict(
            self.device.responses, {"write memory": ["% Error writing configuration."]}
        ):
            code, _, err = self.ix_ssh("-d", "viajump", "--save")
        self.assertEqual(code, 1, err)
        self.assertIn("Error writing configuration", err)
        self.assertNotIn("Traceback", err)
        self.assertEqual(self.device.saved, before)

    def test_wrong_password_fails_cleanly(self):
        code, _, err = self.ix_ssh(
            "--host",
            "direct",
            "--user",
            "admin",
            "--ssh-config",
            str(self.ssh_config),
            "--password-env",
            "FAKEIX_WRONG",
            "show version",
        )
        self.assertEqual(code, 1)
        self.assertIn("$FAKEIX_WRONG is unset or empty", err)
        os.environ["FAKEIX_WRONG"] = "nope"
        try:
            with self.assertRaises(Exception) as cm:
                self.ix_ssh(
                    "--host",
                    "direct",
                    "--user",
                    "admin",
                    "--ssh-config",
                    str(self.ssh_config),
                    "--password-env",
                    "FAKEIX_WRONG",
                    "show version",
                )
        finally:
            del os.environ["FAKEIX_WRONG"]
        self.assertIn("Authentication", type(cm.exception).__name__ + str(cm.exception))


if __name__ == "__main__":
    unittest.main()
