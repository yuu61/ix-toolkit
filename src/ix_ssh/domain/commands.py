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
    if re.search(r"(?im)^%\s*CONFIG process is occupied\.", output):
        raise UsageError(f"ERROR: {command!r}: config mode is occupied by another user")
    match = re.search(COMMAND_ERROR_PATTERN, output)
    if match:
        raise UsageError(f"ERROR: {command!r} failed: {match.group(0)}")
    return output


def config_stopped_at(number: int, total: int) -> str:
    """Names a failed config line by its position only. The line itself can carry a
    secret (pre-shared key, password), so it never goes into an error message."""
    return f"configuration stopped at config line {number} of {total}"


def mask_config_line(text: str, line: str) -> str:
    """`text` with `line`, and each word of it, replaced by ***. IX quotes rejected
    input in its diagnostic (`% <input> -- Invalid command.`), so a message about
    a config line must be masked before it is shown. A word is masked wherever it
    stands apart from letters and digits (`'key'` -> `'***'`), not inside a longer
    word (`in` leaves `Invalid` alone)."""
    parts = sorted({line.strip(), *line.split()} - {""}, key=len, reverse=True)
    if not parts:
        return text
    alternatives = "|".join(re.escape(p) for p in parts)
    return re.sub(rf"(?<![0-9A-Za-z])(?:{alternatives})(?![0-9A-Za-z])", "***", text)


def check_config_line_output(line: str, number: int, total: int, output: str) -> str:
    """check_command_output for one config line: reject the device's diagnostic,
    naming the line by its position and masking it in the diagnostic."""
    match = re.search(COMMAND_ERROR_PATTERN, output)
    if match:
        raise UsageError(
            f"ERROR: {config_stopped_at(number, total)}; earlier lines may already be "
            f"applied: {mask_config_line(match.group(0), line)}"
        )
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
