"""ADR 0041 §6 comparator. Constants are acceptance rules, not CLI options."""
import hashlib

MAX_CHANNEL = 64
MEAN_CHANNEL = 1


def rgba(bgra):
    if len(bgra) % 4:
        raise ValueError("partial BGRA word")
    return [(bgra[i+2], bgra[i+1], bgra[i], bgra[i+3])
            for i in range(0, len(bgra), 4)]


def premult(pixel):
    r, g, b, a = pixel
    return ((r*a+127)//255, (g*a+127)//255, (b*a+127)//255, a)


def compare(actual, reference, width, height):
    size = width * height * 4
    if width <= 0 or height <= 0 or len(actual) != size or len(reference) != size:
        raise ValueError("missing bitmap or dimensions mismatch")
    a, r = rgba(actual), rgba(reference)
    ap, rp = list(map(premult, a)), list(map(premult, r))
    edge = bytearray(width*height)
    for y in range(height):
        for x in range(width):
            i = y*width+x
            if 0 < r[i][3] < 255:
                edge[i] = 1
            for nx, ny in ((x+1, y), (x, y+1)):
                if nx < width and ny < height:
                    j = ny*width+nx
                    if rp[i] != rp[j]:
                        edge[i] = edge[j] = 1
    dilation = bytearray(edge)
    for y in range(height):
        for x in range(width):
            if edge[y*width+x]:
                for ny in range(max(0, y-1), min(height, y+2)):
                    for nx in range(max(0, x-1), min(width, x+2)):
                        dilation[ny*width+nx] = 1
    total = maximum = changed = 0
    for i, (p, q) in enumerate(zip(ap, rp)):
        diff = [abs(u-v) for u, v in zip(p, q)]
        total += sum(diff)
        maximum = max(maximum, *diff)
        if any(diff):
            changed += 1
            if not dilation[i]:
                raise ValueError(f"discrepancy outside oracle edge dilation at {i%width},{i//width}")
        # Exact straight pixels in flat opaque interiors. Transparent flat
        # output is exact too, so unobservable RGB cannot hide wrong output.
        if not edge[i] and r[i][3] in (0, 255) and a[i] != r[i]:
            raise ValueError(f"nonexact flat/interior pixel at {i%width},{i//width}")
    if maximum > MAX_CHANNEL or total > size * MEAN_CHANNEL:
        raise ValueError(f"edge thresholds exceeded: max={maximum}, total={total}, channels={size}")
    return {"max_channel": maximum, "sum_channel": total, "channels": size,
            "mean_channel": total/size, "changed_pixels": changed,
            "actual_sha256": hashlib.sha256(actual).hexdigest(),
            "reference_sha256": hashlib.sha256(reference).hexdigest()}
