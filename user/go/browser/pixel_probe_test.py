import hashlib
import unittest
from pathlib import Path

import pixel_probe


class PixelProbeTests(unittest.TestCase):
    def test_roundtrip_and_one_pixel(self):
        rgb = bytes((0x12, 0x34, 0x56)) * (512*288)
        path = Path(__file__).parent / "testdata/presentation/png-grid.png"
        decoded = pixel_probe.decode_png(path)
        self.assertEqual(len(decoded), len(rgb))
        import tempfile
        with tempfile.TemporaryDirectory() as directory:
            file = Path(directory) / "roundtrip.png"
            file.write_bytes(pixel_probe.encode_png(rgb))
            self.assertEqual(pixel_probe.decode_png(file), rgb)
        changed = bytearray(rgb)
        changed[123] ^= 1
        self.assertNotEqual(hashlib.sha256(rgb).digest(), hashlib.sha256(changed).digest())

    def test_wrong_size(self):
        import tempfile
        with tempfile.TemporaryDirectory() as directory:
            file = Path(directory) / "small.png"
            file.write_bytes(pixel_probe.encode_png(b"\0\0\0", 1, 1))
            with self.assertRaises(ValueError):
                pixel_probe.decode_png(file)

    def test_shifted_content_is_not_identity(self):
        rgb = bytes(range(256)) * 1728
        shifted = rgb[3:] + rgb[:3]
        self.assertNotEqual(hashlib.sha256(rgb).digest(), hashlib.sha256(shifted).digest())


if __name__ == "__main__":
    unittest.main()
