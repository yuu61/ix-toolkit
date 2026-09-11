"""Where the device password comes from, and in which order."""

from collections.abc import Mapping

from .errors import UsageError


def find_password(entry: Mapping, env: Mapping[str, str], password_env: str | None) -> str | None:
    """The password from the non-interactive sources, first match wins:

    1. --password-env NAME -> $NAME (an error if that variable is empty)
    2. the entry's "password" (plaintext in the inventory)
    3. the entry's "password_env" -> that variable
    4. $IX_PASS (mainly for ad-hoc --host runs)

    The device's own credentials beat the global $IX_PASS: with several devices in
    the inventory, a leftover $IX_PASS must never be sent to a box that carries its
    own password. None means no source had one; the caller decides whether key
    auth covers it, whether to prompt, or to fail with missing_password_message."""
    if password_env:
        value = env.get(password_env)
        if not value:
            raise UsageError(f"ERROR: ${password_env} is unset or empty")
        return value
    if entry.get("password"):
        return entry["password"]
    env_name = entry.get("password_env")
    if env_name and env.get(env_name):
        return env[env_name]
    if env.get("IX_PASS"):
        return env["IX_PASS"]
    return None


def missing_password_message(label: str, entry: Mapping) -> str:
    env_name = entry.get("password_env")
    hint = f"${env_name}" if env_name else "$IX_PASS"
    return (
        f"ERROR: no password available for {label}. set {hint}, add "
        '"password"/"password_env" to the inventory entry, use key auth (key_file), '
        "or pass --ask-password to be prompted."
    )
