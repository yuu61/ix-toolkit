"""The dependency rule between the layers (AGENTS.md): domain imports nothing
from the other layers and no SSH library; infrastructure never imports
application or cli; cli never imports infrastructure directly. --list and
--help must keep working on a machine where netmiko/paramiko are absent, so the
SSH libraries may only be imported inside the functions that open a session."""

import subprocess
import sys
import tempfile
import unittest
from pathlib import Path

PROBE = """
import sys
import {module}
loaded = sorted(m for m in sys.modules if m.startswith(("netmiko", "paramiko", "ix_ssh.")))
print("\\n".join(loaded))
"""


def imported_by(module: str) -> set[str]:
    out = subprocess.run(
        [sys.executable, "-c", PROBE.format(module=module)],
        check=True,
        capture_output=True,
        text=True,
    ).stdout
    return set(out.split())


class LayeringTest(unittest.TestCase):
    def test_domain_stands_alone(self):
        loaded = imported_by("ix_ssh.domain")
        self.assertFalse({m for m in loaded if not m.startswith("ix_ssh.domain")}, loaded)

    def test_infrastructure_uses_only_domain(self):
        loaded = imported_by("ix_ssh.infrastructure")
        self.assertFalse(
            {m for m in loaded if m.startswith(("ix_ssh.application", "ix_ssh.cli"))}, loaded
        )

    def test_ssh_libraries_load_lazily(self):
        loaded = imported_by("ix_ssh.cli")
        self.assertFalse({m for m in loaded if m.startswith(("netmiko", "paramiko"))}, loaded)

    def test_list_and_help_work_without_ssh_libraries(self):
        # Hide netmiko/paramiko by making their import fail, then run the CLI.
        code = """
import sys
class Block:
    def find_spec(self, name, path=None, target=None):
        if name.split(".")[0] in ("netmiko", "paramiko"):
            raise ImportError(name)
sys.meta_path.insert(0, Block())
from ix_ssh.cli import main
sys.exit(main(sys.argv[1:]))
"""
        with tempfile.TemporaryDirectory() as tmp:
            inventory = Path(tmp) / "devices.json"
            inventory.write_text('{"devices": {"home": {"host": "room1", "username": "admin"}}}')
            for args in (["--help"], ["--list", "--inventory", str(inventory)]):
                res = subprocess.run(
                    [sys.executable, "-c", code, *args], capture_output=True, text=True, check=False
                )
                self.assertEqual(res.returncode, 0, res.stderr)
            self.assertIn("home  admin@room1:22", res.stdout)


if __name__ == "__main__":
    unittest.main()
