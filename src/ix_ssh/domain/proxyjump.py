"""ssh_config aliases and ProxyJump chains, as rules over an already-parsed config.

The config itself (paramiko.SSHConfig, or anything with a compatible `lookup`) is
parsed by infrastructure; this module only decides what a lookup result means.
"""

from collections.abc import Mapping
from dataclasses import dataclass
from pathlib import Path
from typing import Any, Protocol


class SshConfigLookup(Protocol):
    """What we need from a parsed ssh_config: paramiko.SSHConfig satisfies it."""

    def lookup(self, hostname: str) -> Mapping[str, Any]: ...


@dataclass(frozen=True)
class Hop:
    """One ProxyJump element with its ssh_config settings applied."""

    spec: str  # as written in ProxyJump ([user@]host[:port]); shown in banners
    host: str
    port: int
    user: str | None
    keys: tuple[str, ...]  # IdentityFile entries, ~ expanded; empty -> agent/default


@dataclass(frozen=True)
class Route:
    """What ssh_config says about a host: the real address and the jump chain."""

    hostname: str
    port: int | None
    user: str | None
    hops: tuple[str, ...]  # ProxyJump specs, outermost first


def split_hop(spec: str) -> tuple[str | None, str, int | None]:
    """Split a ProxyJump element ([user@]host[:port]) into its parts."""
    user = None
    port = None
    host = spec.strip()
    if "@" in host:
        user, host = host.rsplit("@", 1)
    if host.count(":") == 1:
        host, raw = host.split(":", 1)
        port = int(raw)
    return user, host, port


def hop_specs(cfg: SshConfigLookup | None, host: str, _seen: set | None = None) -> list[str]:
    """Flatten the ProxyJump chain for `host`, outermost hop first. A jump host
    that itself has a ProxyJump is expanded before it."""
    if cfg is None:
        return []
    _seen = _seen if _seen is not None else set()
    if host in _seen:
        return []
    _seen.add(host)
    proxyjump = cfg.lookup(host).get("proxyjump")
    if not proxyjump:
        return []
    hops: list[str] = []
    for spec in proxyjump.split(","):
        _, hop_host, _ = split_hop(spec)
        hops.extend(hop_specs(cfg, hop_host, _seen))
        hops.append(spec.strip())
    return hops


def resolve_hop(cfg: SshConfigLookup | None, spec: str) -> Hop:
    """Turn one hop spec into concrete connection settings via ssh_config."""
    user, host, port = split_hop(spec)
    entry = cfg.lookup(host) if cfg else {}
    keys = entry.get("identityfile") or []
    if isinstance(keys, str):
        keys = [keys]
    return Hop(
        spec=spec.strip(),
        host=entry.get("hostname", host),
        port=port or int(entry.get("port", 22)),
        user=user or entry.get("user"),
        keys=tuple(str(Path(k).expanduser()) for k in keys),
    )


def describe_route(cfg: SshConfigLookup | None, host: str) -> Route | None:
    """Resolve an alias for display (`--list`). None when there is no config."""
    if cfg is None:
        return None
    looked_up = cfg.lookup(host)
    raw_port = looked_up.get("port")
    return Route(
        hostname=looked_up.get("hostname", host),
        port=int(raw_port) if raw_port else None,
        user=looked_up.get("user"),
        hops=tuple(hop_specs(cfg, host)),
    )
