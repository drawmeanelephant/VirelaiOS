"""Freeze/check the exact installed host oracle. Ordinary generation cannot repin."""
import json
import os
import re
import shutil
import subprocess
import sys
from pathlib import Path

from analytic import reference
from compare import check_clip_image, crosscheck, ppm
from corpus import FIXTURES, ROOT, generate, sha

PIN = "9a44364d0fa42a43c3efdb2cbab47d275973c4a5856c1109f6bf4513def6bb54"
FLAGS = ["-r", "96", "-f", "1", "-l", "1", "-singlefile", "-aa", "yes", "-aaVector", "yes"]
MANIFEST = FIXTURES / "oracle.json"


def installed():
    name = shutil.which("pdftoppm")
    if name is None:
        raise ValueError("BLOCKED: MissingOracle")
    binary = Path(name).resolve(strict=True)
    if sha(binary.read_bytes()) != PIN:
        raise ValueError("BLOCKED: OracleDrift (required installed binary SHA-256)")
    version = subprocess.run([str(binary), "-v"], capture_output=True, text=True, check=True).stderr.splitlines()[0]
    if version != "pdftoppm version 26.09.0":
        raise ValueError("BLOCKED: OracleDrift (version)")
    return binary, version


def dependencies(binary):
    pending, seen, system = [binary], {}, set()
    while pending:
        p = pending.pop().resolve(strict=True)
        if str(p) in seen:
            continue
        seen[str(p)] = sha(p.read_bytes())
        links = subprocess.check_output(["otool", "-L", str(p)], text=True).splitlines()[1:]
        for line in links:
            dep = line.strip().split(" (", 1)[0]
            if dep.startswith(("/usr/lib/", "/System/Library/")):
                system.add(line.strip())
                continue
            if dep.startswith("@rpath/"):
                dep = str(p.parent.parent / "lib" / dep[7:])
                if not Path(dep).exists():
                    dep = str(p.parent / line.strip().split(" (", 1)[0][7:])
            elif dep.startswith("@loader_path/"):
                dep = str(p.parent / dep[len("@loader_path/"):])
            if not Path(dep).is_absolute():
                raise ValueError("unresolved oracle dependency: " + dep)
            pending.append(Path(dep))
    return seen, sorted(system)


def provenance():
    binary, version = installed()
    libraries, system = dependencies(binary)
    for required in ("freetype", "fontconfig", "little-cms2", "harfbuzz"):
        if not any("/"+required+"/" in name for name in libraries):
            raise ValueError("missing font/colour dependency: " + required)
    license_files = sorted(binary.parent.parent.glob("share/doc/poppler/COPYING*"))
    if not license_files:
        license_files = sorted(binary.parent.parent.glob("COPYING*"))
    if not license_files:
        raise ValueError("missing oracle license notices")
    return {
        "binary": str(binary), "binary_sha256": PIN, "version": version,
        "libraries_sha256": libraries, "system_dyld_dependencies": system,
        "system_build": subprocess.check_output(["sw_vers", "-buildVersion"], text=True).strip(),
        "license_sha256": {str(p): sha(p.read_bytes()) for p in license_files},
        "font_policy": "document Type 3 outlines only; no named/system fonts",
        "colour_policy": "DeviceGray/DeviceRGB default Decode; no ICC profiles",
        "invocation": FLAGS, "negatives_sent_to_oracle": False,
    }


def check():
    expected = json.loads(MANIFEST.read_text())
    if expected["provenance"] != provenance():
        raise ValueError("BLOCKED: OracleDrift (frozen font/colour/library closure)")
    return expected


def references(out, *, freeze=False):
    out.mkdir(parents=True, exist_ok=True)
    manifest = json.loads((FIXTURES / "manifest.json").read_text())
    prov = provenance() if freeze else check()["provenance"]
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
    for row in rows:
        src = Path(row["file"]) if row["id"] in maxima or row["id"].startswith("capacity-") else FIXTURES/row["file"]
        if sha(src.read_bytes()) != row["sha256"]:
            raise ValueError("SourceDrift: oracle input")
        flags = FLAGS.copy()
        flags[flags.index("-f")+1] = flags[flags.index("-l")+1] = str(row["page"])
        if row.get("cropbox"):
            flags.append("-cropbox")
        dest = out/row["id"]
        subprocess.run([prov["binary"], *flags, str(src), str(dest)], env=env, capture_output=True, check=True, timeout=30)
        pp = dest.with_suffix(".ppm").read_bytes()
        bgra = ppm(pp)
        dest.with_suffix(".bgra").write_bytes(bgra)
        if list(__import__("struct").unpack_from("<II", bgra, 4)) != row["dimensions"]:
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
        products.append({k: row[k] for k in ("id", "sha256", "page", "box", "dimensions", "background", "comparison")}
                        | {"ppm_sha256": sha(pp), "bgra_sha256": sha(bgra), "invocation": flags,
                           "analytic_sha256": sha(analytic), "poppler_crosscheck": diagnostic})
    actual = {"version": 2, "corpus": "M89-PDF1", "provenance": prov, "references": products}
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
