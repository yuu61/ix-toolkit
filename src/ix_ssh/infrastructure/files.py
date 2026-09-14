"""Config files read from disk and backups written to it."""

from pathlib import Path

from ..domain import UsageError, parse_config_lines


def read_config_file(path: str) -> list[str]:
    try:
        text = Path(path).read_text(encoding="utf-8")
    except (OSError, UnicodeError) as exc:
        raise UsageError(f"ERROR: cannot read config file {path}: {exc}") from exc
    return parse_config_lines(text)


def write_backup(path: Path, text: str) -> None:
    """Write a running-config, creating parent directories, always UTF-8 with a
    single trailing newline (so the skills need no mkdir or shell redirection).
    Files are created with 600 permissions for security."""
    try:
        import os

        path.parent.mkdir(parents=True, exist_ok=True)

        def opener(path_str, flags):
            return os.open(path_str, flags, 0o600)

        with open(path, "w", encoding="utf-8", opener=opener) as f:
            f.write(text.rstrip("\n") + "\n")
        # Explicitly chmod to handle existing files
        os.chmod(path, 0o600)
        if os.name == "nt":
            import subprocess

            subprocess.run(
                [
                    "icacls",
                    str(path),
                    "/inheritance:r",
                    "/grant:r",
                    f"{os.environ.get('USERNAME', '')}:F",
                ],
                capture_output=True,
                check=False,
            )
    except (OSError, UnicodeError) as exc:
        raise UsageError(f"ERROR: cannot write backup {path}: {exc}") from exc
