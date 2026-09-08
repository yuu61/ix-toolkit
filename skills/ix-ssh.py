#!/usr/bin/env python3
"""Run commands on a NEC IX router (IX OS 10.x) over SSH using netmiko's nec_ix driver.

Device-agnostic: no host, credential or model is baked into this file. Targets are
resolved from an inventory file (default ~/.claude/ix-devices.json) via --device, or
given inline with --host/--user. The global ix-* skills call this by absolute path.

Usage:
    # list the configured devices (never prints passwords):
    python ix-ssh.py --list

    # show commands. NEC IX runs running-config and most feature shows only inside
    # "config/enable" mode, so every show is executed there. Paging is auto-off.
    python ix-ssh.py --device home-ix3315 "show version"
    python ix-ssh.py -d home-ix3315 "show ip route" "show interfaces"

    # ad-hoc target without an inventory entry:
    python ix-ssh.py --host 192.0.2.1 --user admin "show running-config"

    # "host" may be a ~/.ssh/config alias; ProxyJump is followed automatically:
    python ix-ssh.py --host room1 --user admin "show version"

    # config changes (DESTRUCTIVE - confirm before running). Each --config is one
    # line; multiple are applied in a single config session.
    python ix-ssh.py -d home-ix3315 \
        --config "ip route default GigaEthernet1.0" \
        --config "logging buffered 100" \
        --save

    # apply a batch of config lines from a file (one per line; # comments allowed):
    python ix-ssh.py -d home-ix3315 --config-file changes.ix --save

    # persist running-config to startup (write memory):
    python ix-ssh.py -d home-ix3315 --save

    # back up running-config to a file (parent dirs auto-created; default name is
    # backups/<device>-<YYYYMMDD-HHMMSS>.conf):
    python ix-ssh.py -d home-ix3315 --backup
    python ix-ssh.py -d home-ix3315 --backup backups/before-change.conf

    # clean output without "===== cmd =====" headers (for redirection):
    python ix-ssh.py -d home-ix3315 --raw "show running-config" > ix.conf

Inventory (JSON, default ~/.claude/ix-devices.json, override with $IX_INVENTORY or
--inventory). Keys starting with "_" are ignored, so they can hold comments:

    {
      "devices": {
        "home-ix3315": {
          "host": "192.0.2.1",
          "username": "admin",
          "password_env": "IX_PASS_HOME",
          "port": 22,
          "note": "free-form; shown by --list"
        }
      }
    }

    Per-device keys: host (or hostname), username (or user), port,
    password, password_env, key_file, use_keys, ssh_config_file, note.
    There is deliberately NO default device: --device or --host is always required,
    so a config push can never land on the wrong box by omission.

ssh_config aliases and ProxyJump:
    "host" can be a plain address or an alias from ~/.ssh/config (--ssh-config or a
    per-device "ssh_config_file" picks another file; --no-ssh-config turns the whole
    mechanism off). The alias is resolved here, not by netmiko: `Include` lines are
    expanded first (paramiko ignores them and would resolve an included alias to
    itself), then HostName / Port / User / ProxyJump are applied. A ProxyJump chain
    - including a jump host that itself has one - is opened with paramiko and handed
    to netmiko as `sock`, so no `ssh` ProxyCommand is spawned. That matters: netmiko
    would build one, and paramiko's ProxyCommand.recv() selects on a pipe, which
    fails on Windows (WinError 10038). Jump hosts authenticate with the IdentityFile
    from ssh_config (or the agent); only the device itself uses the inventory
    password. --list shows the resolved address and the jump chain.

Resolution order (first wins) for each setting:
    1. command-line flag        (--host / --user / --port / ...)
    2. environment variable     ($IX_HOST / $IX_USER / $IX_PORT / $IX_DEVICE)
    3. the inventory entry selected by --device
    4. built-in default         (port 22)

Password resolution order (the device's own credentials beat the global $IX_PASS,
so a leftover variable can never be sent to the wrong box):
    1. --password-env NAME  -> $NAME
    2. the entry's "password" -- plaintext in the inventory; this is the normal
       way to store credentials here. The inventory is a local, machine-only file
       driven by the ix-* skills, so keep it simple and write the password in.
    3. the entry's "password_env" -> that variable
    4. $IX_PASS (fallback, mainly for ad-hoc --host runs)
    5. key authentication if key_file / use_keys is set (no password)
    6. an interactive getpass prompt, only with --ask-password (never automatic:
       it would hang a non-interactive run)

NEC IX specifics handled by the nec_ix netmiko driver:
    * enable mode == configuration mode; entered via `svintr-config` / `configure`
      (prompt becomes `<hostname>(config)#`). Most commands need it ("en" first).
    * paging disabled on connect via `terminal length 0`.
    * save == `write memory`.
    * `show running-config` / `startup-config` / `tech-support` / `ipsec` / `ike` /
      `logging` / `ntp` / `vrrp` ... are only valid inside config mode, so all
      shows run there. The EXEC-mode `show` set is a strict subset.

Requires: pip install netmiko  (>=4.6 for the nec_ix driver; verified on 4.7.0)
"""

import argparse
import contextlib
import getpass
import glob
import io
import json
import os
import re
import sys
from datetime import datetime, timezone
from pathlib import Path

DEFAULT_PORT = 22
READ_TIMEOUT = 120

DEFAULT_SSH_CONFIG = str(Path.home() / ".ssh" / "config")

INVENTORY_ENV = "IX_INVENTORY"
INVENTORY_CANDIDATES = (
    Path.home() / ".claude" / "ix-devices.json",
    Path(__file__).resolve().parent / "ix-devices.json",
)

_HOST_KEYS = ("host", "hostname", "address", "ip")
_USER_KEYS = ("username", "user")


# --------------------------------------------------------------------------- #
# inventory
# --------------------------------------------------------------------------- #
def inventory_path() -> Path | None:
    """Path of the inventory to use, or None when no candidate exists."""
    env = os.environ.get(INVENTORY_ENV)
    if env:
        return Path(env).expanduser()
    for cand in INVENTORY_CANDIDATES:
        if cand.is_file():
            return cand
    return None


def load_inventory() -> tuple[dict, Path | None]:
    path = inventory_path()
    if path is None:
        return {}, None
    if not path.is_file():
        sys.exit(f"ERROR: inventory file not found: {path}")
    try:
        data = json.loads(path.read_text(encoding="utf-8"))
    except json.JSONDecodeError as exc:
        sys.exit(f"ERROR: invalid JSON in {path}: {exc}")
    devices = data.get("devices", data) if isinstance(data, dict) else None
    if not isinstance(devices, dict):
        sys.exit(f'ERROR: {path}: expected {{"devices": {{name: {{...}}}}}}')
    clean = {}
    for name, entry in devices.items():
        if name.startswith("_"):
            continue
        if not isinstance(entry, dict):
            sys.exit(f"ERROR: {path}: device {name!r} must be an object")
        clean[name] = entry
    return clean, path


def _first(entry: dict, keys: tuple[str, ...]):
    for key in keys:
        if entry.get(key):
            return entry[key]
    return None


def describe_auth(entry: dict) -> str:
    if entry.get("password_env"):
        return f"env:${entry['password_env']}"
    if entry.get("password"):
        return "inventory-password"
    if entry.get("key_file") or entry.get("use_keys"):
        return "ssh-key"
    return "prompt/$IX_PASS"


def _quiet_ssh_config(path: str | None):
    """load_ssh_config, but never fatal: --list must work without paramiko."""
    if not path:
        return None
    try:
        return load_ssh_config(Path(path).expanduser())
    except Exception:  # noqa: BLE001 - listing must not depend on ssh_config health
        return None


def format_inventory(devices: dict, path: Path | None) -> str:
    where = str(path) if path else "(none found)"
    if not devices:
        return (
            f"inventory: {where}\n"
            "  no devices configured.\n"
            "  create the file, or pass --host HOST --user USER explicitly.\n"
            '  schema: {"devices": {"NAME": {"host": "...", "username": "...", '
            '"password_env": "IX_PASS_NAME"}}}'
        )
    width = max(len(n) for n in devices)
    lines = [f"inventory: {where}", "devices (no default; pass --device NAME):"]
    for name, entry in devices.items():
        host = _first(entry, _HOST_KEYS) or "?"
        user = _first(entry, _USER_KEYS) or "?"
        port = entry.get("port", DEFAULT_PORT)
        route = ""
        cfg = _quiet_ssh_config(entry.get("ssh_config_file") or DEFAULT_SSH_CONFIG)
        if cfg is not None:
            looked_up = cfg.lookup(host)
            real = looked_up.get("hostname", host)
            real_port = entry.get("port") or looked_up.get("port", DEFAULT_PORT)
            if real != host:
                route = f" -> {real}:{real_port}"
                user = user if user != "?" else looked_up.get("user", "?")
            hops = hop_specs(cfg, host)
            if hops:
                route += " via " + " -> ".join(hops)
        lines.append(
            f"  {name:<{width}}  {user}@{host}:{port}{route}  auth={describe_auth(entry)}"
        )
        if entry.get("note"):
            lines.append(f"  {'':<{width}}  note: {entry['note']}")
    return "\n".join(lines)


# --------------------------------------------------------------------------- #
# ssh_config: alias resolution and ProxyJump
# --------------------------------------------------------------------------- #
def _read_ssh_config_text(path: Path, depth: int = 0) -> str:
    """Read an ssh_config, expanding `Include` in place (paramiko does not do it:
    it parses `include` as an ordinary keyword, so aliases living in an included
    file would silently resolve to themselves)."""
    if depth > 8 or not path.is_file():
        return ""
    out = []
    for line in path.read_text(encoding="utf-8", errors="replace").splitlines():
        stripped = line.strip()
        if stripped.lower().startswith("include "):
            for pattern in stripped.split(None, 1)[1].split():
                pattern = os.path.expanduser(pattern)
                if not os.path.isabs(pattern):
                    pattern = str(path.parent / pattern)
                for inc in sorted(map(Path, glob.glob(pattern))):
                    out.append(_read_ssh_config_text(inc, depth + 1))
        else:
            out.append(line)
    return "\n".join(out)


def load_ssh_config(path: Path):
    import paramiko

    cfg = paramiko.SSHConfig()
    text = _read_ssh_config_text(path)
    if not text:
        return None
    cfg.parse(io.StringIO(text))
    return cfg


def _split_hop(spec: str) -> tuple[str | None, str, int | None]:
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


def hop_specs(cfg, host: str, _seen: set | None = None) -> list[str]:
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
        _, hop_host, _ = _split_hop(spec)
        hops.extend(hop_specs(cfg, hop_host, _seen))
        hops.append(spec.strip())
    return hops


def resolve_hop(cfg, spec: str) -> dict:
    """Turn one hop spec into concrete connection settings via ssh_config."""
    user, host, port = _split_hop(spec)
    entry = cfg.lookup(host) if cfg else {}
    keys = entry.get("identityfile") or []
    if isinstance(keys, str):
        keys = [keys]
    return {
        "host": entry.get("hostname", host),
        "port": port or int(entry.get("port", 22)),
        "user": user or entry.get("user"),
        "keys": [str(Path(k).expanduser()) for k in keys] or None,
    }


def open_jump_socket(cfg, hops: list[str], dest_host: str, dest_port: int, clients: list):
    """Chain through `hops` with paramiko and return a direct-tcpip channel to the
    device, usable as netmiko's `sock`. Unlike netmiko's own ProxyJump handling
    this spawns no `ssh` ProxyCommand, which cannot work on Windows (paramiko's
    ProxyCommand.recv() selects on a pipe -> WinError 10038)."""
    import paramiko

    sock = None
    for index, spec in enumerate(hops):
        hop = resolve_hop(cfg, spec)
        client = paramiko.SSHClient()
        client.load_system_host_keys()
        client.set_missing_host_key_policy(paramiko.AutoAddPolicy())
        client.connect(
            hostname=hop["host"],
            port=hop["port"],
            username=hop["user"],
            key_filename=hop["keys"],
            sock=sock,
            timeout=20,
        )
        clients.append(client)
        if index + 1 < len(hops):
            nxt = resolve_hop(cfg, hops[index + 1])
            target = (nxt["host"], nxt["port"])
        else:
            target = (dest_host, dest_port)
        sock = client.get_transport().open_channel("direct-tcpip", target, ("127.0.0.1", 0))
    return sock


# --------------------------------------------------------------------------- #
# target resolution
# --------------------------------------------------------------------------- #
class Target:
    def __init__(self, name, host, username, port, password,
                 key_file, use_keys, ssh_config_file, ssh_cfg=None, hops=(), alias=None):
        self.name = name
        self.host = host
        self.username = username
        self.port = port
        self.password = password
        self.key_file = key_file
        self.use_keys = use_keys
        self.ssh_config_file = ssh_config_file
        self.ssh_cfg = ssh_cfg
        self.hops = list(hops)
        self.alias = alias  # the ssh_config alias the host came from, if any

    @property
    def label(self) -> str:
        return self.name or self.alias or self.host

    def banner(self) -> str:
        where = f"{self.username}@{self.host}:{self.port}"
        if self.alias and self.alias != self.host:
            where += f" [{self.alias}]"
        if self.hops:
            where += " via " + " -> ".join(self.hops)
        return f"# target: {self.label} ({where})"

    def slug(self) -> str:
        return re.sub(r"[^A-Za-z0-9._-]", "_", self.label)


def resolve_password(args, entry: dict, label: str, use_keys: bool) -> str | None:
    if args.password_env:
        value = os.environ.get(args.password_env)
        if not value:
            sys.exit(f"ERROR: ${args.password_env} is unset or empty")
        return value
    # The device's own credentials win over the global $IX_PASS: with several
    # devices in the inventory, a leftover $IX_PASS must never be sent to a box
    # that carries its own password.
    if entry.get("password"):
        return entry["password"]
    env_name = entry.get("password_env")
    if env_name and os.environ.get(env_name):
        return os.environ[env_name]
    if os.environ.get("IX_PASS"):
        return os.environ["IX_PASS"]
    if use_keys:
        return None
    if args.ask_password:
        # Explicit opt-in: never prompt on its own. stdin.isatty() is not a usable
        # interactivity test here (native Windows python reports True even when
        # stdin is redirected), so an automatic prompt would hang the skill.
        return getpass.getpass(f"password for {label}: ")
    hint = f"${env_name}" if env_name else "$IX_PASS"
    sys.exit(
        f"ERROR: no password available for {label}. set {hint}, add "
        '"password"/"password_env" to the inventory entry, use key auth (key_file), '
        "or pass --ask-password to be prompted."
    )


def resolve_target(args) -> Target:
    devices, inv_path = load_inventory()
    name = args.device or os.environ.get("IX_DEVICE")
    host = args.host or os.environ.get("IX_HOST")

    entry: dict = {}
    if name:
        if name not in devices:
            sys.exit(
                f"ERROR: unknown device {name!r}.\n" + format_inventory(devices, inv_path)
            )
        entry = devices[name]
    elif not host:
        sys.exit(
            "ERROR: no target selected. pass --device NAME (or --host HOST --user USER).\n"
            + format_inventory(devices, inv_path)
        )

    host = host or _first(entry, _HOST_KEYS)
    username = args.user or os.environ.get("IX_USER") or _first(entry, _USER_KEYS)
    port = args.port or os.environ.get("IX_PORT") or entry.get("port")
    key_file = args.key_file or entry.get("key_file")
    ssh_config_file = args.ssh_config or entry.get("ssh_config_file") or DEFAULT_SSH_CONFIG

    if not host:
        sys.exit(f'ERROR: no host for {name!r} (add "host" to the inventory entry)')

    # "host" may be an ssh_config alias: resolve hostname/port/user through it and
    # collect the ProxyJump chain, so an entry can be just {"host": "room1"}.
    alias = host
    ssh_cfg = None
    hops: list[str] = []
    if ssh_config_file and not args.no_ssh_config:
        ssh_cfg = load_ssh_config(Path(ssh_config_file).expanduser())
    if ssh_cfg is not None:
        looked_up = ssh_cfg.lookup(host)
        host = looked_up.get("hostname", host)
        port = port or looked_up.get("port")
        username = username or looked_up.get("user")
        hops = hop_specs(ssh_cfg, alias)
    port = port or DEFAULT_PORT

    if not username:
        sys.exit(
            f"ERROR: no username for {name or host} "
            '(add "username" to the inventory entry, or pass --user)'
        )
    try:
        port = int(port)
    except (TypeError, ValueError):
        sys.exit(f"ERROR: invalid port: {port!r}")

    use_keys = bool(key_file) or bool(entry.get("use_keys"))
    password = resolve_password(args, entry, name or host, use_keys)
    return Target(
        name=name,
        host=host,
        username=username,
        port=port,
        password=password,
        key_file=str(Path(key_file).expanduser()) if key_file else None,
        use_keys=use_keys,
        ssh_config_file=str(Path(ssh_config_file).expanduser()) if ssh_config_file else None,
        ssh_cfg=ssh_cfg,
        hops=hops,
        alias=alias,
    )


# ssh clients holding the ProxyJump chain open; closed after the device session.
_JUMP_CLIENTS: list = []


def connect(target: Target):
    from netmiko import ConnectHandler  # imported late so --list/--help work without it

    params = {
        "device_type": "nec_ix_ssh",  # このスクリプトは NEC IX 専用
        "host": target.host,
        "port": target.port,
        "username": target.username,
        "password": target.password or "",
        "conn_timeout": 20,
        "fast_cli": False,
    }
    if target.use_keys:
        params["use_keys"] = True
    if target.key_file:
        params["key_file"] = target.key_file
    if target.hops:
        # Own the jump chain here and hand netmiko a ready socket. ssh_config_file
        # is deliberately NOT passed on: netmiko would turn ProxyJump into a
        # paramiko ProxyCommand, which is broken on Windows.
        params["sock"] = open_jump_socket(
            target.ssh_cfg, target.hops, target.host, target.port, _JUMP_CLIENTS
        )
    return ConnectHandler(**params)


def close_jump_clients() -> None:
    while _JUMP_CLIENTS:
        # 後始末なので失敗しても続ける。ここで例外を上げると、
        # 本来報告すべきデバイス側のエラーを覆い隠してしまう。
        with contextlib.suppress(Exception):
            _JUMP_CLIENTS.pop().close()


# --------------------------------------------------------------------------- #
# operations
# --------------------------------------------------------------------------- #
def header(text: str, raw: bool) -> None:
    if not raw:
        print(f"===== {text} =====")


def _config_show(conn, cmd: str) -> str:
    """Run a single `show ...` inside config mode (NEC IX needs it for
    running-config and most feature shows). expect_string is pinned to the full
    `<hostname>(config)#` prompt so output that embeds the hostname (e.g. the
    `hostname` line in running-config) cannot terminate the read early."""
    conn.config_mode()
    expect = re.escape(conn.find_prompt())  # e.g. '<hostname>(config)#'
    try:
        return conn.send_command(cmd, expect_string=expect, read_timeout=READ_TIMEOUT)
    finally:
        conn.exit_config_mode()


def run_shows(conn, commands: list[str], raw: bool) -> None:
    for cmd in commands:
        if not cmd.strip().lower().startswith("show"):
            print(
                f"[SKIP] refusing non-show command in show mode: {cmd!r}",
                file=sys.stderr,
            )
            continue
        header(cmd, raw)
        print(_config_show(conn, cmd))
        if not raw:
            print()


def do_backup(conn, target: Target, path: str | None) -> None:
    out = _config_show(conn, "show running-config")
    if path is None:
        # 退避ファイル名は運用者が読む現地時刻。UTC に寄せず tz だけ付ける。
        ts = datetime.now(timezone.utc).astimezone().strftime("%Y%m%d-%H%M%S")
        path = str(Path("backups") / f"{target.slug()}-{ts}.conf")
    dest = Path(path)
    dest.parent.mkdir(parents=True, exist_ok=True)
    dest.write_text(out.rstrip("\n") + "\n", encoding="utf-8")
    print(
        f"[OK] running-config of {target.label} saved to {dest} "
        f"({len(out.splitlines())} lines)"
    )


def apply_config(conn, lines: list[str], raw: bool) -> None:
    header("config", raw)
    # send_config_set enters config mode, applies the lines, then exits.
    out = conn.send_config_set(lines, read_timeout=READ_TIMEOUT)
    print(out)
    if not raw:
        print()


def write_memory(conn, raw: bool) -> None:
    header("write memory", raw)
    out = conn.save_config()  # nec_ix: config_mode() + `write memory`
    print(out)
    if not raw:
        print()


def load_config_file(path: str) -> list[str]:
    lines: list[str] = []
    for raw in Path(path).read_text(encoding="utf-8").splitlines():
        s = raw.strip()
        if not s or s.startswith("#"):
            continue
        lines.append(raw.rstrip())
    return lines


# Sentinel so argparse can tell "--backup omitted" from "--backup with no path".
BACKUP_AUTO = "\x00auto"


def parse_args(argv: list[str]) -> argparse.Namespace:
    p = argparse.ArgumentParser(
        description="NEC IX SSH helper (netmiko nec_ix). Target via --device/--host.",
        formatter_class=argparse.RawDescriptionHelpFormatter,
    )
    p.add_argument("shows", nargs="*", help='show commands, e.g. "show ip route"')

    tgt = p.add_argument_group("target")
    tgt.add_argument(
        "-d", "--device", metavar="NAME", help="inventory device name ($IX_DEVICE)"
    )
    tgt.add_argument(
        "--host", metavar="HOST", help="host/IP, bypassing the inventory ($IX_HOST)"
    )
    tgt.add_argument(
        "--user", "--username", dest="user", metavar="USER", help="login user ($IX_USER)"
    )
    tgt.add_argument(
        "--port", metavar="PORT", help=f"SSH port ($IX_PORT, default {DEFAULT_PORT})"
    )
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
        "--raw",
        action="store_true",
        help="suppress ===== headers (clean output for redirection)",
    )
    return p.parse_args(argv)


def main(argv: list[str]) -> None:
    args = parse_args(argv)
    if args.inventory:
        os.environ[INVENTORY_ENV] = args.inventory

    if args.list:
        devices, path = load_inventory()
        print(format_inventory(devices, path))
        return

    config_lines = list(args.config)
    if args.config_file:
        config_lines.extend(load_config_file(args.config_file))

    if not args.shows and not config_lines and not args.save and args.backup is None:
        sys.exit(
            "ERROR: nothing to do (give show commands, --backup, --config, --save, or --list)"
        )

    target = resolve_target(args)
    if not args.raw:
        print(target.banner(), file=sys.stderr)

    try:
        conn = connect(target)
    except Exception:
        # device auth/connect failed after the jump chain was up: tear it down here,
        # otherwise paramiko threads keep running and bury the real error.
        close_jump_clients()
        raise
    try:
        if args.shows:
            run_shows(conn, args.shows, args.raw)
        if args.backup is not None:
            do_backup(conn, target, None if args.backup == BACKUP_AUTO else args.backup)
        if config_lines:
            apply_config(conn, config_lines, args.raw)
        if args.save:
            write_memory(conn, args.raw)
    finally:
        conn.disconnect()
        close_jump_clients()


if __name__ == "__main__":
    main(sys.argv[1:])
