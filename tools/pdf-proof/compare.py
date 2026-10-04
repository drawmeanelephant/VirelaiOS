"""ADR 0040 §6 comparison. All masks come only from the frozen oracle."""
import struct
from pathlib import Path


def ppm(data):
    """Read binary P6 exactly, including comments and a single header delimiter."""
    pos = 0
    tokens = []
    while len(tokens) < 4:
        while pos < len(data) and data[pos] in b" \t\r\n":
            pos += 1
        if pos < len(data) and data[pos] == 35:
            end = data.find(b"\n", pos)
            if end < 0:
                raise ValueError("truncated PPM comment")
            pos = end+1
            continue
        start = pos
        while pos < len(data) and data[pos] not in b" \t\r\n":
            pos += 1
        if start == pos:
            raise ValueError("truncated PPM header")
        tokens.append(data[start:pos])
    if tokens[0] != b"P6" or tokens[3] != b"255" or pos >= len(data):
        raise ValueError("unsupported PPM")
    w, h = map(int, tokens[1:3])
    if not (0 < w <= 1024 and 0 < h <= 1536):
        raise ValueError("CanvasLimit")
    pos += 2 if data[pos:pos+2] == b"\r\n" else 1
    rgb = data[pos:]
    if len(rgb) != w*h*3:
        raise ValueError("PPM payload length")
    bgra = bytearray(w*h*4)
    bgra[0::4], bgra[1::4], bgra[2::4], bgra[3::4] = rgb[2::3], rgb[1::3], rgb[0::3], b"\xff"*(w*h)
    return b"PDF1" + struct.pack("<III", w, h, w) + bgra


def page(data):
    if len(data) < 16 or data[:4] != b"PDF1":
        raise ValueError("missing PDF1 header")
    w, h, stride = struct.unpack_from("<III", data, 4)
    if not (0 < w <= 1024 and 0 < h <= 1536 and stride == w and len(data) == 16+w*h*4):
        raise ValueError("PDF1 dimensions/payload length")
    pixels = data[16:]
    if pixels[3::4] != b"\xff"*(w*h):
        raise ValueError("nonopaque alpha")
    return w, h, pixels


def oracle_edges(pixels, w, h):
    mask = bytearray(w*h)
    for y in range(h):
        for x in range(w):
            i = y*w+x
            color = pixels[4*i:4*i+3]
            for dx, dy in ((1, 0), (0, 1)):
                if x+dx < w and y+dy < h:
                    j = i+dx+dy*w
                    if color != pixels[4*j:4*j+3]:
                        mask[i] = mask[j] = 1
    dilated = bytearray(w*h)
    for i, edge in enumerate(mask):
        if edge:
            y, x = divmod(i, w)
            for yy in range(max(0, y-1), min(h, y+2)):
                for xx in range(max(0, x-1), min(w, x+2)):
                    dilated[yy*w+xx] = 1
    return dilated


def compare(reference, actual, *, mode="edges", strict_rectangles=()):
    w, h, oracle = page(reference)
    aw, ah, tested = page(actual)
    if (aw, ah) != (w, h):
        raise ValueError("dimension mismatch")
    if mode not in ("exact", "edges"):
        raise ValueError("unrecognized comparison policy")
    mask = oracle_edges(oracle, w, h) if mode == "edges" else bytearray(w*h)
    for rect in strict_rectangles:
        x0, y0, x1, y1 = rect
        if not (0 <= x0 <= x1 <= w and 0 <= y0 <= y1 <= h):
            raise ValueError("invalid strict rectangle")
        for y in range(y0, y1):
            mask[y*w+x0:y*w+x1] = b"\0"*(x1-x0)
    total = maximum = changed = 0
    for i in range(w*h):
        delta = [abs(oracle[i*4+c]-tested[i*4+c]) for c in range(3)]
        if any(delta):
            changed += 1
            if not mask[i]:
                raise ValueError(f"non-edge/exact discrepancy at {i%w},{i//w}")
            if max(delta) > 64:
                raise ValueError(f"edge channel difference >64 at {i%w},{i//w}")
        total += sum(delta)
        maximum = max(maximum, *delta)
    if total > w*h*3:
        raise ValueError("whole-image mean channel difference >1")
    return {"width": w, "height": h, "changed_pixels": changed,
            "max_channel_difference": maximum, "sum_channel_difference": total,
            "mean_denominator": w*h*3}


def compare_files(reference, actual, row):
    return compare(Path(reference).read_bytes(), Path(actual).read_bytes(),
                   mode=row["comparison"], strict_rectangles=row.get("strict_rectangles", []))
