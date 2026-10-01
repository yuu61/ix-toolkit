"""Strict permission checking for sensitive files, and making them private."""

import os
import stat
from pathlib import Path

from . import windows_permissions


def restrict_to_owner(path: Path) -> bool:
    """Best effort: chmod 600 on POSIX, an owner-only DACL on Windows.
    Returns False when the filesystem will not allow it. Verify Windows ACLs
    directly rather than using the optional inventory permission-check bypass.
    """
    try:
        if os.name != "nt":
            Path(path).chmod(0o600)
            return True
        windows_permissions.restrict_to_owner(str(path))
        return _is_secure_windows(str(path))
    except OSError:
        return False


def fix_command(path: Path) -> str:
    """A POSIX shell or Windows PowerShell command to replace broad permissions.
    PowerShell resolves the process-token SID, including domain/Entra accounts,
    and removes explicit grants as well as inherited ones.
    """
    if os.name != "nt":
        return f'chmod 600 "{path.absolute()}"'
    literal = str(path.absolute()).replace("'", "''")
    return (
        f"$acl = Get-Acl -LiteralPath '{literal}'; "
        "$acl.SetAccessRuleProtection($true, $false); "
        "foreach ($rule in @($acl.Access)) { [void]$acl.RemoveAccessRuleSpecific($rule) }; "
        "$acl.AddAccessRule([System.Security.AccessControl.FileSystemAccessRule]::new("
        "[System.Security.Principal.WindowsIdentity]::GetCurrent().User, 'FullControl', 'Allow')); "
        f"Set-Acl -LiteralPath '{literal}' -AclObject $acl"
    )


def is_secure_file(path: Path) -> bool:
    """Only accessible by the current user (or Windows SYSTEM/Administrators)."""
    if os.environ.get("IX_SSH_SKIP_PERM_CHECK"):
        return True
    if os.name == "nt":
        return _is_secure_windows(str(path))
    return _is_secure_posix(path)


def _is_secure_posix(path: Path) -> bool:
    st = path.stat()
    if getattr(os, "getuid", None) is not None and st.st_uid != os.getuid():
        return False
    return (st.st_mode & (stat.S_IRWXG | stat.S_IRWXO)) == 0


def _is_secure_windows(path: str) -> bool:
    try:
        return windows_permissions.is_secure_file(path)
    except OSError:
        return False
