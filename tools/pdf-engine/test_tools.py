import ast
import os
import subprocess
import struct
import sys
import tempfile
import unittest
from pathlib import Path
from unittest.mock import patch

from elf import compare, inspect, reachable, LIMIT
from lock import snapshot
from runtime_ledger import recipe


def elf(extra=0, memextra=0):
    b = bytearray(256+extra)
    b[:7] = b"\x7fELF\x02\x01\x01"
    struct.pack_into("<HH", b, 16, 2, 183)
    struct.pack_into("<Q", b, 32, 64)
    struct.pack_into("<HH", b, 54, 56, 1)
    struct.pack_into("<IIQQQQQQ", b, 64, 1, 5, 0, 65536, 0, len(b), len(b)+memextra, 65536)
    return b


class ToolsTests(unittest.TestCase):
    def test_file_and_initialized_limit(self):
        deps = "runtime\nvirelai/pdf\nvirelai/vector\n"
        r = compare(elf(), elf(LIMIT), "runtime\n", deps)
        self.assertEqual(r["engine_added_bytes"], LIMIT)
        self.assertEqual(r["initialized_added_bytes"], LIMIT)
        with self.assertRaisesRegex(ValueError, "EngineSizeLimit"):
            compare(elf(), elf(LIMIT+1), "runtime\n", deps)

    def test_loader_and_dependency_rejections(self):
        for off, fmt, value in [(16, "<H", 3), (18, "<H", 62), (64, "<I", 3), (68, "<I", 7), (112, "<Q", 3)]:
            b = elf()
            struct.pack_into(fmt, b, off, value)
            with self.assertRaises(ValueError):
                inspect(b)
        deps = "virelai/pdf\nvirelai/vector\n"
        for bad in ["os", "syscall", "runtime/cgo", "C", "net/http", "github.com/pdf"]:
            with self.assertRaises(ValueError):
                compare(elf(), elf(), "", deps+bad+"\n")
        with self.assertRaises(ValueError):
            compare(elf(), elf(), deps, deps)
        with self.assertRaises(ValueError):
            compare(elf(), elf(), "", "virelai/pdf\n")

    def test_runtime_recipe_is_not_a_fit_claim(self):
        root = Path(__file__).resolve().parents[2]
        fork = Path(os.environ.get("GO_FORK_DIR", root.parent/"go-virelai"))
        r = recipe(root, fork)
        self.assertEqual(sum(r["arena_partitions"]), 9_437_184)
        self.assertEqual(r["runtime_upper_bound_required"]["pages"], 768)
        self.assertFalse(r["observed_guest_run"])
        self.assertEqual(r["status"], "guest_receipts_required")
        self.assertFalse(r["alignment_padding"]["fresh_padding_is_cleared"])
        self.assertTrue(r["alignment_padding"]["padding_is_touched"])
        self.assertEqual(r["kernel_tracking"]["inline_page_capacity"], 4096)
        self.assertNotIn("kernel_hard_cap", r)
        with patch.object(Path, "read_text", return_value="different runtime"):
            with self.assertRaisesRegex(ValueError, "SourceDrift"):
                recipe(root, fork)

    def test_padding_zero_invariant_drift_refused(self):
        root = Path(__file__).resolve().parents[2]
        fork = Path(os.environ.get("GO_FORK_DIR", root.parent/"go-virelai"))
        read = Path.read_text
        mutations = (
            ("mem_sbrk.go", 'GOOS == "virelai"', 'GOOS != "virelai"'),
            ("mem_sbrk.go", "memFreeWithClear(r, l, false)", "memFree(r, l)"),
            ("mem_sbrk.go", "memFree(r, l)", "memFreeWithClear(r, l, false)"),
            ("mem_sbrk.go", "memFreeWithClear(ap, n, true)", "memFreeWithClear(ap, n, false)"),
            ("mem_sbrk.go", "memFree(base, startLen)", "memFreeWithClear(base, startLen, false)"),
            ("mem_sbrk.go", "memclrNoHeapPointers(v, n)", "// removed shrink clear"),
            ("mem_sbrk.go", "*p = memHdr{}", "// retained header"),
            ("os_virelai.go", "initBlocFloor()", "lostBlocFloor()"),
            ("os_virelai.go", "virArgBlockBytes = 8 * 256", "virArgBlockBytes = 256"),
            ("sys_virelai_arm64.s", "#define VIR_MAP_ANON      0x20", "#define VIR_MAP_ANON      0x8020"),
            ("exceptions.zig", "zero_phys_page(pa);", "// removed demand zero"),
            ("syscall.zig", "if (process.mmap_collides(pid, va, aligned_len))", "if (false)"),
        )
        for name, old, new in mutations:
            with self.subTest(path=name, term=old):
                def changed(path, *args, **kwargs):
                    text = read(path, *args, **kwargs)
                    if path.name == name:
                        self.assertIn(old, text)
                        return text.replace(old, new)
                    return text
                with patch.object(Path, "read_text", changed):
                    with self.assertRaisesRegex(ValueError, "SourceDrift"):
                        recipe(root, fork)

    def test_missing_toolchain_never_acquires(self):
        with self.assertRaisesRegex(ValueError, "MissingToolchain"):
            snapshot(Path("/nonexistent"), Path("/nonexistent"))

    def test_native_sdk_graph_does_not_authorize_shadow_io(self):
        deps = "virelai/pdf\nvirelai/vector\nos\nsyscall\n"
        compare(elf(), elf(), "os\nsyscall\n", deps)
        reachable("virelai/vi.FileRead\nruntime.init\n")
        for symbol in ("os.(*File).ReadAt", "syscall.virKeep", "syscall.virPull", "net.Dial"):
            with self.assertRaises(ValueError):
                reachable(symbol)

class PaddingEditTests(unittest.TestCase):
    def test_edit_is_atomic_idempotent_and_rejects_drift(self):
        root = Path(__file__).resolve().parents[2]
        fork = Path(os.environ.get("GO_FORK_DIR", root.parent/"go-virelai"))
        script = (root/"tools/go/apply.sh").read_text()
        code = script.split('padding="$(python3 - "$F/runtime/mem_sbrk.go" <<\'PYEOF\'\n', 1)[1].split("\nPYEOF\n", 1)[0]
        changes = next(ast.literal_eval(node.value) for node in ast.parse(code).body
                       if isinstance(node, ast.Assign) and any(
                           isinstance(target, ast.Name) and target.id == "changes"
                           for target in node.targets))
        original = (fork/"src/runtime/mem_sbrk.go").read_text()
        for before, after in changes:
            original = original.replace(after, before)
        with tempfile.TemporaryDirectory() as directory:
            path = Path(directory)/"mem_sbrk.go"
            path.write_text(original)
            def apply():
                return subprocess.run(
                    [sys.executable, "-c", code, str(path)],
                    capture_output=True, text=True)
            self.assertEqual(apply().stdout.strip(), "patched")
            patched = path.read_text()
            self.assertEqual(apply().stdout.strip(), "clean")
            self.assertEqual(path.read_text(), patched)
            # A half edit, changed upstream anchor and duplicate must never
            # leave the first replacement behind when the second refuses.
            for drift in (
                original.replace(*changes[0]),
                original.replace("memFree(r, l)", "unknownFree(r, l)"),
                original + changes[0][0],
            ):
                path.write_text(drift)
                result = apply()
                self.assertNotEqual(result.returncode, 0)
                self.assertIn("SourceDrift", result.stderr)
                self.assertEqual(path.read_text(), drift)


if __name__ == "__main__":
    unittest.main()
