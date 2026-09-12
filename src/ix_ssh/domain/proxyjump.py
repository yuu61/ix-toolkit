"""ssh_config aliases and ProxyJump chains, as rules over an already-parsed config.

The config itself (paramiko.SSHConfig, or anything with a compatible `lookup`) is
parsed by infrastructure; this module only decides what a lookup result means.
"""

from collections.abc import Mapping
from dataclasses import dataclass
from pathlib import Path
from typing import Any, Protocol

from .errors import UsageError
from .inventory import DEFAULT_PORT


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
    """Resolved address and jump chain after applying explicit settings and ssh_config."""

    hostname: str
    port: int
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
        port = parse_port(raw)
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
        port=parse_port(port or entry.get("port") or DEFAULT_PORT),
        user=user or entry.get("user"),
        keys=tuple(str(Path(k).expanduser()) for k in keys),
    )


def parse_port(value: object) -> int:
    try:
        return int(value)
    except (TypeError, ValueError):
        raise UsageError(f"ERROR: invalid port: {value!r}") from None


def resolve_route(
    cfg: SshConfigLookup | None,
    host: str,
    user: str | None = None,
    port: str | int | None = None,
) -> Route:
    """Apply ssh_config defaults after explicit settings, for both listing and connection.

    A user may be absent in an incomplete inventory; connecting validates it later.
    """
    looked_up = cfg.lookup(host) if cfg is not None else {}
    return Route(
        hostname=looked_up.get("hostname", host),
        port=parse_port(port or looked_up.get("port") or DEFAULT_PORT),
        user=user or looked_up.get("user"),
        hops=tuple(hop_specs(cfg, host)),
    )
