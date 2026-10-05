import json
import os
import unittest
from pathlib import Path
from unittest.mock import patch

import compare
import corpus
import oracle


class ComparatorTests(unittest.TestCase):
    def test_exact_and_dimensions(self):
        self.assertEqual(compare.compare(b"\x00"*16, b"\x00"*16, 2, 2)["max_channel"], 0)
        for data in (b"", b"x"*15, b"x"*20):
            with self.assertRaises(ValueError):
                compare.compare(data, b"\x00"*16, 2, 2)

    def test_wrong_interior_and_hidden_rgb(self):
        for actual in (b"\x01\x00\x00\xff"*4, b"\x01\x00\x00\x00"*4):
            with self.assertRaises(ValueError):
                compare.compare(actual, b"\x00"*16 if actual[3] == 0 else b"\x00\x00\x00\xff"*4, 2, 2)

    def test_fixed_edge_max_and_mean(self):
        ref = bytearray(b"\x00\x00\x00\x00"*100)
        ref[4*55:4*56] = b"\xff\xff\xff\x80"
        actual = bytearray(ref)
        actual[4*55:4*56] = b"\xff\xff\xff\xc0"
        self.assertEqual(compare.compare(actual, ref, 10, 10)["max_channel"], 64)
        actual[4*55+3] = 193
        with self.assertRaises(ValueError):
            compare.compare(actual, ref, 10, 10)
        with self.assertRaises(ValueError):
            compare.compare(b"\xff\xff\xff\x82"*100, b"\xff\xff\xff\x80"*100, 10, 10)

    def test_oracle_only_edge_and_outside_dilation(self):
        ref = b"\x00\x00\x00\x00"*100
        actual = bytearray(ref)
        actual[4*55+3] = 1
        with self.assertRaisesRegex(ValueError, "outside"):
            compare.compare(actual, ref, 10, 10)


class ManifestTests(unittest.TestCase):
    def test_source_and_reference_pins(self):
        manifest = json.loads((oracle.FIXTURES / "manifest.json").read_text())
        self.assertEqual(set(manifest), set(corpus.ICONS+corpus.MAXIMUM+["viewport-meet", "consumer-curves"]))
        for name, entry in manifest.items():
            self.assertEqual(entry["source_sha256"], oracle.sha((oracle.FIXTURES / (name+".svg")).read_bytes()))
            self.assertEqual(entry["bgra_sha256"], oracle.sha((oracle.OUT / "references" / (name+".bgra")).read_bytes()))
            self.assertEqual(entry["invocation"]["unsafe"], False)
        lock = json.loads((oracle.FIXTURES / "oracle-lock.json").read_text())
        oracle.check_environment()
        self.assertEqual(lock["source_commit"], oracle.COMMIT)
        requirements = (oracle.ROOT / "tools/svg-proof/requirements.txt").read_text()
        for digest in lock["distribution_sha256"].values():
            self.assertIn("--hash=sha256:"+digest, requirements)

    def test_external_wheel_mode_keeps_all_executable_pins(self):
        frozen = json.loads((oracle.FIXTURES / "oracle-lock.json").read_text())
        wheel = dict(frozen)
        wheel["native_payload"] = dict(frozen["native_payload"])
        del wheel["source_commit"], wheel["source_setup_sha256"]
        overlay = json.loads((oracle.FIXTURES / "oracle-wheel-overlay.json").read_text())
        wheel["native_payload"].update(overlay["native_payload"])
        with patch.dict(os.environ, {"SVG_ORACLE_PROVENANCE": "wheel"}):
            self.assertEqual(oracle.check_environment(wheel), wheel)
            for key in wheel:
                with self.subTest(key=key):
                    with self.assertRaisesRegex(ValueError, "environment drift"):
                        oracle.check_environment(wheel | {key: "drift"})
            with self.assertRaisesRegex(ValueError, "environment drift"):
                oracle.check_environment(frozen) # do not claim unobserved source identity
        with patch.dict(os.environ, {"SVG_ORACLE_PROVENANCE": ""}):
            self.assertEqual(oracle.check_environment(frozen), frozen)
            with self.assertRaisesRegex(ValueError, "environment drift"):
                oracle.check_environment(wheel)

    def test_wheel_overlay_binds_original_lock_and_reference_bytes(self):
        frozen = json.loads((oracle.FIXTURES / "oracle-lock.json").read_text())
        with patch.dict(os.environ, {"SVG_ORACLE_PROVENANCE": "wheel"}):
            with patch.object(oracle, "sha", return_value="drift"):
                with self.assertRaisesRegex(ValueError, "overlay drift"):
                    oracle.check_environment(frozen)

    def test_fetch_and_font_refused(self):
        with self.assertRaises(ValueError):
            oracle.deny("file:///x")
        with self.assertRaises(ValueError):
            oracle.render(b'<svg width="1" height="1"><text>x</text></svg>', 1, 1)

    def test_manifest_drift_fails(self):
        with patch.object(oracle, "environment", return_value={}):
            with self.assertRaisesRegex(ValueError, "environment drift"):
                oracle.references()

    def test_each_exclusion_invisible_variants(self):
        cases = corpus.exclusions()
        for name in ("text", "stroke", "arc", "defs", "image", "filter", "animate", "script", "style", "unknown", "entity"):
            self.assertIn(name, cases)
            self.assertIn(name+"-hidden", cases)
            self.assertIn(name+"-zero", cases)
