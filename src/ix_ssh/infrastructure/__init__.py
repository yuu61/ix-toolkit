"""Everything that touches the outside: the inventory file, ~/.ssh/config,
config files and backups on disk, and the SSH session itself (netmiko over
paramiko, with the ProxyJump chain opened here). Uses domain, never application
or cli."""

from .files import read_config_file, write_backup
from .inventory import INVENTORY_CANDIDATES, INVENTORY_ENV, inventory_path, read_inventory
from .session import NetmikoSession, open_session
from .ssh_config import DEFAULT_SSH_CONFIG, load_ssh_config, quiet_load_ssh_config

__all__ = [
    "DEFAULT_SSH_CONFIG",
    "INVENTORY_CANDIDATES",
    "INVENTORY_ENV",
    "NetmikoSession",
    "inventory_path",
    "load_ssh_config",
    "open_session",
    "quiet_load_ssh_config",
    "read_config_file",
    "read_inventory",
    "write_backup",
]
