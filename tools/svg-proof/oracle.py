"""Frozen host-only CairoSVG 2.8.2 reference preparation and verification."""
import hashlib
import importlib.metadata
import io
import json
import os
import platform
import subprocess
import sys
from pathlib import Path

from corpus import ICONS, MAXIMUM

ROOT = Path(__file__).resolve().parents[2]
FIXTURES = ROOT / "tests/fixtures/svg/acceptance"
OUT = ROOT / "artifacts/m90-acceptance"
ORACLE = Path(os.environ.get("SVG_ORACLE_DIR", OUT / "oracle")).resolve()
COMMIT = "9e8c6ede00dd1c4495fca4809b4cabd628a85eb9"
INVOCATION = {"oracle": "CairoSVG 2.8.2", "unsafe": False,
              "url_fetcher": "deny-all", "dpi": 96, "scale": 1,
              "background": "transparent", "fonts": "forbidden",
              "conversion": "PNG -> Pillow RGBA -> bytes B,G,R,A; top-down straight-alpha"}


def sha(data):
    return hashlib.sha256(data).hexdigest()


def deny(*args, **kwargs):
    raise ValueError("oracle resource access forbidden")


def environment():
    import cairocffi
    import cairosvg
    source = ORACLE / "source"
    mode = os.environ.get("SVG_ORACLE_PROVENANCE", "")
    if mode not in ("", "wheel"):
        raise ValueError("unrecognized oracle provenance mode")
    if cairosvg.__version__ != "2.8.2":
        raise ValueError("wrong oracle version")
    payload = {}
    if mode == "wheel":
        # Exact wheel and installed payload hashes remain mandatory. A missing
        # source checkout is not a verified commit/build recipe: omit those
        # two observations explicitly instead of manufacturing their identity.
        package = Path(cairosvg.__file__).parent
        for p in package.rglob("*"):
            if p.suffix == ".py" or p.name == "VERSION":
                payload["cairosvg/"+p.relative_to(package).as_posix()] = sha(p.read_bytes())
        source_license = importlib.metadata.distribution("CairoSVG").locate_file(
            "cairosvg-2.8.2.dist-info/licenses/LICENSE")
        source_provenance = {"source_license_sha256": sha(source_license.read_bytes())}
    else:
        if subprocess.check_output(["git", "-C", str(source), "rev-parse", "HEAD"], text=True).strip() != COMMIT:
            raise ValueError("wrong oracle source commit")
        for p in (source / "cairosvg").rglob("*"):
            if p.suffix == ".py" or p.name == "VERSION":
                installed = Path(cairosvg.__file__).parent / p.relative_to(source / "cairosvg")
                if installed.read_bytes() != p.read_bytes():
                    raise ValueError("oracle wheel/source mismatch")
                payload[str(p.relative_to(source))] = sha(p.read_bytes())
        source_provenance = {
            "source_commit": COMMIT,
            "source_license_sha256": sha((source / "LICENSE").read_bytes()),
            "source_setup_sha256": sha((source / "setup.cfg").read_bytes()),
        }
    packages = {}
    for name in ("CairoSVG", "cairocffi", "cffi", "cssselect2", "defusedxml",
                 "Pillow", "tinycss2", "pycparser", "webencodings"):
        dist = importlib.metadata.distribution(name)
        files = {}
        for entry in dist.files:
            if str(entry).endswith((".py", ".so")) or "license" in str(entry).lower():
                files[str(entry)] = sha(dist.locate_file(entry).read_bytes())
        packages[name] = {"version": dist.version, "payload": files}
    dylibs = {}
    pending = [Path("/opt/homebrew/opt/cairo/lib/libcairo.2.dylib")]
    while pending:
        p = pending.pop().resolve()
        key = p.name
        if key in dylibs:
            continue
        dylibs[key] = sha(p.read_bytes())
        deps = subprocess.check_output(["otool", "-L", str(p)], text=True).splitlines()[2:]
        for line in deps:
            dep = line.strip().split(" (")[0]
            if dep.startswith("/opt/homebrew/"):
                pending.append(Path(dep))
            elif dep.startswith("@"):
                raise ValueError("unresolved oracle native dependency")
            elif not dep.startswith(("/usr/lib/", "/System/Library/")):
                raise ValueError("unaccounted oracle native dependency")
    return {**source_provenance, "source_payload": payload,
            "python": platform.python_version(), "platform": platform.platform(),
            "python_executable_sha256": sha(Path(sys.executable).resolve().read_bytes()),
            "cairo_version": cairocffi.cairo_version_string(), "native_payload": dylibs,
            "distribution_sha256": {p.name: sha(p.read_bytes())
                                    for p in sorted((ORACLE / "packages").glob("*.whl"))},
            "packages": packages}


def check_environment(actual=None):
    actual = environment() if actual is None else actual
    locked = (FIXTURES / "oracle-lock.json").read_bytes()
    expected = json.loads(locked)
    mode = os.environ.get("SVG_ORACLE_PROVENANCE", "")
    if mode == "wheel":
        overlay = json.loads((FIXTURES / "oracle-wheel-overlay.json").read_text())
        if (overlay["version"] != 1 or overlay["base_lock_sha256"] != sha(locked) or
                overlay["references_manifest_sha256"] != sha((FIXTURES / "manifest.json").read_bytes()) or
                overlay["unobserved_source_fields"] != ["source_commit", "source_setup_sha256"] or
                set(overlay["native_payload"]) != {"libpng16.16.dylib"}):
            raise ValueError("oracle wheel overlay drift")
        for key in overlay["unobserved_source_fields"]:
            expected.pop(key)
        expected["native_payload"].update(overlay["native_payload"])
    elif mode:
        raise ValueError("unrecognized oracle provenance mode")
    if actual != expected:
        raise ValueError("oracle environment drift, reference preparation blocked")
    return actual


def render(source, width, height):
    from cairosvg.surface import PNGSurface
    from PIL import Image
    # Defused parsing also prevents entity/DTD access. Independent validation
    # forbids all text/resource elements before invoking Cairo's font code.
    from defusedxml import ElementTree
    tree = ElementTree.fromstring(source)
    for node in tree.iter():
        tag = node.tag.rsplit("}", 1)[-1]
        if tag not in ("svg", "g", "path", "rect", "circle", "ellipse", "polygon", "title", "desc"):
            raise ValueError("negative or unapproved oracle input")
        if any(k.startswith(("href", "on", "font")) or "url(" in v for k, v in node.attrib.items()):
            raise ValueError("oracle font/resource attribute forbidden")
    png = PNGSurface.convert(bytestring=source, unsafe=False, url_fetcher=deny,
                             dpi=96, output_width=width, output_height=height)
    image = Image.open(io.BytesIO(png)).convert("RGBA")
    if image.size != (width, height):
        raise ValueError("oracle dimensions")
    rgba = image.tobytes()
    bgra = bytearray(rgba)
    bgra[0::4], bgra[2::4] = rgba[2::4], rgba[0::4]
    return bytes(bgra), png


def references(pin=False):
    env = environment()
    lock_path = FIXTURES / "oracle-lock.json"
    if not pin:
        check_environment(env)
    elif os.environ.get("SVG_ORACLE_PROVENANCE"):
        raise ValueError("wheel provenance is verification-only; cannot repin")
    refs = OUT / "references"
    refs.mkdir(parents=True, exist_ok=True)
    entries = {}
    for name in ICONS + ["viewport-meet"] + MAXIMUM + ["consumer-curves"]:
        src = (FIXTURES / (name+".svg")).read_bytes()
        width = height = 1024 if name in MAXIMUM else 64
        bgra, png = render(src, width, height)
        (refs / (name+".bgra")).write_bytes(bgra)
        (refs / (name+".png")).write_bytes(png)
        entries[name] = {"width": width, "height": height, "source_sha256": sha(src),
                         "bgra_sha256": sha(bgra), "png_sha256": sha(png), "invocation": INVOCATION}
    manifest_path = FIXTURES / "manifest.json"
    if pin:
        lock_path.write_text(json.dumps(env, indent=2, sort_keys=True)+"\n")
        manifest_path.write_text(json.dumps(entries, indent=2, sort_keys=True)+"\n")
    elif entries != json.loads(manifest_path.read_text()):
        raise ValueError("reference pin mismatch")
    return entries


if __name__ == "__main__":
    # Pinning is an explicit authoring operation, never part of a gate.
    references(pin=sys.argv[1:] == ["--pin"])
    print("CairoSVG 2.8.2 provenance and references verified")
