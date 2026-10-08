#!/usr/bin/env python3
"""ADR 0028 Amendment D section 9 comparator: exact SHA-256, zero tolerance.

Crops the guest's raw BGRX scanout to the content viewport printed by WEB's
`web: viewport` marker, converts to row-major RGB8, and compares against the
owner-approved presentation hash in the M93 reference manifest. Geometry comes
from the marker and must match the manifest's recorded size and scroll; the
crop is never re-blessed from a guest capture.

Usage: compare.py SCANOUT.RAW NAME
  NAME is a reference page key in the manifest (e.g. wikipedia). The serial
  log is read from $VG_SER, falling back to a sibling vm-serial-<tag>.log the
  way pixel_probe.py does. On mismatch, the actual PNG, a red diff PNG and the
  mismatched-pixel count are written under artifacts/m93g/diffs/.
"""
import hashlib
import json
import os
import re
import struct
import sys
import zlib
from pathlib import Path

SCAN_W, SCAN_H = 1280, 720
# WEB's shim window origin (user/go/browser/main.go winX/winY). The marker's
# x/y are window-local; the scanout crop adds this fixed origin.
SHIM_X, SHIM_Y = 40, 28
# Owner-authorized fixed three-pixel shim border (live-web.spec header):
# the compositor paints these columns over the presentation, so the approved
# pixels are composited with them before comparison.
BORDER_RGB = b"\x3b\x82\xf6"
BORDER_W = 3

MANIFEST = Path("user/go/webrender/testdata/golden/m93-reference.json")
GOLDEN_DIR = Path("user/go/webrender/testdata/golden")
FONT_DIR = Path("image/fonts")
ARTIFACT_DIR = Path("artifacts/m93g/diffs")


class CompareError(Exception):
    pass


def decode_png_rgb(path):
    """Decode a non-interlaced 8-bit RGB/RGBA PNG to (rgb_bytes, w, h)."""
    data = Path(path).read_bytes()
    if data[:8] != b"\x89PNG\r\n\x1a\n":
        raise CompareError("bad golden PNG signature: %s" % path)
    at, compressed, width, height, channels = 8, bytearray(), 0, 0, 0
    while at < len(data):
        size = struct.unpack_from(">I", data, at)[0]
        kind, body = data[at + 4:at + 8], data[at + 8:at + 8 + size]
        if len(body) != size or at + 12 + size > len(data):
            raise CompareError("truncated golden PNG: %s" % path)
        crc = struct.unpack_from(">I", data, at + 8 + size)[0]
        if zlib.crc32(kind + body) & 0xffffffff != crc:
            raise CompareError("golden PNG CRC mismatch: %s" % path)
        if kind == b"IHDR":
            width, height, depth, color, compression, filtering, interlace = \
                struct.unpack(">IIBBBBB", body)
            if depth != 8 or color not in (2, 6) or compression or filtering or interlace:
                raise CompareError("unsupported golden PNG shape: %s" % path)
            channels = 3 if color == 2 else 4
        elif kind == b"IDAT":
            compressed.extend(body)
        at += 12 + size
    if not width or not height:
        raise CompareError("golden PNG missing IHDR: %s" % path)
    stride = width * channels
    stream = zlib.decompressobj()
    raw = stream.decompress(bytes(compressed), (stride + 1) * height + 1)
    if len(raw) != (stride + 1) * height or not stream.eof or stream.unused_data:
        raise CompareError("golden PNG inflate mismatch: %s" % path)
    previous = bytearray(stride)
    rgb = bytearray()
    for y in range(height):
        start = y * (stride + 1)
        mode, row = raw[start], bytearray(raw[start + 1:start + 1 + stride])
        for x in range(stride):
            left = row[x - channels] if x >= channels else 0
            up = previous[x]
            corner = previous[x - channels] if x >= channels else 0
            if mode == 1:
                add = left
            elif mode == 2:
                add = up
            elif mode == 3:
                add = (left + up) // 2
            elif mode == 4:
                p = left + up - corner
                distances = (abs(p - left), abs(p - up), abs(p - corner))
                add = (left, up, corner)[distances.index(min(distances))]
            elif mode == 0:
                add = 0
            else:
                raise CompareError("bad golden PNG filter %d: %s" % (mode, path))
            row[x] = (row[x] + add) & 255
        for x in range(width):
            rgb.extend(row[x * channels:x * channels + 3])
        previous = row
    return bytes(rgb), width, height


def encode_png(rgb, width, height):
    def chunk(kind, body):
        return (struct.pack(">I", len(body)) + kind + body +
                struct.pack(">I", zlib.crc32(kind + body) & 0xffffffff))
    rows = b"".join(b"\0" + bytes(rgb[y * width * 3:(y + 1) * width * 3])
                    for y in range(height))
    return (b"\x89PNG\r\n\x1a\n" +
            chunk(b"IHDR", struct.pack(">IIBBBBB", width, height, 8, 2, 0, 0, 0)) +
            chunk(b"IDAT", zlib.compress(rows)) + chunk(b"IEND", b""))


def sha256(data):
    return hashlib.sha256(bytes(data)).hexdigest()


def marker_geometry(serial):
    rows = re.findall(
        r"^web: viewport css=1280x720 x=(\d+) y=(\d+) w=(\d+) h=(\d+) s=(\d+)$",
        serial, re.MULTILINE)
    if not rows:
        raise CompareError("missing intact web: viewport marker")
    return tuple(int(v) for v in rows[-1])


def crop_bgrx(raw, x, y, w, h):
    rgb = bytearray()
    for yy in range(y, y + h):
        for xx in range(x, x + w):
            at = (yy * SCAN_W + xx) * 4
            rgb.extend((raw[at + 2], raw[at + 1], raw[at]))
    return rgb


def composite_border(rgb, w, h):
    out = bytearray(rgb)
    for yy in range(h):
        for xx in list(range(BORDER_W)) + list(range(w - BORDER_W, w)):
            at = (yy * w + xx) * 3
            out[at:at + 3] = BORDER_RGB
    return bytes(out)


def compare(scanout_path, serial, name, manifest_path=MANIFEST,
            golden_dir=GOLDEN_DIR, font_dir=FONT_DIR,
            artifact_dir=ARTIFACT_DIR):
    raw = Path(scanout_path).read_bytes()
    if len(raw) != SCAN_W * SCAN_H * 4:
        raise CompareError("scanout size %d, want %d"
                           % (len(raw), SCAN_W * SCAN_H * 4))
    manifest = json.loads(Path(manifest_path).read_text())
    if manifest.get("Version") != 1:
        raise CompareError("manifest version is not 1")
    entry = manifest.get("Images", {}).get(name)
    if entry is None:
        raise CompareError("no approved reference entry for %r" % name)
    if not manifest.get("Verdicts", {}).get(name):
        raise CompareError("no owner verdict recorded for %r" % name)
    for font, want in manifest.get("Fonts", {}).items():
        path = Path(font_dir) / font
        if not path.exists() or sha256(path.read_bytes()) != want:
            raise CompareError("font bytes drifted from approved manifest: %s" % font)
    wx, wy, w, h, scroll = marker_geometry(serial)
    if (w, h, scroll) != (entry["Width"], entry["Height"], entry["Scroll"]):
        raise CompareError("marker geometry %dx%d s=%d does not match manifest %dx%d s=%d"
                           % (w, h, scroll, entry["Width"], entry["Height"], entry["Scroll"]))
    x, y = SHIM_X + wx, SHIM_Y + wy
    if x < 0 or y < 0 or x + w > SCAN_W or y + h > SCAN_H:
        raise CompareError("crop %d,%d %dx%d escapes the scanout" % (x, y, w, h))
    golden_rgb, gw, gh = decode_png_rgb(Path(golden_dir) / ("m93-%s-presentation.png" % name))
    if (gw, gh) != (entry["Width"], entry["Height"]):
        raise CompareError("golden size %dx%d, want %dx%d" % (gw, gh, entry["Width"], entry["Height"]))
    if sha256(golden_rgb) != entry["Presentation"]:
        raise CompareError("golden PNG does not match approved manifest hash for %r" % name)
    expected = composite_border(golden_rgb, w, h)
    actual = crop_bgrx(raw, x, y, w, h)
    actual_hash, want_hash = sha256(actual), sha256(expected)
    print("M93g reference %s: crop=%d,%d %dx%d s=%d actual=%s want=%s"
          % (name, x, y, w, h, scroll, actual_hash, want_hash))
    if actual_hash == want_hash:
        return {"name": name, "sha256": actual_hash, "x": x, "y": y, "w": w, "h": h}
    count = sum(1 for i in range(0, len(actual), 3)
                if actual[i:i + 3] != expected[i:i + 3])
    dest = Path(artifact_dir)
    dest.mkdir(parents=True, exist_ok=True)
    stem = "%s-%s" % (Path(scanout_path).stem, name)
    (dest / (stem + "-actual.png")).write_bytes(encode_png(actual, w, h))
    diff = bytearray()
    for i in range(0, len(actual), 3):
        diff.extend(b"\xff\0\0" if actual[i:i + 3] != expected[i:i + 3] else b"\0\0\0")
    (dest / (stem + "-diff.png")).write_bytes(encode_png(diff, w, h))
    raise CompareError("reference %r RGB hash mismatch (zero tolerance), "
                       "mismatched pixels=%d, diffs in %s" % (name, count, dest))


def main():
    if len(sys.argv) != 3:
        raise SystemExit("usage: compare.py SCANOUT.RAW NAME")
    scanout = sys.argv[1]
    serial_path = os.environ.get("VG_SER")
    if not serial_path:
        tag = re.search(r"-(\d+)-", Path(scanout).name)
        if tag:
            serial_path = str(Path(scanout).parent / ("vm-serial-" + tag.group(1) + ".log"))
    if not serial_path or not Path(serial_path).exists():
        raise SystemExit("cannot locate the run's serial log (set VG_SER)")
    try:
        compare(scanout, Path(serial_path).read_text(errors="replace"), sys.argv[2])
    except CompareError as exc:
        raise SystemExit(str(exc))


if __name__ == "__main__":
    main()
