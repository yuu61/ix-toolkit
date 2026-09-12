"""Text output for the CLI. Receives resolved values; never loads settings."""

from collections.abc import Iterable, Sequence
from dataclasses import dataclass
from pathlib import Path
from typing import TextIO

from ..domain import Target


@dataclass(frozen=True)
class InventoryRow:
    name: str
    host: str
    user: str
    port: str | int
    resolved_host: str
    resolved_port: str | int
    hops: tuple[str, ...]
    auth: str
    model: str = ""
    note: str = ""


def format_inventory(
    rows: Sequence[InventoryRow], where: str | None, looked_in: Iterable[str]
) -> str:
    where = where or "(none found)"
    if not rows:
        candidates = "\n".join(f"    {cand}" for cand in looked_in)
        return (
            f"inventory: {where}\n"
            "  no devices configured.\n"
            "  create the file, or pass --host HOST --user USER explicitly.\n"
            f"  looked in ($IX_INVENTORY overrides):\n{candidates}\n"
            '  schema: {"devices": {"NAME": {"host": "...", "username": "...", '
            '"password_env": "IX_PASS_NAME"}}}'
        )
    width = max(len(row.name) for row in rows)
    lines = [f"inventory: {where}", "devices (no default; pass --device NAME):"]
    for row in rows:
        route = ""
        if row.resolved_host != row.host:
            route = f" -> {row.resolved_host}:{row.resolved_port}"
        if row.hops:
            route += " via " + " -> ".join(row.hops)
        model = f"  model={row.model}" if row.model else ""
        lines.append(
            f"  {row.name:<{width}}  {row.user}@{row.host}:{row.port}{route}"
            f"  auth={row.auth}{model}"
        )
        if row.note:
            lines.append(f"  {'':<{width}}  note: {row.note}")
    return "\n".join(lines)


def format_target(target: Target) -> str:
    where = f"{target.username}@{target.host}:{target.port}"
    if target.alias and target.alias != target.host:
        where += f" [{target.alias}]"
    if target.hops:
        where += " via " + " -> ".join(h.spec for h in target.hops)
    if target.model:
        where += f", model {target.model}"
    if target.force_config:
        where += ", config-entry svintr-config (forced)"
    return f"# target: {target.label} ({where})"


class ConsoleOutput:
    """Owns headers and status messages independently of operation ordering."""

    def __init__(self, out: TextIO, err: TextIO, raw: bool):
        self.out, self.err, self.raw = out, err, raw

    def header(self, title: str) -> None:
        if not self.raw:
            print(f"===== {title} =====", file=self.out)

    def result(self, text: str) -> None:
        print(text, file=self.out)
        if not self.raw:
            print(file=self.out)

    def backup_saved(self, label: str, path: Path, line_count: int) -> None:
        print(
            f"[OK] running-config of {label} saved to {path} ({line_count} lines)",
            file=self.out,
        )
