import json
import re
import struct
import tempfile
import unittest
from pathlib import Path
from unittest.mock import patch

import compare
import corpus
import oracle
import analytic
import pause
from check_run import check_memory, check_pause_order, parse_receipts


def bitmap(w, h, pixels):
    return b"PDF1"+struct.pack("<III", w, h, w)+bytes(pixels)


class ComparatorTests(unittest.TestCase):
    def test_glyph_shift_two_pixels_fails(self):
        recipe = corpus.geometry("type3-position")
        reference, partial, strict, _ = analytic.render(recipe)
        shifted = json.loads(json.dumps(recipe))
        shifted["operations"][0]["actions"][0][5] += 1.5  # 96 dpi: two pixels.
        actual, _, _, _ = analytic.render(shifted)
        with self.assertRaises(ValueError):
            compare.compare(reference, actual, partial=partial, strict=strict)

    def test_one_changed_interior_pixel_fails(self):
        reference, partial, strict, _ = analytic.reference("gray-rect")
        actual = bytearray(reference)
        actual[16+4*(50*64+10)] ^= 1
        with self.assertRaisesRegex(ValueError, "non-edge/exact"):
            compare.compare(reference, actual, partial=partial, strict=strict)

    def test_changed_disagreement_count_or_coordinates_fails(self):
        reference, partial, _, band = analytic.reference("nonzero")
        actual = bytearray(reference)
        actual[16+4*(16*64+16)] = 181
        record = compare.crosscheck(reference, actual, partial=partial, boundary_band=band)
        for field, value in (("count", 2), ("coordinates_sha256", "0"*64)):
            changed = json.loads(json.dumps(record))
            changed["disagreements"][field] = value
            with self.assertRaisesRegex(ValueError, "disagreement count/coordinates"):
                compare.crosscheck(reference, actual, partial=partial, boundary_band=band, expected=changed)

    def test_outside_reference_geometry_checks_fail_not_tolerate(self):
        reference, partial, _, band = analytic.reference("gray-rect")
        actual = bytearray(reference)
        actual[16+4*(50*64+10)] ^= 1
        with self.assertRaises(ValueError):
            compare.crosscheck(reference, actual, partial=partial, boundary_band=band)
        empty, partial, _, band = analytic.reference("empty")
        actual = bytearray(empty)
        actual[16+4*(10*64+10)] = 254
        with self.assertRaisesRegex(ValueError, "edge outside"):
            compare.crosscheck(empty, actual, partial=partial, boundary_band=band)
        glyphs, partial, _, band = analytic.reference("type3-position")
        with self.assertRaisesRegex(ValueError, "analytic edge outside"):
            compare.crosscheck(glyphs, empty, partial=partial, boundary_band=band)

    def test_outside_reference_clip_image_interior_cannot_use_edge_tolerance(self):
        reference, _, strict, band = analytic.reference("gray-image")
        actual = bytearray(reference)
        i = 30*64+10
        self.assertFalse(band[i])
        actual[16+4*i] ^= 1
        with self.assertRaisesRegex(ValueError, "outside boundary band"):
            compare.check_clip_image(reference, actual, strict, band)

    def test_clip_image_pixels_are_exact_even_inside_edge_band(self):
        for name, x, y in (("rect-clip", 16, 0), ("gray-image", 36, 28), ("rgb-image", 20, 28)):
            reference, partial, strict, _ = analytic.reference(name)
            actual = bytearray(reference)
            actual[16+4*(y*64+x)] ^= 1
            with self.assertRaisesRegex(ValueError, "exact discrepancy"):
                compare.compare(reference, actual, partial=partial, strict=strict)

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
        for row in manifest["accepted"]:
            self.assertEqual(row["analytic"], json.loads(json.dumps(corpus.geometry(row["id"]))))
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
                if group == "maxima" or row["expected"] == "OK":
                    name = row["id"] if group == "maxima" else "capacity-"+row["id"]
                    self.assertEqual(row["analytic"], json.loads(json.dumps(corpus.geometry(name))))
        self.assertEqual(len(corpus.maxima()["source-4mib"][0]), 4194304)

    def test_split_boundaries_and_preserved_negative_bytes(self):
        data = corpus.anchors()["split-streams"]
        streams = re.findall(rb"\nstream\n(.*?)\nendstream", data, re.S)[:4]
        self.assertEqual(len(streams), 4)
        whitespace_delimiters = b"\0\t\n\f\r ()<>[]{}/%"
        for left, right in zip(streams, streams[1:]):
            self.assertTrue(left[-1] in whitespace_delimiters or right[0] in whitespace_delimiters)
        self.assertTrue(streams[0].endswith(b"1 0 "))  # operands
        self.assertTrue(streams[1].endswith(b"q 0 1 0 rg "))  # path and q frame
        self.assertTrue(streams[2].endswith(b"(A) "))  # open BT and pending Tj
        original = corpus.simple(streams=[corpus.stream(b"1 0 0 r"),
                                         corpus.stream(b"g 3 3 24 24 re q 0 "),
                                         corpus.stream(b"1 0 rg Q f")])
        self.assertEqual(corpus.negatives()["split-token"], (original, "MalformedContent"))

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
        self.assertEqual(frozen["provenance"]["tool_source_sha256"], corpus.sha(oracle.SOURCE.read_bytes()))
        self.assertEqual(frozen["provenance"]["invocation"], oracle.INVOCATION)
        self.assertEqual(frozen["provenance"]["render_contract"], oracle.CONTRACT)
        self.assertEqual(frozen["provenance"]["recipe_sha256"],
                         corpus.sha((corpus.FIXTURES/"recipes.json").read_bytes()))
        self.assertFalse(frozen["provenance"]["negatives_sent_to_oracle"])
        ids = {row["id"] for row in frozen["references"]}
        self.assertEqual(ids, set(corpus.anchors()) | set(corpus.maxima()) |
                         {"capacity-"+name for name, (_, c) in corpus.capacities().items() if c == "OK"})
        for row in frozen["references"]:
            self.assertNotIn(row["id"], corpus.negatives())
            self.assertEqual(row["invocation"], oracle.INVOCATION[:-1]+[str(row["page"])])
            self.assertEqual(row["invocation_sha256"], corpus.sha(json.dumps(row["invocation"]).encode()))
            self.assertEqual(oracle.diagnostics(row["id"], "\n".join(row["diagnostics"])), row["diagnostics"])
            self.assertRegex(row["analytic_sha256"], r"^[0-9a-f]{64}$")
            for key in ("disagreements", "clip_image_disagreements", "outside_band_disagreements"):
                self.assertGreaterEqual(row["outside_reference_crosscheck"][key]["count"], 0)
                self.assertRegex(row["outside_reference_crosscheck"][key]["coordinates_sha256"], r"^[0-9a-f]{64}$")


class CoreGraphicsTests(unittest.TestCase):
    def test_provenance_drift_never_repins(self):
        frozen = json.loads(oracle.MANIFEST.read_text())
        before = oracle.MANIFEST.read_bytes()
        for key in ("macos_product", "macos_build", "swift_version", "tool_source_sha256", "recipe_sha256",
                    "invocation", "invocation_sha256", "render_contract"):
            with self.subTest(key=key):
                changed = frozen["provenance"] | {key: "drift"}
                with patch.object(oracle, "provenance", return_value=changed):
                    with self.assertRaisesRegex(ValueError, "OracleDrift"):
                        oracle.check()
                self.assertEqual(oracle.MANIFEST.read_bytes(), before)
        with patch.object(oracle, "provenance", return_value=frozen["provenance"]):
            self.assertEqual(oracle.check(), frozen)

    def test_provenance_records_system_identities_and_source(self):
        identities = {"-productVersion": "27.2\n", "-buildVersion": "26B5091g\n",
                      "--version": "system Swift identity\n"}
        with patch.object(oracle.subprocess, "check_output",
                          side_effect=lambda args, **kw: identities[args[1]]) as command:
            prov = oracle.provenance()
        self.assertEqual(prov["macos_product"], "27.2")
        self.assertEqual(prov["macos_build"], "26B5091g")
        self.assertEqual(prov["swift_version"], "system Swift identity")
        self.assertEqual(prov["tool_source_sha256"], corpus.sha(oracle.SOURCE.read_bytes()))
        self.assertEqual(prov["invocation_sha256"], corpus.sha(json.dumps(oracle.INVOCATION).encode()))
        self.assertEqual([c.args[0] for c in command.call_args_list], [
            ["/usr/bin/sw_vers", "-productVersion"], ["/usr/bin/sw_vers", "-buildVersion"],
            ["/usr/bin/swift", "--version"],
        ])

    def test_only_absent_contents_rows_may_have_one_known_diagnostic(self):
        self.assertEqual(len(oracle.BLANK_DIAGNOSTICS), 5)
        rows = corpus.maxima() | {"capacity-"+k: v for k, v in corpus.capacities().items() if v[1] == "OK"}
        for name, product in rows.items():
            self.assertEqual(b"/Contents" not in product[0], name in oracle.BLANK_DIAGNOSTICS)
            self.assertEqual(oracle.diagnostics(name, ""), [])
            if name in oracle.BLANK_DIAGNOSTICS:
                self.assertEqual(oracle.diagnostics(name, oracle.CONTENTS_DIAGNOSTIC),
                                 [oracle.CONTENTS_DIAGNOSTIC])
            else:
                with self.assertRaisesRegex(ValueError, "UnexpectedReferenceDiagnostic"):
                    oracle.diagnostics(name, oracle.CONTENTS_DIAGNOSTIC)
        for name in oracle.BLANK_DIAGNOSTICS | set(corpus.anchors()):
            for message in ("unexpected error", "[!] unexpected warning",
                            oracle.CONTENTS_DIAGNOSTIC+"\n"+oracle.CONTENTS_DIAGNOSTIC):
                with self.assertRaisesRegex(ValueError, "UnexpectedReferenceDiagnostic"):
                    oracle.diagnostics(name, message)
        for name in corpus.anchors():
            with self.assertRaisesRegex(ValueError, "UnexpectedReferenceDiagnostic"):
                oracle.diagnostics(name, oracle.CONTENTS_DIAGNOSTIC)

    def test_verbose_lifecycle_trace_is_not_a_diagnostic(self):
        trace = ("[+] Creating CGPDFDocument\n"
                 "    [+] PDFDocumentCore created.\n"
                 "[-] Finalizing CGPDFDocument 0x1234abcd\n"
                 "  [!] Page 1: Dying document being invalidated\n")
        self.assertEqual(oracle.diagnostics("empty", trace), [])
        self.assertEqual(oracle.diagnostics("source-4mib", trace+oracle.CONTENTS_DIAGNOSTIC),
                         [oracle.CONTENTS_DIAGNOSTIC])

    def test_renderer_source_is_in_proof_lock(self):
        import lock
        self.assertEqual(lock.snapshot(corpus.ROOT)["tools/pdf-proof/cgrender.swift"],
                         corpus.sha(oracle.SOURCE.read_bytes()))


class AnalyticTests(unittest.TestCase):
    def test_winding_holes_and_document_order(self):
        for name, xy, color in (("nonzero", (16, 16), [255]*3),
                                ("evenodd", (20, 20), [255]*3),
                                ("rgb-order", (30, 40), [255, 0, 0])):
            ref, _, _, _ = analytic.reference(name)
            w, _, p = compare.page(ref)
            i = xy[1]*w+xy[0]
            self.assertEqual(list(p[4*i:4*i+3]), color)

    def test_sampler_subrows_and_rounding(self):
        recipe = {"box": [0, 0, 3, 3], "operations": [
            corpus.fill(corpus.rect(0, 2.625, .375, .375))]}
        ref, partial, _, _ = analytic.render(recipe)
        _, _, pixels = compare.page(ref)
        # Two subrows, exactly half a horizontal pixel: coverage 1/4.
        self.assertEqual(list(pixels[:4]), [191, 191, 191, 255])
        self.assertEqual(partial[0], 1)

    def test_image_center_tie_chooses_higher_source_index_under_reflection(self):
        for ctm in ([.75, 0, 0, .75, 0, 0], [-.75, 0, 0, .75, .75, 0]):
            ref, _, strict, _ = analytic.render({"box": [0, 0, .75, .75], "operations": [
                corpus.placed_image([17, 99], ctm, width=2, height=1)]})
            self.assertEqual(compare.page(ref)[2], bytes([99, 99, 99, 255]))
            self.assertEqual(strict, b"\1")

    def test_independent_text_and_line_matrices(self):
        op = corpus.type3([("Tm", 1, 0, 0, 1, 3, 36), ("show", "AA"),
                           ("Td", 9, -9), ("show", "A")])
        fills = list(analytic.text_fills(op))
        self.assertEqual([f[1][4:6] for f in fills], [(3, 36), (9, 36), (12, 27)])

    def test_cubic_bound_including_backtracking(self):
        points = [(0, 0), (100, 0), (-100, 0), (1, 0)]
        flattened = [points[0]]+analytic.cubic(points)
        self.assertGreater(len(flattened), 2)
        for t in (i/1000 for i in range(1001)):
            p = tuple((1-t)**3*points[0][c]+3*(1-t)**2*t*points[1][c]+
                      3*(1-t)*t*t*points[2][c]+t**3*points[3][c] for c in (0, 1))
            self.assertLessEqual(min(analytic.distance(p, a, b)
                                     for a, b in zip(flattened, flattened[1:])), 1/64)


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
        serial = self.receipt(reaped=0)*2+self.receipt()
        check_memory(parse_receipts(serial, 8617, runtime=True))
        with self.assertRaisesRegex(ValueError, "growth"):
            check_memory(parse_receipts(self.receipt(pages=3071, reaped=0)+
                                        self.receipt(reaped=0)+self.receipt(), 8617, runtime=True))
        with self.assertRaisesRegex(ValueError, "different processes"):
            parse_receipts(serial.replace("pid=7", "pid=8", 1), 8617, runtime=True)

    def test_cycle_50_must_match_baseline_and_final(self):
        for middle in (self.receipt(pages=3071, reaped=0),
                       self.receipt(regions=11, reaped=0),
                       self.receipt(reaped=0).replace("total_pages=3072", "total_pages=3073")):
            with self.assertRaisesRegex(ValueError, "growth"):
                check_memory(parse_receipts(self.receipt(reaped=0)+middle+self.receipt(), 8617, runtime=True))
        for middle in (self.receipt(reaped=0).replace("pid=7", "pid=8"), self.receipt()):
            with self.assertRaises(ValueError):
                parse_receipts(self.receipt(reaped=0)+middle+self.receipt(), 8617, runtime=True)
        with self.assertRaisesRegex(ValueError, "missing high-water"):
            parse_receipts(self.receipt(reaped=0)+self.receipt(), 8617, runtime=True)

    def paused_serial(self):
        return ("pdf-proof: baseline ns=1\n"+self.receipt(reaped=0)+
                "pdf-proof: resumed\npdf-proof: cycles=50 ns=50\n"+self.receipt(reaped=0)+
                "pdf-proof: resumed cycle=50\npdf-proof: cycles=100 ns=100\n"+
                "pdf-proof: complete\nprocs PDFPROOF.ELF exited status=0\n"+self.receipt())

    def test_live_rows_precede_resume_markers(self):
        serial = self.paused_serial()
        check_pause_order(serial)
        for resume in ("pdf-proof: resumed\n", "pdf-proof: resumed cycle=50\n"):
            changed = serial.replace(self.receipt(reaped=0)+resume,
                                     resume+self.receipt(reaped=0), 1)
            with self.assertRaisesRegex(ValueError, "ReceiptPauseOrder"):
                check_pause_order(changed)
        for marker in ("pdf-proof: baseline ns=1\n", "pdf-proof: cycles=50 ns=50\n",
                       "pdf-proof: resumed\n", "pdf-proof: resumed cycle=50\n"):
            for changed in (serial.replace(marker, "", 1), serial.replace(marker, marker*2, 1)):
                with self.assertRaisesRegex(ValueError, "ReceiptPauseOrder"):
                    check_pause_order(changed)

    def test_host_acknowledges_only_complete_paused_live_rows(self):
        with tempfile.TemporaryDirectory(dir=corpus.ROOT/"artifacts/m89-acceptance") as directory:
            share = Path(directory)
            (share/"PDF").mkdir()
            acknowledged = []
            baseline = "pdf-proof: baseline ns=1\n"
            pause.acknowledge(baseline+"procs receipt PDFPROOF.ELF\n", share, acknowledged)
            pause.acknowledge(baseline+self.receipt(reaped=0).rstrip("\n"), share, acknowledged)
            pause.acknowledge(baseline+self.receipt(), share, acknowledged) # reaped is not live
            self.assertEqual(acknowledged, [])
            self.assertFalse((share/"PDF/baseline.resume").exists())
            baseline += self.receipt(reaped=0)
            pause.acknowledge(baseline, share, acknowledged)
            self.assertEqual(acknowledged, ["baseline"])
            self.assertEqual((share/"PDF/baseline.resume").read_bytes(), b"1")
            middle = baseline+"pdf-proof: resumed\npdf-proof: cycles=50 ns=50\n"
            pause.acknowledge(middle, share, acknowledged)
            self.assertFalse((share/"PDF/cycle-50.resume").exists())
            middle += self.receipt(reaped=0)
            pause.acknowledge(middle, share, acknowledged)
            pause.acknowledge(middle, share, acknowledged) # non-consuming; no duplicate writes
            self.assertEqual(acknowledged, ["baseline", "cycle-50"])
            self.assertEqual((share/"PDF/cycle-50.resume").read_bytes(), b"1")

    def test_host_ack_refuses_early_resume_and_stale_ack(self):
        with tempfile.TemporaryDirectory(dir=corpus.ROOT/"artifacts/m89-acceptance") as directory:
            share = Path(directory)
            (share/"PDF").mkdir()
            serial = "pdf-proof: baseline ns=1\n"+self.receipt(reaped=0)
            with self.assertRaisesRegex(ValueError, "ReceiptPauseOrder"):
                pause.acknowledge(serial+"pdf-proof: resumed\n", share, [])
            (share/"PDF/baseline.resume").write_bytes(b"1")
            with self.assertRaisesRegex(ValueError, "ReceiptPauseDrift"):
                pause.acknowledge(serial, share, [])

    def test_final_only_receipt_never_substitutes_for_live_baseline(self):
        with self.assertRaisesRegex(ValueError, "RuntimeBaselineUnavailable"):
            parse_receipts("runtime-receipt: none\n"+self.receipt(), 8617, runtime=True)


if __name__ == "__main__":
    unittest.main()
