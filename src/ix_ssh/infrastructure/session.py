"""The SSH session to the device: netmiko's nec_ix driver, with the ProxyJump
chain opened by paramiko here and handed to netmiko as a ready socket."""

import contextlib
import re
import time
from collections.abc import Iterator, Sequence

from ..domain import (
    Hop,
    Target,
    UsageError,
    check_command_output,
    check_config_line_output,
    config_stopped_at,
    validate_show_commands,
)
from ..domain.redaction import ConfigRedactor
from .deps import missing_dependency

READ_TIMEOUT = 120
CONNECT_TIMEOUT = 20
# IX-R starts each session 80 columns wide whatever width the SSH pty asked for,
# and folds the echo of a longer line. netmiko waits for the echo of every config
# line, never sees it whole, and gives up after READ_TIMEOUT. 512 is the top of
# IX-R's range (60-512, CRM 1.5a); the width lasts for the session only and is
# not part of running-config (IX keeps `terminal default-width` for that).
TERMINAL_WIDTH = 512


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
        # Preserve the current connection policy: reject changed known keys,
        # accept unknown keys. Tightening that behavior is a separate CLI policy.
        client.set_missing_host_key_policy(paramiko.AutoAddPolicy())  # noqa: S507
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

    def __init__(self, conn, jump_clients: list, read_error: type[Exception]):
        self._conn = conn
        self._jump_clients = jump_clients
        self._read_error = read_error
        self._redactor: ConfigRedactor | None = None

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

    def apply(self, lines: Sequence[str]) -> Iterator[str]:
        """Apply the lines in one config-mode visit, handing them to netmiko one at
        a time so a failure is pinned to its line by position. netmiko waits for
        each line's echo and prompt either way, so this adds no round trips.
        A config line may carry a secret (pre-shared key, password), and netmiko
        quotes the line in its own errors (error_pattern's ConfigInvalidException,
        and ReadTimeout's "Pattern not detected"); neither text is shown, and the
        exception is not chained, so the line cannot surface from a traceback."""
        conn = self._conn
        total = len(lines)
        redactor = ConfigRedactor(tuple(lines))
        self._redactor = redactor
        conn.config_mode()
        for number, line in enumerate(lines, 1):
            try:
                echo = conn.send_config_set(
                    [line],
                    enter_config_mode=False,
                    exit_config_mode=False,
                    read_timeout=READ_TIMEOUT,
                )
            except self._read_error:
                raise UsageError(
                    f"ERROR: {config_stopped_at(number, total)}: no echo or prompt within "
                    f"{READ_TIMEOUT}s; earlier lines may already be applied"
                ) from None
            check_config_line_output(number, total, echo)
            yield redactor.response(number, echo)
        conn.exit_config_mode()

    def save(self) -> str:
        output = self._conn.save_config()
        try:
            check_command_output("write memory", output)
        except UsageError as exc:
            if self._redactor is not None:
                raise UsageError(self._redactor.message(str(exc))) from None
            raise
        return self._redactor.message(output) if self._redactor is not None else output

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


def open_session(target: Target) -> NetmikoSession:  # noqa: C901
    # Complexity includes the nested Netmiko overrides; their signatures and
    # lifecycle must stay together with the selected config-entry command.
    try:
        from netmiko.exceptions import ReadException
        from netmiko.nec.nec_ix import NecIxSSH
    except ImportError as e:
        raise missing_dependency("netmiko") from e

    entry_command = "svintr-config" if target.force_config else "enable-config"

    class IxConnection(NecIxSSH):
        def session_preparation(self):
            # NecIxBase.session_preparation (netmiko 4.7), plus the terminal width
            # in the same config-mode visit as `terminal length 0`.
            self.set_base_prompt()
            self.config_mode()
            time.sleep(0.3 * self.global_delay_factor)
            self.clear_buffer()
            self.disable_paging(command=self.RETURN + "terminal length 0")
            self.widen_terminal()
            self.exit_config_mode()

        def widen_terminal(self):
            # A refusal is not an error: IX2000/IX3000's CRM gives no range for
            # the width, and a narrow terminal only hurts lines longer than it.
            prompt = rf"(?m:^{re.escape(self.base_prompt)}\(config\)#[ \t]*$)"
            self.send_command(
                f"terminal width {TERMINAL_WIDTH}",
                expect_string=prompt,
                read_timeout=READ_TIMEOUT,
            )

        def config_mode(self, config_command="", pattern="", re_flags=re.IGNORECASE):  # noqa: ARG002
            # Keep Netmiko's callback signature; IX uses its own command/prompt.
            # Netmiko invokes this during construction as well as show/config/save.
            # Its default implementation calls enable() with svintr-config.
            current = self.find_prompt()
            in_config = current.endswith(")#")
            in_global_config = current.endswith("(config)#")
            hostname = current.rsplit("(", 1)[0] if in_config else current.removesuffix("#")

            if in_global_config:
                self.base_prompt = hostname
                return ""

            command = "configure" if in_config else entry_command
            global_prompt = f"{hostname}(config)#"
            # Refusal returns the original prompt. Read it too, so a busy device
            # becomes a UsageError immediately rather than a prompt timeout.
            expect = rf"(?m:^(?:{re.escape(current)}|{re.escape(global_prompt)})[ \t]*$)"
            output = self.send_command(
                command,
                expect_string=expect,
                read_timeout=READ_TIMEOUT,
                strip_prompt=False,
                strip_command=False,
            )
            check_command_output(command, output)
            if not output.rstrip().endswith(global_prompt):
                raise UsageError(f"ERROR: {command!r} did not enter config mode")
            # A login in a submode (or a hostname change) invalidates Netmiko's
            # cached prompt. Later paging/config/save calls must use the hostname.
            self.base_prompt = hostname
            return output

        def exit_config_mode(self, exit_config="exit", pattern=""):  # noqa: ARG002
            # Keep Netmiko's callback signature; the IX prompt is pinned below.
            current = self.find_prompt()
            if not current.endswith(")#"):
                return ""
            hostname = current.rsplit("(", 1)[0]
            # If we might be in a submode, 'configure' drops us back to (config)# on IX.
            # But we avoid sending it unless needed.
            if not current.endswith("(config)#"):
                self.send_command("configure", expect_string=rf"{re.escape(hostname)}\(config\)#")

            global_prompt = f"{hostname}#"
            expect = rf"(?m:^{re.escape(global_prompt)}[ \t]*$)"
            output = self.send_command(
                exit_config,
                expect_string=expect,
                read_timeout=READ_TIMEOUT,
                strip_prompt=False,
                strip_command=False,
            )
            self.base_prompt = hostname
            return output

    params = {
        "device_type": "nec_ix_ssh",  # このスクリプトは NEC IX 専用
        "host": target.host,
        "port": target.port,
        "username": target.username,
        "password": target.password or "",
        "global_delay_factor": 0.2,
        "conn_timeout": CONNECT_TIMEOUT,
        "fast_cli": True,
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
        conn = IxConnection(**params)
    except Exception:
        # a hop or the device itself failed after part of the chain was up: tear it
        # down here, otherwise paramiko threads keep running and bury the real error.
        _close_all(jump_clients)
        raise
    return NetmikoSession(conn, jump_clients, ReadException)
