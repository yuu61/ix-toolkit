"""Privacy boundaries: synthetic values, both families and diagnostic variants."""

import unittest

from ix_ssh.domain import UsageError
from ix_ssh.domain.redaction import ConfigRedactor, protect_input, validate_config_protocol

SECRET = "Zq9SensitiveValue"


class RedactionTest(unittest.TestCase):
    def test_inventory_families_and_deletion_forms(self):
        commands = (
            "username user password plain 2 {} administrator",
            "username user password hash {}",
            "password {} NewSecret",
            "authentication password user {}",
            "authentication secret-password 1 user {}",
            "http-server username user password {}",
            "http-server guest-username user secret-password {}",
            "http-server monitor-username user password {}",
            "http-server cross-site password {}",
            "web-auth username user password {}",
            "ike policy vpn key-type char peer any key {}",
            "ikev2 authentication psk id nbma-address key secret {}",
            "ip ospf authentication-key {}",
            "ip ospf message-digest-key 2 {}",
            "area 0 virtual-link 192.0.2.1 message-digest-key 2 {}",
            "neighbor 192.0.2.1 password {} with spaces",
            "vrrp 1 authentication {}",
            "radius host ip 192.0.2.1 auth-port 1812 key 0 {}",
            "snmp-agent ip community {} read-only",
            "snmp-agent vrf blue ipv6 host ::1 {}",
            "snmpv3 user user group auth sha plain {} priv aes secret OtherSecret",
            "l2tp password plain {}",
            "tunnel v6pv user user password {}",
            "nm account group password secret {}",
            "ngn radius-auth password 1 {}",
            "ip dhcp-client authentication delayed-auth secret-id 1 key {}",
            "ip rip authentication text {}",
            "ipsec manualkey vpn esp aes {} sha1 OtherSecret",
            "mobile pin-auth enable {}",
            "mobile pin-code plain {}",
            "mobile puk {} OtherSecret",
            "mobile password secret {}",
            "mobile pin plain {}",
            "pin-authentication on {} device",
            "pin-code {} OtherSecret device",
            "pin-unlock {} OtherSecret device",
            "usbmem authentication password-file file plain {}",
            "pki cert import der ftp://example/cert password {}",
            "pki pkcs12 import crypto {} name test url file:/key",
            "pki private-key import crypto {} file key",
            "software-update https://example/image account user password {}",
            "startup config-download https://example/config password {}",
            "utm license key {}",
            "utm license update {}",
            "scp get remote file account user password {}",
            'command 1 "username user password plain {}"',
            "query arbitrary={}+more",
            "o arbitrary {}",
        )
        for template in commands:
            for prefix in ("", "no "):
                line = prefix + template.format(SECRET)
                with self.subTest(line=line):
                    protected = protect_input(line)
                    self.assertNotIn(SECRET, protected.masked)
                    redactor = ConfigRedactor((line,))
                    for representation in (SECRET, SECRET[:7] + "...", '"' + SECRET + '"'):
                        output = redactor.response(1, line + "\nNotice: " + representation)
                        self.assertNotIn(SECRET, output)
                        self.assertNotIn(SECRET[:7], output)

    def test_echo_and_notices_survive(self):
        line = "ikev2 authentication psk id fqdn router key char " + SECRET
        response = line + "\n% Please reboot the router.\nStrength: strong\nNew unknown notice"
        output = ConfigRedactor((line,)).response(1, response)
        self.assertIn("ikev2 authentication psk id fqdn router key char [REDACTED]", output)
        self.assertIn("% Please reboot the router.", output)
        self.assertIn("Strength: strong", output)
        self.assertIn("New unknown notice", output)

    def test_abbreviated_malformed_embedded_and_escaped_values(self):
        for line in (
            "u user pass plain " + SECRET,
            "ikev2 a psk id fqdn router k char " + SECRET,
            'password old "' + SECRET + ' with spaces"',
            'password old "' + SECRET + r'\"escaped"',
            "fetch https://user:" + SECRET + "@example.invalid/?token=OtherSecret",
        ):
            with self.subTest(line=line):
                redactor = ConfigRedactor((line,))
                output = redactor.response(1, line + "\nValue " + SECRET[:5] + "\n" + SECRET[5:])
                self.assertNotIn(SECRET[:5], output)
                self.assertNotIn(SECRET[5:], output)

    def test_unquoted_hex_unicode_and_octal_device_escapes(self):
        line = "password old " + SECRET
        redactor = ConfigRedactor((line,))
        for escaped in (
            "".join(f"\\x{ord(char):02x}" for char in SECRET),
            "".join(f"\\u{ord(char):04x}" for char in SECRET),
            "".join(f"\\{ord(char):03o}" for char in SECRET),
        ):
            self.assertEqual(
                redactor.response(1, "New notice: " + escaped), "New notice: [REDACTED]"
            )

    def test_nonsecret_commands_and_literal_license_source(self):
        for line in (
            "service password-encryption",
            "snmpv3 no-password enable",
            "tunnel key 123",
            "hash key source-address",
            "community 123",
            "utm license key netmeister",
        ):
            self.assertEqual(protect_input(line).masked, line)

    def test_private_key_export_and_interactive_input(self):
        for command in ("pki private-key export", "PKI PRIVATE-KEY EXP"):
            output = ConfigRedactor((command,)).response(1, "arbitraryPrivateKeyData")
            self.assertNotIn("arbitraryPrivateKeyData", output)
        for command in (
            "pki private-key import crypto " + SECRET,
            "pki private-key import pem rsa crypto colon:secret",
            'pki private-key import pem rsa crypto "contains file word"',
            "pki private-key import pem rsa crypto file",
            "pki priv imp pem rsa cr file extra",
            "pki cert import pem",
            "pki cert import pem password file:/not-a-source",
        ):
            with self.assertRaisesRegex(UsageError, "interactive"):
                validate_config_protocol((command,))
        validate_config_protocol(
            ("pki private-key import file key", "pki cert import der file:/cert")
        )
