"""The inventory: a mapping of device name -> entry, and how an entry is read."""

from collections.abc import Mapping

from .errors import UsageError

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
