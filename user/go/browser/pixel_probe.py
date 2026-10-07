#!/usr/bin/env python3
"""Exact host-approved M93f presentation probe, never a golden generator."""
import hashlib
import json
import os
import re
import struct
import sys
import zlib
from pathlib import Path


def decode_png(path):
    data = Path(path).read_bytes()
    if data[:8] != b"\x89PNG\r\n\x1a\n":
        raise ValueError("bad expected PNG")
    at, compressed, width, height, channels = 8, bytearray(), 0, 0, 0
    while at < len(data):
        size = struct.unpack_from(">I", data, at)[0]
        kind, body = data[at+4:at+8], data[at+8:at+8+size]
        if len(body) != size or at+12+size > len(data):
            raise ValueError("truncated expected PNG")
        crc = struct.unpack_from(">I", data, at+8+size)[0]
        if zlib.crc32(kind+body) & 0xffffffff != crc:
            raise ValueError("expected PNG CRC mismatch")
        if kind == b"IHDR":
            width, height, depth, color, compression, filtering, interlace = struct.unpack(">IIBBBBB", body)
            if depth != 8 or color not in (2, 6) or compression or filtering or interlace:
                raise ValueError("unsupported expected PNG")
            channels = 3 if color == 2 else 4
        elif kind == b"IDAT":
            compressed.extend(body)
        at += 12 + size
    if (width, height) != (512, 288):
        raise ValueError("expected PNG geometry mismatch")
    stride = width * channels
    stream = zlib.decompressobj()
    raw = stream.decompress(compressed, (stride+1)*height+1)
    if len(raw) != (stride+1)*height or not stream.eof or stream.unused_data:
        raise ValueError("expected PNG inflate mismatch")
    previous = bytearray(stride)
    rgb = bytearray()
    for y in range(height):
        start = y*(stride+1)
        mode, row = raw[start], bytearray(raw[start+1:start+1+stride])
        for x in range(stride):
            left = row[x-channels] if x >= channels else 0
            up = previous[x]
            corner = previous[x-channels] if x >= channels else 0
            if mode == 1:
                add = left
            elif mode == 2:
                add = up
            elif mode == 3:
                add = (left+up)//2
            elif mode == 4:
                p = left+up-corner
                distances = (abs(p-left), abs(p-up), abs(p-corner))
                add = (left, up, corner)[distances.index(min(distances))]
            elif mode == 0:
                add = 0
            else:
                raise ValueError("bad expected PNG filter")
            row[x] = (row[x]+add) & 255
        for x in range(width):
            rgb.extend(row[x*channels:x*channels+3])
        previous = row
    return bytes(rgb)


def encode_png(rgb, width=512, height=288):
    def chunk(kind, body):
        return (struct.pack(">I", len(body))+kind+body+
                struct.pack(">I", zlib.crc32(kind+body) & 0xffffffff))
    rows = b"".join(b"\0"+rgb[y*width*3:(y+1)*width*3] for y in range(height))
    return (b"\x89PNG\r\n\x1a\n"+
            chunk(b"IHDR", struct.pack(">IIBBBBB", width, height, 8, 2, 0, 0, 0))+
            chunk(b"IDAT", zlib.compress(rows))+chunk(b"IEND", b""))


def main():
    if len(sys.argv) != 3:
        raise SystemExit("usage: pixel_probe.py scanout.raw fixture")
    raw = Path(sys.argv[1]).read_bytes()
    if len(raw) != 1280 * 720 * 4:
        raise SystemExit("scanout size mismatch")
    manifest = json.loads((Path(__file__).parent / "testdata/presentation-expectations.json").read_text())
    if manifest["Verdict"] != "Approve all ten presentation expectations":
        raise SystemExit("missing owner approval")
    tag = re.search(r"-(\d+)-", Path(sys.argv[1]).name)
    if not tag and "VG_SER" not in os.environ:
        raise SystemExit("cannot identify snapshot boot")
    serial_path = os.environ.get("VG_SER") or str(Path(sys.argv[1]).parent / ("vm-serial-" + tag[1] + ".log"))
    serial = Path(serial_path).read_text(errors="replace")
    rows = re.findall(r"^web: viewport css=1280x720 x=(\d+) y=(\d+) w=(\d+) h=(\d+) s=(\d+)$",
                      serial, re.MULTILINE)
    if not rows:
        raise SystemExit("missing intact viewport marker")
    x, y, w, h, scroll = map(int, rows[-1])
    if (x, y, w, h, scroll) != (0, 64, 512, 288, 0):
        raise SystemExit("wrong presentation geometry: " + str(rows[-1]))
    # These legacy boots intentionally retain the raw shim at 40,28.
    x, y = x + 40, y + 28
    rgb = bytearray()
    for yy in range(y, y + h):
        for xx in range(x, x + w):
            at = (yy * 1280 + xx) * 4
            rgb.extend((raw[at + 2], raw[at + 1], raw[at]))
    actual = hashlib.sha256(rgb).hexdigest()
    expected = manifest["Fixtures"][sys.argv[2]]["Hash"]
    wanted = decode_png(Path(__file__).parent / "testdata/presentation" / (sys.argv[2]+".png"))
    if hashlib.sha256(wanted).hexdigest() != expected:
        raise SystemExit("expected PNG does not match approved manifest")
    if manifest.get("Compositor") != {
            "BorderWidth": 3, "BorderRGB": "3b82f6",
            "Verdict": "Authorize the fixed-border exact comparison"}:
        raise SystemExit("missing precise compositor comparison ruling")
    # The app-only pixels/hash stay pinned above. Compose the shim's existing
    # focus frame into the expected SCANOUT; every border pixel is still graded.
    wanted = bytearray(wanted)
    for yy in range(288):
        for xx in (0, 1, 2, 509, 510, 511):
            at = (yy*512+xx)*3
            wanted[at:at+3] = b"\x3b\x82\xf6"
    composited = hashlib.sha256(wanted).hexdigest()
    print("M93f exact presentation:", sys.argv[2], actual, "composited", composited, "app", expected)
    if actual != composited:
        diff = bytearray()
        count = 0
        for i in range(0, len(rgb), 3):
            different = rgb[i:i+3] != wanted[i:i+3]
            count += different
            diff.extend(b"\xff\0\0" if different else b"\0\0\0")
        dest = Path("artifacts/m93f/diffs") / sys.argv[2]
        dest.mkdir(parents=True, exist_ok=True)
        name = Path(sys.argv[1]).name
        (dest / (name+"-actual.png")).write_bytes(encode_png(rgb))
        (dest / (name+"-diff.png")).write_bytes(encode_png(diff))
        raise SystemExit("presentation RGB hash mismatch (zero tolerance), mismatched pixels="+str(count))


if __name__ == "__main__":
    main()
