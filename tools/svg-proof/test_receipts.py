import json
import tempfile
import unittest
from pathlib import Path
from unittest.mock import patch

import artifacts
import ledger
import receipts


class ReceiptTests(unittest.TestCase):
    def setUp(self):
        self.report = {
            "full": {"loads": [{"memsz": 4097}, {"memsz": 20480}]},
            "consumer": {"loads": [{"memsz": 4097}, {"memsz": 20480}]},
            "fork_runtime_sha256": {"src/runtime/"+name: "pinned" for name in
                                   ("mem_sbrk.go", "malloc.go", "mstats.go", "os_virelai.go")},
            "mapping_touch_audit": {"renderer_arena": {"populated_pages": 2048},
                                   "alignment_hazard": {"padding_is_touched": True}},
        }
        verify = patch.object(ledger, "verify", return_value=self.report)
        verify.start()
        self.addCleanup(verify.stop)

    def serial(self, mode="icons", pages=3584, regions=12, static=7, total=None,
               failures=0, unrecorded=0, reaped=1, before=61121, after=61121):
        name = "CONSUMER.ELF" if mode == "consumer" else "SVG.ELF"
        total = pages if total is None else total
        return (f"pages: armed=1 total=0xfebf free={before:#x}\n"
                "timer: armed=1 ticks=0x0 freq=0x16e3600\n"
                f"exec: loaded {name}\n"
                "svg-proof: outputs closed\n"
                f"procs {name} exited status=0\n"
                "tasks user-exec reaped\n"
                f"runtime-receipt: pid=1 name={name} peak_pages={pages} "
                f"page_cap=4096 peak_regions={regions} region_cap=16 static_pages={static} "
                f"page_tracking=extensible page_saturated={int(pages >= 4096)} total_pages={total} "
                f"record_failures={failures} unrecorded_pages={unrecorded} reaped={reaped}\n"
                f"pages: armed=1 total=0xfebf free={after:#x}\n")

    def test_missing_receipt_is_failure(self):
        with tempfile.TemporaryDirectory() as tmp, patch.object(receipts, "verify", return_value=self.report):
            with self.assertRaises(FileNotFoundError):
                receipts.check("icons", tmp, "")

    def test_missing_kernel_receipt_is_failure(self):
        with tempfile.TemporaryDirectory() as tmp:
            with self.assertRaisesRegex(ValueError, "M90d"):
                ledger.pair("icons", "svg-proof: outputs closed\n", Path(tmp))

    def test_kernel_caps_and_peaks_are_not_skips(self):
        with tempfile.TemporaryDirectory() as tmp:
            for pages, regions in ((3585, 12), (3584, 13), (2047, 1), (15700, 12)):
                with self.subTest(pages=pages, regions=regions), self.assertRaisesRegex(ValueError, "bounds"):
                    ledger.pair("icons", self.serial(pages=pages, regions=regions), Path(tmp))
                saved = json.loads(Path(tmp, "runtime-ledger.json").read_text())
                self.assertEqual(saved["peak_total"]["pages"], pages)
                self.assertEqual(saved["status"], "failed")
                self.assertEqual(saved["page_tracking"]["total_pages"], pages)
                self.assertEqual(saved["physical_pages_before_after"][1]["free"], 61121)

    def test_inclusive_budgets_and_separate_static_pages(self):
        with tempfile.TemporaryDirectory() as tmp:
            for mode in ("icons", "reuse", "maximum", "consumer"):
                result = ledger.pair(mode, self.serial(mode=mode), Path(tmp))
                self.assertEqual(result["status"], "passed")
                self.assertEqual(result["runtime_upper_bound"]["pages"], 1536)
                self.assertEqual(result["runtime_upper_bound"]["regions"], 11)
                self.assertEqual(result["static_elf_pages"], 7)
            with self.assertRaisesRegex(ValueError, "bounds"):
                ledger.pair("icons", self.serial(pages=3585), Path(tmp))
            saved = json.loads(Path(tmp, "runtime-ledger.json").read_text())
            self.assertEqual(saved["runtime_upper_bound"]["pages"], 1537)

    def test_static_and_exit_receipts_cannot_be_guessed(self):
        with tempfile.TemporaryDirectory() as tmp:
            with self.assertRaisesRegex(ValueError, "static ELF"):
                ledger.pair("icons", self.serial(static=0), Path(tmp))
            with self.assertRaisesRegex(ValueError, "successful final process exit"):
                ledger.pair("icons", self.serial().replace("exited status=0", "exited status=1"), Path(tmp))

    def test_complete_extensible_tracking_is_required(self):
        with tempfile.TemporaryDirectory() as tmp:
            for field in ("failures", "unrecorded", "reaped"):
                with self.subTest(field=field), self.assertRaisesRegex(ValueError, "tracking or final reap"):
                    ledger.pair("icons", self.serial(**{field: 0 if field == "reaped" else 1}), Path(tmp))
            text = self.serial()
            for changed in (text.replace("page_tracking=extensible ", ""),
                            text.replace(" unrecorded_pages=0", ""),
                            text+text):
                with self.assertRaisesRegex(ValueError, "missing/unverifiable"):
                    ledger.pair("icons", changed, Path(tmp))
            for changed in (self.serial(total=3583),
                            text.replace("page_cap=4096", "page_cap=8192")):
                with self.assertRaisesRegex(ValueError, "inconsistent"):
                    ledger.pair("icons", changed, Path(tmp))
            with self.assertRaisesRegex(ValueError, "tracking or final reap"):
                ledger.pair("icons", text.replace("page_saturated=0", "page_saturated=1"), Path(tmp))

    def test_exact_free_pool_restoration_after_reap(self):
        with tempfile.TemporaryDirectory() as tmp:
            with self.assertRaisesRegex(ValueError, "not reclaimed exactly"):
                ledger.pair("icons", self.serial(after=61120), Path(tmp))
            text = self.serial()
            first = text.splitlines()[0]+"\n"
            last = text.splitlines()[-1]+"\n"
            for changed in (text[len(first):],
                            text[len(first):]+first,
                            text.replace("tasks user-exec reaped\n", "")+"tasks user-exec reaped\n",
                            text.replace("tasks user-exec reaped\n", ""),
                            text[:-len(last)]):
                with self.assertRaisesRegex(ValueError, "pre-exec and final reap"):
                    ledger.pair("icons", changed, Path(tmp))

    def test_stale_build_fails(self):
        with patch.object(artifacts, "source_pins", return_value={}):
            with self.assertRaisesRegex(ValueError, "stale"):
                artifacts.verify()
