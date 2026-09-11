"""Run commands on a NEC IX router (IX OS 10.x) over SSH using netmiko's nec_ix driver.

Device-agnostic: no host, credential or model is baked into this package. Targets are
resolved from an inventory file (default ~/.ix-toolkit/devices.json) via --device, or
given inline with --host/--user.

Installed as the "ix-ssh" console script (see pyproject.toml) by
`uv tool install -e <clone>`; the ix-* skills call it from PATH. The package
is layered like manualbook (internal/manualbook/): domain holds the rules
(inventory, target resolution, credentials, ProxyJump, commands) and touches no
I/O; infrastructure reads the inventory and ssh_config, writes backups and owns
the netmiko/paramiko session; application runs one invocation end to end; cli
parses arguments and turns errors into an exit code.

Usage:
    # list the configured devices (never prints passwords):
    ix-ssh --list

    # show commands. NEC IX runs running-config and most feature shows only inside
    # "config/enable" mode, so every show is executed there. Paging is auto-off.
    ix-ssh --device home-ix3315 "show version"
    ix-ssh -d home-ix3315 "show ip route" "show interfaces"

    # ad-hoc target without an inventory entry:
    ix-ssh --host 192.0.2.1 --user admin "show running-config"

    # "host" may be a ~/.ssh/config alias; ProxyJump is followed automatically:
    ix-ssh --host room1 --user admin "show version"

    # config changes (DESTRUCTIVE - confirm before running). Each --config is one
    # line; multiple are applied in a single config session.
    ix-ssh -d home-ix3315 \\
        --config "ip route default GigaEthernet1.0" \\
        --config "logging buffered 100" \\
        --save

    # apply a batch of config lines from a file (one per line; # comments allowed):
    ix-ssh -d home-ix3315 --config-file changes.ix --save

    # persist running-config to startup (write memory):
    ix-ssh -d home-ix3315 --save

    # back up running-config to a file (parent dirs auto-created; default name is
    # backups/<device>-<YYYYMMDD-HHMMSS>.conf):
    ix-ssh -d home-ix3315 --backup
    ix-ssh -d home-ix3315 --backup backups/before-change.conf

    # clean output without "===== cmd =====" headers (for redirection):
    ix-ssh -d home-ix3315 --raw "show running-config" > ix.conf

Inventory (JSON, default ~/.ix-toolkit/devices.json; ~/.claude/ix-devices.json is
still read for setups that predate the agent-neutral path. Override with
$IX_INVENTORY or --inventory). Keys starting with "_" are ignored, so they can
hold comments:

    {
      "devices": {
        "home-ix3315": {
          "host": "192.0.2.1",
          "username": "admin",
          "password_env": "IX_PASS_HOME",
          "port": 22,
          "model": "IX3315",
          "note": "free-form; shown by --list"
        }
      }
    }

    Per-device keys: host (or hostname), username (or user), port,
    password, password_env, key_file, use_keys, ssh_config_file, model, note.
    "model" is the product name (IX2215, IX3315 ...). It changes nothing about the
    connection: it is a hint carried into --list and the target banner, so that an
    agent reading the manuals knows which model column of a spec table applies.
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

Requires netmiko >= 4.6 (nec_ix driver; verified on 4.7.0) and paramiko; uv
installs both from pyproject.toml. They are imported only when a connection is
opened, so --list and --help work without them (e.g. `python -m ix_ssh` in a
bare checkout), and a missing one is reported with the interpreter path
instead of a traceback.
"""
