#!/usr/bin/env python3
"""Independent RIFF/sample checks and offline source-boundary regressions."""
from pathlib import Path
import struct
import unittest

import fart


def wav_samples(data):
    if len(data) < 44:
        raise ValueError("short WAV")
    header = struct.unpack("<4sI4s4sIHHIIHH4sI", data[:44])
    riff, size, wave, fmt, fmt_size, encoding, channels, hz, rate, align, bits, tag, count = header
    if (riff, wave, fmt, fmt_size, encoding, channels, hz, rate, align, bits, tag) != (
            b"RIFF", b"WAVE", b"fmt ", 16, 1, 1, 44100, 88200, 2, 16, b"data"):
        raise ValueError("noncanonical WAV")
    if size != len(data) - 8 or count != len(data) - 44 or count % 2:
        raise ValueError("invalid lengths")
    return struct.unpack("<" + "h" * (count // 2), data[44:])


def converted(data, hz, channels, fmt):
    samples = wav_samples(data)
    frames = (len(samples) * hz + 44099) // 44100
    raw = bytearray()
    for i in range(frames):
        sample = samples[i * 44100 // hz]
        value = {5: lambda: struct.pack("<h", sample),
                 17: lambda: struct.pack("<i", sample * 65536),
                 19: lambda: struct.pack("<f", sample / 32768.0)}[fmt]()
        raw += value * channels
    return bytes(raw)


class ContractTests(unittest.TestCase):
    def test_pin_and_guest_source_closure(self):
        fart.verify_sources()
        roots = [fart.ROOT / "user/zig/fart.zig", *(
            fart.ROOT / "user/zig/fart").rglob("*.zig")]
        for path in roots:
            text = path.read_text()
            for prohibited in ('@cImport', '@cInclude', 'std.posix.', 'std.process.Child',
                               '"main.zig"', '"miniaudio', '"ninjam', '"afplay"', '"say"'):
                self.assertNotIn(prohibited, text, path)

    def test_header_rejects_bad_count(self):
        data = bytearray(struct.pack("<4sI4s4sIHHIIHH4sI", b"RIFF", 38, b"WAVE", b"fmt ",
                                    16, 1, 1, 44100, 88200, 2, 16, b"data", 2) + b"\0\0")
        self.assertEqual(wav_samples(data), (0,))
        data[40] = 4
        with self.assertRaises(ValueError):
            wav_samples(data)

    def test_independent_converter_uses_samples_not_header(self):
        data = struct.pack("<4sI4s4sIHHIIHH4sI", b"RIFF", 42, b"WAVE", b"fmt ",
                           16, 1, 1, 44100, 88200, 2, 16, b"data", 6)
        data += struct.pack("<hhh", -32768, 0, 32767)
        self.assertEqual(converted(data, 44100, 1, 5), data[44:])
        self.assertEqual(converted(data, 44100, 2, 17),
                         b"".join(struct.pack("<i", n * 65536) * 2 for n in (-32768, 0, 32767)))
        self.assertEqual(converted(data, 44100, 2, 19),
                         b"".join(struct.pack("<f", n / 32768.0) * 2 for n in (-32768, 0, 32767)))


if __name__ == "__main__":
    unittest.main()
