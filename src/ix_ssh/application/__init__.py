"""What one `ix-ssh` invocation does, from the parsed request to the last line of
output: list the inventory, or resolve a target and run shows / backup / config /
save on it in that order. Combines domain rules with infrastructure; knows
nothing about argparse or exit codes."""

from ..infrastructure import DEFAULT_SSH_CONFIG, INVENTORY_ENV
from .run import BACKUP_AUTO, Request, Session, execute, list_devices, prepare_target, run

__all__ = [
    "BACKUP_AUTO",
    "DEFAULT_SSH_CONFIG",
    "INVENTORY_ENV",
    "Request",
    "Session",
    "execute",
    "list_devices",
    "prepare_target",
    "run",
]
