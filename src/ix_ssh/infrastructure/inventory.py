"""Locating and reading the inventory file."""

import json
import os
from collections.abc import Mapping
from pathlib import Path

from ..domain import UsageError, parse_inventory

INVENTORY_ENV = "IX_INVENTORY"
INVENTORY_CANDIDATES = (
    # Agent-neutral location first: the skills run under Claude Code, Codex and
    # anything else that reads SKILL.md, so the inventory cannot live under one
    # agent's home. The ~/.claude path stays for setups that predate this.
    Path.home() / ".ix-toolkit" / "devices.json",
    Path.home() / ".claude" / "ix-devices.json",
)


def inventory_path(
    override: str | None = None, env: Mapping[str, str] | None = None
) -> Path | None:
    """Path of the inventory to use: --inventory, then $IX_INVENTORY, then the
    first candidate that exists. None when nothing is found."""
    env = os.environ if env is None else env
    chosen = override or env.get(INVENTORY_ENV)
    if chosen:
        return Path(chosen).expanduser()
    for cand in INVENTORY_CANDIDATES:
        if cand.is_file():
            return cand
    return None


def read_inventory(
    override: str | None = None, env: Mapping[str, str] | None = None
) -> tuple[dict[str, dict], Path | None]:
    """({name: entry}, path). An absent inventory is empty, not an error: ad-hoc
    --host runs and `--list` (which then says where it looked) still work."""
    path = inventory_path(override, env)
    if path is None:
        return {}, None
    if not path.is_file():
        raise UsageError(f"ERROR: inventory file not found: {path}")
    try:
        data = json.loads(path.read_text(encoding="utf-8"))
    except json.JSONDecodeError as exc:
        raise UsageError(f"ERROR: invalid JSON in {path}: {exc}") from None
    except (OSError, UnicodeError) as exc:
        raise UsageError(f"ERROR: cannot read inventory {path}: {exc}") from exc
    return parse_inventory(data, str(path)), path
