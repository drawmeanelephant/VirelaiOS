import struct
import unittest
from pathlib import Path
import os
from unittest.mock import patch

from elf_inspect import compare, inspect
from ledger import audit


def elf(extra=0):
    d = bytearray(256 + extra)
    d[:7] = b"\x7fELF\x02\x01\x01"
    struct.pack_into("<HH", d, 16, 2, 183)
    struct.pack_into("<Q", d, 32, 64)
    struct.pack_into("<HH", d, 54, 56, 1)
    struct.pack_into("<IIQQQQQQ", d, 64, 1, 5, 0, 0x10000, 0, len(d), len(d), 0x10000)
    return d


class InspectTests(unittest.TestCase):
    def test_sizes_and_exact_ceiling(self):
        deps = "runtime\nvirelai/svg\nvirelai/vector\n"
        self.assertEqual(compare(elf(), elf(1_048_576), deps)["renderer_added_bytes"], 1_048_576)
        with self.assertRaises(ValueError):
            compare(elf(), elf(1_048_577), deps)

    def test_loader_and_dependency_refusals(self):
        for offset, fmt, value in [(16, "<H", 3), (18, "<H", 62), (64, "<I", 3),
                                    (68, "<I", 7), (112, "<Q", 3)]:
            d = elf()
            struct.pack_into(fmt, d, offset, value)
            with self.assertRaises(ValueError):
                inspect(d)
        with self.assertRaises(ValueError):
            compare(elf(), elf(), "virelai/svg\nvirelai/vector\nruntime/cgo\n")
        with self.assertRaises(ValueError):
            compare(elf(), elf(), "runtime\n")
        with self.assertRaises(ValueError):
            compare(elf(), elf(), "virelai/svg\nvirelai/vector\n", "runtime\nvirelai/vector\n")

class LedgerTests(unittest.TestCase):
    def test_audit_does_not_invent_runtime_receipts(self):
        root = Path(__file__).resolve().parents[2]
        fork = Path(os.environ.get("GO_FORK_DIR", root.parent / "go-virelai"))
        result = audit(root, fork)
        self.assertEqual(result["renderer_arena"]["bytes"], 8_388_608)
        self.assertEqual(result["renderer_arena"]["populated_pages"], 2048)
        self.assertEqual(result["status"], "blocked")
        self.assertFalse(result["observed_guest_run"])
        self.assertTrue(result["alignment_hazard"]["padding_is_touched"])
        self.assertFalse(result["alignment_hazard"]["fresh_padding_is_cleared"])
        self.assertEqual(result["kernel_tracking"]["inline_page_capacity"], 4096)
        self.assertNotIn("kernel_hard_cap", result)
        self.assertEqual(len(result["source_sha256"]), 9)

    def test_padding_clear_guard_and_zero_invariant_drift_refused(self):
        root = Path(__file__).resolve().parents[2]
        fork = Path(os.environ.get("GO_FORK_DIR", root.parent / "go-virelai"))
        read = Path.read_text
        mutations = (
            ("mem_sbrk.go", 'GOOS == "virelai"', 'GOOS != "virelai"'),
            ("mem_sbrk.go", "memFreeWithClear(r, l, false)", "memFree(r, l)"),
            ("mem_sbrk.go", "memFree(r, l)", "memFreeWithClear(r, l, false)"),
            ("mem_sbrk.go", "memFreeWithClear(ap, n, true)", "memFreeWithClear(ap, n, false)"),
            ("mem_sbrk.go", "memFree(unsafe.Pointer(end), endLen)", "unknownFree(end, endLen)"),
            ("mem_sbrk.go", "memclrNoHeapPointers(v, n)", "// removed shrink clear"),
            ("os_virelai.go", "blocMax = f", "// removed floor"),
            ("os_virelai.go", "virEnvBlockBytes = 16 * 128", "virEnvBlockBytes = 128"),
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
                    with self.assertRaisesRegex(ValueError, "audit source drift"):
                        audit(root, fork)

    def test_source_drift_refused(self):
        with patch.object(Path, "read_text", return_value="unsupported runtime"):
            with self.assertRaises(ValueError):
                audit(Path("."), Path("."))


if __name__ == "__main__":
    unittest.main()
