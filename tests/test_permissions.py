"""Exercise the Windows DACL path on real temporary files, never inventories."""

import os
import subprocess
import tempfile
import unittest
from pathlib import Path
from unittest.mock import patch

from ix_ssh.infrastructure.files import write_backup
from ix_ssh.infrastructure.permissions import fix_command, is_secure_file, restrict_to_owner
from ix_ssh.infrastructure.windows_permissions import current_user_sid


@unittest.skipUnless(os.name == "nt", "Windows DACLs")
class WindowsPermissionsTest(unittest.TestCase):
    def setUp(self):
        self.tmp = tempfile.TemporaryDirectory()
        self.addCleanup(self.tmp.cleanup)
        self.path = Path(self.tmp.name) / "owner's backup.conf"
        self.path.write_text("synthetic config\n", encoding="utf-8")
        self.environment = patch.dict(os.environ, {"IX_SSH_SKIP_PERM_CHECK": ""})
        self.environment.start()
        self.addCleanup(self.environment.stop)

    def grant_everyone(self):
        subprocess.run(
            ["icacls", str(self.path), "/grant", "*S-1-1-0:R"],
            capture_output=True,
            check=True,
            timeout=10,
        )
        self.assertFalse(is_secure_file(self.path))

    def test_hardening_removes_explicit_grants_and_preserves_owner_access(self):
        self.grant_everyone()
        with (
            patch("os.getlogin", side_effect=OSError),
            patch.dict(os.environ, {"USERNAME": "invalid"}),
        ):
            self.assertTrue(current_user_sid().startswith("S-1-"))
            self.assertTrue(restrict_to_owner(self.path))
        self.assertTrue(is_secure_file(self.path))
        self.path.write_text("still writable\n", encoding="utf-8")
        self.assertEqual(self.path.read_text(encoding="utf-8"), "still writable\n")

    def test_inventory_bypass_does_not_bypass_hardening_verification(self):
        self.grant_everyone()
        with (
            patch.dict(os.environ, {"IX_SSH_SKIP_PERM_CHECK": "1"}),
            patch("ix_ssh.infrastructure.windows_permissions.restrict_to_owner"),
        ):
            self.assertFalse(restrict_to_owner(self.path))

    def test_backup_overwrite_removes_explicit_grants(self):
        self.grant_everyone()
        self.assertTrue(write_backup(self.path, "new config"))
        self.assertTrue(is_secure_file(self.path))
        self.assertEqual(self.path.read_bytes(), b"new config\n")

    def test_powershell_fix_command_removes_explicit_grants(self):
        self.grant_everyone()
        # A PowerShell 7 parent exports its module path, which Windows
        # PowerShell 5.1 cannot load. Let the child use its own defaults.
        environment = {k: v for k, v in os.environ.items() if k.lower() != "psmodulepath"}
        result = subprocess.run(
            ["powershell", "-NoProfile", "-NonInteractive", "-Command", fix_command(self.path)],
            capture_output=True,
            check=False,
            timeout=15,
            env=environment,
        )
        self.assertEqual(result.returncode, 0, result.stderr.decode("utf-8", "replace"))
        self.assertTrue(is_secure_file(self.path))

    def test_missing_file_is_not_secure_and_cannot_be_hardened(self):
        missing = self.path.parent / "missing.conf"
        self.assertFalse(is_secure_file(missing))
        self.assertFalse(restrict_to_owner(missing))
