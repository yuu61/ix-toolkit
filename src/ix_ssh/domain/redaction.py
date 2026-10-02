"""Configuration-output protection, using the IX 10.11 / IX-R 1.5a inventory.

Both families are considered even without a model. This is a privacy rule, not
a command allowlist. Unknown syntax within a sensitive family is hidden whole.
Show output and files downloaded by the device are outside this boundary.
"""

import re
import shlex
from dataclasses import dataclass
from urllib.parse import unquote

from .errors import UsageError

MASK = "[REDACTED]"
# Prefixes include submode commands and no forms (the latter are stripped first).
_FAMILIES = tuple(
    tuple(command.split())
    for command in (
        "username",
        "password",
        "authentication password",
        "authentication secret-password",
        "http-server username",
        "http-server guest-username",
        "http-server monitor-username",
        "http-server cross-site",
        "web-auth username",
        "ike policy",
        "ike preshared-key",
        "ikev2 authentication",
        "ip ospf authentication-key",
        "ip ospf message-digest-key",
        "area",
        "neighbor",
        "vrrp",
        "radius host",
        "snmp-agent",
        "snmpv3 user",
        "l2tp password",
        "tunnel v6pv user",
        "nm account",
        "ngn radius-auth password",
        "ip dhcp-client authentication",
        "ip rip authentication text",
        "ipsec manualkey",
        "mobile pin-auth",
        "mobile pin-code",
        "mobile puk",
        "mobile password",
        "mobile pin",
        "pin-authentication",
        "pin-code",
        "pin-unlock",
        "usbmem authentication",
        "pki cert import",
        "pki pkcs12 import",
        "pki private-key",
        "software-update",
        "startup",
        "utm license",
        "scp",
        "command",
        "query",
        "o",
    )
)
_TOKEN = re.compile(r""""(?:\\.|[^"\\])*"|'(?:\\.|[^'\\])*'|\S+""")
_QUOTED = re.compile(r""""(?:\\.|[^"\\])*"|'(?:\\.|[^'\\])*' """, re.VERBOSE)
_USERNAME_PASSWORD_MIN_TOKENS = 4
_MIN_FRAGMENT_LENGTH = 4
_DEVICE_ESCAPE = re.compile(r"\\(?:x([0-9a-fA-F]{2})|u([0-9a-fA-F]{4})|([0-7]{1,3}))")
_MODE_FIELDS = (
    (("ikev2", "authentication"), "key", {"char", "hex", "secret"}),
    (("radius", "host"), "key", {"0", "1"}),
    (("nm", "account"), "password", {"plain", "secret"}),
    (("ngn", "radius-auth"), "password", {"0", "1"}),
)
_IMPORT_SOURCES = {
    ("pki", "cert", "import"): (
        ("bundle", "url"),
        ("der", "name", None, "url"),
        ("pem", "name", None, "url"),
    ),
    ("pki", "private-key"): (
        ("bundle", "crypto", None, "file"),
        ("pem", "rsa", "crypto", None, "file"),
        ("pem", "dsa", "crypto", None, "file"),
    ),
}


def _value(token: str) -> str:
    try:
        values = shlex.split(token)
    except ValueError:
        return token.strip("\"'")
    return values[0] if values else token


def _family(words: list[str]) -> tuple[str, ...] | None:
    return next(
        (
            family
            for family in _FAMILIES
            if words
            and all(
                expected.startswith(actual) for actual, expected in zip(words, family, strict=False)
            )
        ),
        None,
    )


def _secret_start(words: list[str]) -> int | None:  # noqa: C901, PLR0911, PLR0912
    # Exact syntax only. Abbreviations and ambiguous/malformed input use the
    # whole-line fallback, including extra arguments on normally secret-free no.
    if words[0] in {"password", "query", "o", "command"}:
        return 1
    if (
        words[0] == "username"
        and len(words) >= _USERNAME_PASSWORD_MIN_TOKENS
        and words[2] == "password"
    ):
        # Consume only the required type. IX's optional numeric mode occupies
        # the same position as a numeric IX-R password, so protect it as well.
        return 4 if words[3] in {"plain", "hash", "secret"} else 3
    if words[:2] == ["authentication", "password"]:
        return 3
    if words[:2] == ["authentication", "secret-password"]:
        return 4
    if words[0] in {"pin-code", "pin-unlock", "pin-authentication"}:
        return 1 if words[0] != "pin-authentication" else 2
    if words[:2] == ["mobile", "puk"]:
        return 2
    if words[:2] in (
        ["mobile", "pin-auth"],
        ["mobile", "pin-code"],
        ["mobile", "password"],
        ["mobile", "pin"],
        ["l2tp", "password"],
    ):
        return 3
    if words[:2] == ["utm", "license"]:
        return 3
    if words[:2] == ["ipsec", "manualkey"] or words[0] in {"snmp-agent", "snmpv3"}:
        return None  # Variable algorithms/community positions: hide the arguments.
    for index, word in enumerate(words):
        if word in {
            "password",
            "secret-password",
            "crypto",
            "authentication-key",
            "message-digest-key",
            "key",
        }:
            start = index + 1
            if word == "message-digest-key":
                start += 1
            # Only these fields take a mode, exactly once. In other families
            # (HTTP, OSPF, PKI, etc.) the next token is already the value.
            for prefix, field, modes in _MODE_FIELDS:
                if (
                    tuple(words[: len(prefix)]) == prefix
                    and word == field
                    and words[start : start + 1]
                    and words[start] in modes
                ):
                    return start + 1
            return start
    if words[0] == "vrrp" and "authentication" in words:
        return words.index("authentication") + 1
    if words[:4] == ["ip", "rip", "authentication", "text"]:
        return 4
    return None


@dataclass(frozen=True)
class ProtectedInput:
    original: str
    masked: str
    secrets: tuple[str, ...]


def protect_input(line: str) -> ProtectedInput:
    tokens = list(_TOKEN.finditer(line))
    words = [_value(t.group()).lower() for t in tokens]
    offset = int(bool(words and words[0] == "no"))
    command = words[offset:]
    if command == ["utm", "license", "key", "netmeister"]:
        return ProtectedInput(line, line, ())
    family = _family(command)
    # URL userinfo/query may embed credentials even in commands without a known
    # credential field. Hide the complete URL rather than interpret vendor syntax.
    urls = [
        i
        for i, t in enumerate(tokens)
        if "://" in t.group() and ("@" in t.group() or "?" in t.group())
    ]
    if not family and not urls:
        return ProtectedInput(line, line, ())
    start = _secret_start(command) if family and tuple(command[: len(family)]) == family else None
    if family:
        start = start + offset if start is not None else offset + len(family)
        # Unknown or incomplete families must also hide partially entered values.
        if start >= len(tokens):
            start = offset
        positions = list(range(start, len(tokens)))
    else:
        positions = urls
    positions = sorted(set(positions + urls))
    secrets = tuple(_value(tokens[i].group()) for i in positions)
    masked = line
    for i in reversed(positions):
        t = tokens[i]
        masked = masked[: t.start()] + MASK + masked[t.end() :]
    return ProtectedInput(line, masked, secrets)


def _external_import_source(words: list[str], family: tuple[str, ...]) -> bool:
    # Follow the argument grammar, consuming name/crypto values even when they
    # are themselves "url" or "file". IX accepts abbreviated keywords.
    arguments = words[3:]
    for prefix in _IMPORT_SOURCES[family]:
        if len(arguments) <= len(prefix):
            continue
        if not all(
            value and (keyword is None or keyword.startswith(value))
            for keyword, value in zip(prefix, arguments, strict=False)
        ):
            continue
        source = arguments[len(prefix)]
        if source and (prefix[-1] == "file" or ":" in source):
            return True
    return False


def validate_config_protocol(lines: tuple[str, ...]) -> None:
    """Reject interactive key/certificate input before opening a device session."""
    for number, line in enumerate(lines, 1):
        words = [_value(token.group()).lower() for token in _TOKEN.finditer(line)]
        family = _family(words)
        if (
            family in _IMPORT_SOURCES
            and any("import".startswith(word) for word in words[2:3])
            and not _external_import_source(words, family)
        ):
            raise UsageError(
                f"ERROR: config line {number}: interactive key/certificate input "
                "is unsupported; use a file/URL form"
            )


class ConfigRedactor:
    """Scrub echoes and quoted/escaped/truncated diagnostic values per response.

    Secrets from earlier lines are retained for later diagnostics. Key exports
    are hidden as a whole: terminal private-key data is not a command echo.
    """

    def __init__(self, lines: tuple[str, ...]):
        self.inputs = tuple(protect_input(line) for line in lines)
        values = {
            variant
            for entry in self.inputs
            for secret in entry.secrets
            for variant in (secret, unquote(secret), secret.replace("\\", ""))
            if variant
        }
        # Arbitrary commands/queries/URLs may embed a value inside a larger
        # argument. Protect components too, without interpreting accepted syntax.
        self.secrets = values | {
            part
            for value in values
            for part in re.findall(r"\w+", value)
            if len(part) >= _MIN_FRAGMENT_LENGTH
        }

    def response(self, number: int, text: str) -> str:
        entry = self.inputs[number - 1]
        words = entry.original.lower().split()
        if "PRIVATE KEY" in text or (
            _family(words) == ("pki", "private-key")
            and any("export".startswith(word) for word in words[2:3])
        ):
            return f"config line {number}: private-key output {MASK}"
        # Use the exact echo's safe representation when possible. The remaining
        # diagnostic passes protect quoting changes, escaped strings and fragments.
        text = text.replace(entry.original, entry.masked)
        return self.message(text)

    def message(self, text: str) -> str:
        """Protect later diagnostics too, without changing failure classification."""
        if self.secrets:
            text = _QUOTED.sub(MASK, text)
            text = re.sub(r"\S*\\\S*", self._escaped_token, text)
        secrets = sorted(self.secrets, key=len, reverse=True)
        for secret in secrets:
            # A long echo can wrap inside the value; protect that before
            # considering the individual output lines or truncated tokens.
            text = re.sub(r"\s*".join(map(re.escape, secret)), MASK, text)
        for secret in secrets:
            for variant in (secret, secret.replace('"', r"\""), secret.replace("'", r"\'")):
                text = text.replace(variant, MASK)
            # Device truncation/wrapping is not assumed to preserve full values.
            parts = secret.split()
            for part in parts:
                prefix = re.escape(part[: min(4, len(part))])
                text = re.sub(prefix + r"[^\s\"']*", MASK, text)
                text = re.sub(re.escape(part[:1]) + r"[^\s]*\.\.\.", MASK, text)
        return text

    def _escaped_token(self, match: re.Match[str]) -> str:
        token = match.group()
        decoded = _DEVICE_ESCAPE.sub(
            lambda m: chr(int(m[1] or m[2] or m[3], 8 if m[3] else 16)), token
        )
        decoded = re.sub(r"\\(.)", r"\1", decoded)
        if any(secret in decoded for secret in self.secrets):
            return MASK
        return token
