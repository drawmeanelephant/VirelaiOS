import json
from pathlib import Path
import tempfile
import unittest
from unittest.mock import patch

import build
import check
import stack

ASM = """
.type entry,@function
entry:
    mov x2, sp
.size entry, .-entry
.type refused,@function
refused:
    svc #0
.size refused, .-refused
.type body,@function
body:
    stp x29, x30, [sp, #-16]!
    sub sp, sp, #2, lsl #12
    add sp, sp, #2, lsl #12
.size body, .-body
_start = entry
boris_stack_refused = refused
"""


class BuildTests(unittest.TestCase):
    def test_frame_accounting_retains_oversize_evidence(self):
        frames = stack.frames(ASM)
        self.assertEqual(frames, {"entry": 0, "refused": 0, "body": 8208})
        with self.assertRaisesRegex(ValueError, "StackBudget"):
            stack.guard(ASM, frames)

    def test_guard_checks_every_nonentry_body_before_its_frame(self):
        text = ASM.replace("#2, lsl #12", "#1, lsl #12")
        result = stack.guard(text, stack.frames(text))
        self.assertEqual(result.count("b boris_stack_refused"), 1)
        self.assertLess(result.index("b boris_stack_refused"), result.index("stp x29"))

    def test_missing_aliases_and_unknown_stack_adjustments_refuse(self):
        with self.assertRaisesRegex(ValueError, "aliases"):
            stack.guard(".type body,@function\nbody:\n", {"body": 0})
        with self.assertRaisesRegex(ValueError, "UnreviewedStackAdjustment"):
            stack.frames(ASM.replace("mov x2, sp", "mov sp, x2"))

    def test_source_bytes_additions_and_symlinks_fail_closed(self):
        with tempfile.TemporaryDirectory() as temp:
            root = Path(temp)
            (root / "src").mkdir()
            source = root / "src/one.zig"
            source.write_text("pub const value = 1;\n")
            lock = {"boris": {"files": {"src/one.zig": build.sdk.digest(source.read_bytes())}}}
            with patch.object(build, "LOCK", lock):
                build.verify("boris", root)
                source.write_text("pub const value = 2;\n")
                with self.assertRaisesRegex(ValueError, "checksum"):
                    build.verify("boris", root)
                source.write_text("pub const value = 1;\n")
                extra = root / "src/extra.zig"
                extra.write_text("")
                with self.assertRaisesRegex(ValueError, "inventory"):
                    build.verify("boris", root)
                extra.unlink()
                source.unlink()
                source.symlink_to(root / "outside")
                with self.assertRaisesRegex(ValueError, "checksum"):
                    build.verify("boris", root)

    def test_pins_and_exclusions_are_exact(self):
        self.assertEqual(build.LOCK["boris"]["revision"], "08969742f85238443ce5cd1cd53ceab1b1f3f85a")
        self.assertEqual(build.LOCK["oliver"]["revision"], "80d53b2118005b314d4c551d18a023f293eecc75")
        patches = json.loads((build.HERE / "patches.json").read_text())
        self.assertEqual(set(patches), {"src/source_provider.zig", "src/scanner.zig",
                                       "src/artifact_sink.zig", "src/pipeline.zig"})

    def test_probe_frames_are_length_checked_and_never_accept_partial_artifacts(self):
        frame = b"boris-closure-probe 1 1\n1 1 2\nxm{}"
        self.assertEqual(check.unpack(frame), {"x": ("m", b"{}")})
        for n in range(len(frame)):
            with self.assertRaises(ValueError):
                check.unpack(frame[:n])
        for malformed in (frame + b"x", frame.replace(b"1 1 2\n", b"-1 1 2\n"),
                          b"boris-closure-probe 1 -1\n", b"boris-closure-probe 1 129\n"):
            with self.assertRaises(ValueError):
                check.unpack(malformed)


if __name__ == "__main__":
    unittest.main()
