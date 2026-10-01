"""Complement Ruff's open() encoding check for pathlib and subprocess calls."""

import ast
import unittest
from pathlib import Path


class TextIOPolicyTest(unittest.TestCase):
    def test_text_io_specifies_encoding(self):
        root = Path(__file__).resolve().parents[1]
        violations = []
        for directory in (root / "src", root / "tests"):
            for path in directory.rglob("*.py"):
                tree = ast.parse(path.read_text(encoding="utf-8"), filename=str(path))
                for call in ast.walk(tree):
                    if not isinstance(call, ast.Call) or not isinstance(call.func, ast.Attribute):
                        continue
                    keywords = {k.arg: k.value for k in call.keywords}
                    text_file = call.func.attr in {"read_text", "write_text"}
                    text_pipe = call.func.attr in {"run", "Popen", "check_output"} and any(
                        isinstance(keywords.get(key), ast.Constant) and keywords[key].value is True
                        for key in ("text", "universal_newlines")
                    )
                    if (text_file or text_pipe) and "encoding" not in keywords:
                        violations.append(f"{path.relative_to(root)}:{call.lineno}")
        self.assertEqual(violations, [], "Text I/O without encoding: " + ", ".join(violations))
