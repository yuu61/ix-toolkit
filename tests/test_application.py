"""One invocation end to end, with the SSH session replaced by a fake and the
inventory / config file / backup on a temporary directory."""

import io
import json
import os
import tempfile
import unittest
from datetime import datetime, timezone
from pathlib import Path

from ix_ssh.application import BACKUP_AUTO, Request, execute, list_devices, prepare_target, run
from ix_ssh.domain import Target, TargetRequest, UsageError


class FakeSession:
    def __init__(self):
        self.calls: list[tuple] = []
        self.closed = False

    def show(self, cmd):
        self.calls.append(("show", cmd))
        return f"<{cmd}>"

    def apply(self, lines):
        self.calls.append(("apply", tuple(lines)))
        return "applied"

    def save(self):
        self.calls.append(("save",))
        return "saved"

    def close(self):
        self.closed = True


def target(**over) -> Target:
    base = {
        "name": "home",
        "host": "10.0.0.1",
        "username": "admin",
        "port": 22,
        "password": "pw",
        "key_file": None,
        "use_keys": False,
        "ssh_config_file": None,
    }
    return Target(**{**base, **over})


class ExecuteTest(unittest.TestCase):
    def setUp(self):
        self.tmp = tempfile.TemporaryDirectory()
        self.cwd = os.getcwd()
        os.chdir(self.tmp.name)  # default backup path is relative (backups/...)

    def tearDown(self):
        os.chdir(self.cwd)
        self.tmp.cleanup()

    def test_order_is_shows_backup_config_save(self):
        session, out, err = FakeSession(), io.StringIO(), io.StringIO()
        req = Request(
            shows=("show version", "ip route x"),
            backup=BACKUP_AUTO,
            config_lines=("logging buffered 100",),
            save=True,
        )
        execute(
            req,
            target(),
            session,
            out,
            err,
            now=lambda: datetime(2026, 9, 11, 1, 2, 3, tzinfo=timezone.utc),
        )
        self.assertEqual(
            session.calls,
            [
                ("show", "show version"),
                ("show", "show running-config"),
                ("apply", ("logging buffered 100",)),
                ("save",),
            ],
        )
        self.assertIn("[SKIP] refusing non-show command in show mode: 'ip route x'", err.getvalue())
        text = out.getvalue()
        self.assertIn("===== show version =====\n<show version>\n\n", text)
        self.assertIn("===== config =====\napplied\n\n", text)
        self.assertIn("===== write memory =====\nsaved\n\n", text)
        backup = Path("backups/home-20260911-010203.conf")
        self.assertEqual(backup.read_text(encoding="utf-8"), "<show running-config>\n")
        self.assertIn(f"[OK] running-config of home saved to {backup} (1 lines)", text)

    def test_raw_drops_headers_and_explicit_backup_path(self):
        session, out = FakeSession(), io.StringIO()
        execute(
            Request(shows=("show version",), backup="x/y.conf", raw=True), target(), session, out
        )
        self.assertEqual(out.getvalue().splitlines()[0], "<show version>")
        self.assertNotIn("=====", out.getvalue())
        self.assertEqual(Path("x/y.conf").read_text(encoding="utf-8"), "<show running-config>\n")


class RunTest(unittest.TestCase):
    def setUp(self):
        self.tmp = tempfile.TemporaryDirectory()
        self.inventory = Path(self.tmp.name) / "devices.json"
        self.inventory.write_text(
            json.dumps(
                {
                    "devices": {
                        "home": {"host": "10.0.0.1", "username": "admin", "password": "pw"},
                        "keyed": {"host": "10.0.0.2", "username": "admin", "use_keys": True},
                        "bare": {"host": "10.0.0.3", "username": "admin"},
                    }
                }
            ),
            encoding="utf-8",
        )
        self.no_ssh = TargetRequest(no_ssh_config=True)

    def tearDown(self):
        self.tmp.cleanup()

    def request(self, **over) -> Request:
        return Request(**{"inventory": str(self.inventory), "target": self.no_ssh, **over})

    def test_list(self):
        out = io.StringIO()
        run(self.request(list=True), env={}, out=out)
        text = out.getvalue()
        self.assertIn(f"inventory: {self.inventory}", text)
        self.assertIn("home   admin@10.0.0.1:22  auth=inventory-password", text)
        self.assertIn("keyed  admin@10.0.0.2:22  auth=ssh-key", text)
        self.assertIn("bare   admin@10.0.0.3:22  auth=prompt/$IX_PASS", text)
        self.assertNotIn("pw", text)

    def test_nothing_to_do(self):
        with self.assertRaises(UsageError) as cm:
            run(self.request(), env={})
        self.assertIn("nothing to do", str(cm.exception))

    def test_show_run_opens_closes_and_prints_banner(self):
        session, out, err = FakeSession(), io.StringIO(), io.StringIO()
        opened = []

        def open_session(t):
            opened.append(t)
            return session

        req = self.request(
            target=TargetRequest(device="home", no_ssh_config=True), shows=("show version",)
        )
        run(req, env={}, out=out, err=err, open_session=open_session)
        self.assertEqual(opened[0].password, "pw")
        self.assertEqual(err.getvalue(), "# target: home (admin@10.0.0.1:22)\n")
        self.assertEqual(session.calls, [("show", "show version")])
        self.assertTrue(session.closed)

    def test_config_file_is_read_before_connecting(self):
        cfg = Path(self.tmp.name) / "changes.ix"
        cfg.write_text("# c\nlogging buffered 100\n", encoding="utf-8")
        session = FakeSession()
        req = self.request(
            target=TargetRequest(device="home", no_ssh_config=True),
            config_file=str(cfg),
            config_lines=("hostname x",),
            raw=True,
        )
        run(req, env={}, out=io.StringIO(), open_session=lambda t: session)
        self.assertEqual(session.calls, [("apply", ("hostname x", "logging buffered 100"))])

        missing = self.request(target=TargetRequest(device="home"), config_file=str(cfg) + ".nope")
        with self.assertRaises(FileNotFoundError):
            run(missing, env={}, open_session=lambda t: self.fail("must not connect"))

    def test_password_sources(self):
        # key auth needs no password
        t = prepare_target(
            self.request(target=TargetRequest(device="keyed", no_ssh_config=True)), env={}
        )
        self.assertIsNone(t.password)
        self.assertTrue(t.use_keys)
        # nothing available, no --ask-password -> error, never a prompt
        with self.assertRaises(UsageError) as cm:
            prepare_target(
                self.request(target=TargetRequest(device="bare", no_ssh_config=True)),
                env={},
                prompt=lambda _: self.fail("must not prompt"),
            )
        self.assertIn("no password available for bare", str(cm.exception))
        # --ask-password prompts with the label
        asked = []
        t = prepare_target(
            self.request(
                target=TargetRequest(device="bare", ask_password=True, no_ssh_config=True)
            ),
            env={},
            prompt=lambda msg: asked.append(msg) or "typed",
        )
        self.assertEqual(asked, ["password for bare: "])
        self.assertEqual(t.password, "typed")
        # $IX_PASS covers an ad-hoc host
        t = prepare_target(
            self.request(target=TargetRequest(host="192.0.2.1", user="u", no_ssh_config=True)),
            env={"IX_PASS": "g"},
        )
        self.assertEqual((t.label, t.password), ("192.0.2.1", "g"))

    def test_unknown_device_error_carries_listing(self):
        with self.assertRaises(UsageError) as cm:
            prepare_target(
                self.request(target=TargetRequest(device="nope", no_ssh_config=True)), env={}
            )
        self.assertIn("unknown device 'nope'", str(cm.exception))
        self.assertIn("home   admin@10.0.0.1:22", str(cm.exception))

    def test_missing_inventory_file_is_an_error_but_none_is_not(self):
        with self.assertRaises(UsageError) as cm:
            list_devices(str(self.inventory) + ".nope")
        self.assertIn("inventory file not found", str(cm.exception))
        self.inventory.write_text("{", encoding="utf-8")
        with self.assertRaises(UsageError) as cm:
            list_devices(str(self.inventory))
        self.assertIn("invalid JSON", str(cm.exception))


if __name__ == "__main__":
    unittest.main()
