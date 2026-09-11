"""Domain rules: inventory reading, ProxyJump flattening, setting precedence,
password order, command guards. No files, no network."""

import unittest
from datetime import datetime, timezone
from typing import ClassVar

from ix_ssh.domain import (
    Route,
    TargetRequest,
    UsageError,
    default_backup_path,
    describe_auth,
    describe_route,
    find_password,
    format_inventory,
    hop_specs,
    is_show_command,
    missing_password_message,
    parse_config_lines,
    parse_inventory,
    resolve_hop,
    resolve_target,
    select_entry,
    split_hop,
    ssh_config_path,
)


class FakeSshConfig:
    """Stands in for paramiko.SSHConfig: lookup() returns the host's block, or
    {"hostname": host} for an unknown host, like paramiko does."""

    def __init__(self, hosts: dict[str, dict]):
        self.hosts = hosts

    def lookup(self, hostname: str) -> dict:
        return {"hostname": hostname, **self.hosts.get(hostname, {})}


class ParseInventoryTest(unittest.TestCase):
    def test_devices_key_and_bare_object_are_equivalent(self):
        entry = {"host": "192.0.2.1", "username": "admin"}
        self.assertEqual(parse_inventory({"devices": {"a": entry}}, "inv"), {"a": entry})
        self.assertEqual(parse_inventory({"a": entry}, "inv"), {"a": entry})

    def test_underscore_keys_are_comments(self):
        devices = parse_inventory({"_note": "x", "a": {"host": "h"}}, "inv")
        self.assertEqual(list(devices), ["a"])

    def test_entry_must_be_an_object(self):
        with self.assertRaises(UsageError) as cm:
            parse_inventory({"devices": {"a": "192.0.2.1"}}, "inv")
        self.assertIn("device 'a' must be an object", str(cm.exception))

    def test_top_level_must_be_an_object(self):
        with self.assertRaises(UsageError):
            parse_inventory(["a"], "inv")


class DescribeAuthTest(unittest.TestCase):
    def test_order(self):
        self.assertEqual(describe_auth({"password_env": "P", "password": "x"}), "env:$P")
        self.assertEqual(describe_auth({"password": "x", "key_file": "k"}), "inventory-password")
        self.assertEqual(describe_auth({"use_keys": True}), "ssh-key")
        self.assertEqual(describe_auth({}), "prompt/$IX_PASS")


class FormatInventoryTest(unittest.TestCase):
    def test_empty_inventory_says_where_it_looked(self):
        text = format_inventory({}, None, ["/a/devices.json", "/b/devices.json"])
        self.assertIn("inventory: (none found)", text)
        self.assertIn("    /a/devices.json\n    /b/devices.json", text)
        self.assertIn("$IX_INVENTORY overrides", text)

    def test_rows_show_route_model_and_note(self):
        devices = {
            "home": {"host": "room1", "password": "x", "model": "IX2215", "note": "core"},
            "lab": {"host": "192.0.2.9", "username": "ops"},
        }
        routes = {
            "home": Route(hostname="10.0.0.1", port=2222, user="admin", hops=("bastion",)),
            "lab": Route(hostname="192.0.2.9", port=None, user=None, hops=()),
        }
        text = format_inventory(devices, "/inv.json", [], routes)
        self.assertIn("inventory: /inv.json", text)
        self.assertIn(
            "  home  admin@room1:22 -> 10.0.0.1:2222 via bastion  auth=inventory-password"
            "  model=IX2215",
            text,
        )
        self.assertIn("        note: core", text)
        self.assertIn("  lab   ops@192.0.2.9:22  auth=prompt/$IX_PASS", text)

    def test_inventory_port_beats_ssh_config_port(self):
        devices = {"a": {"host": "alias", "port": 2200, "username": "u"}}
        routes = {"a": Route(hostname="10.0.0.1", port=22, user=None, hops=())}
        self.assertIn("u@alias:2200 -> 10.0.0.1:2200", format_inventory(devices, "i", [], routes))


class ProxyJumpTest(unittest.TestCase):
    def test_split_hop(self):
        self.assertEqual(split_hop("user@host:2222"), ("user", "host", 2222))
        self.assertEqual(split_hop(" host "), (None, "host", None))
        self.assertEqual(split_hop("u@h"), ("u", "h", None))

    def test_nested_chain_is_flattened_outermost_first(self):
        cfg = FakeSshConfig(
            {
                "device": {"proxyjump": "inner"},
                "inner": {"proxyjump": "outer"},
                "outer": {},
            }
        )
        self.assertEqual(hop_specs(cfg, "device"), ["outer", "inner"])

    def test_comma_list(self):
        cfg = FakeSshConfig({"d": {"proxyjump": "a, b"}})
        self.assertEqual(hop_specs(cfg, "d"), ["a", "b"])
        self.assertEqual(hop_specs(None, "d"), [])

    def test_cycle_terminates(self):
        # a misconfigured loop (d -> a -> d) must not recurse forever
        cfg = FakeSshConfig({"d": {"proxyjump": "a"}, "a": {"proxyjump": "d"}})
        self.assertEqual(hop_specs(cfg, "d")[-1], "a")

    def test_resolve_hop_applies_ssh_config(self):
        cfg = FakeSshConfig(
            {
                "bastion": {
                    "hostname": "203.0.113.1",
                    "port": "2222",
                    "user": "jump",
                    "identityfile": ["~/.ssh/id_ed25519"],
                }
            }
        )
        hop = resolve_hop(cfg, "bastion")
        self.assertEqual((hop.host, hop.port, hop.user), ("203.0.113.1", 2222, "jump"))
        self.assertEqual(len(hop.keys), 1)
        self.assertNotIn("~", hop.keys[0])
        # explicit user@host:port in the spec wins over ssh_config
        hop = resolve_hop(cfg, "me@bastion:22")
        self.assertEqual((hop.port, hop.user), (22, "me"))
        # a single IdentityFile string is accepted too; no config -> bare spec
        self.assertEqual(resolve_hop(None, "x").keys, ())

    def test_describe_route(self):
        cfg = FakeSshConfig({"r": {"hostname": "10.0.0.2", "port": "22", "proxyjump": "b"}})
        self.assertEqual(
            describe_route(cfg, "r"), Route(hostname="10.0.0.2", port=22, user=None, hops=("b",))
        )
        self.assertIsNone(describe_route(None, "r"))


class SelectEntryTest(unittest.TestCase):
    devices: ClassVar[dict] = {"home": {"host": "h"}}

    def test_device_flag_then_env(self):
        self.assertEqual(
            select_entry(TargetRequest(device="home"), self.devices, {}, lambda: ""),
            ("home", {"host": "h"}),
        )
        self.assertEqual(
            select_entry(TargetRequest(), self.devices, {"IX_DEVICE": "home"}, lambda: "")[0],
            "home",
        )

    def test_unknown_device_lists_inventory(self):
        with self.assertRaises(UsageError) as cm:
            select_entry(TargetRequest(device="nope"), self.devices, {}, lambda: "LISTING")
        self.assertIn("unknown device 'nope'", str(cm.exception))
        self.assertIn("LISTING", str(cm.exception))

    def test_no_default_device(self):
        with self.assertRaises(UsageError) as cm:
            select_entry(TargetRequest(), self.devices, {}, lambda: "LISTING")
        self.assertIn("no target selected", str(cm.exception))

    def test_adhoc_host_has_no_entry(self):
        self.assertEqual(
            select_entry(TargetRequest(host="192.0.2.1"), self.devices, {}, lambda: ""), (None, {})
        )


class SshConfigPathTest(unittest.TestCase):
    def test_precedence(self):
        self.assertEqual(
            ssh_config_path(TargetRequest(ssh_config="f"), {"ssh_config_file": "e"}, "d"), "f"
        )
        self.assertEqual(ssh_config_path(TargetRequest(), {"ssh_config_file": "e"}, "d"), "e")
        self.assertEqual(ssh_config_path(TargetRequest(), {}, "d"), "d")
        self.assertIsNone(ssh_config_path(TargetRequest(no_ssh_config=True), {}, "d"))


class ResolveTargetTest(unittest.TestCase):
    def test_flag_beats_env_beats_inventory_beats_default(self):
        entry = {"host": "10.0.0.1", "username": "inv", "port": 3}
        t = resolve_target(
            TargetRequest(user="flag"), "n", entry, {"IX_USER": "env", "IX_PORT": "2"}, None, None
        )
        self.assertEqual((t.host, t.username, t.port), ("10.0.0.1", "flag", 2))
        t = resolve_target(TargetRequest(), "n", entry, {}, None, None)
        self.assertEqual((t.username, t.port), ("inv", 3))
        t = resolve_target(TargetRequest(), "n", {"host": "h", "user": "u"}, {}, None, None)
        self.assertEqual(t.port, 22)
        self.assertIsNone(t.password)

    def test_alias_resolves_through_ssh_config(self):
        cfg = FakeSshConfig(
            {
                "room1": {
                    "hostname": "10.0.0.5",
                    "port": "2222",
                    "user": "cfguser",
                    "proxyjump": "bastion",
                },
                "bastion": {"hostname": "203.0.113.1"},
            }
        )
        t = resolve_target(TargetRequest(), "home", {"host": "room1"}, {}, cfg, "/ssh/config")
        self.assertEqual(
            (t.host, t.port, t.username, t.alias), ("10.0.0.5", 2222, "cfguser", "room1")
        )
        self.assertEqual([h.host for h in t.hops], ["203.0.113.1"])
        self.assertEqual(t.banner(), "# target: home (cfguser@10.0.0.5:2222 [room1] via bastion)")
        # inventory user/port still beat ssh_config
        t = resolve_target(
            TargetRequest(), "home", {"host": "room1", "username": "inv", "port": 1}, {}, cfg, None
        )
        self.assertEqual((t.port, t.username), (1, "inv"))

    def test_missing_host_username_and_bad_port(self):
        with self.assertRaises(UsageError) as cm:
            resolve_target(TargetRequest(), "n", {}, {}, None, None)
        self.assertIn("no host for 'n'", str(cm.exception))
        with self.assertRaises(UsageError) as cm:
            resolve_target(TargetRequest(), None, {"host": "h"}, {}, None, None)
        self.assertIn("no username for h", str(cm.exception))
        with self.assertRaises(UsageError) as cm:
            resolve_target(TargetRequest(port="x"), "n", {"host": "h", "user": "u"}, {}, None, None)
        self.assertIn("invalid port: 'x'", str(cm.exception))

    def test_keys_and_model(self):
        t = resolve_target(
            TargetRequest(key_file="~/k"),
            "n",
            {"host": "h", "user": "u", "model": "IX2215"},
            {},
            None,
            None,
        )
        self.assertTrue(t.use_keys)
        self.assertNotIn("~", t.key_file)
        self.assertIn(", model IX2215)", t.banner())
        t = resolve_target(
            TargetRequest(), "n", {"host": "h", "user": "u", "use_keys": True}, {}, None, None
        )
        self.assertTrue(t.use_keys)
        self.assertIsNone(t.key_file)

    def test_label_and_slug(self):
        t = resolve_target(TargetRequest(host="a b/c"), None, {}, {"IX_USER": "u"}, None, None)
        self.assertEqual(t.label, "a b/c")
        self.assertEqual(t.slug(), "a_b_c")


class FindPasswordTest(unittest.TestCase):
    def test_order(self):
        entry = {"password": "inv", "password_env": "DEV"}
        env = {"FLAG": "flag", "DEV": "dev", "IX_PASS": "glob"}
        self.assertEqual(find_password(entry, env, "FLAG"), "flag")
        self.assertEqual(find_password(entry, env, None), "inv")
        self.assertEqual(find_password({"password_env": "DEV"}, env, None), "dev")
        self.assertEqual(find_password({}, env, None), "glob")
        self.assertIsNone(find_password({}, {}, None))

    def test_flag_variable_must_be_set(self):
        with self.assertRaises(UsageError) as cm:
            find_password({"password": "inv"}, {}, "FLAG")
        self.assertIn("$FLAG is unset or empty", str(cm.exception))

    def test_missing_message_names_the_entry_variable(self):
        self.assertIn("set $DEV,", missing_password_message("home", {"password_env": "DEV"}))
        self.assertIn("set $IX_PASS,", missing_password_message("home", {}))


class CommandsTest(unittest.TestCase):
    def test_is_show_command(self):
        self.assertTrue(is_show_command("  SHOW ip route"))
        self.assertFalse(is_show_command("ip route default"))

    def test_parse_config_lines(self):
        text = "# comment\n\nip route default GigaEthernet1.0  \n  logging buffered 100\n"
        self.assertEqual(
            parse_config_lines(text), ["ip route default GigaEthernet1.0", "  logging buffered 100"]
        )

    def test_default_backup_path(self):
        path = default_backup_path("home", datetime(2026, 9, 11, 1, 2, 3, tzinfo=timezone.utc))
        self.assertEqual(path.as_posix(), "backups/home-20260911-010203.conf")


if __name__ == "__main__":
    unittest.main()
