"""Unsupported SSH routes must fail before opening any connection."""

import tempfile
import unittest
from pathlib import Path
from unittest.mock import patch

from ix_ssh.application.listing import describe_devices
from ix_ssh.domain import UsageError, resolve_hop, resolve_route
from ix_ssh.infrastructure import load_ssh_config


class Config:
    def __init__(self, entries):
        self.entries = entries

    def lookup(self, host):
        return self.entries.get(host, {})


class ProxyRouteTest(unittest.TestCase):
    def setUp(self):
        self.tmp = tempfile.TemporaryDirectory()
        self.addCleanup(self.tmp.cleanup)
        self.root = Path(self.tmp.name)

    def load_config(self, text):
        path = self.root / "config"
        path.write_text(text, encoding="utf-8")
        return load_ssh_config(path)

    def test_first_effective_proxy_wins_in_matching_blocks(self):
        cases = (
            ("ProxyJump jump", "ProxyCommand fallback", ("jump",)),
            ("ProxyCommand fallback", "ProxyJump jump", None),
            ("ProxyCommand none", "ProxyJump jump", ()),
            ("ProxyJump none", "ProxyCommand fallback", None),
            ("ProxyCommand fallback", "ProxyCommand none", None),
            ("ProxyCommand none", "ProxyCommand fallback", ()),
            ("ProxyJump none", "ProxyJump jump", ()),
        )
        for first, second, hops in cases:
            for separator in ("", "Host device\n", "Host * !jump\n", "Match host device\n"):
                with self.subTest(first=first, second=second, separator=separator):
                    cfg = self.load_config(f"Host device\n  {first}\n{separator}  {second}\n")
                    if hops is None:
                        with self.assertRaisesRegex(UsageError, "ProxyCommand is unsupported"):
                            resolve_route(cfg, "device")
                    else:
                        self.assertEqual(resolve_route(cfg, "device").hops, hops)

    def test_include_nested_hops_and_listing_keep_proxy_precedence(self):
        (self.root / "routes.conf").write_text(
            "Host device\n  ProxyJump jump\n"
            "Host jump\n  ProxyJump outer\n  User jumpuser\n"
            "Host outer\n  ProxyCommand none\n"
            "Host *\n  ProxyCommand fallback token=hidden\n",
            encoding="utf-8",
        )
        cfg = self.load_config("Include routes.conf\n")
        self.assertEqual(resolve_route(cfg, "device").hops, ("outer", "jump"))
        self.assertEqual(resolve_hop(cfg, "jump").user, "jumpuser")
        with patch(
            "ix_ssh.application.listing.infrastructure.quiet_load_ssh_config", return_value=cfg
        ):
            output = describe_devices({"good": {"host": "device"}}, None, {})
        self.assertIn("via outer -> jump", output)
        self.assertNotIn("unresolved", output)
        self.assertNotIn("hidden", output)

    def test_final_match_cannot_override_an_earlier_proxy(self):
        for first, second, hops in (
            ("ProxyJump jump", "ProxyCommand fallback", ("jump",)),
            ("ProxyCommand none", "ProxyJump jump", ()),
        ):
            cfg = self.load_config(f"Match final host device\n  {second}\nHost device\n  {first}\n")
            self.assertEqual(resolve_route(cfg, "device").hops, hops)

    def test_target_and_used_nested_jumps_are_checked(self):
        for entries in (
            {"device": {"proxycommand": "secret-command token=hidden"}},
            {
                "device": {"proxyjump": "user@jump:2222"},
                "jump": {"proxycommand": "secret-command token=hidden"},
            },
            {
                "device": {"proxyjump": "jump"},
                "jump": {"proxyjump": "outer"},
                "outer": {"proxycommand": "secret-command token=hidden"},
            },
        ):
            cfg = Config(entries)
            with self.subTest(entries=entries), self.assertRaises(UsageError) as cm:
                resolve_route(cfg, "device")
            self.assertIn("ProxyCommand is unsupported", str(cm.exception))
            self.assertNotIn("secret-command", str(cm.exception))
            self.assertNotIn("hidden", str(cm.exception))

    def test_none_and_unrelated_definitions_do_not_block(self):
        cfg = Config(
            {
                "device": {"proxycommand": "none", "proxyjump": "jump"},
                "jump": {"proxycommand": None},
                "unrelated": {"proxycommand": "secret-command"},
            }
        )
        self.assertEqual(resolve_route(cfg, "device").hops, ("jump",))

    def test_listing_marks_unresolved_and_keeps_other_rows(self):
        cfg = Config({"device": {"proxycommand": "secret-command token=hidden"}})
        with patch(
            "ix_ssh.application.listing.infrastructure.quiet_load_ssh_config", return_value=cfg
        ):
            output = describe_devices(
                {"bad": {"host": "device"}, "good": {"host": "other"}}, None, {}
            )
        self.assertIn("route=unresolved", output)
        self.assertIn("good", output)
        self.assertNotIn("secret-command", output)
        self.assertNotIn("hidden", output)
