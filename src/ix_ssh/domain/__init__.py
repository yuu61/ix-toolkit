"""Rules of the ix-ssh client that hold regardless of how anything is read or sent.

What an inventory entry means, which setting wins when several sources name it,
where a password may come from and in which order, how a ProxyJump chain flattens,
which commands count as `show`, and how a backup file is named. Nothing here
opens a file, a socket or imports netmiko / paramiko; infrastructure does that and
hands the results in as plain values.
"""

from .commands import default_backup_path, is_show_command, parse_config_lines
from .credentials import find_password, missing_password_message, resolve_password
from .errors import UsageError
from .inventory import (
    DEFAULT_PORT,
    entry_host,
    entry_user,
    parse_inventory,
)
from .proxyjump import (
    Hop,
    Route,
    SshConfigLookup,
    hop_specs,
    resolve_hop,
    resolve_route,
    split_hop,
)
from .target import Target, TargetRequest, resolve_target, select_entry, ssh_config_path

__all__ = [
    "DEFAULT_PORT",
    "Hop",
    "Route",
    "SshConfigLookup",
    "Target",
    "TargetRequest",
    "UsageError",
    "default_backup_path",
    "entry_host",
    "entry_user",
    "find_password",
    "hop_specs",
    "is_show_command",
    "missing_password_message",
    "parse_config_lines",
    "parse_inventory",
    "resolve_hop",
    "resolve_password",
    "resolve_route",
    "resolve_target",
    "select_entry",
    "split_hop",
    "ssh_config_path",
]
