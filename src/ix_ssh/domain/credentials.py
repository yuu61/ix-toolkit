"""Where the device password comes from, and in which order."""

from collections.abc import Mapping
from dataclasses import dataclass, field

from .errors import UsageError


@dataclass(frozen=True)
class ResolvedPassword:
    value: str | None = field(repr=False)
    source: str | None = None
    variable: str | None = None


def resolve_password(
    entry: Mapping, env: Mapping[str, str], password_env: str | None = None
) -> ResolvedPassword:
    """The password from the non-interactive sources, first match wins:

    1. --password-env NAME -> $NAME (an error if that variable is empty)
    2. the entry's "password" (plaintext in the inventory)
    3. the entry's "password_env" -> that variable
    4. $IX_PASS (mainly for ad-hoc --host runs)

    The device's own credentials beat the global $IX_PASS: with several devices in
    the inventory, a leftover $IX_PASS must never be sent to a box that carries its
    own password. A result with no value means no source had one; the caller
    decides whether key auth covers it, whether to prompt, or to fail. The source
    accompanies the value so listing cannot invent a different precedence."""
    if password_env:
        value = env.get(password_env)
        if not value:
            raise UsageError(f"ERROR: ${password_env} is unset or empty")
        return ResolvedPassword(value, "environment", password_env)
    if entry.get("password"):
        return ResolvedPassword(entry["password"], "inventory")
    env_name = entry.get("password_env")
    if env_name and env.get(env_name):
        return ResolvedPassword(env[env_name], "environment", env_name)
    if env.get("IX_PASS"):
        return ResolvedPassword(env["IX_PASS"], "environment", "IX_PASS")
    return ResolvedPassword(None)


def find_password(entry: Mapping, env: Mapping[str, str], password_env: str | None) -> str | None:
    return resolve_password(entry, env, password_env).value


def missing_password_message(label: str, entry: Mapping) -> str:
    env_name = entry.get("password_env")
    hint = f"${env_name}" if env_name else "$IX_PASS"
    return (
        f"ERROR: no password available for {label}. set {hint}, add "
        '"password"/"password_env" to the inventory entry, use key auth (key_file), '
        "or pass --ask-password to be prompted."
    )
