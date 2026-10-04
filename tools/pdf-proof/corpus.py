"""Authored M89-PDF1 recipes. No downloaded documents or reference renderer."""
import hashlib
import json
import sys
import zlib
from functools import lru_cache
from pathlib import Path

ROOT = Path(__file__).resolve().parents[2]
FIXTURES = ROOT / "tests/fixtures/pdf/acceptance"


def sha(data):
    return hashlib.sha256(data).hexdigest()


def document(objects, *, trailer=b"", version=b"1.4", comment=b""):
    out = bytearray(b"%PDF-" + version + b"\n%\xe2\xe3\xcf\xd3\n" + comment)
    offsets = [0]
    for i, obj in enumerate(objects, 1):
        offsets.append(len(out))
        out += f"{i} 0 obj\n".encode() + obj + b"\nendobj\n"
    xref = len(out)
    out += f"xref\n0 {len(offsets)}\n0000000000 65535 f \n".encode()
    for offset in offsets[1:]:
        out += f"{offset:010d} 00000 n \n".encode()
    out += (f"trailer\n<< /Size {len(offsets)} /Root 1 0 R ".encode() + trailer
            + f">>\nstartxref\n{xref}\n%%EOF\n".encode())
    return bytes(out)


def stream(data=b"", keys=b""):
    return b"<< /Length " + str(len(data)).encode() + b" " + keys + b" >>\nstream\n" + data + b"\nendstream"


def flate(data, kind="dynamic"):
    if kind == "stored":
        co = zlib.compressobj(0)
    elif kind == "fixed":
        co = zlib.compressobj(9, zlib.DEFLATED, 15, 8, zlib.Z_FIXED)
    else:
        co = zlib.compressobj(9)
    return co.compress(data) + co.flush()


def simple(content=b"", *, page=b"", resources=b"", box=b"0 0 48 48", streams=None,
           extras=(), trailer=b"", version=b"1.4", comment=b""):
    if streams is None:
        streams = [stream(content)]
    refs = b" ".join(f"{4+i} 0 R".encode() for i in range(len(streams)))
    contents = refs if len(streams) == 1 else b"[" + refs + b"]"
    objs = [b"<< /Type /Catalog /Pages 2 0 R >>",
            b"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
            b"<< /Type /Page /Parent 2 0 R /MediaBox [" + box + b"] /Contents " + contents
            + b" " + page + b" " + resources + b" >>", *streams, *extras]
    return document(objs, trailer=trailer, version=version, comment=comment)


def text(content, *, glyph=None, font_extra=b"", glyph_extra=b"", streams=None):
    glyph = glyph or b"1 0 0 0 1 1 d1 0 0 m 1 0 l .5 1 l h f"
    font_id = 4 + (len(streams) if streams is not None else 1)
    font = (b"<< /Type /Font /Subtype /Type3 /FontBBox [0 0 1 1] /FontMatrix [1 0 0 1 0 0] "
            + f"/CharProcs << /A {font_id+1} 0 R /Space {font_id+2} 0 R >> ".encode()
            + b"/Encoding << /Type /Encoding "
            b"/Differences [32 /Space 65 /A] >> /FirstChar 32 /LastChar 65 /Widths ["
            + b"1 " * 34 + b"] /Resources << >> /Name /Authored " + font_extra + b" >>")
    return simple(content, streams=streams,
                  resources=f"/Resources << /Font << /F1 {font_id} 0 R >> >>".encode(),
                  extras=[font, stream(glyph + glyph_extra), stream(b"1 0 0 0 0 0 d1")])


def image(content, data, *, keys=b"", rgb=False, compressed=False, width=2, height=2, box=b"0 0 48 48"):
    color = b"DeviceRGB" if rgb else b"DeviceGray"
    ikeys = (b"/Type /XObject /Subtype /Image /Width " + str(width).encode()
             + b" /Height " + str(height).encode()
             + b" /BitsPerComponent 8 /ColorSpace /" + color + b" " + keys)
    if compressed:
        data = flate(data)
        ikeys += b" /Filter [/FlateDecode] /DecodeParms [<< /Predictor 1 >>]"
    return simple(content, resources=b"/Resources << /XObject << /I1 5 0 R >> >>",
                  extras=[stream(data, ikeys)], box=box)


IDENTITY = [1, 0, 0, 1, 0, 0]
BLACK, WHITE = [0, 0, 0], [255, 255, 255]
RED, GREEN, BLUE = [255, 0, 0], [0, 255, 0], [0, 0, 255]


def rect(x, y, w, h):
    return [("M", x, y), ("L", x+w, y), ("L", x+w, y+h),
            ("L", x, y+h), ("Z",)]


def fill(path, color=BLACK, *, ctm=IDENTITY, rule="nonzero"):
    return {"kind": "fill", "path": path, "ctm": ctm, "color": color, "rule": rule}


def type3(actions, *, size=6, spacing=0, word_spacing=0, scale=100,
          leading=0, rise=0, color=BLACK, outline=None):
    # Authored font metrics and both matrices, not parsed from the tested PDF.
    return {"kind": "text", "ctm": IDENTITY, "color": color,
            "font": {"matrix": IDENTITY, "bbox": [0, 0, 1, 1],
                     "glyphs": {"A": {"advance": 1, "d1": [1, 0, 0, 0, 1, 1],
                                     "path": outline or [("M", 0, 0), ("L", 1, 0),
                                                        ("L", .5, 1), ("Z",)]},
                                " ": {"advance": 1, "d1": [1, 0, 0, 0, 0, 0], "path": []}}},
            "state": {"size": size, "spacing": spacing, "word_spacing": word_spacing,
                      "scale": scale, "leading": leading, "rise": rise,
                      "text_matrix": IDENTITY, "line_matrix": IDENTITY},
            "actions": actions}


def placed_image(samples, ctm, *, width=2, height=2, channels=1):
    return {"kind": "image", "ctm": ctm, "width": width, "height": height,
            "channels": channels, "samples": list(samples)}


@lru_cache(maxsize=1)
def anchor_recipes():
    # Each byte recipe has a separate, explicit analytic geometry declaration.
    # The analytic consumer reads these declarations, never PDF/engine output.
    p = {
        "empty": (simple(), []),
        "gray-rect": (simple(b"0 g 3 3 12 12 re f .5 g 18 3 12 12 re F 1 g 3 3 3 3 re f"),
                      [fill(rect(3, 3, 12, 12)), fill(rect(18, 3, 12, 12), [128]*3),
                       fill(rect(3, 3, 3, 3), WHITE)]),
        "rgb-order": (simple(b"1 0 0 rg 3 3 30 30 re f 0 1 0 rg 12 12 30 30 re f 0 0 1 rg 21 3 12 30 re f"),
                      [fill(rect(3, 3, 30, 30), RED), fill(rect(12, 12, 30, 30), GREEN),
                       fill(rect(21, 3, 12, 30), BLUE)]),
        "open-lines": (simple(b"3 3 m 30 3 l 15 30 l f 33 3 m 42 3 l 42 30 l 33 30 l h F 0 0 m 48 48 l n"),
                       [fill([("M", 3, 3), ("L", 30, 3), ("L", 15, 30)]),
                        fill(rect(33, 3, 9, 27))]),
        "cubic": (simple(b"3 3 m 3 36 36 36 36 3 c h f 3 42 m 12 48 21 42 v 30 36 39 42 y h f"),
                  [fill([("M", 3, 3), ("C", 3, 36, 36, 36, 36, 3), ("Z",)]),
                   fill([("M", 3, 42), ("C", 3, 42, 12, 48, 21, 42),
                         ("C", 30, 36, 39, 42, 39, 42), ("Z",)])]),
        "nonzero": (simple(b"3 3 42 42 re 12 12 m 12 36 l 36 36 l 36 12 l h f"),
                    [fill(rect(3, 3, 42, 42)+[("M", 12, 12), ("L", 12, 36),
                                             ("L", 36, 36), ("L", 36, 12), ("Z",)])]),
        "evenodd": (simple(b"3 3 30 30 re 15 15 30 30 re f*"),
                    [fill(rect(3, 3, 30, 30)+rect(15, 15, 30, 30), rule="evenodd")]),
        "affine": (simple(b"q 1 .25 .125 1 6 6 cm 0 0 18 18 re f Q q -1 0 0 1 45 3 cm 0 0 9 9 re f Q q 0 0 0 1 0 0 cm 3 3 9 9 re f Q"),
                   [fill(rect(0, 0, 18, 18), ctm=[1, .25, .125, 1, 6, 6]),
                    fill(rect(0, 0, 9, 9), ctm=[-1, 0, 0, 1, 45, 3]),
                    fill(rect(3, 3, 9, 9), ctm=[0, 0, 0, 1, 0, 0])]),
        "save-restore": (text(b"0 0 1 rg q 1 0 0 rg 3 3 12 12 re f Q 21 3 12 12 re f 3 Tc q 9 Tc Q BT /F1 6 Tf 1 0 0 1 3 30 Tm (AA) Tj ET"),
                         [fill(rect(3, 3, 12, 12), RED), fill(rect(21, 3, 12, 12), BLUE),
                          type3([("Tm", 1, 0, 0, 1, 3, 30), ("show", "AA")],
                                spacing=3, color=BLUE)]),
        "rect-clip": (simple(b"q 1 0 0 rg 0 0 24 48 re W f 0 0 1 rg 0 0 48 48 re f 0 0 12 48 re W* n 0 1 0 rg 0 0 48 48 re f Q"),
                      [fill(rect(0, 0, 24, 48), RED),
                       {"kind": "clip", "path": rect(0, 0, 24, 48), "ctm": IDENTITY},
                       fill(rect(0, 0, 48, 48), BLUE),
                       {"kind": "clip", "path": rect(0, 0, 12, 48), "ctm": IDENTITY},
                       fill(rect(0, 0, 48, 48), GREEN)]),
        "split-streams": (text(b"", streams=[
            stream(b"1 0 "), stream(b"0 rg 3 3 m 27 3 l q 0 1 0 rg "),
            stream(b"27 27 l 3 27 l h Q f BT /F1 6 Tf 1 0 0 1 30 30 Tm (A) "),
            stream(b"Tj 0 -9 Td (A) Tj ET")]),
            [fill(rect(3, 3, 24, 24), RED),
             type3([("Tm", 1, 0, 0, 1, 30, 30), ("show", "A"),
                    ("Td", 0, -9), ("show", "A")], color=RED)]),
        "flate-content": (simple(streams=[
            stream(flate(b"0 g 3 3 9 9 re f ", "stored"), b"/Filter /FlateDecode"),
            stream(flate(b".5 g 15 3 9 9 re f ", "fixed"), b"/Filter [/FlateDecode] /DecodeParms [null]"),
            stream(flate(b"% authored dynamic coding\n" + b"% alpha alpha beta gamma delta " * 50 + b"\n1 0 0 rg 27 3 9 9 re f"),
                   b"/Filter /FlateDecode /DecodeParms << /Predictor 1 >>")]),
            [fill(rect(3, 3, 9, 9)), fill(rect(15, 3, 9, 9), [128]*3),
             fill(rect(27, 3, 9, 9), RED)]),
        "type3-position": (text(b"BT /F1 6 Tf 1 0 0 1 3 36 Tm (A) Tj 9 -9 Td (A) Tj 0 -9 TD (A) Tj T* (A) Tj ET"),
                           [type3([("Tm", 1, 0, 0, 1, 3, 36), ("show", "A"),
                                   ("Td", 9, -9), ("show", "A"), ("TD", 0, -9),
                                   ("show", "A"), ("next",), ("show", "A")])]),
        "type3-spacing": (text(b"BT /F1 4.5 Tf 1 Tc 2 Tw 100 Tz 6 TL 1.5 Ts 0 Tr 1 0 0 1 3 36 Tm (A A) Tj [(A) -250 <41>] TJ (A) ' 1 1 (AA) \" ET"),
                          [type3([("Tm", 1, 0, 0, 1, 3, 36), ("show", "A A"),
                                  ("show", "A"), ("adjust", -250), ("show", "A"),
                                  ("next",), ("show", "A"), ("spacing", 1, 1),
                                  ("next",), ("show", "AA")],
                                 size=4.5, spacing=1, word_spacing=2, leading=6, rise=1.5)]),
        "gray-image": (image(b"q 24 0 0 24 3 3 cm /I1 Do Q q -12 0 0 12 45 3 cm /I1 Do Q", bytes([0, 85, 170, 255]), keys=b"/Decode [0 1] /Interpolate false /ImageMask false"),
                       [placed_image([0, 85, 170, 255], [24, 0, 0, 24, 3, 3]),
                        placed_image([0, 85, 170, 255], [-12, 0, 0, 12, 45, 3])]),
        "rgb-image": (image(b"1 0 1 rg 0 0 48 48 re f q 3 3 36 36 re W n 24 0 0 -24 3 39 cm /I1 Do Q 0 0 0 rg 30 30 6 6 re f",
                           bytes([255, 0, 0, 0, 255, 0, 0, 0, 255, 255, 255, 0]), rgb=True, compressed=True,
                           keys=b"/Decode [0 1 0 1 0 1]"),
                      [fill(rect(0, 0, 48, 48), [255, 0, 255]),
                       {"kind": "clip", "path": rect(3, 3, 36, 36), "ctm": IDENTITY},
                       placed_image([255, 0, 0, 0, 255, 0, 0, 0, 255, 255, 255, 0],
                                    [24, 0, 0, -24, 3, 39], channels=3),
                       {"kind": "reset_clip"}, fill(rect(30, 30, 6, 6))]),
    }
    return p


def anchors():
    return {name: data for name, (data, _) in anchor_recipes().items()}


def negatives():
    # One independently authored witness per policy, including invisible data.
    return {
        "encrypted": (simple(trailer=b"/Encrypt null "), "Encrypted"),
        "structure": (simple(version=b"1.5"), "UnsupportedStructure"),
        "page": (simple(page=b"/Rotate 90"), "UnsupportedPageGeometry"),
        "stroke": (simple(b"0 0 m 1 1 l n S"), "UnsupportedStroke"),
        "clip": (simple(b"0 0 m 12 12 l 0 12 l h W n"), "UnsupportedClip"),
        "text": (simple(b"1 Tr"), "UnsupportedText"),
        "font": (simple(resources=b"/Resources << /Font << /F 5 0 R >> >>",
                        extras=[b"<< /Type /Font /Subtype /Type1 >>"]), "UnsupportedFont"),
        "glyph": (text(b"", glyph_extra=b" 1 g"), "UnsupportedGlyph"),
        "missing-glyph": (text(b"BT /F1 3 Tf (Z) Tj ET"), "MissingGlyph"),
        "inline-image": (simple(b"BI"), "UnsupportedImage"),
        "filter": (simple(streams=[stream(b"", b"/Filter /DCTDecode")]), "UnsupportedFilter"),
        "image": (image(b"", bytes([1, 2, 3, 4]), keys=b"/Interpolate true"), "UnsupportedImage"),
        "paint": (simple(b"gs"), "UnsupportedFeature"),
        "interactive": (simple(page=b"/Annots []"), "UnsupportedFeature"),
        "marked": (simple(b"BX EX"), "UnsupportedFeature"),
        "external": (simple(page=b"/Unknown null /URI (never-resolved)"), "ExternalResource"),
        "unknown": (simple(page=b"/Unlisted null"), "UnsupportedFeature"),
        "metadata-invalid": (simple(extras=[b"<< /Title 42 >>"], trailer=b"/Info 5 0 R "), "Malformed"),
        "malformed": (simple(page=b"/Rotate 0 /Rotate 0"), "Malformed"),
        "split-token": (simple(streams=[stream(b"1 0 0 r"), stream(b"g 3 3 24 24 re q 0 "),
                                       stream(b"1 0 rg Q f")]), "MalformedContent"),
        "malformed-stream": (simple(streams=[stream(b"x\x9c\x00", b"/Filter /FlateDecode")]), "MalformedStream"),
        "limit": (simple(b"q " * 17 + b"Q " * 17), "GraphicsDepthLimit"),
    }


@lru_cache(maxsize=1)
def maximum_recipes():
    # Large products live only under ignored artifacts.
    rgb = bytes([31, 95, 159]) * (1024 * 1536)
    base = document([b"<< /Type /Catalog /Pages 2 0 R >>",
                     b"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
                     b"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 48 48] >>"])
    padded = document([b"<< /Type /Catalog /Pages 2 0 R >>",
                       b"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
                       b"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 48 48] >>"],
                      comment=b"%" + b"x" * (4194304-len(base)-32) + b"\n")
    padded += b" " * (4194304-len(padded))
    time = image(b"0.5 g 0 0 768 1086 re f q 96 0 0 96 0 0 cm /I1 Do Q",
                 bytes([17, 34, 51]) * 4, rgb=True, compressed=True, box=b"0 0 768 1086")
    # A second resource supplies genuine encoded Type 3 text in the time page.
    objects = [
        b"<< /Type /Catalog /Pages 2 0 R >>", b"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
        b"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 768 1086] /Contents 4 0 R /Resources << /Font << /F1 5 0 R >> /XObject << /I1 7 0 R >> >> >>",
        stream(b".5 g 0 0 768 1086 re f BT /F1 24 Tf 1 0 0 1 96 96 Tm (A) Tj ET q 96 0 0 96 0 0 cm /I1 Do Q"),
        b"<< /Type /Font /Subtype /Type3 /FontBBox [0 0 1 1] /FontMatrix [1 0 0 1 0 0] /CharProcs << /A 6 0 R >> /Encoding << /Differences [65 /A] >> /FirstChar 65 /LastChar 65 /Widths [1] >>",
        stream(b"1 0 0 0 1 1 d1 0 0 1 1 re f"),
        stream(flate(bytes([17, 34, 51]) * 4), b"/Type /XObject /Subtype /Image /Width 2 /Height 2 /BitsPerComponent 8 /ColorSpace /DeviceRGB /Filter /FlateDecode"),
    ]
    return {
        "time-1024x1448": (document(objects), (1024, 1448), "edges",
                          [fill(rect(0, 0, 768, 1086), [128]*3),
                           type3([("Tm", 1, 0, 0, 1, 96, 96), ("show", "A")],
                                 size=24, color=[128]*3, outline=rect(0, 0, 1, 1)),
                           placed_image([17, 34, 51]*4, [96, 0, 0, 96, 0, 0], channels=3)]),
        "bottom-1024x1536": (simple(b"0 0 1 rg 0 0 768 .75 re f", box=b"0 0 768 1152"), (1024, 1536), "exact",
                            [fill(rect(0, 0, 768, .75), BLUE)]),
        "full-rgb": (image(b"768 0 0 1152 0 0 cm /I1 Do", rgb, rgb=True, compressed=True,
                           width=1024, height=1536, box=b"0 0 768 1152"), (1024, 1536), "exact",
                     [{"kind": "image", "ctm": [768, 0, 0, 1152, 0, 0], "width": 1024,
                       "height": 1536, "channels": 3, "solid": [31, 95, 159]}]),
        "source-4mib": (padded, (64, 64), "exact", []),
    }


def maxima():
    return {name: recipe[:3] for name, recipe in maximum_recipes().items()}


def geometry(name):
    if name in anchor_recipes():
        return {"box": [0, 0, 48, 48], "operations": anchor_recipes()[name][1]}
    if name in maximum_recipes():
        _, (w, h), _, operations = maximum_recipes()[name]
        return {"box": [0, 0, w*3/4, h*3/4], "operations": operations}
    if name.startswith("capacity-"):
        capacity = name[len("capacity-"):]
        if capacity not in capacities() or capacities()[capacity][1] != "OK":
            raise ValueError("no analytic reference for a negative")
        w, h = (1024, 64) if capacity == "canvas-width-limit" else (
            (64, 1536) if capacity == "canvas-height-limit" else (64, 64))
        # These capacities intentionally have no visible paints/glyphs/images.
        return {"box": [0, 0, w*3/4, h*3/4], "operations": []}
    raise ValueError("missing analytic recipe: "+name)


@lru_cache(maxsize=1)
def capacities():
    products = {}
    def pair(name, low, high, code):
        products[name+"-limit"] = (low, "OK")
        products[name+"-plus1"] = (high, code)
    pair("graphics", simple(b"q "*16+b"Q "*16), simple(b"q "*17+b"Q "*17), "GraphicsDepthLimit")
    pair("commands", simple(b"0 0 m "+b"0 0 l "*4095+b"f"),
         simple(b"0 0 m "+b"0 0 l "*4096+b"f"), "SceneLimit")
    pair("paints-contours", simple(b"0 0 m 0 0 l f "*512),
         simple(b"0 0 m 0 0 l f "*513), "SceneLimit")
    pair("canvas-width", simple(box=b"0 0 768 48"), simple(box=b"0 0 768.75 48"), "CanvasLimit")
    pair("canvas-height", simple(box=b"0 0 48 1152"), simple(box=b"0 0 48 1152.75"), "CanvasLimit")
    pair("coordinate", simple(b".5 0 0 .5 0 0 cm 32768 0 m n"),
         simple(b"32768.000001 0 m n"), "CoordinateLimit")
    pair("name", simple(b"/"+b"F"*64+b" 0 Tf", resources=b"/Resources << /Font << /"+b"F"*64+b" 5 0 R >> >>",
                        extras=[b"<< /Type /Font /Subtype /Type3 /FontBBox [0 0 0 0] /FontMatrix [1 0 0 1 0 0] /CharProcs << >> /Encoding << /Differences [] >> /FirstChar 0 /LastChar 0 /Widths [0] >>"]),
         simple(b"/"+b"F"*65+b" 0 Tf"), "TokenLimit")
    pair("number", simple(b"0"*32+b" g"), simple(b"0"*33+b" g"), "TokenLimit")
    pair("string", simple(extras=[b"<< /Title ("+b"A"*4096+b") >>"], trailer=b"/Info 5 0 R "),
         simple(extras=[b"<< /Title ("+b"A"*4097+b") >>"], trailer=b"/Info 5 0 R "), "TokenLimit")
    pair("font-size", text(b"/F1 256 Tf BT ET"), text(b"/F1 256.000001 Tf BT ET"), "UnsupportedText")
    pair("text-scale", simple(b"256 Tz"), simple(b"256.000001 Tz"), "UnsupportedText")
    pair("tj", text(b"/F1 0 Tf BT ["+b"0 "*512+b"] TJ ET"),
         text(b"/F1 0 Tf BT ["+b"0 "*513+b"] TJ ET"), "ContainerLimit")
    pair("contents", simple(streams=[stream()]*16), simple(streams=[stream()]*17), "ContainerLimit")
    for axis, low, high in (("width", (1024, 1), (1025, 1)), ("height", (1, 1536), (1, 1537))):
        pair("image-"+axis,
             image(b"", b"\0"*(low[0]*low[1]), width=low[0], height=low[1]),
             image(b"", b"\0"*(high[0]*high[1]), width=high[0], height=high[1]), "CanvasLimit")
    def page_tree(n):
        # Two child nodes keep a 257-leaf test independent of Kids' 256 cap.
        objects = [b"<< /Type /Catalog /Pages 2 0 R >>",
                   f"<< /Type /Pages /Kids [3 0 R 4 0 R] /Count {n} >>".encode(), b"", b""]
        for node, count in ((3, n//2), (4, n-n//2)):
            refs = []
            for _ in range(count):
                refs.append(f"{len(objects)+1} 0 R".encode())
                objects.append(f"<< /Type /Page /Parent {node} 0 R /MediaBox [0 0 48 48] >>".encode())
            objects[node-1] = b"<< /Type /Pages /Parent 2 0 R /Count "+str(count).encode()+b" /Kids ["+b" ".join(refs)+b"] >>"
        return document(objects)
    pair("pages", page_tree(256), page_tree(257), "PageLimit")
    def deep(n):
        objects = [b"<< /Type /Catalog /Pages 2 0 R >>"]
        for i in range(n):
            parent = f"/Parent {i+1} 0 R ".encode() if i else b""
            objects.append(b"<< /Type /Page "+parent+b"/MediaBox [0 0 48 48] >>" if i == n-1 else
                           b"<< /Type /Pages "+parent+f"/Kids [{i+3} 0 R] /Count 1 >>".encode())
        return document(objects)
    pair("depth", deep(31), deep(32), "DepthLimit")
    def objects_capacity(extra=False):
        # One small page, 16 fonts, empty outlines and individually indirect
        # Widths values reach object capacity without combining page capacity.
        objects = [b"<< /Type /Catalog /Pages 2 0 R >>",
                   b"<< /Type /Pages /Kids [3 0 R] /Count 1 >>", b""]
        fonts = []
        for i in range(16):
            font = len(objects)+1
            fonts.append(f"/F{i} {font} 0 R".encode())
            objects.append(b"")
            procs, widths = [], []
            for j in range(237 if i == 15 else 256):
                procs.append(f"/G{j} {len(objects)+1} 0 R".encode())
                objects.append(stream(b"0 0 0 0 0 0 d1"))
            for j in range(256):
                widths.append(f"{len(objects)+1} 0 R".encode())
                objects.append(b"0")
            objects[font-1] = (b"<< /Type /Font /Subtype /Type3 /FontBBox [0 0 0 0] /FontMatrix [1 0 0 1 0 0] /CharProcs << "
                               + b" ".join(procs)+b" >> /Encoding << /Differences [] >> /FirstChar 0 /LastChar 255 /Widths ["
                               + b" ".join(widths)+b"] >>")
        objects[2] = b"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 48 48] /Resources << /Font << "+b" ".join(fonts)+b" >> >> >>"
        assert len(objects) == 8192
        if extra:
            objects.append(b"null")
        return document(objects)
    pair("objects", objects_capacity(), objects_capacity(True), "ObjectLimit")
    source = maxima()["source-4mib"][0]
    pair("source", source, source+b" ", "SourceLimit")
    return products


def write_authored():
    FIXTURES.mkdir(parents=True, exist_ok=True)
    accepted = []
    for name, data in anchors().items():
        if len(data) > 16384:
            raise ValueError("SourceLimit: authored page")
        (FIXTURES / (name + ".pdf")).write_bytes(data)
        accepted.append({"id": name, "file": name + ".pdf", "sha256": sha(data),
                         "bytes": len(data), "page": 1, "box": [0, 0, 48, 48],
                         "dimensions": [64, 64], "background": "ffffffff",
                         "analytic": geometry(name),
                         "comparison": "exact" if name in {"empty", "gray-rect", "rgb-order", "rect-clip", "flate-content", "gray-image", "rgb-image"} else "edges"})
    negative = []
    for name, (data, code) in negatives().items():
        (FIXTURES / ("negative-" + name + ".pdf")).write_bytes(data)
        negative.append({"id": name, "file": "negative-" + name + ".pdf",
                         "sha256": sha(data), "expected": code})
    recipes = [{"id": name, "sha256": sha(data), "bytes": len(data), "dimensions": list(dims),
                "comparison": comparison, "analytic": geometry(name)}
               for name, (data, dims, comparison) in maxima().items()]
    boundaries = [{"id": name, "sha256": sha(data), "bytes": len(data), "expected": code,
                   **({"analytic": geometry("capacity-"+name)} if code == "OK" else {})}
                  for name, (data, code) in capacities().items()]
    (FIXTURES / "recipes.json").write_text(json.dumps({"version": 1, "generator_sha256": sha(Path(__file__).read_bytes()),
                                                     "zlib": zlib.ZLIB_VERSION, "maxima": recipes,
                                                     "capacities": boundaries}, indent=2) + "\n")
    (FIXTURES / "manifest.json").write_text(json.dumps({"corpus": "M89-PDF1", "version": 1,
                                                      "accepted": accepted, "negatives": negative}, indent=2) + "\n")


def generate(out):
    pinned = json.loads((FIXTURES / "recipes.json").read_text())
    if sha(Path(__file__).read_bytes()) != pinned["generator_sha256"] or zlib.ZLIB_VERSION != pinned["zlib"]:
        raise ValueError("SourceDrift: corpus recipe")
    out.mkdir(parents=True, exist_ok=True)
    products = maxima()
    for row in pinned["maxima"]:
        data, dims, comparison = products[row["id"]]
        if sha(data) != row["sha256"] or len(data) != row["bytes"]:
            raise ValueError("SourceDrift: maximum PDF")
        (out / (row["id"] + ".pdf")).write_bytes(data)
    capacity_dir = out/"capacity"
    capacity_dir.mkdir(exist_ok=True)
    products2 = capacities()
    for row in pinned["capacities"]:
        data, code = products2[row["id"]]
        if sha(data) != row["sha256"] or code != row["expected"] or len(data) != row["bytes"]:
            raise ValueError("SourceDrift: capacity PDF")
        (capacity_dir / (row["id"]+".pdf")).write_bytes(data)
    return products


if __name__ == "__main__":
    if sys.argv[1] == "author":
        write_authored()
    elif sys.argv[1] == "generate":
        generate(Path(sys.argv[2]))
    else:
        raise SystemExit("usage: corpus.py author | generate OUTPUT")
