import os
import struct
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
        with patch.object(Path, "read_text", return_value="different runtime"):
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


if __name__ == "__main__":
    unittest.main()
