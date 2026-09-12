"""Build an inventory listing, using the same resolution rules as connections."""

import os
from collections.abc import Mapping
from pathlib import Path

from .. import infrastructure
from ..domain import (
    DEFAULT_PORT,
    TargetRequest,
    entry_host,
    entry_user,
    resolve_password,
    resolve_route,
    ssh_config_path,
)
from .presentation import InventoryRow, format_inventory


def describe_devices(
    devices: Mapping[str, Mapping],
    path: Path | None,
    env: Mapping[str, str],
    target: TargetRequest | None = None,
) -> str:
    target = target if target is not None else TargetRequest()
    rows = []
    for name, entry in devices.items():
        host = entry_host(entry) or "?"
        user = entry_user(entry)
        port = entry.get("port") or DEFAULT_PORT
        cfg_path = ssh_config_path(target, entry, infrastructure.DEFAULT_SSH_CONFIG)
        cfg = infrastructure.quiet_load_ssh_config(cfg_path)
        # Listing incomplete/broken entries must remain possible. This boundary
        # includes lookup and route resolution, not just parsing the config file.
        try:
            route = resolve_route(cfg, host, user, entry.get("port"))
            resolved_host, resolved_port = route.hostname, route.port
            user, hops = route.user, route.hops
            if resolved_host == host:
                port = resolved_port
        except Exception:  # noqa: BLE001 - diagnostics must survive broken ssh_config
            resolved_host, resolved_port, hops = host, port, ()
        password = resolve_password(entry, env)
        if password.source == "inventory":
            auth = "inventory-password"
        elif password.source == "environment":
            auth = f"env:${password.variable}"
        elif entry.get("key_file") or entry.get("use_keys"):
            auth = "ssh-key"
        elif entry.get("password_env"):
            # No value available: retain the configured variable as a setup hint.
            auth = f"env:${entry['password_env']}"
        else:
            auth = "prompt/$IX_PASS"
        rows.append(
            InventoryRow(
                name,
                host,
                user or "?",
                port,
                resolved_host,
                resolved_port,
                hops,
                auth,
                entry.get("model") or "",
                entry.get("note") or "",
            )
        )
    return format_inventory(
        rows, str(path) if path else None, map(str, infrastructure.INVENTORY_CANDIDATES)
    )


def list_devices(
    inventory: str | None = None,
    env: Mapping[str, str] | None = None,
    target: TargetRequest | None = None,
) -> str:
    env = os.environ if env is None else env
    devices, path = infrastructure.read_inventory(inventory, env)
    return describe_devices(devices, path, env, target)
