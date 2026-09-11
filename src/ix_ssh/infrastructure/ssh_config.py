"""Reading ~/.ssh/config into something the domain can `lookup` against."""

import glob
import io
import os
from pathlib import Path

from .deps import missing_dependency

DEFAULT_SSH_CONFIG = str(Path.home() / ".ssh" / "config")


def _read_ssh_config_text(path: Path, depth: int = 0) -> str:
    """Read an ssh_config, expanding `Include` in place (paramiko does not do it:
    it parses `include` as an ordinary keyword, so aliases living in an included
    file would silently resolve to themselves)."""
    if depth > 8 or not path.is_file():
        return ""
    out = []
    for line in path.read_text(encoding="utf-8", errors="replace").splitlines():
        stripped = line.strip()
        if stripped.lower().startswith("include "):
            for pattern in stripped.split(None, 1)[1].split():
                pattern = os.path.expanduser(pattern)
                if not os.path.isabs(pattern):
                    pattern = str(path.parent / pattern)
                for inc in sorted(map(Path, glob.glob(pattern))):
                    out.append(_read_ssh_config_text(inc, depth + 1))
        else:
            out.append(line)
    return "\n".join(out)


def load_ssh_config(path: str | Path):
    """paramiko.SSHConfig for `path`, or None when the file is missing/empty."""
    try:
        import paramiko
    except ImportError as e:
        raise missing_dependency("paramiko") from e

    cfg = paramiko.SSHConfig()
    text = _read_ssh_config_text(Path(path).expanduser())
    if not text:
        return None
    cfg.parse(io.StringIO(text))
    return cfg


def quiet_load_ssh_config(path: str | None):
    """load_ssh_config, but never fatal: --list must work without paramiko and
    must not depend on ssh_config health."""
    if not path:
        return None
    try:
        return load_ssh_config(path)
    except Exception:  # noqa: BLE001 - listing must not depend on ssh_config health
        return None
