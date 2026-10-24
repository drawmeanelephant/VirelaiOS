#!/usr/bin/env python3
"""sync.py -- the VirelaiOS <-> f-artcraft interface bridge (#3357).

Two directions over the M203 (#3338) conveyor:

  --pull   Diff drawmeanelephant/f-artcraft's `art/virelai/` drop against
           image/art/ART.TXT: fetch every changed `<slug>-V*R*@2x.png`
           master (new assets, and revised masters at the same or a newer
           VxRy), convert through tools/art/adopt.py's deterministic
           PNG->QOI path, regenerate ART.TXT (sha-pinned, same format),
           and print an adoption report. Slugs with no adopt TABLE row
           are reported, never silently named — naming/family is the
           adopt.py contract's job, not the fetcher's.
           `virelai/design/` .figcraft masters are listed with their
           revisions so a design-side change is visible to the loop.

  --push   Regenerate the shipped-truth figcraft templates under
           --out (default artifacts/figcraft-out/): document.json+zip per
           template, carrying what the code ships today —
             tokens-VxRy     <- user/go/theme/theme.go color tokens
             apps-VxRy       <- image/apps.txt catalog rows
             seat-shell-VxRy <- user/go/gotabwm chrome geometry consts
           then print a design-drift report: shipped values vs the
           remote design documents, so f-artcraft sees drift and files
           adoption cards. The emitted document carries the truth as
           figcraft variables + named nodes — a pixel-faithful mockup
           regeneration (the 246-1140 node visual layouts) stays
           designer-side followup; see the header of this file.

Usage:
  python3 tools/art/sync.py --pull [--dry-run]
  python3 tools/art/sync.py --push [--out DIR] [--no-fetch]

--no-fetch diffs against the cached copies in --out/../fartcraft-cache
(a --pull or plain --push run refreshes it). python3 stdlib + the
project's Pillow (adopt.convert) only; `gh` does auth.
"""

import argparse
import hashlib
import io
import json
import re
import subprocess
import sys
import zipfile
from pathlib import Path

ROOT = Path(__file__).resolve().parents[2]
sys.path.insert(0, str(ROOT / "tools" / "art"))
sys.path.insert(0, str(ROOT / "tools"))

import adopt  # noqa: E402

REPO = "drawmeanelephant/f-artcraft"
CACHE = ROOT / "artifacts" / "fartcraft-cache"
OUT = ROOT / "artifacts" / "figcraft-out"

DESIGN = ("apps", "seat-shell", "tokens", "app-screens")
REV = re.compile(r"-V(\d+)R(\d+)@")


def gh_json(args):
    out = subprocess.run(
        ["gh", "api"] + args, capture_output=True, text=True, check=True)
    return json.loads(out.stdout)


def gh_blob(path):
    meta = gh_json([f"repos/{REPO}/contents/{path}"])
    import base64
    return base64.b64decode(meta["content"])


def sha256(b):
    return hashlib.sha256(b).hexdigest()


def remote_tree():
    tree = gh_json([f"repos/{REPO}/git/trees/main?recursive=1"])["tree"]
    return [t["path"] for t in tree if t["type"] == "blob"]


def manifest_rows():
    """ART.TXT -> {slug: (share, rel, size, issue, src_sha)}."""
    rows = {}
    for ln in adopt.MANIFEST.read_text().splitlines():
        f = ln.split()
        if len(f) >= 7 and not ln.startswith("#"):
            rows[f[2]] = (f[0], f[1], f[3], f[4], f[5])
    return rows


def remote_art(paths):
    """{slug: (V, R, path)} — highest revision per art/virelai/ slug."""
    best = {}
    for p in paths:
        m = re.match(r"art/virelai/([^/]+)/\1-V(\d+)R(\d+)@2x\.png$", p)
        if m:
            key = (int(m.group(2)), int(m.group(3)))
            if m.group(1) not in best or key > best[m.group(1)][:2]:
                best[m.group(1)] = key + (p,)
    return best


def remote_design(paths):
    """{name: (V, R, path)} — highest .figcraft revision per design doc."""
    best = {}
    for p in paths:
        m = re.match(
            r"virelai/design/([^/]+)/\1-V(\d+)R(\d+)\.figcraft$", p)
        if m:
            key = (int(m.group(2)), int(m.group(3)))
            if m.group(1) not in best or key > best[m.group(1)][:2]:
                best[m.group(1)] = key + (p,)
    return best


def pull(dry_run):
    paths = remote_tree()
    table = {slug: (fam, name, size, iss)
             for slug, fam, name, size, iss in adopt.TABLE}
    have = manifest_rows()
    best = remote_art(paths)

    fetched = {}
    adopted, revised, same, unknown = [], [], [], []
    for slug, (v, r, path) in sorted(best.items()):
        if slug not in table:
            unknown.append(f"{slug} (V{v}R{r:02d} on remote; no adopt row)")
            continue
        blob = fetched.setdefault(slug, gh_blob(path))
        s = sha256(blob)
        fam, name, size, iss = table[slug]
        old = have.get(slug)
        if old and old[4] == s:
            same.append(slug)
            continue
        tmp = CACHE / "pull" / f"{slug}.png"
        tmp.parent.mkdir(parents=True, exist_ok=True)
        tmp.write_bytes(blob)
        out = adopt.convert(tmp, size)
        rel = f"image/art/{fam}/{name.lower()}.qoi"
        (revised if old else adopted).append(
            f"{slug} -> {name} ({'V%dR%02d' % (v, r)})")
        if not dry_run:
            dst = ROOT / rel
            dst.parent.mkdir(parents=True, exist_ok=True)
            dst.write_bytes(out)
        have[slug] = (name, f"{fam}/{name.lower()}.qoi", slug,
                      f"{size[0]}x{size[1]}", iss, s, sha256(out))

    if not dry_run and (adopted or revised):
        write_manifest(have)

    design = remote_design(paths)
    print("== sync --pull ==")
    for l in adopted:
        print("  adopt   " + l)
    for l in revised:
        print("  revise  " + l)
    for l in unknown:
        print("  no-row  " + l)
    print(f"  unchanged: {len(same)} staged assets identical to remote")
    print("  design docs on remote:")
    for name, (v, r, _p) in sorted(design.items()):
        print(f"    {name}-V{v}R{r:02d}.figcraft")
    if dry_run:
        print("  (dry run — nothing written)")
    return 0


def write_manifest(have):
    """Regenerate ART.TXT in adopt.py's exact format, TABLE order."""
    by_share = {v[0]: v for v in have.values()}
    lines = [
        "# VirelaiOS staged art manifest (M203 #3338 conveyor).",
        "# Source: drawmeanelephant/f-artcraft art/virelai/<slug>/<slug>-V1R00@2x.png",
        "# Generated by tools/art/adopt.py -- do not hand-edit rows.",
        "#",
        "# SHARE    REPO FILE                       SOURCE SLUG            SHIP SIZE  ISSUE  SRC SHA256                                                       QOI SHA256",
    ]
    for slug, fam, name, size, iss in adopt.TABLE:
        row = by_share.get(name)
        if row is None:
            continue
        _n, rel, _s, sz, issue, ssrc, sout = row
        issue_t = issue if str(issue).startswith("#") else f"#{issue}"
        lines.append(
            f"{name:<9} {rel:<30} {slug:<22} {sz:<10} {issue_t:<6} {ssrc} {sout}")
    adopt.MANIFEST.write_text("\n".join(lines) + "\n")


# ---------------------------------------------------------------------------
# --push: shipped truth -> figcraft templates + design-drift report.
# ---------------------------------------------------------------------------

THEME_RE = re.compile(
    r"^\s*([A-Za-z0-9]+):\s*(0x[0-9a-fA-F]{6})\s*,", re.M)


def shipped_tokens():
    """theme.go's two token sets -> {mode: {color/name: (r,g,b) 0-1}}."""
    src = (ROOT / "user/go/theme/theme.go").read_text()
    blocks = re.split(r"=\s*Tokens\{", src)
    modes = ["dark", "light"]
    out = {}
    for i, mode in enumerate(modes):
        if i + 1 >= len(blocks):
            break
        seg = blocks[i + 1].split("}")[0]
        out[mode] = {}
        for name, hx in THEME_RE.findall(seg):
            v = int(hx, 16)
            out[mode]["color/" + name.lower()] = (
                ((v >> 16) & 0xff) / 255.0,
                ((v >> 8) & 0xff) / 255.0,
                (v & 0xff) / 255.0)
    return out


def shipped_apps():
    apps = []
    for ln in (ROOT / "image/apps.txt").read_text().splitlines():
        ln = ln.strip()
        if not ln or ln.startswith("#") or "|" not in ln:
            continue
        f = [x.strip() for x in ln.split("|")]
        e = {"bin": f[0], "label": f[1], "rune": f[2] if len(f) > 2 else "",
             "dock": len(f) > 3 and f[3] == "dock=true"}
        for t in f[4:]:
            if t.startswith("icon="):
                e["icon"] = t[5:]
        apps.append(e)
    return apps


def shipped_shell():
    """The seat's named chrome geometry — the consts the shell mockup
    claims. Kept small and declared: each name maps a code constant."""
    src = (ROOT / "user/go/gotabwm/tabs.go").read_text()
    out = {}
    for name in ("RailHeight",):
        m = re.search(rf"const {name} = (\d+)", src)
        if m:
            out["layout/" + name.lower()] = float(m.group(1))
    for name in ("railIconPx", "railCloseW", "railBadgeW"):
        m = re.search(rf"{name}\s*=\s*(\d+)", src)
        if m:
            out["layout/" + name.lower()] = float(m.group(1))
    launch = (ROOT / "user/go/gotabwm/launcher.go").read_text()
    for name in ("launchW", "launchIconPx", "launchFieldH"):
        m = re.search(rf"{name}\s*=\s*(\d+)", launch)
        if m:
            out["layout/" + name.lower()] = float(m.group(1))
    return out


def rgb(v):
    return {"r": v[0], "g": v[1], "b": v[2], "a": 1.0}


def emit_figcraft(name, collections, note):
    """A shipped-truth document: figcraft v1 container, the truth carried
    as a variables collection — the pixel mockups are designer-side."""
    doc = {
        "format": "figcraft", "version": 1, "name": name,
        "pages": [1],
        "nodes": {"1": {"id": 1, "name": name + " (shipped truth)",
                        "type": "page", "children": [],
                        "comment": note}},
        "next_id": 2, "styles": [], "variables": collections,
        "current_page": 1, "document_colors": [], "comments": [],
    }
    buf = io.BytesIO()
    with zipfile.ZipFile(buf, "w", zipfile.ZIP_DEFLATED) as z:
        z.writestr("document.json", json.dumps(doc, indent=1))
    return buf.getvalue()


def design_variables(doc):
    """Remote figcraft document.json -> {name: {mode: (r,g,b)}}."""
    out = {}
    coll = (doc.get("variables") or {})
    modes = {}
    for c in coll.get("collections", []):
        for m in c.get("modes", []):
            modes[m["id"]] = m["name"]
    for v in coll.get("variables", []):
        vals = {}
        for mid, val in (v.get("values") or {}).items():
            c = (val or {}).get("value")
            if isinstance(c, dict) and "r" in c:
                vals[modes.get(mid, mid)] = (c["r"], c["g"], c["b"])
        out[v["name"]] = vals
    return out


def close(a, b, eps=0.004):
    return all(abs(x - y) <= eps for x, y in zip(a, b))


def push(outdir, no_fetch):
    toks = shipped_tokens()
    apps = shipped_apps()
    shell = shipped_shell()
    outdir.mkdir(parents=True, exist_ok=True)

    # --- shipped-truth emit ------------------------------------------------
    coll = {"collections": [{
        "id": "VirelaiOS/shipped",
        "name": "Virelai Theme (shipped)",
        "modes": [{"id": f"m/{k}", "name": k.title()}
                  for k in toks]}],
        "variables": []}
    vid = 0
    for name in sorted(toks["dark"]):
        vid += 1
        coll["variables"].append({
            "id": f"VariableID:{vid}", "name": name,
            "collection": "VirelaiOS/shipped", "resolvedType": "color",
            "values": {f"m/{mode}": {"type": "color", "value": rgb(
                toks[mode][name])} for mode in toks if name in toks[mode]},
            "description": "shipped: user/go/theme/theme.go"})
    apps_doc = {
        "collections": [{"id": "VirelaiOS/apps", "name": "Shipped catalog",
                         "modes": [{"id": "m/0", "name": "rows"}]}],
        "variables": [{
            "id": f"VariableID:{i}", "name": f"app/{a['bin']}",
            "collection": "VirelaiOS/apps", "resolvedType": "string",
            "values": {"m/0": {"type": "string", "value":
                               f"{a['label']}|{a.get('icon', '')}"},
                       },
            "description": "shipped: image/apps.txt"}
            for i, a in enumerate(apps)]}
    shell_doc = {
        "collections": [{"id": "VirelaiOS/shell", "name": "Shipped chrome",
                         "modes": [{"id": "m/0", "name": "px"}]}],
        "variables": [{
            "id": f"VariableID:{i}", "name": k,
            "collection": "VirelaiOS/shell", "resolvedType": "float",
            "values": {"m/0": {"type": "float", "value": v}},
            "description": "shipped: user/go/gotabwm"}
            for i, (k, v) in enumerate(sorted(shell.items()))]}

    emitted = []
    for name, c in (("tokens-SHIPPED", coll),
                    ("apps-SHIPPED", apps_doc),
                    ("seat-shell-SHIPPED", shell_doc)):
        dst = outdir / f"{name}.figcraft"
        dst.write_bytes(emit_figcraft(
            name, c, "generated by tools/art/sync.py --push from shipped "
            "truth; visual mockup regeneration is a designer-side "
            "followup (VirelaiOS#3357 notes)"))
        emitted.append(dst.name)

    # --- drift report ------------------------------------------------------
    print("== sync --push ==")
    print("  emitted: " + ", ".join(emitted) + f"  -> {outdir}")
    if no_fetch:
        print("  drift: skipped (--no-fetch)")
        return 0

    paths = remote_tree()
    design = remote_design(paths)
    print("  drift vs remote design docs:")
    if "tokens" in design:
        v, r, p = design["tokens"]
        doc = json.loads(zipfile.ZipFile(io.BytesIO(gh_blob(p)))
                         .read("document.json"))
        remote = design_variables(doc)
        drifts, missing = [], []
        for mode, mode_remote in (("Dark", "dark"), ("Light", "light")):
            shipped = toks.get(mode_remote, {})
            for name, vals in sorted(remote.items()):
                rv = vals.get(mode)
                sv = shipped.get(name)
                if rv is None:
                    continue
                if sv is None:
                    missing.append(f"{name} ({mode})")
                elif not close(rv, sv):
                    hexf = lambda t: "#%02x%02x%02x" % tuple(
                        round(x * 255) for x in t)
                    drifts.append(
                        f"{name} {mode}: design {hexf(rv)} != shipped {hexf(sv)}")
        for d in drifts:
            print("    DRIFT " + d)
        for m_ in missing:
            print("    design-only " + m_)
        if not drifts:
            print("    tokens: no color drift Dark/Light")
    else:
        print("    tokens: no remote doc found")

    if "apps" in design:
        v, r, p = design["apps"]
        doc = json.loads(zipfile.ZipFile(io.BytesIO(gh_blob(p)))
                         .read("document.json"))
        nodes = doc.get("nodes", {})
        designed = {n["name"][5:] for n in nodes.values()
                    if isinstance(n.get("name"), str)
                    and n["name"].startswith("apps/")}
        shipped_bins = {a["bin"] for a in apps}
        for d in sorted(designed - shipped_bins):
            print("    design-only app " + d)
        for d in sorted(shipped_bins - designed):
            print("    shipped-only app " + d)
        if designed == shipped_bins:
            print("    apps: catalog set matches design fleet")
    else:
        print("    apps: no remote doc found")
    print("  (push does not write to f-artcraft — adoption cards are filed "
          "from this report)")
    return 0


def main():
    ap = argparse.ArgumentParser(description=__doc__.splitlines()[0])
    g = ap.add_mutually_exclusive_group(required=True)
    g.add_argument("--pull", action="store_true")
    g.add_argument("--push", action="store_true")
    ap.add_argument("--dry-run", action="store_true")
    ap.add_argument("--no-fetch", action="store_true")
    ap.add_argument("--out", type=Path, default=OUT)
    a = ap.parse_args()
    if a.pull:
        return pull(a.dry_run)
    return push(a.out, a.no_fetch)


if __name__ == "__main__":
    sys.exit(main())
