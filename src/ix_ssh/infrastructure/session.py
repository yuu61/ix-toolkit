"""The SSH session to the device: netmiko's nec_ix driver, with the ProxyJump
chain opened by paramiko here and handed to netmiko as a ready socket."""

import contextlib
import re
from collections.abc import Sequence

from ..domain import (
    COMMAND_ERROR_PATTERN,
    Hop,
    Target,
    UsageError,
    check_command_output,
    validate_show_commands,
)
from .deps import missing_dependency

READ_TIMEOUT = 120
CONNECT_TIMEOUT = 20


def open_jump_socket(hops: Sequence[Hop], dest_host: str, dest_port: int, clients: list):
    """Chain through `hops` with paramiko and return a direct-tcpip channel to the
    device, usable as netmiko's `sock`. Unlike netmiko's own ProxyJump handling
    this spawns no `ssh` ProxyCommand, which cannot work on Windows (paramiko's
    ProxyCommand.recv() selects on a pipe -> WinError 10038). Jump hosts
    authenticate with the IdentityFile from ssh_config (or the agent); only the
    device itself uses the inventory password. The opened clients are appended
    to `clients` so the caller can close them after the device session."""
    import paramiko

    sock = None
    for index, hop in enumerate(hops):
        client = paramiko.SSHClient()
        client.load_system_host_keys()
        client.set_missing_host_key_policy(paramiko.AutoAddPolicy())
        client.connect(
            hostname=hop.host,
            port=hop.port,
            username=hop.user,
            key_filename=list(hop.keys) or None,
            sock=sock,
            timeout=CONNECT_TIMEOUT,
        )
        clients.append(client)
        if index + 1 < len(hops):
            nxt = hops[index + 1]
            target = (nxt.host, nxt.port)
        else:
            target = (dest_host, dest_port)
        sock = client.get_transport().open_channel("direct-tcpip", target, ("127.0.0.1", 0))
    return sock


class NetmikoSession:
    """One open connection. `show` runs inside config mode because NEC IX only
    accepts running-config and most feature shows there; `apply` and `save` map
    onto the nec_ix driver's config/save handling."""

    def __init__(self, conn, jump_clients: list, config_error: type[Exception]):
        self._conn = conn
        self._jump_clients = jump_clients
        self._config_error = config_error

    def show(self, cmd: str) -> str:
        """Run a single `show ...` inside config mode. expect_string is pinned to
        the full `<hostname>(config)#` prompt so output that embeds the hostname
        (e.g. the `hostname` line in running-config) cannot end the read early."""
        validate_show_commands((cmd,))
        conn = self._conn
        conn.config_mode()
        expect = re.escape(conn.find_prompt())  # e.g. '<hostname>(config)#'
        try:
            output = conn.send_command(cmd, expect_string=expect, read_timeout=READ_TIMEOUT)
        finally:
            conn.exit_config_mode()
        return check_command_output(cmd, output)

    def apply(self, lines: Sequence[str]) -> str:
        # send_config_set enters config mode, applies the lines, then exits.
        try:
            return self._conn.send_config_set(
                list(lines), read_timeout=READ_TIMEOUT, error_pattern=COMMAND_ERROR_PATTERN
            )
        except self._config_error as exc:
            raise UsageError(
                f"ERROR: configuration stopped; earlier lines may already be applied: {exc}"
            ) from exc

    def save(self) -> str:
        return check_command_output("write memory", self._conn.save_config())

    def close(self) -> None:
        try:
            self._conn.disconnect()
        finally:
            _close_all(self._jump_clients)


def _close_all(clients: list) -> None:
    while clients:
        # 後始末なので失敗しても続ける。ここで例外を上げると、
        # 本来報告すべきデバイス側のエラーを覆い隠してしまう。
        with contextlib.suppress(Exception):
            clients.pop().close()


def open_session(target: Target) -> NetmikoSession:
    try:
        from netmiko import ConnectHandler
        from netmiko.exceptions import ConfigInvalidException
    except ImportError as e:
        raise missing_dependency("netmiko") from e

    params = {
        "device_type": "nec_ix_ssh",  # このスクリプトは NEC IX 専用
        "host": target.host,
        "port": target.port,
        "username": target.username,
        "password": target.password or "",
        "conn_timeout": CONNECT_TIMEOUT,
        "fast_cli": False,
    }
    if target.use_keys:
        params["use_keys"] = True
    if target.key_file:
        params["key_file"] = target.key_file
    jump_clients: list = []
    try:
        if target.hops:
            # Own the jump chain here and hand netmiko a ready socket. ssh_config_file
            # is deliberately NOT passed on: netmiko would turn ProxyJump into a
            # paramiko ProxyCommand, which is broken on Windows.
            params["sock"] = open_jump_socket(target.hops, target.host, target.port, jump_clients)
        conn = ConnectHandler(**params)
    except Exception:
        # a hop or the device itself failed after part of the chain was up: tear it
        # down here, otherwise paramiko threads keep running and bury the real error.
        _close_all(jump_clients)
        raise
    return NetmikoSession(conn, jump_clients, ConfigInvalidException)
