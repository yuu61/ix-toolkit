"""Config files read from disk and backups written to it."""

from pathlib import Path

from ..domain import parse_config_lines


def read_config_file(path: str) -> list[str]:
    return parse_config_lines(Path(path).read_text(encoding="utf-8"))


def write_backup(path: Path, text: str) -> None:
    """Write a running-config, creating parent directories, always UTF-8 with a
    single trailing newline (so the skills need no mkdir or shell redirection)."""
    path.parent.mkdir(parents=True, exist_ok=True)
    path.write_text(text.rstrip("\n") + "\n", encoding="utf-8")
