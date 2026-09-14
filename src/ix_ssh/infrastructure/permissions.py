"""Strict permission checking for sensitive files, and making them private."""

import os
import stat
import subprocess
from pathlib import Path

# Upper bound for icacls when the file sits on a hung network share.
ICACLS_TIMEOUT = 10


def restrict_to_owner(path: Path) -> bool:
    """Best effort: make `path` readable and writable by the current user only
    (chmod 600 on POSIX, an owner-only DACL on Windows). Returns False instead of
    raising when the platform or filesystem will not allow it (vfat, some CIFS
    mounts, icacls missing): the caller has already written the file, so this is
    never a write failure."""
    try:
        if os.name != "nt":
            os.chmod(path, 0o600)
            return True
        user = _current_windows_user()
        if not user:
            # icacls happily grants ":F" to the BUILTIN domain and strips every
            # other ACE, locking the owner out; refuse rather than guess.
            return False
        proc = subprocess.run(
            ["icacls", str(path), "/inheritance:r", "/grant:r", f"{user}:F"],
            capture_output=True,
            check=False,
            timeout=ICACLS_TIMEOUT,
        )
        return proc.returncode == 0
    except (OSError, subprocess.TimeoutExpired):
        return False


def fix_command(path: Path) -> str:
    """The command an operator can paste into any shell to make `path` private.
    The user name is resolved here rather than written as %USERNAME%, which only
    cmd.exe expands (PowerShell hands icacls the literal string, which fails)."""
    if os.name != "nt":
        return f'chmod 600 "{path.absolute()}"'
    user = _current_windows_user() or "%USERNAME%"
    return f'icacls "{path.absolute()}" /inheritance:r /grant:r "{user}:F"'


def _current_windows_user() -> str:
    """The account the process actually runs as (GetUserNameW), falling back to
    $USERNAME; empty when neither is available."""
    try:
        return os.getlogin()
    except OSError:
        return os.environ.get("USERNAME", "")


def is_secure_file(path: Path) -> bool:
    """
    Check if the file is securely protected (only accessible by owner/admin).
    Equivalent to checking for 0600 on POSIX, and checking the DACL on Windows.
    """
    if os.environ.get("IX_SSH_SKIP_PERM_CHECK"):
        return True

    if os.name == "nt":
        return _is_secure_windows(str(path))
    else:
        return _is_secure_posix(path)


def _is_secure_posix(path: Path) -> bool:
    st = path.stat()
    if getattr(os, "getuid", None) is not None and st.st_uid != os.getuid():
        return False
    # Check if group or others have any permissions (must be 600 or stricter)
    return (st.st_mode & (stat.S_IRWXG | stat.S_IRWXO)) == 0


def _is_secure_windows(path: str) -> bool:
    import ctypes
    from ctypes import wintypes

    advapi32 = ctypes.windll.advapi32
    kernel32 = ctypes.windll.kernel32

    # Constants
    SE_FILE_OBJECT = 1
    OWNER_SECURITY_INFORMATION = 1
    DACL_SECURITY_INFORMATION = 4
    ACCESS_ALLOWED_ACE_TYPE = 0

    class ACL(ctypes.Structure):
        _fields_ = [
            ("AclRevision", wintypes.BYTE),
            ("Sbz1", wintypes.BYTE),
            ("AclSize", wintypes.WORD),
            ("AceCount", wintypes.WORD),
            ("Sbz2", wintypes.WORD),
        ]

    class ACE_HEADER(ctypes.Structure):
        _fields_ = [
            ("AceType", wintypes.BYTE),
            ("AceFlags", wintypes.BYTE),
            ("AceSize", wintypes.WORD),
        ]

    # Setup argtypes to prevent access violations
    advapi32.GetNamedSecurityInfoW.argtypes = [
        wintypes.LPCWSTR,
        wintypes.DWORD,
        wintypes.DWORD,
        ctypes.POINTER(wintypes.LPVOID),
        ctypes.POINTER(wintypes.LPVOID),
        ctypes.POINTER(ctypes.POINTER(ACL)),
        ctypes.POINTER(wintypes.LPVOID),
        ctypes.POINTER(wintypes.LPVOID),
    ]
    advapi32.GetNamedSecurityInfoW.restype = wintypes.DWORD

    advapi32.ConvertSidToStringSidW.argtypes = [wintypes.LPVOID, ctypes.POINTER(wintypes.LPWSTR)]
    advapi32.ConvertSidToStringSidW.restype = wintypes.BOOL

    advapi32.GetAce.argtypes = [
        ctypes.POINTER(ACL),
        wintypes.DWORD,
        ctypes.POINTER(wintypes.LPVOID),
    ]
    advapi32.GetAce.restype = wintypes.BOOL

    pOwner = wintypes.LPVOID()
    pDacl = ctypes.POINTER(ACL)()
    pSD = wintypes.LPVOID()

    res = advapi32.GetNamedSecurityInfoW(
        path,
        SE_FILE_OBJECT,
        OWNER_SECURITY_INFORMATION | DACL_SECURITY_INFORMATION,
        ctypes.byref(pOwner),
        None,
        ctypes.byref(pDacl),
        None,
        ctypes.byref(pSD),
    )
    if res != 0:
        return False  # Failed to get security info

    try:
        owner_sid_str = wintypes.LPWSTR()
        if not advapi32.ConvertSidToStringSidW(pOwner, ctypes.byref(owner_sid_str)):
            return False
        owner_sid = owner_sid_str.value
        kernel32.LocalFree(ctypes.cast(owner_sid_str, wintypes.HLOCAL))

        # Well-known SIDs: SYSTEM, Administrators, and the Owner
        allowed_sids = {"S-1-5-18", "S-1-5-32-544", owner_sid}

        if not pDacl:
            return False  # No DACL means everyone has access

        for i in range(pDacl.contents.AceCount):
            pAce = wintypes.LPVOID()
            if not advapi32.GetAce(pDacl, i, ctypes.byref(pAce)):
                continue

            header = ctypes.cast(pAce, ctypes.POINTER(ACE_HEADER)).contents
            if header.AceType == ACCESS_ALLOWED_ACE_TYPE:
                # In ACCESS_ALLOWED_ACE, the SID starts 8 bytes into the structure
                pSid = pAce.value + 8
                ace_sid_str = wintypes.LPWSTR()
                if advapi32.ConvertSidToStringSidW(pSid, ctypes.byref(ace_sid_str)):
                    ace_sid = ace_sid_str.value
                    kernel32.LocalFree(ctypes.cast(ace_sid_str, wintypes.HLOCAL))
                    if ace_sid not in allowed_sids:
                        return False  # An unauthorized user has access
        return True
    finally:
        if pSD:
            kernel32.LocalFree(ctypes.cast(pSD, wintypes.HLOCAL))
