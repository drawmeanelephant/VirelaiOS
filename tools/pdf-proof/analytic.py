"""Independent authored-geometry reference, ADR 0040 A1 / 0041 §3.2.

No PDF parsing, Go imports, renderer outputs or oracle pixels enter this module.
Coordinates are transformed before de Casteljau subdivision (1/64 px bound).
"""
import math
import struct
from functools import lru_cache

from corpus import geometry


def multiply(a, b):
    aa, ab, ac, ad, ae, af = a
    ba, bb, bc, bd, be, bf = b
    return (aa*ba+ac*bb, ab*ba+ad*bb, aa*bc+ac*bd, ab*bc+ad*bd,
            aa*be+ac*bf+ae, ab*be+ad*bf+af)


def point(m, p):
    a, b, c, d, e, f = m
    x, y = p
    return a*x+c*y+e, b*x+d*y+f


def distance(p, a, b):
    dx, dy = b[0]-a[0], b[1]-a[1]
    t = max(0, min(1, ((p[0]-a[0])*dx+(p[1]-a[1])*dy)/(dx*dx+dy*dy))) if dx or dy else 0
    return math.hypot(p[0]-a[0]-t*dx, p[1]-a[1]-t*dy)


def cubic(points, depth=0):
    # The curve is in its control polygon's convex hull. Bounding both controls
    # against the finite chord also covers degenerate/backtracking curves.
    if max(distance(p, points[0], points[3]) for p in points[1:3]) <= 1/64:
        return [points[3]]
    if depth == 20:
        raise ValueError("analytic curve failed 1/64 px bound")
    a, b, c, d = points
    mid = lambda p, q: ((p[0]+q[0])/2, (p[1]+q[1])/2)
    ab, bc, cd = mid(a, b), mid(b, c), mid(c, d)
    abc, bcd = mid(ab, bc), mid(bc, cd)
    center = mid(abc, bcd)
    return cubic((a, ab, abc, center), depth+1)+cubic((center, bcd, cd, d), depth+1)


def contours(path, matrix):
    result, current = [], []
    for command in path:
        verb, *values = command
        points = [point(matrix, values[i:i+2]) for i in range(0, len(values), 2)]
        if verb == "M":
            if current:
                result.append(current)
            current = points
        elif verb == "L":
            current.extend(points)
        elif verb == "C":
            current.extend(cubic((current[-1], *points)))
        elif verb == "Z":
            result.append(current)
            current = []
        else:
            raise ValueError("unknown analytic path verb")
    if current:
        result.append(current)
    return result


def spans(edges, y, rule):
    crossings = {}
    for a, b in edges:
        if min(a[1], b[1]) <= y < max(a[1], b[1]):
            x = a[0]+(y-a[1])*(b[0]-a[0])/(b[1]-a[1])
            crossings[x] = crossings.get(x, 0)+(1 if b[1] > a[1] else -1)
    winding, start = 0, None
    for x, delta in sorted(crossings.items()):
        before = bool(winding if rule == "nonzero" else winding % 2)
        winding += delta
        after = bool(winding if rule == "nonzero" else winding % 2)
        if not before and after:
            start = x
        elif before and not after:
            yield start, x


def raster_fill(pixels, partial, w, h, path, matrix, color, rule, clip):
    if rule not in ("nonzero", "evenodd"):
        raise ValueError("unknown analytic winding rule")
    edges = [(a, b) for contour in contours(path, matrix) if contour
             for a, b in zip(contour, contour[1:]+contour[:1])]
    if not edges:
        return
    x0, y0, x1, y1 = clip
    ymin = max(y0, math.floor(min(a[1] for a, _ in edges)))
    ymax = min(y1, math.ceil(max(a[1] for a, _ in edges)))
    previous, covered = None, None
    for y in range(ymin, ymax):
        subrows = tuple(tuple(spans(edges, y+offset, rule)) for offset in (1/8, 3/8, 5/8, 7/8))
        if subrows != previous:
            coverage = [0.0]*w
            for subrow in subrows:
                for left, right in subrow:
                    left, right = max(x0, left), min(x1, right)
                    for x in range(max(x0, math.floor(left)), min(x1, math.ceil(right))):
                        coverage[x] += max(0, min(x+1, right)-max(x, left))
            covered = [(x, min(255, math.floor(c*255/4+.5)))
                       for x, c in enumerate(coverage) if c > 0]
            previous = subrows
        for x, alpha in covered:
            i = y*w+x
            if alpha == 255:
                pixels[4*i:4*i+3] = bytes(color[::-1])
                partial[i] = 0
            elif alpha:
                partial[i] = 1
                for channel, source in enumerate(color[::-1]):
                    pixels[4*i+channel] = (source*alpha+pixels[4*i+channel]*(255-alpha)+127)//255


def boundary(mask, w, h, bounds):
    """Pixels whose centers are at most one Chebyshev pixel from the boundary."""
    x0, y0, x1, y1 = bounds
    for left, top, right, bottom in ((x0-1, y0-1, x0+1, y1+1),
                                     (x1-1, y0-1, x1+1, y1+1),
                                     (x0-1, y0-1, x1+1, y0+1),
                                     (x0-1, y1-1, x1+1, y1+1)):
        begin, end = max(0, math.ceil(left-.5)), min(w, math.floor(right-.5)+1)
        for y in range(max(0, math.ceil(top-.5)), min(h, math.floor(bottom-.5)+1)):
            if begin < end:
                mask[y*w+begin:y*w+end] = b"\1"*(end-begin)


def path_bounds(path, matrix):
    pts = [p for contour in contours(path, matrix) for p in contour]
    bounds = (min(p[0] for p in pts), min(p[1] for p in pts),
              max(p[0] for p in pts), max(p[1] for p in pts))
    return bounds


def integral_bounds(path, matrix):
    bounds = path_bounds(path, matrix)
    if any(n != int(n) for n in bounds):
        raise ValueError("nonintegral analytic clip/image")
    return tuple(map(int, bounds))


def intersection(a, b):
    x0, y0, x1, y1 = max(a[0], b[0]), max(a[1], b[1]), min(a[2], b[2]), min(a[3], b[3])
    return x0, y0, max(x0, x1), max(y0, y1)


def text_fills(op):
    state = op["state"].copy()
    tm, lm = state["text_matrix"], state["line_matrix"]
    font = op["font"]
    translate = lambda x, y: (1, 0, 0, 1, x, y)
    for action in op["actions"]:
        verb, *args = action
        if verb == "Tm":
            tm = lm = args
        elif verb in ("Td", "TD", "next"):
            if verb == "next":
                args = [0, -state["leading"]]
            elif verb == "TD":
                state["leading"] = -args[1]
            tm = lm = multiply(lm, translate(*args))
        elif verb == "spacing":
            state["word_spacing"], state["spacing"] = args
        elif verb == "adjust":
            tm = multiply(tm, translate(-args[0]/1000*state["size"]*state["scale"]/100, 0))
        elif verb == "show":
            for char in args[0]:
                glyph = font["glyphs"][char]
                size, scale = state["size"], state["scale"]/100
                trm = (size*scale, 0, 0, size, 0, state["rise"])
                matrix = multiply(op["ctm"], multiply(tm, multiply(trm, font["matrix"])))
                yield glyph["path"], matrix, op["color"]
                advance = point(font["matrix"], (glyph["advance"], 0))[0]
                displacement = (advance*size+state["spacing"]+
                                (state["word_spacing"] if char == " " else 0))*scale
                tm = multiply(tm, translate(displacement, 0))
        else:
            raise ValueError("unknown analytic text action")


def render(recipe):
    x0, y0, x1, y1 = recipe["box"]
    w, h = round((x1-x0)*4/3), round((y1-y0)*4/3)
    mapping = (4/3, 0, 0, -4/3, -x0*4/3, y1*4/3)
    pixels = bytearray(b"\xff"*(w*h*4))
    partial, strict, band = bytearray(w*h), bytearray(w*h), bytearray(w*h)
    clip, full = (0, 0, w, h), (0, 0, w, h)
    for op in recipe["operations"]:
        kind = op["kind"]
        matrix = multiply(mapping, op.get("ctm", (1, 0, 0, 1, 0, 0)))
        if kind == "reset_clip":
            clip = full
        elif kind == "clip":
            bounds = integral_bounds(op["path"], matrix)
            boundary(band, w, h, bounds)
            clip = intersection(clip, bounds)
            # Both sides of every clip boundary remain exact, including pixels
            # in the vector tolerance band, not just its painted interior.
            strict[:] = b"\1"*(w*h)
        elif kind == "fill":
            raster_fill(pixels, partial, w, h, op["path"], matrix, op["color"], op["rule"], clip)
        elif kind == "text":
            for path, ctm, color in text_fills(op):
                raster_fill(pixels, partial, w, h, path, multiply(mapping, ctm), color, "nonzero", clip)
        elif kind == "image":
            bounds = integral_bounds([("M", 0, 0), ("L", 1, 0), ("L", 1, 1), ("L", 0, 1)], matrix)
            boundary(band, w, h, bounds)
            # Source-cell boundaries are part of nearest-neighbor geometry,
            # not vector edges. Constant images have no internal boundaries.
            if "solid" not in op:
                for sy in range(op["height"]):
                    for sx in range(op["width"]):
                        sw, sh = op["width"], op["height"]
                        cell = [("M", sx/sw, sy/sh), ("L", (sx+1)/sw, sy/sh),
                                ("L", (sx+1)/sw, (sy+1)/sh), ("L", sx/sw, (sy+1)/sh)]
                        boundary(band, w, h, path_bounds(cell, matrix))
            # The unpainted exterior edge is exact too.
            for y in range(max(0, bounds[1]-1), min(h, bounds[3]+1)):
                left, right = max(0, bounds[0]-1), min(w, bounds[2]+1)
                if left < right:
                    strict[y*w+left:y*w+right] = b"\1"*(right-left)
            left, top, right, bottom = intersection(clip, intersection(full, bounds))
            a, _, _, d, e, f = matrix
            sw, sh, channels = op["width"], op["height"], op["channels"]
            for y in range(top, bottom):
                # Floor selects the higher source index at an exact cell tie,
                # even under axis reflection. Source row 0 is local y=1.
                sy = min(sh-1, max(0, math.floor((1-(y+.5-f)/d)*sh)))
                for x in range(left, right):
                    sx = min(sw-1, max(0, math.floor((x+.5-e)/a*sw)))
                    start = (sy*sw+sx)*channels
                    color = op["solid"] if "solid" in op else op["samples"][start:start+channels]
                    if channels == 1:
                        color = color*3
                    i = y*w+x
                    pixels[4*i:4*i+3] = bytes(color[::-1])
                    partial[i] = 0
        else:
            raise ValueError("unknown analytic operation")
    return b"PDF1"+struct.pack("<III", w, h, w)+pixels, partial, strict, band


@lru_cache(maxsize=64)
def reference(name):
    return render(geometry(name))
