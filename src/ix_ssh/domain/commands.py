"""What may be sent to the device, and how a backup is named."""

import re
from collections.abc import Sequence
from datetime import datetime
from pathlib import Path

from .errors import UsageError

# IX 10.11 / IX-R 1.5a CLI diagnostics start with '%'. Successful saves and
# informational warnings do too, so '%' alone must never mean failure.
COMMAND_ERROR_PATTERN = (
    r"(?im)^%[^\r\n]*\b(?:invalid|incomplete|ambiguous|error|failed|failure|denied|"
    r"cannot|can't|unable|insufficient|not found|not allowed|not permitted|not supported)"
    r"\b[^\r\n]*"
)


def is_show_command(cmd: str) -> bool:
    """Show mode only ever sends `show ...`; anything else is refused there so a
    stray config line cannot slip through the read-only skills."""
    # Check before stripping: even a trailing newline or TAB can execute or
    # complete input on the device's interactive terminal.
    return all(c.isprintable() for c in cmd) and cmd.strip().split(" ", 1)[0].lower() == "show"


def validate_show_commands(commands: Sequence[str]) -> None:
    for cmd in commands:
        if not is_show_command(cmd):
            raise UsageError(
                f"ERROR: expected a single show command without control characters: {cmd!r}"
            )


def check_command_output(command: str, output: str) -> str:
    """Reject known CLI error diagnostics, returning successful output intact."""
    match = re.search(COMMAND_ERROR_PATTERN, output)
    if match:
        raise UsageError(f"ERROR: {command!r} failed: {match.group(0)}")
    return output


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
