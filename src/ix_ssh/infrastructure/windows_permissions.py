"""Replace/check Windows DACLs using process-token SIDs, without account lookup.

DLLs are loaded only when called so this module also imports on POSIX. Replacing
the DACL removes explicit and inherited grants in a single operation.
"""

import ctypes
from ctypes import wintypes


def _api():
    security = ctypes.WinDLL("advapi32", use_last_error=True)
    kernel = ctypes.WinDLL("kernel32", use_last_error=True)
    pointer = wintypes.LPVOID
    out_pointer = ctypes.POINTER(pointer)
    definitions = (
        (
            security,
            "OpenProcessToken",
            [wintypes.HANDLE, wintypes.DWORD, out_pointer],
            wintypes.BOOL,
        ),
        (
            security,
            "GetTokenInformation",
            [
                wintypes.HANDLE,
                wintypes.DWORD,
                pointer,
                wintypes.DWORD,
                ctypes.POINTER(wintypes.DWORD),
            ],
            wintypes.BOOL,
        ),
        (
            security,
            "ConvertSidToStringSidW",
            [pointer, ctypes.POINTER(wintypes.LPWSTR)],
            wintypes.BOOL,
        ),
        (
            security,
            "ConvertStringSecurityDescriptorToSecurityDescriptorW",
            [wintypes.LPCWSTR, wintypes.DWORD, out_pointer, ctypes.POINTER(wintypes.DWORD)],
            wintypes.BOOL,
        ),
        (
            security,
            "GetSecurityDescriptorDacl",
            [pointer, ctypes.POINTER(wintypes.BOOL), out_pointer, ctypes.POINTER(wintypes.BOOL)],
            wintypes.BOOL,
        ),
        (
            security,
            "SetNamedSecurityInfoW",
            [wintypes.LPCWSTR, wintypes.DWORD, wintypes.DWORD, pointer, pointer, pointer, pointer],
            wintypes.DWORD,
        ),
        (
            security,
            "GetNamedSecurityInfoW",
            [
                wintypes.LPCWSTR,
                wintypes.DWORD,
                wintypes.DWORD,
                out_pointer,
                out_pointer,
                out_pointer,
                out_pointer,
                out_pointer,
            ],
            wintypes.DWORD,
        ),
        (security, "GetAce", [pointer, wintypes.DWORD, out_pointer], wintypes.BOOL),
        (kernel, "GetCurrentProcess", [], wintypes.HANDLE),
        (kernel, "CloseHandle", [wintypes.HANDLE], wintypes.BOOL),
        (kernel, "LocalFree", [pointer], pointer),
    )
    for dll, name, arguments, result in definitions:
        function = getattr(dll, name)
        function.argtypes = arguments
        function.restype = result
    return security, kernel


def _sid_string(security, kernel, sid) -> str:
    value = wintypes.LPWSTR()
    if not security.ConvertSidToStringSidW(sid, ctypes.byref(value)):
        raise ctypes.WinError(ctypes.get_last_error())
    try:
        return value.value
    finally:
        kernel.LocalFree(ctypes.cast(value, wintypes.LPVOID))


def current_user_sid() -> str:
    security, kernel = _api()
    token = wintypes.HANDLE()
    if not security.OpenProcessToken(kernel.GetCurrentProcess(), 0x0008, ctypes.byref(token)):
        raise ctypes.WinError(ctypes.get_last_error())
    try:
        size = wintypes.DWORD()
        # TokenUser (1) starts with a SID pointer. Retain the buffer until the
        # SID has been converted; the sizing call normally returns FALSE.
        security.GetTokenInformation(token, 1, None, 0, ctypes.byref(size))
        if not size.value:
            raise ctypes.WinError(ctypes.get_last_error())
        buffer = ctypes.create_string_buffer(size.value)
        if not security.GetTokenInformation(token, 1, buffer, size, ctypes.byref(size)):
            raise ctypes.WinError(ctypes.get_last_error())
        sid = ctypes.cast(buffer, ctypes.POINTER(wintypes.LPVOID)).contents.value
        return _sid_string(security, kernel, sid)
    finally:
        kernel.CloseHandle(token)


def restrict_to_owner(path: str) -> None:
    security, kernel = _api()
    descriptor = wintypes.LPVOID()
    sddl = f"D:P(A;;FA;;;{current_user_sid()})"
    if not security.ConvertStringSecurityDescriptorToSecurityDescriptorW(
        sddl, 1, ctypes.byref(descriptor), None
    ):
        raise ctypes.WinError(ctypes.get_last_error())
    try:
        present, defaulted = wintypes.BOOL(), wintypes.BOOL()
        dacl = wintypes.LPVOID()
        if not security.GetSecurityDescriptorDacl(
            descriptor, ctypes.byref(present), ctypes.byref(dacl), ctypes.byref(defaulted)
        ):
            raise ctypes.WinError(ctypes.get_last_error())
        # SE_FILE_OBJECT; DACL_SECURITY_INFORMATION | PROTECTED_DACL_SECURITY_INFORMATION.
        result = security.SetNamedSecurityInfoW(path, 1, 0x80000004, None, None, dacl, None)
        if result:
            raise ctypes.WinError(result)
    finally:
        kernel.LocalFree(descriptor)


class _ACL(ctypes.Structure):
    _fields_ = [
        ("revision", wintypes.BYTE),
        ("reserved", wintypes.BYTE),
        ("size", wintypes.WORD),
        ("count", wintypes.WORD),
        ("reserved2", wintypes.WORD),
    ]


class _ACEHeader(ctypes.Structure):
    _fields_ = [("kind", wintypes.BYTE), ("flags", wintypes.BYTE), ("size", wintypes.WORD)]


MIN_ALLOW_ACE_SIZE = 12  # Header, access mask, and the start of a SID.


def is_secure_file(path: str) -> bool:
    security, kernel = _api()
    dacl, descriptor = wintypes.LPVOID(), wintypes.LPVOID()
    result = security.GetNamedSecurityInfoW(
        path, 1, 4, None, None, ctypes.byref(dacl), None, ctypes.byref(descriptor)
    )
    if result:
        return False
    try:
        if not dacl:
            return False  # A NULL DACL permits everyone.
        allowed = {current_user_sid(), "S-1-5-18", "S-1-5-32-544"}
        acl = ctypes.cast(dacl, ctypes.POINTER(_ACL)).contents
        for index in range(acl.count):
            ace = wintypes.LPVOID()
            if not security.GetAce(dacl, index, ctypes.byref(ace)):
                return False
            header = ctypes.cast(ace, ctypes.POINTER(_ACEHeader)).contents
            if header.flags & 0x08 or header.kind == 1:
                continue  # INHERIT_ONLY or a deny ACE cannot grant file access.
            if header.kind != 0 or header.size < MIN_ALLOW_ACE_SIZE:
                return False  # Fail closed on unsupported object/callback ACEs.
            mask = ctypes.cast(ace.value + 4, ctypes.POINTER(wintypes.DWORD)).contents.value
            if mask and _sid_string(security, kernel, ace.value + 8) not in allowed:
                return False
        return True
    finally:
        kernel.LocalFree(descriptor)
