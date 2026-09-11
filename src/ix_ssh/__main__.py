"""Allow `python -m ix_ssh` alongside the installed `ix-ssh` command."""

import sys

from .cli import main

sys.exit(main())
