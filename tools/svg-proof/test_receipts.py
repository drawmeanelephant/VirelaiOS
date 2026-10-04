import json
import tempfile
import unittest
from pathlib import Path
from unittest.mock import patch

import artifacts
import ledger
import receipts


class ReceiptTests(unittest.TestCase):
    def test_missing_receipt_is_failure(self):
        with tempfile.TemporaryDirectory() as tmp:
            with self.assertRaises(FileNotFoundError):
                receipts.check("icons", tmp, "")

    def test_missing_kernel_receipt_is_failure(self):
        with tempfile.TemporaryDirectory() as tmp:
            with self.assertRaisesRegex(ValueError, "M90d"):
                ledger.pair("icons", "svg-proof: outputs closed\n", Path(tmp))

    def test_kernel_caps_and_peaks_are_not_skips(self):
        with tempfile.TemporaryDirectory() as tmp:
            for pages, regions in ((3585, 12), (3584, 13), (2047, 1)):
                text = (f"runtime-receipt: pid=1 name=SVG.ELF peak_pages={pages} "
                        f"page_cap=4096 peak_regions={regions} region_cap=16 static_pages=1")
                with self.assertRaisesRegex(ValueError, "bounds"):
                    ledger.pair("icons", text, Path(tmp))

    def test_static_and_exit_receipts_cannot_be_guessed(self):
        with tempfile.TemporaryDirectory() as tmp:
            text = ("runtime-receipt: pid=1 name=SVG.ELF peak_pages=2048 "
                    "page_cap=4096 peak_regions=1 region_cap=16 static_pages=0")
            with self.assertRaisesRegex(ValueError, "static ELF"):
                ledger.pair("icons", text, Path(tmp))

    def test_stale_build_fails(self):
        with patch.object(artifacts, "source_pins", return_value={}):
            with self.assertRaisesRegex(ValueError, "stale"):
                artifacts.verify()
