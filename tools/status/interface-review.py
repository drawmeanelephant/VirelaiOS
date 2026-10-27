#!/usr/bin/env python3
"""interface-review.py -- the interface-review lane for the issue-conveyor (#3784).

One pass reviews ONE app in rotation over the open `polish` audit issues
("App audit: <name> (<BIN>)"). Rotation state lives on the audit issues
themselves -- the newest comment carrying the lane marker is that app's
last-reviewed stamp -- never in a repo file, so the hourly sweep can invoke
this from any checkout.

For the picked app the lane checks the shipped face against the figcraft
design lane (f-artcraft `virelai/design/apps/` -- the same paths
tools/art/sync.py reads; the in-repo migration is #3783) and the gate
specs that assert on it, then either files ONE bounded `polish` card via
`gh issue create` or posts a no-drift comment on the audit issue. Drift
classes (per the card):

  missing-art      apps.txt icon= has no ART.TXT row / shipped .qoi
  no-design-master no `apps/<BIN>` frame in the figcraft apps sheet
  layout           frame size != declared window + 28px titlebar, or a
                   designed control (`btn/`, `ctab`) has no shipped label
  stale-spec       a spec's `serial-contains/exact` marker prefix is no
                   longer emitted by the app source
  dead-end         an argv verb paints a windowed face nothing in the
                   window path names (the gosound/goirc pattern)

Guardrails: one card per pass; an audit with an open derived card is
skipped; so is an app whose files an active `claim` Touches (the same
overlap rule verify-issue-coordination.sh enforces).

Usage:
  python3 tools/status/interface-review.py                 # live pass
  python3 tools/status/interface-review.py --dry-run       # reads live, posts nothing
  python3 tools/status/interface-review.py --app GOCLOCK.ELF [--dry-run]
  offline fixtures (tools/status/test-coordination.sh):
    --audits-json F   `gh issue list --json number,title,body,comments,labels`
    --claims-json F   `gh issue list --json number,title,body,updatedAt,labels`
    --open-json F     `gh issue list --search <bin> --json number,title,body,labels`
    --figcraft F      a local apps-*.figcraft (skips the f-artcraft fetch)
"""

import argparse
import base64
import io
import json
import re
import subprocess
import sys
import zipfile
from datetime import datetime, timezone
from pathlib import Path

ROOT = Path(__file__).resolve().parents[2]
DESIGN_REPO = "drawmeanelephant/f-artcraft"
CACHE = ROOT / "artifacts" / "fartcraft-cache" / "design"

MARKER = "iface-review"
AUDIT_RE = re.compile(r"App audit:\s*(.+?)\s*\((\w+\.(?:ELF|BIN))\)")
TITLEBAR_H = 28  # the seat chrome every apps-sheet frame carries (titlebar node)

# Design text nodes that name a control the app must ship. `btn label` /
# `ctab` are the sheet's control vocabulary; sample texts ('ztab', hint
# strings, status rows) are deliberately excluded -- they are mock data.
CONTROL_NAME_RE = re.compile(r"^(?:btn label|ctab)\s+(.+)$")


def gh_json(args):
    out = subprocess.run(
        ["gh"] + args, capture_output=True, text=True, check=True)
    return json.loads(out.stdout)


def gh_text(args):
    out = subprocess.run(
        ["gh"] + args, capture_output=True, text=True, check=True)
    return out.stdout.strip()


# ---------------------------------------------------------------------------
# Inputs: audits, claims, open issues, the design master.
# ---------------------------------------------------------------------------

def load_audits(path):
    if path:
        return json.loads(Path(path).read_text())
    return gh_json(["issue", "list", "--label", "polish", "--state", "open",
                    "--limit", "200",
                    "--json", "number,title,body,comments,labels"])


def load_claims(path):
    if path:
        return json.loads(Path(path).read_text())
    return gh_json(["issue", "list", "--label", "claim", "--state", "open",
                    "--limit", "200",
                    "--json", "number,title,body,updatedAt,labels"])


def load_open_issues(binbase, path):
    """Open issues naming the binary -- the derived-card scan."""
    if path:
        return json.loads(Path(path).read_text())
    return gh_json(["issue", "list", "--state", "open", "--limit", "100",
                    "--search", binbase,
                    "--json", "number,title,body,labels"])


def load_design_doc(figcraft_path, no_fetch):
    """-> (document.json dict, revision string, provenance string)."""
    if figcraft_path:
        doc = json.loads(zipfile.ZipFile(figcraft_path).read("document.json"))
        return doc, Path(figcraft_path).name, f"local file {figcraft_path}"

    if no_fetch:
        cached = sorted(CACHE.glob("apps-V*R*.figcraft"))
        if not cached:
            return None, None, "no cached design doc (--no-fetch)"
        doc = json.loads(zipfile.ZipFile(cached[-1]).read("document.json"))
        return doc, cached[-1].name, f"cache {cached[-1]}"

    try:
        tree = gh_json(["api",
                        f"repos/{DESIGN_REPO}/git/trees/main?recursive=1"])
        cands = []
        for t in tree["tree"]:
            m = re.match(
                r"virelai/design/apps/apps-V(\d+)R(\d+)\.figcraft$",
                t["path"])
            if m:
                cands.append(((int(m.group(1)), int(m.group(2))), t["path"]))
        if not cands:
            return None, None, "no apps-*.figcraft on f-artcraft"
        (v, r), path = sorted(cands)[-1]
        meta = gh_json(["api", f"repos/{DESIGN_REPO}/contents/{path}"])
        blob = base64.b64decode(meta["content"])
        CACHE.mkdir(parents=True, exist_ok=True)
        (CACHE / f"apps-V{v}R{r:02d}.figcraft").write_bytes(blob)
        doc = json.loads(zipfile.ZipFile(io.BytesIO(blob))
                         .read("document.json"))
        return doc, f"V{v}R{r:02d}", f"f-artcraft {path}"
    except (subprocess.CalledProcessError, KeyError, json.JSONDecodeError) as e:
        return None, None, f"design fetch failed: {e}"


# ---------------------------------------------------------------------------
# Rotation.
# ---------------------------------------------------------------------------

def last_reviewed(audit):
    """Newest iface-review comment's createdAt, else None."""
    best = None
    for c in audit.get("comments") or []:
        if MARKER in (c.get("body") or ""):
            at = c.get("createdAt") or ""
            if best is None or at > best:
                best = at
    return best


def rotation_order(audits):
    """[(audit, bin, name)] oldest review first; never-reviewed first."""
    rows = []
    for a in audits:
        m = AUDIT_RE.search(a.get("title") or "")
        if m:
            rows.append((a, m.group(2), m.group(1)))
    rows.sort(key=lambda r: (last_reviewed(r[0]) or "", r[0]["number"]))
    return rows


# ---------------------------------------------------------------------------
# Guardrails.
# ---------------------------------------------------------------------------

def derived_cards(open_issues, audit_num, binbase):
    """Open issues that are cards derived from this audit: the binary in
    the title ("GOCLOCK: alarm labels") or a body reference to the audit
    ("From polish audit #3733")."""
    hits = []
    for i in open_issues:
        if i["number"] == audit_num:
            continue
        title_hit = binbase.lower() in (i.get("title") or "").lower()
        body_hit = f"audit #{audit_num}" in (i.get("body") or "")
        if title_hit or body_hit:
            hits.append(i["number"])
    return hits


def claim_touches(claims, app_paths):
    """[(claim_num, token, app_path)] where an active claim's Touches
    overlap an app path: exact, a `prefix*` glob, or a parent-dir prefix
    (a claim on `user/go/x` covers `user/go/x/main.go`). ⛔ claims are
    excluded -- same rule as the coordination gate."""
    hits = []
    for c in claims:
        body = c.get("body") or ""
        status = re.search(r"^- \*\*Status:\*\* (.+)$", body, re.M)
        if status and status.group(1).startswith("⛔"):
            continue
        tm = re.search(r"^- \*\*Touches:\*\* (.+)$", body, re.M)
        if not tm:
            continue
        for tok in tm.group(1).split(","):
            tok = tok.strip()
            if not tok or tok == "—":
                continue
            for p in app_paths:
                if (tok == p or p.startswith(tok.rstrip("/") + "/")
                        or tok.startswith(p.rstrip("/") + "/")
                        or (tok.endswith("*")
                            and p.startswith(tok[:-1]))):
                    hits.append((c["number"], tok, p))
    return hits


# ---------------------------------------------------------------------------
# Per-app checks. Each returns findings [(cls, text)] and notes [text].
# ---------------------------------------------------------------------------

def declared_path(audit_body):
    """The `user/...` path the audit names as evidence — existence not
    required (a claim on it still holds the app's files)."""
    m = re.search(r"`?(user/(?:go|src)/[\w./-]+)`?", audit_body or "")
    if not m:
        return None
    return Path(m.group(1).rstrip("/"))


def app_source_dir(audit_body, binbase):
    """The app's source dir/file ON DISK, for the checks themselves.
    user/go/<dir>/file.go resolves to the dir (the app is the package);
    user/src/<file>.zig is the single-file app itself."""
    p = declared_path(audit_body)
    if p is not None:
        if p.suffix in (".go", ".zig") and (ROOT / p).is_file():
            if p.parts[1] == "go":
                return p.parent
            return p
        if (ROOT / p).is_dir():
            return p
    b = binbase.lower()
    for cand in (f"user/go/{b}", f"user/go/{b[2:] if b.startswith('go') else b}",
                 f"user/src/{b}.zig"):
        if (ROOT / cand).exists():
            return Path(cand)
    return None


def app_paths(audit_body, bin_):
    """The surface a claim Touches would hold: the audit-declared path,
    the resolved source dir, and every spec asserting the app's markers."""
    out = set()
    p = declared_path(audit_body)
    if p is not None:
        out.add(p.as_posix().rstrip("/"))
        if p.suffix in (".go", ".zig"):
            out.add(p.parent.as_posix())
    d = app_source_dir(audit_body, bin_)
    if d is not None:
        out.add(d.as_posix().rstrip("/"))
    out |= {f"tools/gate/specs/{s}" for s, _m in spec_markers(bin_)}
    return sorted(out)


def src_texts(srcdir):
    """[(relpath, text)] of app source files."""
    out = []
    if srcdir is None:
        return out
    d = ROOT / srcdir
    pats = ("*.go", "*.zig") if d.is_dir() else ()
    for pat in pats:
        for f in sorted(d.glob(pat)):
            out.append((f.relative_to(ROOT).as_posix(),
                        f.read_text(errors="replace")))
    if d.is_file():
        out.append((d.relative_to(ROOT).as_posix(),
                    d.read_text(errors="replace")))
    return out


def check_art(bin_, findings, notes):
    row = None
    for ln in (ROOT / "image/apps.txt").read_text().splitlines():
        f = [x.strip() for x in ln.split("|")]
        if f and f[0] == bin_:
            row = f
            break
    if row is None:
        notes.append(f"{bin_}: no image/apps.txt row — icon check skipped")
        return
    icon = next((t[5:] for t in row[4:] if t.startswith("icon=")), None)
    if not icon:
        findings.append(("missing-art",
                         f"{bin_}: manifest row declares no icon= — rune "
                         "fallback only"))
        return
    art = (ROOT / "image/art/ART.TXT").read_text()
    m = re.search(rf"^{re.escape(icon)}\s+(\S+)", art, re.M)
    if not m:
        findings.append(("missing-art",
                         f"{bin_}: icon={icon} has no ART.TXT row — not "
                         "adopted from f-artcraft"))
        return
    rel = m.group(1)
    if not (ROOT / "image/art" / rel).is_file():
        findings.append(("missing-art",
                         f"{bin_}: ART.TXT names image/art/{rel} but the "
                         "shipped raster is absent"))
        return
    notes.append(f"icon={icon} adopted (image/art/{rel})")


def declared_size(texts):
    """(w, h) the app declares: the W/H const pairs the fleet uses
    (natW/natH, winW/winH, viewW/viewH, uiNatW/uiNatH), else the Declare
    W:/H: literals."""
    pairs = (("natW", "natH"), ("winW", "winH"),
             ("viewW", "viewH"), ("uiNatW", "uiNatH"))
    names = [n for pr in pairs for n in pr]
    vals = {}
    for _rel, text in texts:
        # Tuple decls first (`winW, winH = 344, 152`, either order): the
        # single-name regex would otherwise misread `winH = 344` inside.
        for wn, hn in pairs:
            m = re.search(rf"\b{wn}\s*,\s*{hn}\s*=\s*(\d+)\s*,\s*(\d+)",
                          text)
            if m:
                vals.setdefault(wn, int(m.group(1)))
                vals.setdefault(hn, int(m.group(2)))
            m = re.search(rf"\b{hn}\s*,\s*{wn}\s*=\s*(\d+)\s*,\s*(\d+)",
                          text)
            if m:
                vals.setdefault(hn, int(m.group(1)))
                vals.setdefault(wn, int(m.group(2)))
        for name, v in re.findall(
                r"\b(" + "|".join(names) + r")(?!\s*,)\s*=\s*(\d+)", text):
            vals.setdefault(name, int(v))
    for pair in pairs:
        if pair[0] in vals and pair[1] in vals:
            return vals[pair[0]], vals[pair[1]]
    for _rel, text in texts:
        mw = re.search(r"\bW:\s*(\d{3,4})\s*,", text)
        mh = re.search(r"\bH:\s*(\d{3,4})\s*,", text)
        if mw and mh:
            return int(mw.group(1)), int(mh.group(1))
    return None


def design_frame(doc, bin_):
    nodes = (doc or {}).get("nodes") or {}
    for nid, n in nodes.items():
        if n.get("name") == f"apps/{bin_}":
            return nid, n
    return None, None


def frame_subtree(doc, nid):
    nodes = doc.get("nodes") or {}
    out = []

    def walk(i):
        n = nodes.get(str(i)) or {}
        out.append(n)
        for c in n.get("children") or []:
            walk(c)
    walk(nid)
    return out


def check_design(doc, rev, bin_, texts, findings, notes):
    if doc is None:
        notes.append("design master unavailable — figcraft checks skipped")
        return
    nid, frame = design_frame(doc, bin_)
    if frame is None:
        findings.append(("no-design-master",
                         f"{bin_}: no `apps/{bin_}` frame on the design "
                         "sheet — the face has no drawn truth"))
        return
    decl = declared_size(texts)
    fw, fh = frame.get("width"), frame.get("height")
    if decl is None:
        notes.append(f"frame {fw:g}x{fh:g}; declared window size not "
                     "parseable — geometry unchecked")
    else:
        w, h = decl
        if fw != w or fh != h + TITLEBAR_H:
            findings.append((
                "layout",
                f"{bin_}: design frame {fw:g}x{fh:g} vs shipped window "
                f"{w}x{h}+{TITLEBAR_H} titlebar ({w}x{h + TITLEBAR_H})"))
        else:
            notes.append(f"frame {fw:g}x{fh:g} = declared {w}x{h} + "
                         f"{TITLEBAR_H}px titlebar")
    # Control vocabulary: designed btn/ctab labels must exist in source.
    missing, matched = [], 0
    for n in frame_subtree(doc, nid):
        if n.get("type") != "text":
            continue
        m = CONTROL_NAME_RE.match(n.get("name") or "")
        chars = (n.get("characters") or "").strip()
        if not m or not chars:
            continue
        pat = re.compile(r"(?<![\w])" + re.escape(chars) + r"(?![\w])")
        if any(pat.search(t) for _r, t in texts):
            matched += 1
        else:
            missing.append(f"{n['name']} '{chars}'")
    if missing and matched:
        findings.append((
            "layout",
            f"{bin_}: designed controls with no shipped label: "
            + "; ".join(missing[:6])
            + (" …" if len(missing) > 6 else "")))
    elif missing:
        notes.append(f"{len(missing)} designed control label(s) unmatched "
                     "and none matched — comparison inconclusive")
    elif matched:
        notes.append(f"{matched} designed control label(s) present")


def spec_markers(bin_):
    """[(spec_name, marker)] every spec asserts on the app's own marker
    prefix (`<lbin>:`) — the app's gate surface for both the staleness
    check and the claim-Touches path set."""
    lbin = bin_.split(".")[0].lower()
    marker_re = re.compile(
        r"^vgate_assert\s+\S+\s+serial-(?:contains|exact)\s+'([^']+)'",
        re.M)
    out = []
    for spec in sorted((ROOT / "tools/gate/specs").glob("*.spec")):
        for mk in marker_re.findall(spec.read_text(errors="replace")):
            if mk.startswith(lbin + ":"):
                out.append((spec.name, mk))
    return out


def check_specs(bin_, texts, findings, notes):
    """A spec marker is stale only when its prefix is emitted NOWHERE in
    the guest tree — an app's markers are printed by the monitor, the
    seat, and the loader on its behalf too (`tabwm: starting` lives in
    kernel/src/monitor/win.zig)."""
    stale = []
    tree_src = "\n".join(t for _r, t in texts)
    if not hasattr(check_specs, "_tree"):
        # tools/go is guest source too: the top-level fixtures compile
        # into the .build/go ELFs the gates exec (goroutines.go prints
        # the `go-goroutines done n=8 counter=8` marker), and
        # tools/go/overlay holds the pinned-tree apps' whole virelai
        # face — every `gostalgia:` marker lives there. Subdirs other
        # than overlay (rfbprobe, tabcodec, vlfs) are host tools.
        parts = [
            f.read_text(errors="replace")
            for root in ("user/src", "user/go", "kernel/src",
                         "tools/go/overlay")
            for f in sorted((ROOT / root).rglob("*"))
            if f.suffix in (".go", ".zig") and f.is_file()]
        parts += [f.read_text(errors="replace")
                  for f in sorted((ROOT / "tools/go").glob("*.go"))]
        check_specs._tree = "\n".join(parts)
    all_src = tree_src + "\n" + check_specs._tree
    markers = spec_markers(bin_)
    for spec_name, mk in markers:
        toks = mk.split()
        # The namespace token itself must exist somewhere — a spec
        # asserting `gostalgia:` when nothing prints it is stale outright.
        if toks[0] not in all_src:
            stale.append((spec_name, mk))
            continue
        fresh = False
        for i in range(len(toks), 1, -1):
            cand = " ".join(toks[:i])
            cands = [cand]
            # Markers are emitted as <const prefix> + a dynamic tail; when
            # the last token carries KEY=VALUE the source holds the KEY=
            # half (e.g. "gosound: resampler=" + rs name).
            if "=" in toks[i - 1]:
                cands.append(cand[:cand.rindex("=") + 1])
            if any(c in all_src for c in cands):
                fresh = True
                break
        if not fresh:
            # Or the marker is BUILT: `"gostore: " + verb + ...` leaves no
            # literal `gostore: update`. Fresh iff every STATIC word token
            # (plain lowercase word; `=`, `->`, digits, paths, CAPS count
            # as dynamic) still appears as a word in the tree.
            static_re = re.compile(r"^[a-z][a-z0-9_-]{2,}$")
            static_toks = [t for t in toks[1:] if static_re.match(t)]
            fresh = all(
                re.search(rf"(?<![\w]){re.escape(t)}(?![\w])", all_src)
                for t in static_toks) if static_toks else False
        if not fresh:
            stale.append((spec_name, mk))
    for spec_name, mk in stale[:5]:
        findings.append(("stale-spec",
                         f"{spec_name}: marker '{mk}' — even "
                         f"'{' '.join(mk.split()[:2])}' is absent from "
                         "the app source"))
    if len(stale) > 5:
        findings.append(("stale-spec",
                         f"+{len(stale) - 5} more stale marker(s)"))
    notes.append(f"{len(markers)} spec marker(s) across "
                 f"{len({s for s, _m in markers})} spec(s); "
                 f"{len(stale)} stale")


def check_dead_end(srcdir, texts, findings, notes):
    """argv-mode verbs whose windowed face nothing else names. A verb is
    suspect when the function its dispatch calls opens a window, and the
    verb word appears nowhere else in non-comment code (no key, no hint,
    no chooser arm)."""
    verbs = []  # (verb, dispatch_file, impl_names)
    sw_re = re.compile(r"switch\s+[^\n{]*(?:argv|args|Args)\[\d+\][^\n{]*{")
    case_re = re.compile(r"""case\s+"([a-z][\w-]*)"\s*:""")
    for rel, text in texts:
        for sw in sw_re.finditer(text):
            block = text[sw.start():sw.start() + 4000]
            # Don't bleed into the NEXT switch or function in the window.
            nxt = re.search(r"\n[ \t]*(?:switch|func)\s", block[1:])
            if nxt:
                block = block[:1 + nxt.start()]
            for cm in case_re.finditer(block):
                seg = block[cm.end():cm.end() + 400]
                call = re.search(r'\.(\w+)\s*\(', seg)
                if call is None or call.group(1) in (
                        "Exit", "Println", "Sprintf", "Errorf"):
                    bare = [m for m in re.finditer(r'(\w+)\s*\(', seg)
                            if m.group(1) not in (
                                "Exit", "Println", "Sprintf", "Errorf")]
                    call = bare[0] if bare else None
                verbs.append((cm.group(1), rel,
                              call.group(1) if call else None))
    if not verbs:
        return
    impl_windowed = {}   # impl name -> (file, body_start, body_end)
    for _v, _r, impl in verbs:
        if not impl:
            continue
        for rel, text in texts:
            for fm in re.finditer(
                    rf"func[^{{]*\b{re.escape(impl)}\s*\([^{{]*{{", text):
                body = text[fm.start():fm.start() + 6000]
                end = re.search(r"^func ", body[1:], re.M)
                bend = fm.start() + (1 + end.start() if end else len(body))
                if re.search(r"openWindow|Declare|tabapp\.|\.Present\(",
                             body):
                    impl_windowed[impl] = (rel, fm.start(), bend)
    flagged = []
    for verb, rel, impl in verbs:
        if impl not in impl_windowed:
            continue  # headless verb (import/export pattern) — by design
        # Evidence the verb is window-reachable = the word anywhere in
        # non-comment code OUTSIDE its dispatch case and its own impl.
        irel, istart, iend = impl_windowed[impl]
        rest_lines = []
        for r2, t in texts:
            if r2 == irel:
                t = t[:istart] + t[iend:]
            rest_lines += [ln for ln in t.splitlines()
                           if not ln.lstrip().startswith("//")]
        rest = re.sub(rf'case\s+"{re.escape(verb)}"[^\n]*', "",
                      "\n".join(rest_lines))
        if not re.search(re.escape(verb), rest, re.I):
            flagged.append((verb, impl))
    notes.append("argv verbs: "
                 + ", ".join(v for v, _r, _i in verbs)
                 + f"; windowed impls: {sorted(impl_windowed) or 'none'}")
    for verb, impl in flagged:
        findings.append(("dead-end",
                         f"verb '{verb}' opens a windowed face "
                         f"({impl} in {impl_windowed[impl]}) "
                         "nothing in the window path names — "
                         "the gosound/goirc dead-end pattern"))


def review_app(audit, bin_, name, doc, rev):
    """-> (findings, notes, srcdir)."""
    findings, notes = [], []
    srcdir = app_source_dir(audit.get("body"), bin_)
    texts = src_texts(srcdir)
    if srcdir is None:
        notes.append("no app source dir found — source checks skipped")
    check_art(bin_, findings, notes)
    check_design(doc, rev, bin_, texts, findings, notes)
    check_specs(bin_, texts, findings, notes)
    if srcdir is not None and (ROOT / srcdir).is_dir():
        check_dead_end(srcdir, texts, findings, notes)
    return findings, notes, srcdir


# ---------------------------------------------------------------------------
# Output.
# ---------------------------------------------------------------------------

def review_comment(bin_, audit_num, findings, notes, srcdir, rev, prov,
                   filed_url, skipped):
    today = datetime.now(timezone.utc).strftime("%Y-%m-%d")
    lines = [
        f"<!-- {MARKER}:v1 -->",
        f"**Interface review — {bin_}** ({today}, lane #3784)",
        "",
        f"- face reviewed: `{srcdir or 'unknown'}` vs design master "
        f"apps-{rev or '?'} ({prov})",
        "- checks: art adoption · design frame + geometry · control "
        "labels · spec-marker freshness · argv-only faces",
    ]
    for n in notes:
        lines.append(f"  - {n}")
    if findings:
        lines.append(f"- **drift found ({len(findings)}):**")
        for cls, text in findings:
            lines.append(f"  - `{cls}` — {text}")
        lines.append(f"- **filed:** {filed_url}")
    else:
        lines.append("- **no drift** — the shipped face matches the "
                     "design truth the lane can see")
    if skipped:
        lines.append("- skipped ahead in rotation: "
                     + "; ".join(f"{b} ({r})" for b, r in skipped))
    lines.append("")
    lines.append("_Rotation state is this comment's timestamp; next pass "
                 "takes the stalest unreviewed audit._")
    return "\n".join(lines)


def card_body(bin_, name, audit_num, findings, notes, rev):
    lines = [
        f"## Interface drift — {bin_} ({name})",
        "",
        "Filed by the interface-review lane (#3784) from drift the "
        f"shipped face shows against the design master (apps-{rev}) "
        f"and the gate specs. Audit: #{audit_num}.",
        "",
        "**Findings:**",
    ]
    for cls, text in findings:
        lines.append(f"- `{cls}` — {text}")
    lines += [
        "",
        "**Review notes:**",
    ]
    for n in notes:
        lines.append(f"- {n}")
    lines += [
        "",
        "**Scope (bounded):** fix the finding(s) above in the app and/or "
        "its spec — do not redesign the face. If the design master is "
        "what's stale, say so on the audit issue and route it to the "
        "figcraft lane instead of patching code.",
        "",
        "**Verify:** `just verify-portable`; the app's gate "
        "(`just gate <spec>`) stays green.",
        "",
        f"From polish audit #{audit_num} via interface-review lane #3784.",
    ]
    return "\n".join(lines)


def main():
    ap = argparse.ArgumentParser(
        description=__doc__.splitlines()[0],
        formatter_class=argparse.RawDescriptionHelpFormatter)
    ap.add_argument("--dry-run", action="store_true",
                    help="read live state; print, post nothing")
    ap.add_argument("--app", help="review this BIN regardless of rotation")
    ap.add_argument("--audits-json", help="fixture: polish issue list")
    ap.add_argument("--claims-json", help="fixture: claim issue list")
    ap.add_argument("--open-json", help="fixture: open issues naming the bin")
    ap.add_argument("--figcraft", help="local apps-*.figcraft design doc")
    ap.add_argument("--no-fetch", action="store_true",
                    help="design doc from artifacts/fartcraft-cache only")
    a = ap.parse_args()

    audits = load_audits(a.audits_json)
    order = rotation_order(audits)
    if not order:
        print("interface-review: no open polish audits — nothing to do")
        return 0
    never = sum(1 for au, _b, _n in order if last_reviewed(au) is None)
    print(f"rotation: {len(order)} audit(s), {never} never reviewed; "
          f"next up: "
          + ", ".join(f"{b}(#{au['number']})" for au, b, _n in order[:4]))

    doc, rev, prov = load_design_doc(a.figcraft, a.no_fetch)
    print(f"design master: {prov}")

    skipped = []
    picked = None
    claims = None
    candidates = order
    if a.app:
        candidates = [r for r in order if r[1] == a.app]
        if not candidates:
            print(f"interface-review: --app {a.app} names no open audit")
            return 2

    for audit, bin_, name in candidates:
        num = audit["number"]
        open_issues = load_open_issues(bin_.split(".")[0], a.open_json)
        dups = derived_cards(open_issues, num, bin_.split(".")[0])
        if dups:
            skipped.append((bin_, f"open derived card(s) "
                            f"{'/'.join('#' + str(d) for d in dups)}"))
            print(f"skip {bin_} (audit #{num}): open derived card(s) "
                  + ", ".join(f"#{d}" for d in dups))
            continue
        if claims is None:
            claims = load_claims(a.claims_json)
        hits = claim_touches(claims, app_paths(audit.get("body"), bin_))
        if hits:
            by = ", ".join(sorted({f"#{n}" for n, _t, _p in hits}))
            skipped.append((bin_, f"active claim(s) {by} touch its files"))
            print(f"skip {bin_} (audit #{num}): active claim(s) {by} "
                  "touch its files")
            continue
        picked = (audit, bin_, name)
        break

    if picked is None:
        print(f"interface-review: no eligible app this pass "
              f"({len(skipped)} skipped: "
              + "; ".join(f"{b} — {r}" for b, r in skipped) + ")")
        return 0

    audit, bin_, name = picked
    num = audit["number"]
    findings, notes, srcdir = review_app(audit, bin_, name, doc, rev)
    print(f"review {bin_} ({name}, audit #{num}): "
          f"{len(findings)} finding(s)")
    for cls, text in findings:
        print(f"  finding {cls}: {text}")
    for n in notes:
        print(f"  note: {n}")

    filed_url = None
    if findings:
        title = f"{bin_}: interface drift — {findings[0][0]} lane"
        body = card_body(bin_, name, num, findings, notes, rev)
        if a.dry_run:
            filed_url = "(dry-run — not filed)"
            print(f"card (dry-run): {title}")
            print(body)
        else:
            filed_url = gh_text(["issue", "create", "--label", "polish",
                                 "--label", "enhancement",
                                 "--title", title, "--body", body])
            print(f"filed card: {filed_url}")

    comment = review_comment(bin_, num, findings, notes, srcdir, rev,
                             prov, filed_url or "", skipped)
    if a.dry_run:
        print(f"comment (dry-run) on audit #{num}:")
        print(comment)
    else:
        gh_text(["issue", "comment", str(num), "--body", comment])
        print(f"commented on audit #{num}")

    verdict = ("filed card" if filed_url and not a.dry_run
               else "would file card" if findings else "no drift")
    print(f"interface-review: done — {bin_} {verdict}, "
          f"{len(skipped)} skipped ahead")
    return 0


if __name__ == "__main__":
    sys.exit(main())
