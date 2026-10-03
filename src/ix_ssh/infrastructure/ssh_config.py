"""Reading ~/.ssh/config into something the domain can `lookup` against."""

import glob
import io
import re
import shlex
from pathlib import Path

from ..domain import UsageError
from ..domain.proxyjump import SshConfigLookup
from .deps import missing_dependency
from .files import HAND_EDITED_ENCODING

DEFAULT_SSH_CONFIG = str(Path.home() / ".ssh" / "config")
MAX_INCLUDE_DEPTH = 8


class _EffectiveSshConfig:
    """Expose only the proxy selected by OpenSSH's directive precedence."""

    def __init__(self, config: SshConfigLookup):
        self._config = config

    def lookup(self, hostname: str):
        # Paramiko inserts keys in the order matching directives first set them,
        # including Match final's second pass, but treats the proxy keys as
        # independent. Resolve their competition before domain route checks.
        entry = self._config.lookup(hostname)
        options = dict(entry)
        for key, value in entry.items():
            if key == "proxycommand":
                options.pop("proxyjump", None)
                break
            if key == "proxyjump" and value.strip().lower() != "none":
                options.pop("proxycommand", None)
                break
        return options


def _read_ssh_config_text(path: Path, depth: int = 0) -> str:
    """Read an ssh_config, expanding `Include` in place (paramiko does not do it:
    it parses `include` as an ordinary keyword, so aliases living in an included
    file would silently resolve to themselves)."""
    if depth > MAX_INCLUDE_DEPTH:
        raise UsageError(f"ERROR: ssh_config Include nesting is too deep: {path}")
    if not path.is_file():
        return ""
    out = []
    for line in path.read_text(encoding=HAND_EDITED_ENCODING, errors="replace").splitlines():
        stripped = line.strip()
        include = re.fullmatch(r"include(?:\s*=\s*|\s+)(.*)", stripped, re.IGNORECASE)
        if include:
            # OpenSSH quotes paths containing spaces. Preserve Windows path
            # separators: shlex's POSIX backslash escaping would eat them.
            lexer = shlex.shlex(include[1], posix=True)
            lexer.whitespace_split = True
            lexer.quotes = '"'
            lexer.escape = ""
            patterns = list(lexer)
            if not patterns:
                raise ValueError("Include needs at least one path")
            for pattern in patterns:
                expanded = Path(pattern).expanduser()
                if not expanded.is_absolute():
                    expanded = path.parent / expanded
                # Keep glob's hidden-file and nonrecursive ** semantics; Path.glob
                # differs and cannot accept these absolute Include patterns.
                matches = sorted(map(Path, glob.glob(str(expanded))))  # noqa: PTH207
                out.extend(_read_ssh_config_text(inc, depth + 1) for inc in matches)
        else:
            out.append(line)
    return "\n".join(out)


def load_ssh_config(path: str | Path):
    """Effective SSH settings for `path`, or None when the file is missing/empty."""
    try:
        import paramiko
    except ImportError as e:
        raise missing_dependency("paramiko") from e

    cfg = paramiko.SSHConfig()
    try:
        text = _read_ssh_config_text(Path(path).expanduser())
        if not text:
            return None
        # Paramiko's special handling of unquoted `ProxyCommand none` overwrites
        # earlier commands in the same block. Quoting preserves first-value wins
        # while keeping an explicit none present to block later ProxyJump values.
        text = re.sub(
            r"(?im)^([ \t]*proxycommand(?:[ \t]*=[ \t]*|[ \t]+))none[ \t]*$",
            r'\1"none"',
            text,
        )
        cfg.parse(io.StringIO(text))
    except (OSError, UnicodeError, ValueError, RuntimeError, paramiko.SSHException) as exc:
        raise UsageError(f"ERROR: cannot read ssh_config {path}: {exc}") from exc
    return _EffectiveSshConfig(cfg)


def quiet_load_ssh_config(path: str | None):
    """load_ssh_config, but never fatal: --list must work without paramiko and
    must not depend on ssh_config health."""
    if not path:
        return None
    try:
        return load_ssh_config(path)
    except Exception:  # noqa: BLE001 - listing must not depend on ssh_config health
        return None
