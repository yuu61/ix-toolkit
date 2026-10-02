"""Unsupported SSH routes must fail before opening any connection."""

import unittest
from unittest.mock import patch

from ix_ssh.application.listing import describe_devices
from ix_ssh.domain import UsageError, resolve_route


class Config:
    def __init__(self, entries):
        self.entries = entries

    def lookup(self, host):
        return self.entries.get(host, {})


class ProxyRouteTest(unittest.TestCase):
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
