"""Listing and connecting must agree, while diagnostics tolerate broken SSH config."""

import io
import json
import os
import tempfile
import unittest
from pathlib import Path
from unittest.mock import patch

from ix_ssh.application import Request, list_devices, prepare_target, run
from ix_ssh.application.listing import describe_devices
from ix_ssh.application.presentation import format_inventory, format_target
from ix_ssh.domain import TargetRequest, UsageError, resolve_target
from ix_ssh.infrastructure import read_inventory

from .test_domain import FakeSshConfig


class ListingTest(unittest.TestCase):
    def test_empty_inventory_says_where_it_looked(self):
        text = format_inventory([], None, ["/a/devices.json", "/b/devices.json"])
        self.assertIn("inventory: (none found)", text)
        self.assertIn("    /a/devices.json\n    /b/devices.json", text)
        self.assertIn("$IX_INVENTORY overrides", text)

    def describe(self, devices, cfg=None, env=None):
        with patch("ix_ssh.infrastructure.quiet_load_ssh_config", return_value=cfg):
            return describe_devices(devices, Path("inv.json"), env or {})

    def test_rows_show_route_model_and_note(self):
        devices = {
            "home": {"host": "room1", "password": "x", "model": "IX2215", "note": "core"},
            "lab": {"host": "192.0.2.9", "username": "ops"},
        }
        cfg = FakeSshConfig(
            {
                "room1": {
                    "hostname": "10.0.0.1",
                    "port": "2222",
                    "user": "admin",
                    "proxyjump": "bastion",
                }
            }
        )
        text = self.describe(devices, cfg)
        self.assertIn(
            "home  admin@room1:22 -> 10.0.0.1:2222 via bastion"
            "  auth=inventory-password  model=IX2215",
            text,
        )
        self.assertIn("        note: core", text)
        self.assertIn("lab   ops@192.0.2.9:22  auth=prompt/$IX_PASS", text)
        target = resolve_target(TargetRequest(), "home", devices["home"], {}, cfg, None)
        self.assertEqual(
            format_target(target),
            "# target: home (admin@10.0.0.1:2222 [room1] via bastion, model IX2215)",
        )

    def test_same_hostname_uses_resolved_user_and_port(self):
        cfg = FakeSshConfig({"router": {"user": "ops", "port": "2222"}})
        for overrides in ({}, {"username": "inv", "port": 2200}):
            with self.subTest(overrides=overrides):
                entry = {"host": "router", **overrides}
                target = resolve_target(TargetRequest(), "r", entry, {}, cfg, None)
                self.assertIn(
                    f"{target.username}@{target.host}:{target.port}",
                    self.describe({"r": entry}, cfg),
                )

    def test_inventory_port_beats_alias_port(self):
        entry = {"host": "alias", "port": 2200, "username": "u"}
        cfg = FakeSshConfig({"alias": {"hostname": "10.0.0.1", "port": "22"}})
        self.assertIn("u@alias:2200 -> 10.0.0.1:2200", self.describe({"a": entry}, cfg))

    def test_auth_source_matches_password_resolution_and_never_discloses_values(self):
        cases = [
            (
                {"password": "secret-inv", "password_env": "P"},
                {"P": "secret-env"},
                "inventory-password",
            ),
            ({"password_env": "P"}, {"P": "secret-env"}, "env:$P"),
            ({"password_env": "P"}, {"IX_PASS": "secret-global"}, "env:$IX_PASS"),
            ({"use_keys": True}, {}, "ssh-key"),
            ({"use_keys": True}, {"IX_PASS": "secret-global"}, "env:$IX_PASS"),
            ({"password_env": "P"}, {}, "env:$P"),
            ({}, {}, "prompt/$IX_PASS"),
        ]
        for entry, env, expected in cases:
            with self.subTest(entry=entry, env=env):
                text = self.describe({"r": {"host": "router", **entry}}, env=env)
                self.assertIn(f"auth={expected}", text)
                self.assertNotIn("secret-", text)


class ListingFilesTest(unittest.TestCase):
    def setUp(self):
        self.tmp = tempfile.TemporaryDirectory()
        self.addCleanup(self.tmp.cleanup)
        self.root = Path(self.tmp.name)
        self.inventory = self.root / "devices.json"
        self.ssh = self.root / "config"

    def write_inventory(self, path, host):
        path.write_text(
            json.dumps(
                {
                    "devices": {
                        "r": {
                            "host": host,
                            "username": "ops",
                            "use_keys": True,
                            "ssh_config_file": str(self.ssh),
                        }
                    }
                }
            ),
            encoding="utf-8",
        )

    def test_injected_environment_controls_listing_connection_and_error_listing(self):
        other = self.root / "other.json"
        self.write_inventory(self.inventory, "injected")
        self.write_inventory(other, "process")
        env = {"IX_INVENTORY": str(self.inventory)}
        with patch.dict(os.environ, {"IX_INVENTORY": str(other)}):
            out = io.StringIO()
            run(Request(list=True), env=env, out=out)
            self.assertIn("ops@injected:22", out.getvalue())
            target = prepare_target(
                Request(target=TargetRequest(device="r", no_ssh_config=True)), env
            )
            self.assertEqual(target.host, "injected")
            with self.assertRaises(UsageError) as cm:
                prepare_target(Request(target=TargetRequest(device="missing")), env)
            self.assertIn("ops@injected:22", str(cm.exception))
            self.assertNotIn("ops@process", str(cm.exception))
            # Explicit path still wins over the injected environment.
            self.assertIn("ops@process:22", list_devices(str(other), env))
            # An explicitly empty environment must not inherit IX_INVENTORY.
            with patch("ix_ssh.infrastructure.inventory.INVENTORY_CANDIDATES", ()):
                self.assertIn("no devices configured", list_devices(env={}))

    def test_bad_ssh_config_cannot_hide_selection_errors_or_break_listing(self):
        self.write_inventory(self.inventory, "router")
        for config in (
            "Host router\n  Port invalid\n",
            "Host router\n  ProxyJump jump:invalid\n",
            "Host\n",
        ):
            self.ssh.write_text(config, encoding="utf-8")
            with self.subTest(config=config):
                self.assertIn("ops@router:22", list_devices(str(self.inventory), {}))
                for device, expected in (
                    ("missing", "unknown device"),
                    (None, "no target selected"),
                ):
                    with patch(
                        "ix_ssh.infrastructure.read_inventory", wraps=read_inventory
                    ) as read:
                        with self.assertRaisesRegex(UsageError, expected) as cm:
                            prepare_target(
                                Request(
                                    inventory=str(self.inventory),
                                    target=TargetRequest(device=device),
                                ),
                                {},
                            )
                        self.assertIn("ops@router:22", str(cm.exception))
                        self.assertEqual(read.call_count, 1)

    def test_list_honors_ssh_config_flags(self):
        self.write_inventory(self.inventory, "router")
        self.ssh.write_text("Host router\n  Port 2222\n", encoding="utf-8")
        alternate = self.root / "alternate"
        alternate.write_text("Host router\n  Port 2200\n", encoding="utf-8")
        for target, port in (
            (TargetRequest(), 2222),
            (TargetRequest(no_ssh_config=True), 22),
            (TargetRequest(ssh_config=str(alternate)), 2200),
        ):
            with self.subTest(target=target):
                out = io.StringIO()
                run(
                    Request(list=True, inventory=str(self.inventory), target=target),
                    env={},
                    out=out,
                )
                self.assertIn(f"ops@router:{port}", out.getvalue())
