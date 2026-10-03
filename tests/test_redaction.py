"""Privacy boundaries: synthetic values, both families and diagnostic variants."""

import json
import subprocess
import sys
import unittest

from ix_ssh.domain import UsageError
from ix_ssh.domain.redaction import ConfigRedactor, protect_input, validate_config_protocol

SECRET = "Zq9SensitiveValue"


class RedactionTest(unittest.TestCase):
    def _run_bounded_script(self, script: str, data=()):
        # Backtracking regressions must fail without hanging the test runner.
        result = subprocess.run(
            [sys.executable, "-c", script],
            input=json.dumps(data),
            capture_output=True,
            text=True,
            encoding="utf-8",
            timeout=5,
            check=True,
        )
        return json.loads(result.stdout)

    def test_long_diagnostic_misses_finish_within_a_bounded_time(self):
        script = """
import json
import sys
from ix_ssh.domain.redaction import ConfigRedactor
secret, text = json.load(sys.stdin)
redactor = ConfigRedactor(('password ' + json.dumps(secret),))
print(json.dumps(redactor.message(text)))
"""
        cases = (
            (" " * 12 + "X", " " * 40 + "hostname router"),
            (" " * 12 + "X", " " * 200000 + "hostname router"),
            (SECRET, "a" * 200000),
            (SECRET, "Z" * 200000),
            (SECRET, '"' + r"\" " * 100000),
            (SECRET, "'" + r"\' " * 100000),
        )
        for secret, text in cases:
            with self.subTest(secret=secret, length=len(text)):
                self.assertEqual(self._run_bounded_script(script, (secret, text)), text)

    def test_indented_echo_does_not_stop_later_lines_or_save(self):
        script = """
import json
from unittest.mock import Mock
from ix_ssh.infrastructure.session import NetmikoSession
lines = (' ' * 40 + 'hostname router', 'password "' + ' ' * 12 + 'X"',
         'logging buffered 100')
conn = Mock()
conn.send_config_set.side_effect = lines
conn.save_config.return_value = 'Configuration saved.'
session = NetmikoSession(conn, [], TimeoutError)
outputs = list(session.apply(lines))
saved = session.save()
print(json.dumps((outputs, saved, conn.send_config_set.call_count,
                  conn.exit_config_mode.call_count, conn.save_config.call_count)))
"""
        outputs, saved, applied, exited, saves = self._run_bounded_script(script)
        self.assertEqual(
            outputs,
            [" " * 40 + "hostname router", "password [REDACTED]", "logging buffered 100"],
        )
        self.assertEqual(saved, "Configuration saved.")
        self.assertEqual((applied, exited, saves), (3, 1, 1))

    def test_wrapped_whitespace_runs_are_masked(self):
        cases = (
            ("a   b", "a \n\t b"),
            ("a\t\tb", "a\t\n\t b"),
            (" " * 12 + "X", " " * 40 + "X"),
            ("XYZ" + " " * 12, "X\nY\tZ" + " " * 40),
            (" " * 12, " " * 40),
            ("a   b", "a   b / a\n  b"),
        )
        for secret, representation in cases:
            with self.subTest(secret=secret, representation=representation):
                redactor = ConfigRedactor(('password "' + secret + '"',))
                expected = "[REDACTED] / [REDACTED]" if " / " in representation else "[REDACTED]"
                self.assertEqual(redactor.message(representation), expected)

    def test_truncated_tokens_preserve_surrounding_text(self):
        for first in ("Z", "$", "."):
            with self.subTest(first=first):
                redactor = ConfigRedactor(("password " + first + "q9SensitiveValue",))
                text = "Notice: prefix" + first + "...tail...suffix"
                self.assertEqual(redactor.message(text), "Notice: prefix[REDACTED]suffix")
        redactor = ConfigRedactor(("password .q9SensitiveValue",))
        self.assertEqual(redactor.message("Notice: ..."), "Notice: ...")

    def test_unterminated_quotes_and_contractions_preserve_notices(self):
        redactor = ConfigRedactor(("password " + SECRET,))
        for text in ("Can't save current config.", 'Notice: "unfinished', "Notice: 'unfinished"):
            with self.subTest(text=text):
                self.assertEqual(redactor.message(text), text)

    def test_quoted_short_fragments_keep_existing_protection(self):
        redactor = ConfigRedactor(("password " + SECRET,))
        cases = (
            ('"Zq"', "[REDACTED]"),
            (r'\"Zq"', r"\[REDACTED]"),
            (r"\'Zq'", r"\[REDACTED]"),
            ("\"unfinished 'Zq'", '"unfinished [REDACTED]'),
            ('"unfinished\\\n"Zq"', '"unfinished\\\n[REDACTED]'),
        )
        for text, expected in cases:
            with self.subTest(text=text):
                self.assertEqual(redactor.message(text), expected)

    def test_inventory_families_and_deletion_forms(self):
        commands = (
            "username user password plain 1 {} administrator",
            "username user password hash {}",
            "password {} NewSecret",
            "authentication password user {}",
            "authentication secret-password 1 user {}",
            "http-server username user password {}",
            "http-server guest-username user secret-password {}",
            "http-server monitor-username user password {}",
            "http-server cross-site password {}",
            "web-auth username user password {}",
            "ike policy vpn peer any key {} key-type char",
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
            "pki cert import der name cert1 url https://example/cert password {}",
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

    def test_mode_keywords_in_values_are_secrets(self):
        templates = (
            "username user password plain {} administrator",
            "username user password plain 0 {} administrator",
            "username user password plain 1 {} administrator",
            "username user password hash {} administrator",
            "username user password secret {} administrator",
            "http-server username user password {}",
            "http-server guest-username user secret-password {}",
            "web-auth username user password {}",
            "neighbor 192.0.2.1 password {}",
            "ip ospf authentication-key {}",
            "ip ospf message-digest-key 2 {}",
            "area 0 virtual-link 192.0.2.1 message-digest-key 2 {}",
            "ike policy vpn peer any key {} key-type char",
            "ikev2 authentication psk id fqdn router key char {}",
            "ikev2 authentication psk id nbma-address key secret {}",
            "radius host ip 192.0.2.1 key 0 {}",
            "nm account group password plain {}",
            "ngn radius-auth password 1 {}",
            "ip dhcp-client authentication delayed-auth secret-id 1 key {}",
            "pki cert import pem name cert1 url https://example/cert password {} ipv6",
            "pki private-key import pem rsa crypto {} file key.pem",
            "software-update https://example/image password {} ipv6",
        )
        for template in templates:
            for secret in ("Secret", "PLAIN", "hash", "char", "hex", "0", "1"):
                for prefix in ("", "no "):
                    line = prefix + template.format(secret)
                    with self.subTest(line=line):
                        self.assertIn(secret, protect_input(line).secrets)
                        self.assertEqual(ConfigRedactor((line,)).message(secret), "[REDACTED]")

    def test_numeric_username_password_without_model_is_not_assumed_to_be_a_mode(self):
        for secret in ("0", "1"):
            line = f"username user password plain {secret} administrator"
            self.assertIn(secret, protect_input(line).secrets)

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
            "pki cert import pem name cert1",
            "pki cert import pem name url",
            "pki cert import pem name url url",
            "pki cert import pem name cert1 password url file:cert.pem",
            "pki cert import pem name cert1 url",
            'pki cert import pem name cert1 url ""',
            "pki cert import pem name cert1 url not-a-url",
            "pki cert import pem name cert1 account url file:cert.pem",
            "pki cert import pem name cert1 crypto url file:cert.pem",
            "pki cert import pem file:/cert",
            "pki c i p n cert1",
            "pki private-key import pem rsa crypto file",
            "pki private-key import pem rsa crypto pass file",
        ):
            with self.subTest(command=command), self.assertRaisesRegex(UsageError, "interactive"):
                validate_config_protocol((command,))

    def test_noninteractive_certificate_and_key_sources(self):
        for command in (
            "pki cert import pem name cert1 url file:cert.pem",
            "pki cert import der name cert1 url file:///cert.der",
            "pki cert import bundle url https://example/certs",
            "pki cert import pem name url url file:cert.pem",
            "pki cert import pem name cert1 url https://example/cert password file",
            "PKI CERT IMPORT PEM NAME cert1 URL file:cert.pem",
            "pki c i p n cert1 u file:cert.pem",
            "pki private-key import bundle crypto pass file key.bundle",
            "pki private-key import pem rsa crypto pass file key.pem",
            "pki private-key import pem dsa crypto crypto file key.pem",
            "pki private-key import pem rsa crypto file file key.pem",
            "pki priv imp p r cr pass f key.pem",
        ):
            with self.subTest(command=command):
                validate_config_protocol((command,))
