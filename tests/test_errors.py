"""Expected input/output failures reach the CLI as concise usage errors."""

import contextlib
import io
import json
import tempfile
import unittest
from pathlib import Path
from unittest.mock import patch

from ix_ssh.application import Request, run
from ix_ssh.cli import main
from ix_ssh.domain import UsageError
from ix_ssh.infrastructure import load_ssh_config, read_inventory, write_backup

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
            patch.object(Path, "write_text", side_effect=PermissionError("denied")),
            self.assertRaisesRegex(UsageError, "cannot write backup"),
        ):
            write_backup(self.root / "backup.conf", "running config")

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
