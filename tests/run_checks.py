"""Run required tests, rejecting skipped SSH integration and Windows DACL checks."""

import argparse
import os
import sys
import unittest

from .test_integration import HAVE_SSH


def main() -> int:
    parser = argparse.ArgumentParser()
    parser.add_argument("--integration-only", action="store_true")
    args = parser.parse_args()
    if not HAVE_SSH:
        parser.error("netmiko/paramiko are required for these checks")
    loader = unittest.defaultTestLoader
    suite = (
        loader.loadTestsFromName("tests.test_integration")
        if args.integration_only
        else loader.discover("tests", top_level_dir=".")
    )
    result = unittest.TextTestRunner(verbosity=2).run(suite)
    forbidden = [
        test.id()
        for test, _reason in result.skipped
        if "test_integration" in test.id()
        or (os.name == "nt" and "WindowsPermissionsTest" in test.id())
    ]
    if forbidden:
        print("Required tests were skipped: " + ", ".join(forbidden), file=sys.stderr)
    return int(bool(forbidden) or not result.wasSuccessful())


if __name__ == "__main__":
    sys.exit(main())
