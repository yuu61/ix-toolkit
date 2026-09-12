"""Command line: argument parsing, the final error message and the exit code.
Calls application only."""

import argparse
import sys

from ..application import BACKUP_AUTO, DEFAULT_SSH_CONFIG, INVENTORY_ENV, Request, run
from ..domain import DEFAULT_PORT, TargetRequest, UsageError


def parse_args(argv: list[str]) -> argparse.Namespace:
    p = argparse.ArgumentParser(
        prog="ix-ssh",
        description="NEC IX SSH helper (netmiko nec_ix). Target via --device/--host.",
        formatter_class=argparse.RawDescriptionHelpFormatter,
    )
    p.add_argument("shows", nargs="*", help='show commands, e.g. "show ip route"')

    tgt = p.add_argument_group("target")
    tgt.add_argument("-d", "--device", metavar="NAME", help="inventory device name ($IX_DEVICE)")
    tgt.add_argument("--host", metavar="HOST", help="host/IP, bypassing the inventory ($IX_HOST)")
    tgt.add_argument(
        "--user", "--username", dest="user", metavar="USER", help="login user ($IX_USER)"
    )
    tgt.add_argument("--port", metavar="PORT", help=f"SSH port ($IX_PORT, default {DEFAULT_PORT})")
    tgt.add_argument("--password-env", metavar="VAR", help="read the password from $VAR")
    tgt.add_argument(
        "--ask-password",
        action="store_true",
        help="prompt for the password when no other source has it (interactive only)",
    )
    tgt.add_argument("--key-file", metavar="PATH", help="private key for SSH key auth")
    tgt.add_argument(
        "--ssh-config",
        metavar="PATH",
        help=f"ssh_config used to resolve aliases/ProxyJump (default {DEFAULT_SSH_CONFIG})",
    )
    tgt.add_argument(
        "--no-ssh-config",
        action="store_true",
        help="treat host as a literal address; skip ssh_config alias/ProxyJump resolution",
    )
    tgt.add_argument("--inventory", metavar="PATH", help=f"inventory JSON (${INVENTORY_ENV})")
    tgt.add_argument("--list", action="store_true", help="list inventory devices and exit")

    p.add_argument(
        "--config",
        action="append",
        default=[],
        metavar="LINE",
        help="config line to apply (repeatable)",
    )
    p.add_argument(
        "--config-file",
        metavar="PATH",
        help="file with config lines (one per line; # comments ok)",
    )
    p.add_argument(
        "--backup",
        nargs="?",
        const=BACKUP_AUTO,
        default=None,
        metavar="PATH",
        help="save running-config to PATH (default backups/<device>-<timestamp>.conf)",
    )
    p.add_argument("--save", action="store_true", help="write memory after config")
    p.add_argument(
        "--force-config",
        action="store_true",
        help="enter with svintr-config, displacing another config user (default: enable-config)",
    )
    p.add_argument(
        "--raw",
        action="store_true",
        help="suppress ===== headers (clean output for redirection)",
    )
    return p.parse_args(argv)


def request_from(args: argparse.Namespace) -> Request:
    return Request(
        target=TargetRequest(
            device=args.device,
            host=args.host,
            user=args.user,
            port=args.port,
            password_env=args.password_env,
            ask_password=args.ask_password,
            key_file=args.key_file,
            ssh_config=args.ssh_config,
            no_ssh_config=args.no_ssh_config,
            force_config=args.force_config,
        ),
        shows=tuple(args.shows),
        config_lines=tuple(args.config),
        config_file=args.config_file,
        backup=args.backup,
        save=args.save,
        raw=args.raw,
        list=args.list,
        inventory=args.inventory,
    )


def main(argv: list[str] | None = None) -> int:
    """Console-script entry point (see [project.scripts] in pyproject.toml)."""
    args = parse_args(sys.argv[1:] if argv is None else argv)
    try:
        run(request_from(args))
    except UsageError as exc:
        print(exc, file=sys.stderr)
        return 1
    return 0
