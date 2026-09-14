"""Config files read from disk and backups written to it."""

import os
from pathlib import Path

from ..domain import UsageError, parse_config_lines
from .permissions import restrict_to_owner


def read_config_file(path: str) -> list[str]:
    try:
        text = Path(path).read_text(encoding="utf-8")
    except (OSError, UnicodeError) as exc:
        raise UsageError(f"ERROR: cannot read config file {path}: {exc}") from exc
    return parse_config_lines(text)


def _private_opener(file, flags):
    """open() opener: new files are born 0600, and a pre-existing file is tightened
    before anything is written to it (the mode passed to os.open is ignored for
    files that already exist, and chmod-after-write would leave the fresh
    running-config readable by others while it is being written)."""
    fd = os.open(file, flags, 0o600)
    if os.name != "nt":
        try:
            os.fchmod(fd, 0o600)
        except OSError:
            pass  # filesystem without POSIX modes (vfat, some CIFS): nothing to tighten
    return fd


def write_backup(path: Path, text: str) -> bool:
    """Write a running-config, creating parent directories, always UTF-8 with a
    single trailing newline (so the skills need no mkdir or shell redirection).
    The file is private to the current user (0600 / owner-only ACL); returns
    False when that could not be enforced, which is not a write failure."""
    try:
        path.parent.mkdir(parents=True, exist_ok=True)
        with open(path, "w", encoding="utf-8", opener=_private_opener) as f:
            f.write(text.rstrip("\n") + "\n")
    except (OSError, UnicodeError) as exc:
        raise UsageError(f"ERROR: cannot write backup {path}: {exc}") from exc
    # The POSIX mode is set at open time; the Windows DACL can only be applied
    # to a file that exists, so this must come after the write and outside the
    # "cannot write backup" boundary.
    return restrict_to_owner(path)
