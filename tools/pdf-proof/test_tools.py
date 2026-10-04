import json
import re
import struct
import unittest
from pathlib import Path

import compare
import corpus
import oracle
from check_run import check_memory, parse_receipts


def bitmap(w, h, pixels):
    return b"PDF1"+struct.pack("<III", w, h, w)+bytes(pixels)


class ComparatorTests(unittest.TestCase):
    def test_ppm_bytes_are_not_whitespace_tokens(self):
        b = compare.ppm(b"P6\n# pinned\n1 1\n255\n"+bytes([10, 32, 13]))
        self.assertEqual(b, bitmap(1, 1, [13, 32, 10, 255]))
        for suffix in (b"", b"1234"):
            with self.assertRaises(ValueError):
                compare.ppm(b"P6\n1 1\n255\n"+suffix)

    def test_exact_dimensions_alpha_and_length(self):
        b = bitmap(1, 1, [1, 2, 3, 255])
        compare.compare(b, b, mode="exact")
        for wrong in (b[:-1], b+b"\0", bitmap(1, 1, [1, 2, 3, 254]),
                      bitmap(2, 1, [1, 2, 3, 255]*2)):
            with self.assertRaises(ValueError):
                compare.compare(b, wrong)

    def test_edge_thresholds_and_oracle_only_mask(self):
        pixels = bytearray([255, 255, 255, 255]*400)
        for y in range(20):
            for x in range(10):
                pixels[(y*20+x)*4:(y*20+x)*4+3] = b"\0\0\0"
        reference = bitmap(20, 20, pixels)
        tested = pixels.copy()
        tested[4*(10*20+9)] = 64
        compare.compare(reference, bitmap(20, 20, tested))
        tested[4*(10*20+9)] = 65
        with self.assertRaisesRegex(ValueError, ">64"):
            compare.compare(reference, bitmap(20, 20, tested))
        tested = pixels.copy()
        tested[4*(10*20+3)] = 1
        with self.assertRaisesRegex(ValueError, "non-edge"):
            compare.compare(reference, bitmap(20, 20, tested))
        tested = pixels.copy()
        tested[4*(10*20+9)] = 1
        with self.assertRaisesRegex(ValueError, "exact discrepancy"):
            compare.compare(reference, bitmap(20, 20, tested), strict_rectangles=[[9, 0, 10, 20]])

    def test_mean_inclusive_and_plus_one(self):
        p = bytearray([128, 128, 128, 255, 129, 129, 129, 255]*2)
        ref = bitmap(2, 2, p)
        q = p.copy()
        for i in range(4):
            for c in range(3):
                q[4*i+c] += 1
        compare.compare(ref, bitmap(2, 2, q))
        q[0] += 1
        with self.assertRaisesRegex(ValueError, "mean"):
            compare.compare(ref, bitmap(2, 2, q))


class ManifestTests(unittest.TestCase):
    def test_authored_hashes_and_finite_corpus(self):
        manifest = json.loads((corpus.FIXTURES/"manifest.json").read_text())
        self.assertEqual(manifest["corpus"], "M89-PDF1")
        self.assertEqual({r["id"] for r in manifest["accepted"]}, set(corpus.anchors()))
        self.assertEqual(len(manifest["accepted"]), 16)
        for row in manifest["accepted"]+manifest["negatives"]:
            data = (corpus.FIXTURES/row["file"]).read_bytes()
            self.assertLessEqual(len(data), 16384)
            self.assertEqual(row["sha256"], corpus.sha(data))
        for name, data in corpus.anchors().items():
            self.assertEqual(data, (corpus.FIXTURES/(name+".pdf")).read_bytes())
        for name, (data, code) in corpus.negatives().items():
            self.assertEqual(data, (corpus.FIXTURES/("negative-"+name+".pdf")).read_bytes())

    def test_recipe_pins(self):
        recipe = json.loads((corpus.FIXTURES/"recipes.json").read_text())
        self.assertEqual(recipe["generator_sha256"], corpus.sha(Path(corpus.__file__).read_bytes()))
        for group, products in (("maxima", corpus.maxima()), ("capacities", corpus.capacities())):
            for row in recipe[group]:
                self.assertEqual(row["sha256"], corpus.sha(products[row["id"]][0]))
                self.assertEqual(row["bytes"], len(products[row["id"]][0]))
        self.assertEqual(len(corpus.maxima()["source-4mib"][0]), 4194304)

    def test_coverage_and_exclusion_rows(self):
        coverage = json.loads((corpus.FIXTURES/"coverage.json").read_text())
        self.assertEqual([r["row"] for r in coverage["exclusions"]], list(range(1, 19)))
        engine_tests = "\n".join(p.read_text() for p in (corpus.ROOT/"user/go/pdf").glob("*_test.go"))
        adapter_tests = "\n".join(p.read_text() for p in (corpus.ROOT/"user/go/pdfproof").glob("*_test.go"))
        known = set(corpus.anchors()) | set(corpus.negatives())
        recipes = set(corpus.maxima()) | set(corpus.capacities())
        for row in coverage["features"]+coverage["exclusions"]+coverage["boundaries"]:
            self.assertTrue(any(row.get(k) for k in ("fixtures", "recipes", "engine_tests", "adapter_tests")))
            for name in row.get("fixtures", []): self.assertIn(name, known)
            for name in row.get("recipes", []): self.assertIn(name, recipes)
            for name in row.get("engine_tests", []): self.assertRegex(engine_tests, r"func "+name+r"\(")
            for name in row.get("adapter_tests", []): self.assertRegex(adapter_tests, r"func "+name+r"\(")

    def test_oracle_closed_to_negatives_and_fixed_thresholds(self):
        frozen = json.loads((corpus.FIXTURES/"oracle.json").read_text())
        self.assertEqual(frozen["provenance"]["binary_sha256"], oracle.PIN)
        self.assertEqual(frozen["provenance"]["invocation"], oracle.FLAGS)
        self.assertFalse(frozen["provenance"]["negatives_sent_to_oracle"])
        ids = {row["id"] for row in frozen["references"]}
        self.assertEqual(ids, set(corpus.anchors()) | set(corpus.maxima()) |
                         {"capacity-"+name for name, (_, c) in corpus.capacities().items() if c == "OK"})
        for row in frozen["references"]:
            self.assertNotIn(row["id"], corpus.negatives())
            self.assertEqual(row["invocation"], oracle.FLAGS)


class RuntimeReceiptTests(unittest.TestCase):
    def receipt(self, pages=3072, regions=12, reaped=1):
        return (f"runtime-receipt: pid=7 name=PDFPROOF.ELF peak_pages={pages} page_cap=4096 "
                f"peak_regions={regions} region_cap=16 static_pages=8617 page_tracking=extensible "
                f"page_saturated={int(pages >= 4096)} total_pages={pages} "
                f"record_failures=0 unrecorded_pages=0 reaped={reaped}\n")

    def test_inclusive_memory_and_true_overflow(self):
        check_memory(parse_receipts(self.receipt(), 8617))
        for pages, regions in ((3073, 12), (3072, 13), (15000, 8)):
            with self.assertRaisesRegex(ValueError, "MemoryLimit"):
                check_memory(parse_receipts(self.receipt(pages, regions), 8617))

    def test_missing_false_or_unreaped_counters_fail(self):
        for old, new in (("page_tracking=extensible ", ""), ("record_failures=0", "record_failures=1"),
                         ("unrecorded_pages=0", "unrecorded_pages=1"), ("page_saturated=0", "page_saturated=1"),
                         ("reaped=1", "reaped=0"), ("static_pages=8617", "static_pages=1")):
            with self.assertRaises(ValueError):
                parse_receipts(self.receipt().replace(old, new), 8617)

    def test_reuse_brackets_one_process_without_growth(self):
        serial = self.receipt(reaped=0)+self.receipt()
        check_memory(parse_receipts(serial, 8617, runtime=True))
        with self.assertRaisesRegex(ValueError, "growth"):
            check_memory(parse_receipts(self.receipt(pages=3071, reaped=0)+self.receipt(), 8617, runtime=True))
        with self.assertRaisesRegex(ValueError, "different processes"):
            parse_receipts(serial.replace("pid=7", "pid=8", 1), 8617, runtime=True)


if __name__ == "__main__":
    unittest.main()
