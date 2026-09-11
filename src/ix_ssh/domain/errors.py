class UsageError(Exception):
    """A problem the user can fix: a malformed inventory, an unknown device, a
    missing password. The message is complete on its own; the CLI prints it
    verbatim and exits 1 without a traceback."""
