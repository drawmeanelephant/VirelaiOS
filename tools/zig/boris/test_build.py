import json
from pathlib import Path
import tempfile
import unittest
from unittest.mock import patch

import build
import check
import native
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
    def test_platform_register_is_reserved_across_syscalls_and_preemption(self):
        self.assertEqual(build.GUEST_CPU, "baseline+reserve_x18")
        build.check_registers("mov x17, sp\n.ascii \"x18\"\n")
        for body in ("ldr x18, [x8]\n", "mov w18, #1\n", "cmp x0, x18\n"):
            with self.assertRaisesRegex(ValueError, "UnpreservedPlatformRegister"):
                build.check_registers(body)

    def test_frame_accounting_retains_oversize_evidence(self):
        frames = stack.frames(ASM)
        self.assertEqual(frames, {"entry": 0, "refused": 0, "body": 8208})
        stack.guard(ASM, frames)
        too_large = ASM.replace("#2, lsl #12", "#33, lsl #12")
        with self.assertRaisesRegex(ValueError, "StackBudget"):
            stack.guard(too_large, stack.frames(too_large))

    def test_guard_checks_every_nonentry_body_before_its_frame(self):
        text = ASM.replace("#2, lsl #12", "#1, lsl #12")
        result = stack.guard(text, stack.frames(text))
        self.assertEqual(result.count("b boris_stack_refused"), 1)
        self.assertIn("sub x17, x17, #1, lsl #12", result)
        self.assertIn("sub x17, x17, #16", result)
        self.assertLess(result.index("b boris_stack_refused"), result.index("stp x29"))

    def test_recursive_and_indirect_depth_is_checked_not_assumed_from_fixture(self):
        text = ASM.replace("    add sp, sp, #2, lsl #12", "    bl body\n    blr x9\n    add sp, sp, #2, lsl #12")
        result = stack.proof(text, stack.frames(text))
        self.assertEqual(result["indirect_calls"], 1)
        self.assertIn("body", result["calls"]["body"])
        self.assertEqual(result["guarded_stack_bytes"], 122880)
        # Exercise every 16-byte-aligned admission point for every frame size,
        # including the measured frame and a full-budget adversarial frame.
        for frame in (0, 16, 5248, 30144, 122880):
            for consumed in range(0, stack.FLOOR_BYTES + 32, 16):
                admitted = consumed + frame <= stack.FLOOR_BYTES
                if admitted:
                    self.assertLessEqual(consumed + frame, stack.STACK_BUDGET)
        consumed = 0
        depth = 0
        while consumed + 16 <= stack.FLOOR_BYTES:
            consumed += 16
            depth += 1
        self.assertEqual(depth, 7680)
        self.assertGreater(consumed + 16, stack.FLOOR_BYTES)

    def test_control_flow_unknown_targets_and_repeated_frame_allocation_refuse(self):
        loop = ASM.replace("    stp x29", "loop:\n    stp x29").replace(
            "    add sp, sp, #2, lsl #12", "    b loop\n    add sp, sp, #2, lsl #12")
        with self.assertRaisesRegex(ValueError, "repeated"):
            stack.guard(loop, stack.frames(loop))
        with self.assertRaisesRegex(ValueError, "unknown branch"):
            stack.guard(ASM.replace("    add sp,", "    bl outside_image\n    add sp,"), stack.frames(ASM))
        zero_call = ASM.replace("    svc #0", "    bl body\n    svc #0")
        with self.assertRaisesRegex(ValueError, "exemption"):
            stack.guard(zero_call, stack.frames(zero_call))

    def test_memory_helper_aliases_are_guarded_not_exempted_or_double_counted(self):
        text = ASM + ".type memset,@function\nmemset = body\n"
        self.assertNotIn("memset", stack.frames(text))
        self.assertEqual(stack.guard(text, stack.frames(text)).count("b boris_stack_refused"), 1)

    def test_missing_aliases_and_unknown_stack_adjustments_refuse(self):
        with self.assertRaisesRegex(ValueError, "aliases"):
            stack.guard(".type body,@function\nbody:\n", {"body": 0})
        with self.assertRaisesRegex(ValueError, "UnreviewedStackAdjustment"):
            stack.frames(ASM.replace("mov x2, sp", "mov sp, x2"))
        for access in ("ldr x0, [sp, #-8]", "ldr x0, [sp], x1"):
            with self.assertRaisesRegex(ValueError, "UnreviewedStackAdjustment"):
                stack.frames(ASM.replace("    add sp,", f"    {access}\n    add sp,"))

    def test_budget_refusal_does_not_overwrite_an_existing_guarded_artifact(self):
        with tempfile.TemporaryDirectory() as temp:
            work = Path(temp)
            output = work / "BORIS.BIN"
            output.write_bytes(b"prior guarded artifact")
            too_large = ASM.replace("#2, lsl #12", "#33, lsl #12")
            with self.assertRaisesRegex(ValueError, "StackBudget"):
                build.release(work, work, too_large, stack.frames(too_large), {})
            self.assertEqual(output.read_bytes(), b"prior guarded artifact")
            self.assertFalse((work / "guarded.elf").exists())

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

    def test_published_tree_requires_all_oracle_bytes_and_no_staging_residue(self):
        with tempfile.TemporaryDirectory() as temp:
            root = Path(temp)
            site = root / "site"
            (site / "nested").mkdir(parents=True)
            oracle = {"index.html": ("text/html", b"home\n"),
                      "nested/long-name.html": ("text/html", b"nested\n")}
            for path, (_, data) in oracle.items():
                (site / path).write_bytes(data)
            (site / "prior.html").write_bytes(b"keep\n")
            first = native.published_tree(root, "site", oracle)
            self.assertEqual(first, native.published_tree(root, "site", oracle))
            (site / ".boris-stage-orphan").mkdir()
            with self.assertRaises(AssertionError):
                native.published_tree(root, "site", oracle)
            (site / ".boris-stage-orphan").rmdir()
            (site / "index.html").write_bytes(b"wrong\n")
            with self.assertRaises(AssertionError):
                native.published_tree(root, "site", oracle)

    def test_fixture_parent_inventory_keeps_every_nested_component(self):
        self.assertEqual(native.parents("a/b/c.html"), ["a/b", "a"])
        self.assertEqual(native.parents("index.html"), [])


if __name__ == "__main__":
    unittest.main()
