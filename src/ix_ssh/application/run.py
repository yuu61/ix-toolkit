import getpass
import os
import sys
from collections.abc import Callable, Mapping, Sequence
from dataclasses import dataclass, field, replace
from datetime import datetime, timezone
from pathlib import Path
from typing import Protocol, TextIO

from .. import infrastructure
from ..domain import (
    Target,
    TargetRequest,
    UsageError,
    default_backup_path,
    describe_route,
    entry_host,
    find_password,
    format_inventory,
    is_show_command,
    missing_password_message,
    resolve_target,
    select_entry,
    ssh_config_path,
)

# Sentinel so the CLI can tell "--backup omitted" from "--backup with no path".
BACKUP_AUTO = "\x00auto"


@dataclass(frozen=True)
class Request:
    """One invocation, as the CLI parsed it. Operations run in the order shows ->
    backup -> config -> save, so a backup taken in the same run is always the
    state before the change."""

    target: TargetRequest = field(default_factory=TargetRequest)
    shows: tuple[str, ...] = ()
    config_lines: tuple[str, ...] = ()
    config_file: str | None = None
    backup: str | None = None  # None: no backup; BACKUP_AUTO: default name; else a path
    save: bool = False
    raw: bool = False
    list: bool = False
    inventory: str | None = None


class Session(Protocol):
    """What execute() needs from an open connection. infrastructure.NetmikoSession
    is the real one; tests substitute a fake."""

    def show(self, cmd: str) -> str: ...
    def apply(self, lines: Sequence[str]) -> str: ...
    def save(self) -> str: ...
    def close(self) -> None: ...


def list_devices(inventory: str | None = None) -> str:
    """The `--list` text. ssh_config is consulted per device to show the resolved
    address and jump chain, but quietly: listing must work without paramiko."""
    devices, path = infrastructure.read_inventory(inventory)
    routes = {}
    for name, entry in devices.items():
        host = entry_host(entry)
        if not host:
            continue
        cfg = infrastructure.quiet_load_ssh_config(
            entry.get("ssh_config_file") or infrastructure.DEFAULT_SSH_CONFIG
        )
        routes[name] = describe_route(cfg, host)
    looked_in = [str(c) for c in infrastructure.INVENTORY_CANDIDATES]
    return format_inventory(devices, str(path) if path else None, looked_in, routes)


def prepare_target(
    req: Request,
    env: Mapping[str, str] = os.environ,
    prompt: Callable[[str], str] = getpass.getpass,
) -> Target:
    """Inventory + ssh_config + flags -> a Target with its password resolved."""
    devices, _ = infrastructure.read_inventory(req.inventory)
    name, entry = select_entry(req.target, devices, env, lambda: list_devices(req.inventory))
    cfg_path = ssh_config_path(req.target, entry, infrastructure.DEFAULT_SSH_CONFIG)
    cfg = infrastructure.load_ssh_config(cfg_path) if cfg_path else None
    target = resolve_target(req.target, name, entry, env, cfg, cfg_path)

    password = find_password(entry, env, req.target.password_env)
    if password is None and not target.use_keys:
        if not req.target.ask_password:
            raise UsageError(missing_password_message(target.label, entry))
        # Explicit opt-in: never prompt on its own. stdin.isatty() is not a usable
        # interactivity test here (native Windows python reports True even when
        # stdin is redirected), so an automatic prompt would hang the skill.
        password = prompt(f"password for {target.label}: ")
    return replace(target, password=password)


def _local_now() -> datetime:
    # 退避ファイル名は運用者が読む現地時刻。UTC に寄せず tz だけ付ける。
    return datetime.now(timezone.utc).astimezone()


def execute(
    req: Request,
    target: Target,
    session: Session,
    out: TextIO | None = None,
    err: TextIO | None = None,
    now: Callable[[], datetime] = _local_now,
) -> None:
    """Run the requested operations on an open session, in a fixed order.
    `req.config_lines` must already include the lines of `req.config_file`."""
    out = out or sys.stdout
    err = err or sys.stderr

    def header(text: str) -> None:
        if not req.raw:
            print(f"===== {text} =====", file=out)

    def trailer() -> None:
        if not req.raw:
            print(file=out)

    for cmd in req.shows:
        if not is_show_command(cmd):
            print(f"[SKIP] refusing non-show command in show mode: {cmd!r}", file=err)
            continue
        header(cmd)
        print(session.show(cmd), file=out)
        trailer()

    if req.backup is not None:
        text = session.show("show running-config")
        if req.backup == BACKUP_AUTO:
            dest = default_backup_path(target.slug(), now())
        else:
            dest = Path(req.backup)
        infrastructure.write_backup(dest, text)
        print(
            f"[OK] running-config of {target.label} saved to {dest} "
            f"({len(text.splitlines())} lines)",
            file=out,
        )

    if req.config_lines:
        header("config")
        print(session.apply(req.config_lines), file=out)
        trailer()

    if req.save:
        header("write memory")
        print(session.save(), file=out)
        trailer()


def run(
    req: Request,
    env: Mapping[str, str] = os.environ,
    prompt: Callable[[str], str] = getpass.getpass,
    out: TextIO | None = None,
    err: TextIO | None = None,
    open_session: Callable[[Target], Session] = infrastructure.open_session,
) -> None:
    # stdout/stderr are looked up at call time so redirect_stdout() works on them.
    out = out or sys.stdout
    err = err or sys.stderr
    if req.list:
        print(list_devices(req.inventory), file=out)
        return

    # Read the config file before touching the network, so a typo in its path
    # fails here and not after a connection is up.
    config_lines = list(req.config_lines)
    if req.config_file:
        config_lines.extend(infrastructure.read_config_file(req.config_file))
    if not req.shows and not config_lines and not req.save and req.backup is None:
        raise UsageError(
            "ERROR: nothing to do (give show commands, --backup, --config, --save, or --list)"
        )

    target = prepare_target(req, env, prompt)
    if not req.raw:
        print(target.banner(), file=err)

    session = open_session(target)
    try:
        execute(
            replace(req, config_lines=tuple(config_lines), config_file=None),
            target,
            session,
            out,
            err,
        )
    finally:
        session.close()
