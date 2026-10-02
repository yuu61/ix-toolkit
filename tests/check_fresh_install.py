"""Check the distributed editable install in disposable, unlocked tool directories."""

import argparse
import os
import subprocess
import tempfile
from pathlib import Path


def main() -> None:
    parser = argparse.ArgumentParser()
    parser.add_argument("--uv", default="uv")
    parser.add_argument("--python", default="3.14")
    args = parser.parse_args()
    repository = Path(__file__).resolve().parent.parent
    scratch = (repository / ".make").resolve()
    if not scratch.is_relative_to(repository):
        parser.error(".make must be inside the repository")
    scratch.mkdir(exist_ok=True)

    with tempfile.TemporaryDirectory(prefix="fresh-", dir=scratch) as temporary:
        root = Path(temporary).resolve()
        if not root.is_relative_to(scratch):
            parser.error("fresh install directory must be inside .make")
        tools, executables = root / "tools", root / "bin"
        env = {**os.environ, "UV_TOOL_DIR": str(tools), "UV_TOOL_BIN_DIR": str(executables)}
        subprocess.run(
            [args.uv, "tool", "install", "--python", args.python, "-e", str(repository)],
            cwd=repository,
            env=env,
            check=True,
        )
        windows = os.name == "nt"
        tool = executables / ("ix-ssh.exe" if windows else "ix-ssh")
        interpreter = tools / "ix-toolkit" / ("Scripts/python.exe" if windows else "bin/python")
        subprocess.run([str(tool), "--help"], cwd=repository, env=env, check=True)
        subprocess.run(
            [str(interpreter), "-m", "tests.run_checks", "--integration-only"],
            cwd=repository,
            env=env,
            check=True,
        )


if __name__ == "__main__":
    main()
