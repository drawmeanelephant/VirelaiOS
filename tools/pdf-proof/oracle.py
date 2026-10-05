"""Freeze/check the exact installed host oracle. Ordinary generation cannot repin."""
import json
import os
import re
import subprocess
import sys
from pathlib import Path

from analytic import reference
from compare import check_clip_image, crosscheck, page
from corpus import FIXTURES, ROOT, generate, sha

MANIFEST = FIXTURES / "oracle.json"
SOURCE = ROOT / "tools/pdf-proof/cgrender.swift"
BINARY = ROOT / ".build/pdf-proof/cgrender"
INVOCATION = ["cgrender", "INPUT.pdf", "OUTPUT.bgra", "PAGE"]
CONTRACT = {
    "page": "one-based; Rotate=0",
    "box": "CropBox intersect MediaBox",
    "scale": "96/72",
    "context": "8-bit DeviceRGB; little-endian premultiplied-first",
    "background": "opaque white",
    "antialiasing": True,
    "image_interpolation": "none",
    "output": "PDF1 header; packed opaque BGRA; top-down rows",
    "environment": {"CG_PDF_VERBOSE": "1"},
}
BLANK_DIAGNOSTICS = {
    "source-4mib", "capacity-pages-limit", "capacity-depth-limit",
    "capacity-objects-limit", "capacity-source-limit",
}
CONTENTS_DIAGNOSTIC = "CGPDFContentStreamCreate caught exception: invalid `Contents': not a stream or array."
# Verbose CoreGraphics also prints lifecycle/cache traces with pointer values.
# They are not diagnostics or stable provenance; retain the raw log in artifacts
# and exclude only these exact known trace forms from the per-row diagnostics.
TRACE_LINES = {
    "[+] Creating CGPDFDocument", "[+] PDFDocumentCore created.",
    "[+] CGPDFDocumentImpl created.", "Lazily parsing Group attributes for page 1.",
    "Parsing Optional Content Properties.", "[!] CGPDFDocumentImpl destroying...",
    "[!] Page 1: Dying document being invalidated", "[-] CGPDFDocumentImpl destroyed.",
    "[-] PDFDocumentCore destroyed.",
}
TRACE_PATTERNS = (
    r"\[\+\] PDFPageCore for page \d+ created\.",
    r"\[\+\] CGPDFPageImpl for page \d+ created\.",
    r"Creating PDFResources for page \d+\.",
    r"\[-\] Finalizing CGPDFDocument 0x[0-9a-f]+",
    r"\[-\] CGPDFPageImpl for page \d+ destroyed\.",
    r"\[-\] PDFPageCore for page \d+ destroyed\.",
    r"\[i\] CACHE MISS: Lazily creating CGImageRef with key \{\d+, \d+, \d+\}, mutextIndex = \d+",
    r"CGPDFImage\(0x[0-9a-f]+\): Creating image with subsample_factor = 1",
    r"\[i\] Releasing cached image: 0x[0-9a-f]+",
)


def provenance():
    return {
        "renderer": "Apple CoreGraphics (host OS only)",
        "macos_product": subprocess.check_output(["/usr/bin/sw_vers", "-productVersion"], text=True).strip(),
        "macos_build": subprocess.check_output(["/usr/bin/sw_vers", "-buildVersion"], text=True).strip(),
        "swift_version": subprocess.check_output(["/usr/bin/swift", "--version"], text=True,
                                               stderr=subprocess.STDOUT).strip(),
        "tool_source": SOURCE.relative_to(ROOT).as_posix(),
        "tool_source_sha256": sha(SOURCE.read_bytes()),
        "recipe_sha256": sha((FIXTURES / "recipes.json").read_bytes()),
        "font_policy": "document Type 3 outlines only; no named/system fonts",
        "colour_policy": "DeviceGray/DeviceRGB default Decode; no ICC profiles",
        "invocation": INVOCATION, "invocation_sha256": sha(json.dumps(INVOCATION).encode()),
        "render_contract": CONTRACT, "negatives_sent_to_oracle": False,
    }


def check():
    expected = json.loads(MANIFEST.read_text())
    if expected["provenance"] != provenance():
        raise ValueError("BLOCKED: OracleDrift (CoreGraphics OS/compiler/source/contract)")
    return expected


def build():
    """System Swift, no package manager or acquisition; all output is ignored."""
    BINARY.parent.mkdir(parents=True, exist_ok=True)
    subprocess.run(["/usr/bin/swiftc", "-O", "-module-cache-path",
                    str(BINARY.parent / "module-cache"), str(SOURCE), "-o", str(BINARY)],
                   check=True)
    return BINARY


def diagnostics(name, stderr):
    """Only the five authored absent-Contents rows may emit the known warning."""
    messages = []
    for line in stderr.splitlines():
        line = line.strip()
        if not line or line in TRACE_LINES or any(re.fullmatch(p, line) for p in TRACE_PATTERNS):
            continue
        messages.append(line)
    if messages and (name not in BLANK_DIAGNOSTICS or messages != [CONTENTS_DIAGNOSTIC]):
        raise ValueError("UnexpectedReferenceDiagnostic: " + name + ": " + "; ".join(messages))
    return messages


def references(out, *, freeze=False):
    out.mkdir(parents=True, exist_ok=True)
    manifest = json.loads((FIXTURES / "manifest.json").read_text())
    prov = provenance() if freeze else check()["provenance"]
    binary = build()
    maxima = generate(out / "sources")
    rows = list(manifest["accepted"])
    for name, (data, dims, policy) in maxima.items():
        rows.append({"id": name, "file": str(out/"sources"/(name+".pdf")),
                     "sha256": sha(data), "dimensions": list(dims), "page": 1,
                     "box": [0, 0, 768, 1086 if dims[1] == 1448 else 1152] if dims[0] == 1024 else [0, 0, 48, 48],
                     "background": "ffffffff", "comparison": policy})
    recipe = json.loads((FIXTURES/"recipes.json").read_text())
    for capacity in recipe["capacities"]:
        if capacity["expected"] != "OK":
            continue
        name = capacity["id"]
        dims, box = [64, 64], [0, 0, 48, 48]
        if name == "canvas-width-limit":
            dims, box = [1024, 64], [0, 0, 768, 48]
        if name == "canvas-height-limit":
            dims, box = [64, 1536], [0, 0, 48, 1152]
        rows.append({"id": "capacity-"+name, "file": str(out/"sources/capacity"/(name+".pdf")),
                     "sha256": capacity["sha256"], "dimensions": dims, "box": box,
                     "page": 1, "background": "ffffffff", "comparison": "exact"})
    products, failures = [], []
    env = {k: v for k, v in os.environ.items() if k not in ("DYLD_LIBRARY_PATH", "DYLD_INSERT_LIBRARIES", "DYLD_FALLBACK_LIBRARY_PATH")}
    env.update(CONTRACT["environment"])
    for row in rows:
        src = Path(row["file"]) if row["id"] in maxima or row["id"].startswith("capacity-") else FIXTURES/row["file"]
        if sha(src.read_bytes()) != row["sha256"]:
            raise ValueError("SourceDrift: oracle input")
        dest = out/row["id"]
        rendered = subprocess.run([str(binary), str(src), str(dest.with_suffix(".bgra")), str(row["page"])],
                                  env=env, capture_output=True, text=True, check=True, timeout=30)
        dest.with_suffix(".coregraphics.log").write_text(rendered.stderr)
        messages = diagnostics(row["id"], rendered.stderr)
        if rendered.stdout:
            raise ValueError("UnexpectedReferenceDiagnostic: stdout: " + row["id"])
        bgra = dest.with_suffix(".bgra").read_bytes()
        w, h, pixels = page(bgra)
        rgb = bytearray(w*h*3)
        rgb[0::3], rgb[1::3], rgb[2::3] = pixels[2::4], pixels[1::4], pixels[0::4]
        pp = f"P6\n{w} {h}\n255\n".encode() + rgb
        dest.with_suffix(".ppm").write_bytes(pp)
        if [w, h] != row["dimensions"]:
            raise ValueError("oracle dimension mismatch")
        analytic, partial, strict, band = reference(row["id"])
        dest.with_suffix(".analytic.bgra").write_bytes(analytic)
        diagnostic = crosscheck(analytic, bgra, partial=partial, boundary_band=band, validate=False)
        try:
            check_clip_image(analytic, bgra, strict, band)
        except ValueError as exc:
            diagnostic["geometry_failure"] = diagnostic["geometry_failure"] or str(exc)
        if diagnostic["geometry_failure"]:
            failures.append(row["id"]+": "+diagnostic["geometry_failure"])
        invocation = INVOCATION[:-1] + [str(row["page"])]
        products.append({k: row[k] for k in ("id", "sha256", "page", "box", "dimensions", "background", "comparison")}
                        | {"ppm_sha256": sha(pp), "bgra_sha256": sha(bgra),
                           "invocation": invocation, "invocation_sha256": sha(json.dumps(invocation).encode()),
                           "diagnostics": messages,
                           "analytic_sha256": sha(analytic), "outside_reference_crosscheck": diagnostic})
    actual = {"version": 3, "corpus": "M89-PDF1", "provenance": prov, "references": products}
    if freeze:
        MANIFEST.write_text(json.dumps(actual, indent=2)+"\n")
    elif actual != json.loads(MANIFEST.read_text()):
        raise ValueError("BLOCKED: reference drift")
    if failures:
        raise ValueError("OracleGeometryMismatch (references retained, not acceptance): "+"; ".join(failures))
    return actual


if __name__ == "__main__":
    if sys.argv[1] == "check":
        check()
    elif sys.argv[1] in ("freeze", "generate"):
        references(Path(sys.argv[2]).resolve(), freeze=sys.argv[1] == "freeze")
    else:
        raise SystemExit("usage: oracle.py check | freeze/generate OUTPUT")
