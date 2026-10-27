#!/usr/bin/env python3
"""sync.py -- the VirelaiOS design lane's sync tool (#3783).

VirelaiOS is the design truth: the figcraft design sheets and the
delivered piece masters live in-repo under design/ (vendored from
drawmeanelephant/f-artcraft; layout + provenance in design/README.md).
f-artcraft is now renderer/preview only — nothing fetches from it.
(#3357 is the superseded pull/push conveyor, kept here as history.)

Two modes over the M203 (#3338) staged-art seam:

  (default) Local-truth adoption. Scan design/art/virelai/<slug>/ for
            each slug's highest `<slug>-V*R*@2x.png` master, convert new
            or revised masters through tools/art/adopt.py's deterministic
            PNG->QOI path into image/art/, regenerate ART.TXT
            (sha-pinned, same format), and print an adoption report.
            Slugs with no adopt TABLE row are reported, never silently
            named — naming/family is the adopt.py contract's job.
            `design/virelai/design/` sheets are listed with their
            revisions so a design-side change is visible to the loop.

  --check   Same scan, verify-only: fail when image/art/ or ART.TXT
            differ from what the local masters produce.

  --export  Regenerate the shipped-truth figcraft templates under
            --out (default artifacts/figcraft-out/): document.json+zip
            per template, carrying what the code ships today —
              tokens-SHIPPED     <- user/go/theme/theme.go color tokens
              apps-SHIPPED       <- image/apps.txt catalog rows
              seat-shell-SHIPPED <- user/go/gotabwm chrome geometry consts
            then print a design-drift report: shipped values vs the
            local design sheets, so f-artcraft sees drift when it picks
            the templates up for preview. The emitted document carries
            the truth as figcraft variables + named nodes — a
            pixel-faithful mockup regeneration (the 246-1140 node visual
            layouts) stays designer-side followup.

Usage:
  python3 tools/art/sync.py [--dry-run]
  python3 tools/art/sync.py --check
  python3 tools/art/sync.py --export [--out DIR]

python3 stdlib + the project's Pillow (adopt.convert) only.
"""

import argparse
import hashlib
import io
import json
import re
import sys
import zipfile
from pathlib import Path

ROOT = Path(__file__).resolve().parents[2]
sys.path.insert(0, str(ROOT / "tools" / "art"))
sys.path.insert(0, str(ROOT / "tools"))

import adopt  # noqa: E402

OUT = ROOT / "artifacts" / "figcraft-out"
ART_MASTERS = ROOT / "design" / "art" / "virelai"
SHEETS = ROOT / "design" / "virelai" / "design"


def sha256(b):
    return hashlib.sha256(b).hexdigest()


def manifest_rows():
    """ART.TXT -> {slug: (share, rel, slug, size, issue, src_sha, qoi_sha)}."""
    rows = {}
    for ln in adopt.MANIFEST.read_text().splitlines():
        f = ln.split()
        if len(f) >= 7 and not ln.startswith("#"):
            rows[f[2]] = (f[0], f[1], f[2], f[3], f[4], f[5], f[6])
    return rows


def local_art():
    """{slug: (V, R, path)} — highest revision master per slug."""
    best = {}
    for d in sorted(ART_MASTERS.iterdir()):
        if not d.is_dir():
            continue
        slug = d.name
        for p in d.glob(f"{slug}-V*R*@2x.png"):
            m = re.fullmatch(
                rf"{re.escape(slug)}-V(\d+)R(\d+)@2x\.png", p.name)
            if not m:
                continue
            key = (int(m.group(1)), int(m.group(2)))
            if slug not in best or key > best[slug][:2]:
                best[slug] = key + (p,)
    return best


def local_design(figcraft_only=False):
    """{name: (V, R, path)} — highest-revision native per design sheet.

    A sheet's native is .figcraft (drawn in FigCraft) or .vectorcraft
    (drawn by f-artcraft scripts) per design/virelai/design/README.md;
    .figcraft wins when a revision carries both."""
    best = {}
    if not SHEETS.is_dir():
        return best
    exts = ("figcraft",) if figcraft_only else ("figcraft", "vectorcraft")
    for d in sorted(SHEETS.iterdir()):
        if not d.is_dir():
            continue
        name = d.name
        for ext in exts:
            for p in d.glob(f"{name}-V*R*.{ext}"):
                m = re.fullmatch(
                    rf"{re.escape(name)}-V(\d+)R(\d+)\.{ext}", p.name)
                if not m:
                    continue
                key = (int(m.group(1)), int(m.group(2)),
                       ext == "figcraft")
                if name not in best or key > best[name][0]:
                    best[name] = (key, p)
    return {name: key[:2] + (p,) for name, (key, p) in best.items()}


def sync(dry_run, check):
    table = {slug: (fam, name, size, iss)
             for slug, fam, name, size, iss in adopt.TABLE}
    have = manifest_rows()
    best = local_art()

    adopted, revised, same, unknown = [], [], [], []
    fails = []
    for slug, (v, r, path) in sorted(best.items()):
        if slug not in table:
            unknown.append(f"{slug} (V{v}R{r:02d} master; no adopt row)")
            continue
        blob = path.read_bytes()
        s = sha256(blob)
        fam, name, size, iss = table[slug]
        rel = f"image/art/{fam}/{name.lower()}.qoi"
        dst = ROOT / rel
        old = have.get(slug)
        if old and old[5] == s:
            if check and (not dst.exists()
                          or sha256(dst.read_bytes()) != old[6]):
                fails.append(f"{rel} does not match ART.TXT's qoi sha")
            same.append(slug)
            continue
        out = adopt.convert(path, size)
        (revised if old else adopted).append(
            f"{slug} -> {name} ({'V%dR%02d' % (v, r)})")
        if check:
            if not dst.exists() or dst.read_bytes() != out:
                fails.append(f"{rel} differs from the local master")
            continue
        if not dry_run:
            dst.parent.mkdir(parents=True, exist_ok=True)
            dst.write_bytes(out)
        have[slug] = (name, f"{fam}/{name.lower()}.qoi", slug,
                      f"{size[0]}x{size[1]}", iss, s, sha256(out))

    # The manifest is regenerated text — a sync rewrites it whenever the
    # generated form drifts (new rows, or a header/ver change), not only
    # when an asset's bytes moved.
    want = manifest_text(have)
    drifted = want != adopt.MANIFEST.read_text()
    if check:
        if drifted:
            fails.append("image/art/ART.TXT differs from a fresh sync")
    elif not dry_run and drifted:
        adopt.MANIFEST.write_text(want)

    design = local_design()
    print("== sync (local truth) ==")
    for l in adopted:
        print("  adopt   " + l)
    for l in revised:
        print("  revise  " + l)
    for l in unknown:
        print("  no-row  " + l)
    print(f"  unchanged: {len(same)} staged assets identical to masters")
    print("  design sheets vendored:")
    for name, (v, r, p) in sorted(design.items()):
        print(f"    {name}-V{v}R{r:02d}{p.suffix}")
    if dry_run:
        print("  (dry run — nothing written)")
    if check:
        for f in fails:
            print("  FAIL " + f)
        if fails:
            return 1
        print("  --check ok: staged rasters + ART.TXT match local masters")
    return 0


def manifest_text(have):
    """ART.TXT in adopt.py's exact format, TABLE order -> text."""
    by_share = {v[0]: v for v in have.values()}
    lines = list(adopt.MANIFEST_HEAD)
    for slug, fam, name, size, iss in adopt.TABLE:
        row = by_share.get(name)
        if row is None:
            continue
        _n, rel, _s, sz, issue, ssrc, sout = row
        issue_t = issue if str(issue).startswith("#") else f"#{issue}"
        lines.append(
            f"{name:<9} {rel:<30} {slug:<22} {sz:<10} {issue_t:<6} {ssrc} {sout}")
    return "\n".join(lines) + "\n"


# ---------------------------------------------------------------------------
# --export: shipped truth -> figcraft templates + design-drift report.
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
    """figcraft document.json -> {name: {mode: (r,g,b)}}."""
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


def export(outdir):
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
            name, c, "generated by tools/art/sync.py --export from shipped "
            "truth; visual mockup regeneration is a designer-side "
            "followup (VirelaiOS#3357 notes)"))
        emitted.append(dst.name)

    # --- drift report vs the local design sheets ---------------------------
    print("== sync --export ==")
    print("  emitted: " + ", ".join(emitted) + f"  -> {outdir}")

    design = local_design(figcraft_only=True)
    print("  drift vs local design sheets:")
    if "tokens" in design:
        v, r, p = design["tokens"]
        doc = json.loads(zipfile.ZipFile(p).read("document.json"))
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
        print("    tokens: no local sheet found")

    if "apps" in design:
        v, r, p = design["apps"]
        doc = json.loads(zipfile.ZipFile(p).read("document.json"))
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
        print("    apps: no local sheet found")
    print("  (export does not write to f-artcraft — it previews from "
          "these templates)")
    return 0


def main():
    ap = argparse.ArgumentParser(description=__doc__.splitlines()[0])
    g = ap.add_mutually_exclusive_group()
    g.add_argument("--check", action="store_true",
                   help="verify image/art + ART.TXT match local masters")
    g.add_argument("--export", action="store_true",
                   help="emit shipped-truth figcraft templates for "
                        "f-artcraft preview")
    ap.add_argument("--dry-run", action="store_true")
    ap.add_argument("--out", type=Path, default=OUT)
    a = ap.parse_args()
    if a.export:
        return export(a.out)
    return sync(a.dry_run, a.check)


if __name__ == "__main__":
    sys.exit(main())
