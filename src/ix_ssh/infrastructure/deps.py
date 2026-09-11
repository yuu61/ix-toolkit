"""netmiko and paramiko are imported late, inside the functions that need them,
so --list/--help work without either. When one is missing, the error names the
interpreter that looked for it: ix-ssh runs straight out of the clone with
whatever `python` the caller has, so the fix is a pip install into *that* one."""

import sys

from ..domain import UsageError


def missing_dependency(module: str) -> UsageError:
    return UsageError(
        f"ERROR: {module} is not installed for {sys.executable}\n"
        "  run: pip install netmiko paramiko   (with that python; see README)"
    )
