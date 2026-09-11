"""Which device a run talks to, and how the settings for it are put together."""

import re
from collections.abc import Callable, Mapping
from dataclasses import dataclass
from pathlib import Path

from .errors import UsageError
from .inventory import DEFAULT_PORT, entry_host, entry_user
from .proxyjump import Hop, SshConfigLookup, hop_specs, resolve_hop


@dataclass(frozen=True)
class TargetRequest:
    """The target-related inputs of one run, as the user gave them (flags)."""

    device: str | None = None
    host: str | None = None
    user: str | None = None
    port: str | None = None
    password_env: str | None = None
    ask_password: bool = False
    key_file: str | None = None
    ssh_config: str | None = None
    no_ssh_config: bool = False


@dataclass(frozen=True)
class Target:
    name: str | None
    host: str
    username: str
    port: int
    password: str | None
    key_file: str | None
    use_keys: bool
    ssh_config_file: str | None
    hops: tuple[Hop, ...] = ()
    alias: str | None = None  # the ssh_config alias the host came from, if any
    model: str | None = None  # inventory hint (IX2215 ...); unused by the connection

    @property
    def label(self) -> str:
        return self.name or self.alias or self.host

    def banner(self) -> str:
        where = f"{self.username}@{self.host}:{self.port}"
        if self.alias and self.alias != self.host:
            where += f" [{self.alias}]"
        if self.hops:
            where += " via " + " -> ".join(h.spec for h in self.hops)
        if self.model:
            where += f", model {self.model}"
        return f"# target: {self.label} ({where})"

    def slug(self) -> str:
        return re.sub(r"[^A-Za-z0-9._-]", "_", self.label)


def select_entry(
    req: TargetRequest,
    devices: Mapping[str, Mapping],
    env: Mapping[str, str],
    describe: Callable[[], str],
) -> tuple[str | None, Mapping]:
    """Pick the inventory entry for the run: (name, entry). An ad-hoc --host run
    has no name and an empty entry. There is deliberately no default device, so
    a config push can never land on the wrong box by omission; the error carries
    the device listing (`describe`) so the user can pick one."""
    name = req.device or env.get("IX_DEVICE")
    host = req.host or env.get("IX_HOST")
    if name:
        if name not in devices:
            raise UsageError(f"ERROR: unknown device {name!r}.\n" + describe())
        return name, devices[name]
    if not host:
        raise UsageError(
            "ERROR: no target selected. pass --device NAME (or --host HOST --user USER).\n"
            + describe()
        )
    return None, {}


def ssh_config_path(req: TargetRequest, entry: Mapping, default: str) -> str | None:
    """The ssh_config to resolve aliases/ProxyJump with; None when turned off."""
    if req.no_ssh_config:
        return None
    return req.ssh_config or entry.get("ssh_config_file") or default


def resolve_target(
    req: TargetRequest,
    name: str | None,
    entry: Mapping,
    env: Mapping[str, str],
    cfg: SshConfigLookup | None,
    ssh_config_file: str | None,
) -> Target:
    """Combine flags, environment, the inventory entry and ssh_config into a
    Target. Each setting takes the first source that has it:

    1. command-line flag        (--host / --user / --port / ...)
    2. environment variable     ($IX_HOST / $IX_USER / $IX_PORT)
    3. the inventory entry
    4. ssh_config (HostName / User / Port of the alias)
    5. built-in default         (port 22)

    The password is left None: credentials are resolved separately."""
    host = req.host or env.get("IX_HOST") or entry_host(entry)
    username = req.user or env.get("IX_USER") or entry_user(entry)
    port = req.port or env.get("IX_PORT") or entry.get("port")
    key_file = req.key_file or entry.get("key_file")

    if not host:
        raise UsageError(f'ERROR: no host for {name!r} (add "host" to the inventory entry)')

    # "host" may be an ssh_config alias: resolve hostname/port/user through it and
    # collect the ProxyJump chain, so an entry can be just {"host": "room1"}.
    alias = host
    hops: tuple[Hop, ...] = ()
    if cfg is not None:
        looked_up = cfg.lookup(host)
        host = looked_up.get("hostname", host)
        port = port or looked_up.get("port")
        username = username or looked_up.get("user")
        hops = tuple(resolve_hop(cfg, spec) for spec in hop_specs(cfg, alias))
    port = port or DEFAULT_PORT

    if not username:
        raise UsageError(
            f"ERROR: no username for {name or host} "
            '(add "username" to the inventory entry, or pass --user)'
        )
    try:
        port = int(port)
    except (TypeError, ValueError):
        raise UsageError(f"ERROR: invalid port: {port!r}") from None

    return Target(
        name=name,
        host=host,
        username=username,
        port=port,
        password=None,
        key_file=_expanduser(key_file),
        use_keys=bool(key_file) or bool(entry.get("use_keys")),
        ssh_config_file=_expanduser(ssh_config_file),
        hops=hops,
        alias=alias,
        model=entry.get("model"),
    )


def _expanduser(path: str | None) -> str | None:
    return str(Path(path).expanduser()) if path else None
