"""The inventory: a mapping of device name -> entry, and how an entry is read."""

from collections.abc import Iterable, Mapping

from .errors import UsageError
from .proxyjump import Route

DEFAULT_PORT = 22

_HOST_KEYS = ("host", "hostname", "address", "ip")
_USER_KEYS = ("username", "user")


def parse_inventory(data: object, where: str) -> dict[str, dict]:
    """Validate decoded inventory JSON and return {name: entry}.

    Accepts either {"devices": {...}} or a bare {name: entry} object. Names
    starting with "_" are dropped so the file can carry comments."""
    devices = data.get("devices", data) if isinstance(data, Mapping) else None
    if not isinstance(devices, Mapping):
        raise UsageError(f'ERROR: {where}: expected {{"devices": {{name: {{...}}}}}}')
    clean: dict[str, dict] = {}
    for name, entry in devices.items():
        if name.startswith("_"):
            continue
        if not isinstance(entry, Mapping):
            raise UsageError(f"ERROR: {where}: device {name!r} must be an object")
        clean[name] = dict(entry)
    return clean


def _first(entry: Mapping, keys: tuple[str, ...]):
    for key in keys:
        if entry.get(key):
            return entry[key]
    return None


def entry_host(entry: Mapping) -> str | None:
    return _first(entry, _HOST_KEYS)


def entry_user(entry: Mapping) -> str | None:
    return _first(entry, _USER_KEYS)


def describe_auth(entry: Mapping) -> str:
    if entry.get("password_env"):
        return f"env:${entry['password_env']}"
    if entry.get("password"):
        return "inventory-password"
    if entry.get("key_file") or entry.get("use_keys"):
        return "ssh-key"
    return "prompt/$IX_PASS"


def format_inventory(
    devices: Mapping[str, Mapping],
    where: str | None,
    looked_in: Iterable[str],
    routes: Mapping[str, Route | None] = {},
) -> str:
    """Render `--list`. `routes` carries what ssh_config says about each device's
    host (already looked up by the caller), keyed by device name; a device with
    no entry, or None, is shown as a literal address."""
    where = where or "(none found)"
    if not devices:
        # The skills read the location off this output instead of hardcoding a
        # path, so an empty inventory has to say where the file is looked for.
        candidates = "\n".join(f"    {cand}" for cand in looked_in)
        return (
            f"inventory: {where}\n"
            "  no devices configured.\n"
            "  create the file, or pass --host HOST --user USER explicitly.\n"
            f"  looked in ($IX_INVENTORY overrides):\n{candidates}\n"
            '  schema: {"devices": {"NAME": {"host": "...", "username": "...", '
            '"password_env": "IX_PASS_NAME"}}}'
        )
    width = max(len(n) for n in devices)
    lines = [f"inventory: {where}", "devices (no default; pass --device NAME):"]
    for name, entry in devices.items():
        host = entry_host(entry) or "?"
        user = entry_user(entry) or "?"
        port = entry.get("port", DEFAULT_PORT)
        route = ""
        found = routes.get(name)
        if found is not None:
            if found.hostname != host:
                real_port = entry.get("port") or found.port or DEFAULT_PORT
                route = f" -> {found.hostname}:{real_port}"
                user = user if user != "?" else (found.user or "?")
            if found.hops:
                route += " via " + " -> ".join(found.hops)
        # model is short and structured, so it rides on the device row; note is
        # free-form prose and keeps its own line.
        model = f"  model={entry['model']}" if entry.get("model") else ""
        lines.append(
            f"  {name:<{width}}  {user}@{host}:{port}{route}  auth={describe_auth(entry)}{model}"
        )
        if entry.get("note"):
            lines.append(f"  {'':<{width}}  note: {entry['note']}")
    return "\n".join(lines)
