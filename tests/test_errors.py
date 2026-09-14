"""Expected input/output failures reach the CLI as concise usage errors."""

import contextlib
import io
import json
import os
import tempfile
import unittest
from pathlib import Path
from unittest.mock import patch

from ix_ssh.application import Request, run
from ix_ssh.cli import main
from ix_ssh.domain import UsageError
from ix_ssh.infrastructure import fix_command, load_ssh_config, read_inventory, write_backup

from .test_application import FakeSession, target


class ErrorBoundaryTest(unittest.TestCase):
    def setUp(self):
        self.tmp = tempfile.TemporaryDirectory()
        self.addCleanup(self.tmp.cleanup)
        self.root = Path(self.tmp.name)

    def assert_cli_error(self, args, expected):
        err = io.StringIO()
        with contextlib.redirect_stderr(err):
            self.assertEqual(main(args), 1)
        self.assertIn(expected, err.getvalue())
        self.assertNotIn("Traceback", err.getvalue())

    def test_missing_config_file_fails_before_connecting(self):
        path = self.root / "missing.conf"
        with patch("ix_ssh.application.run.prepare_target") as prepare:
            self.assert_cli_error(["--config-file", str(path)], f"cannot read config file {path}")
            prepare.assert_not_called()

    def test_invalid_show_fails_before_target_or_connection(self):
        for command in ("showcase", "ip route x", "show version\nwrite memory", "show\tip route"):
            with (
                self.subTest(command=command),
                patch("ix_ssh.application.run.prepare_target") as prepare,
            ):
                self.assert_cli_error(
                    ["show version", command, "--config", "hostname x", "--save"],
                    "single show command",
                )
                prepare.assert_not_called()

    def test_invalid_utf8_config_is_a_cli_error(self):
        path = self.root / "invalid.conf"
        path.write_bytes(b"\xff")
        self.assert_cli_error(["--config-file", str(path)], "cannot read config file")

    def test_unreadable_inventory_is_a_usage_error(self):
        path = self.root / "devices.json"
        path.write_text("{}", encoding="utf-8")
        with (
            patch.object(Path, "read_text", side_effect=PermissionError("denied")),
            self.assertRaisesRegex(UsageError, "cannot read inventory"),
        ):
            read_inventory(str(path), {})
        path.write_bytes(b"\xff")
        self.assert_cli_error(["--list", "--inventory", str(path)], "cannot read inventory")

    def test_backup_write_failure_prevents_config_and_save_and_closes_session(self):
        blocked = self.root / "blocked"
        blocked.write_text("a file, not a directory", encoding="utf-8")
        session = FakeSession()
        req = Request(backup=str(blocked / "backup.conf"), config_lines=("hostname x",), save=True)
        with (
            patch("ix_ssh.application.run.prepare_target", return_value=target()),
            self.assertRaisesRegex(UsageError, "cannot write backup"),
        ):
            run(req, env={}, out=io.StringIO(), err=io.StringIO(), open_session=lambda _: session)
        self.assertEqual(session.calls, [("show", "show running-config")])
        self.assertTrue(session.closed)

    def test_backup_permission_error_is_translated(self):
        with (
            patch("os.open", side_effect=PermissionError("denied")),
            self.assertRaisesRegex(UsageError, "cannot write backup"),
        ):
            write_backup(self.root / "backup.conf", "running config")

    @unittest.skipIf(os.name == "nt", "POSIX modes")
    def test_backup_is_private_even_when_overwriting_a_readable_file(self):
        path = self.root / "backup.conf"
        self.assertTrue(write_backup(path, "new"))
        self.assertEqual(path.stat().st_mode & 0o777, 0o600)
        path.chmod(0o644)
        self.assertTrue(write_backup(path, "newer"))
        self.assertEqual(path.stat().st_mode & 0o777, 0o600)
        self.assertEqual(path.read_text(encoding="utf-8"), "newer\n")

    def test_failed_hardening_is_reported_but_is_not_a_write_failure(self):
        path = self.root / "backup.conf"
        with patch("ix_ssh.infrastructure.files.restrict_to_owner", return_value=False):
            self.assertFalse(write_backup(path, "running config"))
        self.assertEqual(path.read_text(encoding="utf-8"), "running config\n")

        session, out, err = FakeSession(), io.StringIO(), io.StringIO()
        req = Request(backup=str(path), config_lines=("hostname x",), save=True)
        with (
            patch("ix_ssh.application.run.prepare_target", return_value=target()),
            patch("ix_ssh.infrastructure.files.restrict_to_owner", return_value=False),
        ):
            run(req, env={}, out=out, err=err, open_session=lambda _: session)
        self.assertIn(f"saved to {path}", out.getvalue())
        self.assertIn(f"[WARN] could not restrict {path}", err.getvalue())
        self.assertIn(fix_command(path), err.getvalue())
        self.assertEqual([c[0] for c in session.calls], ["show", "apply", "save"])

    def test_fix_command_names_the_user_and_needs_no_shell_expansion(self):
        cmd = fix_command(self.root / "devices.json")
        self.assertIn(str((self.root / "devices.json").absolute()), cmd)
        self.assertNotIn("%USERNAME%", cmd)
        if os.name == "nt":
            self.assertIn(f'"{os.getlogin()}:F"', cmd)
        else:
            self.assertTrue(cmd.startswith("chmod 600 "))

    def test_unreadable_ssh_config_is_a_usage_error(self):
        path = self.root / "config"
        path.write_text("Host router\n", encoding="utf-8")
        with (
            patch.object(Path, "read_text", side_effect=PermissionError("denied")),
            self.assertRaisesRegex(UsageError, "cannot read ssh_config"),
        ):
            load_ssh_config(str(path))

    def test_malformed_ssh_config_and_ports_are_cli_errors(self):
        ssh = self.root / "config"
        inv = self.root / "devices.json"
        inv.write_text(
            json.dumps(
                {
                    "devices": {
                        "r": {
                            "host": "router",
                            "username": "ops",
                            "use_keys": True,
                            "ssh_config_file": str(ssh),
                        }
                    }
                }
            ),
            encoding="utf-8",
        )
        for config, expected in (
            ("Host\n", "cannot read ssh_config"),
            ("Host router\n  Port invalid\n", "invalid port"),
            ("Host router\n  ProxyJump jump:invalid\n", "invalid port"),
            ("Host router\n  ProxyJump jump\nHost jump\n  Port invalid\n", "invalid port"),
        ):
            with self.subTest(config=config):
                ssh.write_text(config, encoding="utf-8")
                self.assert_cli_error(
                    ["--inventory", str(inv), "-d", "r", "show version"], expected
                )

    def test_programming_errors_are_not_swallowed(self):
        with (
            patch("ix_ssh.cli.run", side_effect=RuntimeError("bug")),
            self.assertRaisesRegex(RuntimeError, "bug"),
        ):
            main(["show version"])
