"""A fake NEC IX behind a real SSH server (paramiko), for the integration test.

Just enough of IX OS for netmiko's nec_ix driver and ix-ssh: exec / config
prompts (`fakeix#` / `fakeix(config)#`), `svintr-config` / `configure` / `exit`,
`terminal length 0`, a few `show`s (running-config contains a `hostname` line on
purpose, to exercise the pinned-prompt read), `write memory`, and any other line
in config mode is recorded as applied. Input is echoed like a terminal would.

The same server also accepts `direct-tcpip` channels and forwards them, so it can
be its own ProxyJump host: password auth for the device user, public-key auth
for the jump user, as ix-ssh does it.
"""

import contextlib
import logging
import socket
import threading

import paramiko

# Server-side teardown diagnostics must not be captured as the CLI's stderr.
_server_log = logging.getLogger(__name__)
_server_log.addHandler(logging.NullHandler())
_server_log.propagate = False

DEVICE_USER = "admin"
JUMP_USER = "jump"
HOSTNAME = "fakeix"

SHOW_VERSION = [
    "NEC Portable Internetwork Core Operating System Software",
    "IX Series IX2215 (magellan-sec) Software, Version 10.11.19, RELEASE SOFTWARE",
]
RUNNING_CONFIG = [
    "! NEC Portable Internetwork Core Operating System Software",
    f"hostname {HOSTNAME}",
    "timezone +09 00",
    "!",
    "ip route default GigaEthernet0.0",
    "!",
]


class _Server(paramiko.ServerInterface):
    def __init__(self, password: str, jump_key: paramiko.PKey):
        self.password = password
        self.jump_key = jump_key
        self.forwards: dict[int, tuple[str, int]] = {}

    def get_allowed_auths(self, username):
        return "password,publickey"

    def check_auth_password(self, username, password):
        ok = username == DEVICE_USER and password == self.password
        return paramiko.AUTH_SUCCESSFUL if ok else paramiko.AUTH_FAILED

    def check_auth_publickey(self, username, key):
        ok = username == JUMP_USER and key == self.jump_key
        return paramiko.AUTH_SUCCESSFUL if ok else paramiko.AUTH_FAILED

    def check_channel_request(self, kind, chanid):
        if kind == "session":
            return paramiko.OPEN_SUCCEEDED
        return paramiko.OPEN_FAILED_ADMINISTRATIVELY_PROHIBITED

    def check_channel_direct_tcpip_request(self, chanid, origin, destination):
        self.forwards[chanid] = destination
        return paramiko.OPEN_SUCCEEDED

    def check_channel_pty_request(self, channel, term, width, height, pw, ph, modes):
        return True

    def check_channel_shell_request(self, channel):
        return True


class FakeIX:
    """Listens on 127.0.0.1:<port>. `applied` collects config lines, `saved`
    counts `write memory`, `sessions` counts shells opened."""

    def __init__(self, password: str = "secret"):
        self.password = password
        self.host_key = paramiko.RSAKey.generate(2048)
        self.jump_key = paramiko.RSAKey.generate(2048)
        self.applied: list[str] = []
        self.saved = 0
        self.sessions = 0
        # Tests can supply device-side failures without changing the client.
        self.responses: dict[str, list[str]] = {}
        self._sock = socket.socket()
        self._sock.setsockopt(socket.SOL_SOCKET, socket.SO_REUSEADDR, 1)
        self._sock.bind(("127.0.0.1", 0))
        self._sock.listen(8)
        self.port = self._sock.getsockname()[1]
        self._transports: list[paramiko.Transport] = []
        self._stop = False
        self._thread = threading.Thread(target=self._accept_loop, daemon=True)

    def start(self) -> "FakeIX":
        self._thread.start()
        return self

    def stop(self) -> None:
        self._stop = True
        for t in self._transports:
            t.close()
        self._sock.close()

    def _accept_loop(self) -> None:
        while not self._stop:
            try:
                client, _ = self._sock.accept()
            except OSError:
                return
            threading.Thread(target=self._serve, args=(client,), daemon=True).start()

    def _serve(self, client: socket.socket) -> None:
        transport = paramiko.Transport(client)
        transport.set_log_channel(__name__)
        self._transports.append(transport)
        transport.add_server_key(self.host_key)
        server = _Server(self.password, self.jump_key)
        try:
            transport.start_server(server=server)
        except EOFError:
            # A client can close a forwarded connection during teardown before
            # its SSH handshake finishes.
            transport.close()
            return
        while transport.is_active():
            chan = transport.accept(30)
            if chan is None:
                break
            dest = server.forwards.pop(chan.get_id(), None)
            target = self._forward if dest else self._shell
            threading.Thread(target=target, args=(chan, dest), daemon=True).start()

    def _forward(self, chan: paramiko.Channel, dest: tuple[str, int]) -> None:
        try:
            upstream = socket.create_connection(dest)
        except OSError:
            chan.close()
            return

        def pump(src_recv, dst_send, close):
            try:
                while True:
                    data = src_recv(4096)
                    if not data:
                        break
                    dst_send(data)
            except (OSError, EOFError):
                pass
            finally:
                with contextlib.suppress(OSError, EOFError):
                    close()

        threading.Thread(
            target=pump, args=(chan.recv, upstream.sendall, upstream.close), daemon=True
        ).start()
        pump(upstream.recv, chan.sendall, chan.close)

    # -- the device itself ------------------------------------------------ #
    def _shell(self, chan: paramiko.Channel, _dest=None) -> None:
        self.sessions += 1
        config = False

        def prompt() -> str:
            return f"{HOSTNAME}(config)#" if config else f"{HOSTNAME}#"

        def reply(lines: list[str]) -> None:
            chan.sendall("".join(f"{line}\r\n" for line in lines) + prompt())

        reply([])
        buf = b""
        while True:
            data = chan.recv(1024)
            if not data:
                break
            chan.sendall(data)  # terminal echo
            buf += data
            while b"\n" in buf:
                raw, buf = buf.split(b"\n", 1)
                line = raw.decode("utf-8", "replace").strip("\r ")
                if line == "":
                    reply([])
                elif line in ("svintr-config", "enable-config", "configure"):
                    config = True
                    reply([])
                elif line == "exit":
                    if not config:
                        chan.close()
                        return
                    config = False
                    reply([])
                elif line == "terminal length 0":
                    reply([])
                elif not config:
                    reply([f"% Command not found: {line}"])
                elif line in self.responses:
                    reply(self.responses[line])
                elif line == "show version":
                    reply(SHOW_VERSION)
                elif line == "show running-config":
                    reply(RUNNING_CONFIG)
                elif line.startswith("show "):
                    reply(["% Invalid input"])
                elif line == "write memory":
                    self.saved += 1
                    reply(["% Saving configuration... done."])
                else:
                    self.applied.append(line)
                    reply([])
