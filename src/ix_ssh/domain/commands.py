"""What may be sent to the device, and how a backup is named."""

from datetime import datetime
from pathlib import Path


def is_show_command(cmd: str) -> bool:
    """Show mode only ever sends `show ...`; anything else is refused there so a
    stray config line cannot slip through the read-only skills."""
    return cmd.strip().lower().startswith("show")


def parse_config_lines(text: str) -> list[str]:
    """Config lines from a file: one per line, blank lines and # comments dropped,
    indentation kept (NEC IX accepts it and it keeps the file readable)."""
    lines: list[str] = []
    for raw in text.splitlines():
        s = raw.strip()
        if not s or s.startswith("#"):
            continue
        lines.append(raw.rstrip())
    return lines


def default_backup_path(slug: str, now: datetime) -> Path:
    """backups/<device>-<YYYYMMDD-HHMMSS>.conf. The timestamp is the operator's
    local time (they read the file names), so `now` should be local-aware."""
    return Path("backups") / f"{slug}-{now.strftime('%Y%m%d-%H%M%S')}.conf"
