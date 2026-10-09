#!/usr/bin/env python3
"""Host self-test for compare.py: identity, one pixel, wrong size, and a
one-pixel crop shift must each produce the right verdict (ADR 0028 D9)."""
import hashlib
import json
import tempfile
import unittest
from pathlib import Path

import compare


def make_fixture(root):
    """Build a synthetic manifest, golden PNG, fonts, serial and scanout."""
    root = Path(root)
    fonts = root / "fonts"
    fonts.mkdir()
    font_hashes = {}
    for name, byte in (("Inter-Regular.ttf", 0x11), ("FiraCode-Regular.ttf", 0x22)):
        (fonts / name).write_bytes(bytes([byte]) * 64)
        font_hashes[name] = hashlib.sha256(bytes([byte]) * 64).hexdigest()
    goldens = root / "golden"
    goldens.mkdir()
    w, h = 512, 288
    # A deterministic pattern with some structure, not a flat fill.
    rgb = bytearray()
    for y in range(h):
        for x in range(w):
            rgb.extend(((x * 7 + y) & 255, (x + y * 5) & 255, (x ^ y) & 255))
    golden_rgb = bytes(rgb)
    (goldens / "m93-widget-presentation.png").write_bytes(
        compare.encode_png(golden_rgb, w, h))
    manifest = {
        "Version": 1,
        "Fonts": font_hashes,
        "Images": {"widget": {
            "Native": "0" * 64, "Presentation": hashlib.sha256(golden_rgb).hexdigest(),
            "NativeWidth": 1280, "NativeHeight": 720, "Width": w, "Height": h,
            "Scroll": 0}},
        "Verdicts": {"widget": "Approve all 15 pairs"},
    }
    manifest_path = root / "manifest.json"
    manifest_path.write_text(json.dumps(manifest))
    serial = ("web: settled\n"
              "web: viewport css=1280x720 x=0 y=64 w=512 h=288 s=0\n"
              "web: repaint items=4\nweb: ready\n")
    # Scanout: neutral fill, then the approved pixels composited with the
    # authorized border at the marker's crop (40+0, 28+64).
    raw = bytearray(bytes((0x10, 0x20, 0x30, 0x00)) * (compare.SCAN_W * compare.SCAN_H))
    scanout = root / "snap.raw"
    return rgb, golden_rgb, manifest_path, goldens, fonts, serial, raw, scanout, w, h


def burn(raw, rgb, x0=40, y0=92, w=512, h=288):
    """Write RGB content + authorized border columns into a BGRX scanout."""
    composited = compare.composite_border(rgb, w, h)
    for yy in range(h):
        for xx in range(w):
            at = ((y0 + yy) * compare.SCAN_W + (x0 + xx)) * 4
            p = composited[(yy * w + xx) * 3:(yy * w + xx) * 3 + 3]
            raw[at:at + 4] = bytes((p[2], p[1], p[0], 0))


class CompareTests(unittest.TestCase):
    def run_compare(self, raw, serial, manifest_path, goldens, fonts,
                    artifacts, name="widget", expect_actual=None):
        with tempfile.TemporaryDirectory() as tmp:
            scanout = Path(tmp) / "snap.raw"
            Path(scanout).write_bytes(raw)
            return compare.compare(str(scanout), serial, name,
                                   manifest_path=manifest_path,
                                   golden_dir=goldens, font_dir=fonts,
                                   artifact_dir=artifacts,
                                   expect_actual=expect_actual)

    def test_identity(self):
        with tempfile.TemporaryDirectory() as tmp:
            (_, golden, manifest, goldens, fonts, serial, raw, _, w, h) = make_fixture(tmp)
            burn(raw, golden)
            out = self.run_compare(raw, serial, manifest, goldens, fonts, Path(tmp) / "diffs")
            self.assertEqual((out["w"], out["h"]), (w, h))
            self.assertEqual(out["sha256"],
                             hashlib.sha256(compare.composite_border(golden, w, h)).hexdigest())

    def test_one_pixel_off(self):
        with tempfile.TemporaryDirectory() as tmp:
            (_, golden, manifest, goldens, fonts, serial, raw, _, w, h) = make_fixture(tmp)
            burn(raw, golden)
            # Flip one interior (non-border) scanout pixel.
            at = ((92 + 100) * compare.SCAN_W + (40 + 200)) * 4
            raw[at + 2] ^= 1
            artifacts = Path(tmp) / "diffs"
            with self.assertRaises(compare.CompareError) as ctx:
                self.run_compare(raw, serial, manifest, goldens, fonts, artifacts)
            self.assertIn("mismatched pixels=1", str(ctx.exception))
            self.assertTrue(list(artifacts.glob("*-actual.png")))
            self.assertTrue(list(artifacts.glob("*-diff.png")))

    def test_wrong_size_golden(self):
        with tempfile.TemporaryDirectory() as tmp:
            (_, golden, manifest, goldens, fonts, serial, raw, _, _, _) = make_fixture(tmp)
            burn(raw, golden)
            (goldens / "m93-widget-presentation.png").write_bytes(
                compare.encode_png(golden[:256 * 3], 256, 1))
            manifest_obj = json.loads(Path(manifest).read_text())
            # Keep the manifest's recorded size; only the PNG is wrong-sized.
            Path(manifest).write_text(json.dumps(manifest_obj))
            with self.assertRaises(compare.CompareError) as ctx:
                self.run_compare(raw, serial, manifest, goldens, fonts, Path(tmp) / "d")
            self.assertIn("golden size", str(ctx.exception))

    def test_crop_shift(self):
        with tempfile.TemporaryDirectory() as tmp:
            (_, golden, manifest, goldens, fonts, serial, raw, _, w, h) = make_fixture(tmp)
            # Content written one pixel right of the marker's crop.
            burn(raw, golden, x0=41)
            with self.assertRaises(compare.CompareError) as ctx:
                self.run_compare(raw, serial, manifest, goldens, fonts, Path(tmp) / "d")
            self.assertIn("mismatch", str(ctx.exception))

    def test_geometry_mismatch(self):
        with tempfile.TemporaryDirectory() as tmp:
            (_, golden, manifest, goldens, fonts, _, raw, _, _, _) = make_fixture(tmp)
            burn(raw, golden)
            serial = "web: viewport css=1280x720 x=0 y=64 w=512 h=288 s=4\n"
            with self.assertRaises(compare.CompareError) as ctx:
                self.run_compare(raw, serial, manifest, goldens, fonts, Path(tmp) / "d")
            self.assertIn("does not match manifest", str(ctx.exception))

    def test_expect_actual_pins_guest_render(self):
        # The owner-authorized exception path: the crop must equal a pinned
        # hash even when it differs from the composited golden, and a wrong
        # pin must still go red.
        with tempfile.TemporaryDirectory() as tmp:
            (_, golden, manifest, goldens, fonts, serial, raw, _, w, h) = make_fixture(tmp)
            burn(raw, golden)
            raw[(92 * compare.SCAN_W + 40) * 4 + 2] ^= 1  # diverge from golden
            actual_hash = hashlib.sha256(
                compare.crop_bgrx(bytes(raw), 40, 92, w, h)).hexdigest()
            out = self.run_compare(raw, serial, manifest, goldens, fonts,
                                   Path(tmp) / "d1", expect_actual=actual_hash)
            self.assertEqual(out["sha256"], actual_hash)
            with self.assertRaises(compare.CompareError) as ctx:
                self.run_compare(raw, serial, manifest, goldens, fonts,
                                 Path(tmp) / "d2", expect_actual="f" * 64)
            self.assertIn("mismatch", str(ctx.exception))
            with self.assertRaises(compare.CompareError) as ctx:
                self.run_compare(raw, serial, manifest, goldens, fonts,
                                 Path(tmp) / "d3", expect_actual="nothex")
            self.assertIn("SHA-256", str(ctx.exception))

    def test_no_verdict(self):
        with tempfile.TemporaryDirectory() as tmp:
            (_, golden, manifest, goldens, fonts, serial, raw, _, _, _) = make_fixture(tmp)
            burn(raw, golden)
            obj = json.loads(Path(manifest).read_text())
            obj["Verdicts"] = {}
            Path(manifest).write_text(json.dumps(obj))
            with self.assertRaises(compare.CompareError) as ctx:
                self.run_compare(raw, serial, manifest, goldens, fonts, Path(tmp) / "d")
            self.assertIn("owner verdict", str(ctx.exception))


if __name__ == "__main__":
    unittest.main()
