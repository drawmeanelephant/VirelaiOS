#!/usr/bin/env python3
"""Host policy negatives. No network, compiler or VM required."""
import importlib.util
from pathlib import Path
import sys
import tempfile
import unittest
from unittest.mock import patch

sys.dont_write_bytecode = True
HERE = Path(__file__).resolve().parent


def load(name):
    spec = importlib.util.spec_from_file_location(name, HERE / (name + ".py"))
    module = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(module)
    return module


build = load("build")
stack = load("stack")
check = load("check")


class Policy(unittest.TestCase):
    def test_recursive_frames_are_charged_at_nine_activations(self):
        assembly = ".type .Lengine.Interp.evalNodes,@function\nbl .Lengine.Interp.evalNodes\n"
        result = stack.bound(assembly, {".Lengine.Interp.evalNodes": 32, "other": 48})
        self.assertEqual(result["bound_bytes"], 9 * 32 + 48)

    def test_unreviewed_recursion_and_excess_stack_refuse(self):
        with self.assertRaisesRegex(ValueError, "UnreviewedStackRecursion"):
            stack.bound(".type unknown,@function\nbl unknown\n", {"unknown": 16})
        with self.assertRaisesRegex(ValueError, "StackBudget"):
            stack.bound("", {"oversized": 131073})

    def test_source_archive_corruption_refuses_before_materialization(self):
        with tempfile.TemporaryDirectory() as temp:
            cache = Path(temp)
            (cache / (build.LOCK["commit"] + ".tar.gz")).write_bytes(b"not the pinned archive")
            with patch.object(build, "CACHE", cache):
                with self.assertRaisesRegex(ValueError, "checksum"):
                    build.source(cache)

    def test_golden_comparison_is_byte_exact_and_diagnostics_cannot_alias(self):
        with tempfile.TemporaryDirectory() as temp:
            root = Path(temp)
            share, expected, evidence = root / "share", root / "expected", root / "evidence"
            share.mkdir()
            expected.mkdir()
            (expected / "cases.json").write_text('[{"id":"case","status":0,"error":null,"metrics":false}]')
            (expected / "case.out").write_bytes(b"golden\n")
            (share / "case.out").write_bytes(b"golden\n")
            (share / "case.err").write_bytes(b"")
            check.compare(share, expected, evidence)
            (share / "case.out").write_bytes(b"golden\r\n")
            with self.assertRaisesRegex(ValueError, "golden output mismatch"):
                check.compare(share, expected, evidence)
            (share / "case.out").write_bytes(b"golden\n")
            (share / "case.err").write_bytes(b"unexpected diagnostic")
            with self.assertRaisesRegex(ValueError, "contaminated stderr"):
                check.compare(share, expected, evidence)

    def test_failure_requires_empty_stdout_and_explicit_stderr(self):
        with tempfile.TemporaryDirectory() as temp:
            root = Path(temp)
            share, expected = root / "share", root / "expected"
            share.mkdir()
            expected.mkdir()
            (expected / "cases.json").write_text('[{"id":"case","status":70,"error":"InputLimit","metrics":false}]')
            (expected / "case.out").write_bytes(b"")
            (share / "case.out").write_bytes(b"")
            (share / "case.err").write_bytes(b"")
            with self.assertRaisesRegex(ValueError, "missing explicit stderr"):
                check.compare(share, expected, root / "evidence")
            (share / "case.err").write_bytes(b"k4o: InputLimit\n")
            check.compare(share, expected, root / "evidence")


if __name__ == "__main__":
    unittest.main(verbosity=2)
