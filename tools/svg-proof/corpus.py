"""Small authored exclusion recipes; large bound fixtures are never committed."""
from pathlib import Path
import hashlib
import json

FIXTURES = Path(__file__).resolve().parents[2] / "tests/fixtures/svg/acceptance"

ICONS = "rect ellipse polygon lines quadratic cubic nonzero evenodd affine group viewport alpha".split()
MAXIMUM = ["max-fill", "max-curves"]
CONSUMERS = "curves nonzero evenodd affine clip alpha page padding empty".split()


def document(body, attrs=""):
    return f'<svg width="64" height="64" {attrs}>{body}</svg>'.encode()


def exclusions():
    cases = {}
    def add(name, body, code=2, attrs=""):
        cases[name] = (document(body, attrs), code)
        # fill=none and zero fill alpha are supported hidden content, unlike
        # CSS display/visibility, which themselves must be refused.
        if body and not body.startswith("<!"):
            cases[name+"-hidden"] = (document('<g fill="none">'+body+"</g>"), code)
            cases[name+"-zero"] = (document('<g fill-opacity="0">'+body+"</g>"), code)
    for tag in ("text", "tspan", "textPath", "font", "font-face", "glyph"):
        add(tag, f"<{tag}/>", 3 if tag in ("text", "tspan", "textPath", "font", "font-face", "glyph") else 2)
    for attr in ("font-family", "font-size", "font-weight"):
        add(attr, f'<rect width="1" height="1" {attr}="sans"/>', 3)
    for tag in ("line", "polyline", "marker", "linearGradient", "radialGradient",
                "stop", "pattern", "defs", "use", "symbol", "clipPath", "mask",
                "filter", "feGaussianBlur", "animate", "animateTransform",
                "animateMotion", "set", "script", "style", "unknown"):
        add(tag, f"<{tag}/>")
    add("nested-svg", '<svg width="1" height="1"/>')
    for attr, value in {
        "stroke": "red", "stroke-width": "1", "stroke-linecap": "round",
        "stroke-linejoin": "round", "stroke-dasharray": "1 2", "marker-start": "none",
        "vector-effect": "non-scaling-stroke", "filter": "none", "onload": "x",
        "onclick": "x", "style": "fill:red", "class": "x", "opacity": ".5",
        "display": "none", "visibility": "hidden", "mix-blend-mode": "multiply",
        "paint-order": "stroke", "fill": "currentColor", "unknown": "x",
    }.items():
        add(attr, f'<rect width="1" height="1" {attr}="{value}"/>')
    add("inherit", '<rect width="1" height="1" fill="inherit"/>')
    add("arc", '<path d="M0 0A1 1 0 0 0 2 2"/>')
    add("arc-relative", '<path d="M0 0a1 1 0 0 0 2 2"/>')
    for name in ("rotate", "skewX", "skewY"):
        add(name, f'<g transform="{name}(1)"/>')
    for unit in ("%", "cm", "em", "pt"):
        add("unit-"+unit.replace("%", "percent"), f'<rect width="1{unit}" height="1"/>')
    add("color-function", '<rect width="1" height="1" fill="rgb(0,0,0)"/>')
    add("color-name", '<rect width="1" height="1" fill="orange"/>')
    add("aspect", "", attrs='preserveAspectRatio="xMinYMin slice"')
    add("version", "", attrs='version="1.1"')
    add("foreign-namespace", "", attrs='xmlns="urn:foreign"')
    add("prefix", '<x:path d="M0 0"/>')
    for tag in ("image", "foreignObject", "a"):
        add(tag, f"<{tag}/>", 4)
    for attr in ("href", "xlink:href", "src", "srcset", "resource", "xml:base"):
        add("resource-"+attr.replace(":", "-"), f'<g {attr}="x"/>', 4)
    for name, value in (("url", "url(#x)"), ("data", "data:image/png;base64,AA"),
                        ("file", "file:///x"), ("http", "https://example.invalid/x")):
        add("resource-"+name, f'<rect width="1" height="1" fill="{value}"/>', 4)
    for name, body in (("dtd", "<!DOCTYPE svg>"), ("entity-definition", '<!ENTITY x "a">'),
                       ("cdata", "<![CDATA[x]]>"), ("processing", "<?other x?>")):
        add(name, body, 5)
    for name, body in (("entity", "<title>&bogus;</title>"), ("mismatch", "<g></path>"),
                       ("duplicate", '<g id="x" id="y"/>'), ("path-group", '<path d="M0"/>'),
                       ("negative-radius", '<circle r="-1"/>'), ("negative-extent", '<rect width="-1" height="1"/>'),
                       ("nonfinite", '<circle r="NaN"/>')):
        add(name, body, 1)
    add("coordinate", '<circle r="32769"/>', 15)
    add("exponent", '<circle r="1e999"/>', 15)
    cases["utf8"] = (document("<title>")+b"\xff</title></svg>", 1)
    return cases


def boundaries():
    cases = {}
    def pair(name, low, high, high_code):
        cases[name+"-limit"] = (low, 0)
        cases[name+"-over"] = (high, high_code)
    pair("source", document("") + b" "*(1048576-len(document(""))),
         document("") + b" "*(1048577-len(document(""))), 6)
    pair("depth", document("<g>"*31+"</g>"*31), document("<g>"*32+"</g>"*32), 7)
    pair("nodes", document("<g/>"*4095), document("<g/>"*4096), 8)
    pair("paints", document('<path d=""/>'*1024), document('<path d=""/>'*1025), 17)
    pair("commands", document('<path d="M0 0 '+'L0 0 '*8190+'Z"/>'),
         document('<path d="M0 0 '+'L0 0 '*8191+'Z"/>'), 11)
    pair("contours", document('<path d="'+'M0 0Z '*1024+'"/>'),
         document('<path d="'+'M0 0Z '*1025+'"/>'), 12)
    pair("id", document('<g id="'+'a'*64+'"/>'), document('<g id="'+'a'*65+'"/>'), 10)
    pair("number", document('<circle r="'+'0'*32+'"/>'), document('<circle r="'+'0'*33+'"/>'), 10)
    pair("transforms-element", document('<g transform="'+'translate(0) '*16+'"/>'),
         document('<g transform="'+'translate(0) '*17+'"/>'), 10)
    body = ('<g transform="'+'translate(0) '*16+'"/>')*16
    pair("transforms-document", document(body), document(body+'<g transform="scale(1)"/>'), 10)
    pair("coordinate", document('<path d="M32768 -32768"/>'),
         document('<path d="M32768.00000001 -32768"/>'), 15)
    pair("coefficient", document('<g transform="scale(256)"/>'),
         document('<g transform="scale(256.00000001)"/>'), 15)
    pair("translation", document('<g transform="translate(32768)"/>'),
         document('<g transform="translate(32768.00000001)"/>'), 15)
    pair("canvas", b'<svg width="1024" height="1024"/>', b'<svg width="1025" height="1024"/>', 16)
    # Attribute totals are unreachable under the closed public allowlist;
    # these public documents must refuse unsupported names, not invent
    # acceptance at an unreachable bound. Scanner-only bounds stay M90b.
    for n in (16, 17):
        cases[f"attributes-{n}"] = (document('<g '+' '.join(f'a{i}="x"' for i in range(n))+'/>'), 2)
    return cases


def materialize(destination):
    destination = Path(destination)
    destination.mkdir(parents=True, exist_ok=True)
    pinned = json.loads((FIXTURES / "negatives.json").read_text())
    cases = {name: (bytes.fromhex(row["source_hex"]), row["code"]) for name, row in pinned.items()}
    if cases != exclusions():
        raise ValueError("exclusion recipe drift")
    bounds = boundaries()
    pins = json.loads((FIXTURES / "boundaries.json").read_text())
    if pins != {name: {"sha256": hashlib.sha256(source).hexdigest(), "bytes": len(source), "code": code}
                for name, (source, code) in bounds.items()}:
        raise ValueError("boundary recipe drift")
    cases.update(bounds)
    rows = []
    for i, (name, (source, code)) in enumerate(cases.items()):
        # Short stable transport names fit the guest argv and filename caps.
        filename = f"n{i:03}.svg"
        (destination / filename).write_bytes(source)
        rows.append(f"{filename}\t{code}\t{name}\n")
    (destination / "negative.tsv").write_text("".join(rows))
    return cases


def pin():
    # Only small authored recipes/checksums are committed. Source-cap fixtures
    # are generated byte-for-byte from these recipes under ignored artifacts.
    (FIXTURES / "negatives.json").write_text(json.dumps({
        name: {"source_hex": source.hex(), "code": code}
        for name, (source, code) in exclusions().items()}, indent=2)+"\n")
    (FIXTURES / "boundaries.json").write_text(json.dumps({
        name: {"sha256": hashlib.sha256(source).hexdigest(), "bytes": len(source), "code": code}
        for name, (source, code) in boundaries().items()}, indent=2)+"\n")
