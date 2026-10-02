#!/usr/bin/env python3
"""C1 source/guard tests; --integration proves two clean offline builds."""
import argparse
import json
from pathlib import Path
import sys
import tempfile
import unittest
from unittest.mock import patch

sys.dont_write_bytecode = True
import oliver
import sdk


class OliverTests(unittest.TestCase):
    def test_frozen_pin_and_source_edit_scope(self):
        self.assertEqual(oliver.LOCK["revision"], "3615e6253f0e17b410cf1b987507d30bfcde537c")
        self.assertEqual(set(oliver.LOCK["edits"]), {"src/main.zig", "src/oliver.zig"})
        self.assertEqual(len(oliver.LOCK["sources"]), 23)
        for edit in oliver.LOCK["edits"]["src/main.zig"]:
            self.assertEqual(edit["after"], "pub " + edit["before"])

    def test_offline_source_drift_and_missing_inputs_refuse(self):
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            (root / "src").mkdir()
            data = b"pinned engine\n"
            lock = dict(oliver.LOCK, sources={"src/engine.zig": sdk.digest(data)})
            with patch.object(oliver, "LOCK", lock):
                with self.assertRaisesRegex(ValueError, "checksum mismatch"):
                    oliver.verify(root)
                (root / "src/engine.zig").write_bytes(data)
                oliver.verify(root)
                (root / "src/engine.zig").write_bytes(data + b"drift")
                with self.assertRaisesRegex(ValueError, "checksum mismatch"):
                    oliver.verify(root)
                (root / "src/engine.zig").write_bytes(data)
                (root / "src/addition.zig").write_text("extra")
                with self.assertRaisesRegex(ValueError, "inventory drift"):
                    oliver.verify(root)

    def test_every_function_entry_is_guarded_except_exact_naked_aliases(self):
        text = (
            ".type entry,@function\nentry:\n nop\n"
            ".type recursive,@function\nrecursive:\n sub sp, sp, #4096\n bl recursive\n"
            ".type refusal,@function\nrefusal:\n svc #0\n"
            ".type _start,@function\n_start = entry\n"
            ".type oliver_stack_refused,@function\noliver_stack_refused = refusal\n"
        )
        guarded = oliver.guard_assembly(text)
        self.assertEqual(guarded.count("adrp x16, oliver_stack_floor"), 1)
        self.assertIn("recursive:\n\tadrp", guarded)
        self.assertLess(guarded.index("cmp x17, x16"), guarded.index("sub sp, sp, #4096"))
        self.assertIn("b oliver_stack_refused", guarded)
        self.assertNotIn("entry:\n\tadrp", guarded)
        with self.assertRaisesRegex(ValueError, "unknown assembly"):
            oliver.guard_assembly(text.replace("recursive:", "missing:"))
        with self.assertRaisesRegex(ValueError, "missing native entry"):
            oliver.guard_assembly(text.replace("_start = entry", "_start ="))

    def test_native_target_has_no_hosted_entry_or_libc_flags(self):
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            with patch.object(oliver, "materialize", return_value=root):
                args = oliver.command(root, root, root, root / "root.zig")
            self.assertIn("aarch64-freestanding-none", args)
            self.assertIn("-fsingle-threaded", args)
            self.assertIn("--zig-lib-dir", args)
            self.assertNotIn("-lc", args)
            self.assertNotIn("-fentry=main", args)
            self.assertIn("probe = false", (root / "options.zig").read_text())
            link = oliver.link_command(root, root / "guarded.o", root / "OLIVER.BIN")
            self.assertIn("-fno-compiler-rt", link)
            self.assertNotIn("-lc", link)
            self.assertIn("aarch64-freestanding-none", link)


def integration(cache):
    with tempfile.TemporaryDirectory(prefix="oliver-repro-") as tmp:
        work = Path(tmp)
        outputs = []
        # Both prepare and build must be independent of the network.
        with patch.object(sdk.urllib.request, "urlopen", side_effect=AssertionError("offline")):
            for index in range(2):
                output = work / str(index) / "OLIVER.BIN"
                oliver.build(cache, oliver.DEFAULT_SOURCE, output.parent, output)
                guarded = (output.parent / "guarded.s").read_text()
                for helper in ("memcpy", "memset", "memmove", "__udivti3", "__umodti3"):
                    assert f".Loliver_builtins.{helper}:\n\tadrp x16, oliver_stack_floor" in guarded
                outputs.append(output)
        assert outputs[0].read_bytes() == outputs[1].read_bytes(), "nondeterministic guest image"
        first = json.loads(outputs[0].with_suffix(".BIN.json").read_text())
        second = json.loads(outputs[1].with_suffix(".BIN.json").read_text())
        assert first == second, "nondeterministic build receipt"
        evidence = sdk.ROOT / "artifacts/oliver-repro.json"
        evidence.write_text(json.dumps(first, indent=2, sort_keys=True) + "\n")
        print("C1 two clean offline builds: byte-identical images and receipts")


if __name__ == "__main__":
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--integration", action="store_true")
    parser.add_argument("--cache", type=Path, default=sdk.DEFAULT_CACHE)
    args = parser.parse_args()
    result = unittest.TextTestRunner(verbosity=2).run(unittest.defaultTestLoader.loadTestsFromTestCase(OliverTests))
    if not result.wasSuccessful():
        sys.exit(1)
    if args.integration:
        integration(args.cache.resolve())
