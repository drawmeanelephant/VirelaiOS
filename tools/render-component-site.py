#!/usr/bin/env python3
"""site renderer — M189c component page (issue #3227) plus the per-app
showcase pages (issue #3473).

One source of truth (the docs/registry/<key>.md row fragments, compiled
into docs/component-registry.md by tools/component-registry/compile.py),
these generated surfaces:

1. `site/components.md` — front matter and prose are authored by hand;
   the BEGIN/END "generated component-registry table" region is this
   script's output: the registry's table sections verbatim except each
   `app` row, whose name cell gains a wikilink to its `apps/<slug>`
   showcase page (issue #3670 — the join to surface 2 is the renderer's;
   the registry itself stays free of site routes).
2. `site/apps/` — a wholly generated directory: `index.md` links every
   app page in registry row order, and one `<slug>.md` per `app` registry
   row carries name, binary, version cell, owning-spec links, and state.
   The slug is the binary's lowercase stem (`GOCALC.ELF` -> `apps/gocalc`)
   so page ids survive display-name renames.
3. `site/includes/apps/<slug>.story.md` — an optional hand-written
   fragment that inlines verbatim into the app page (the "what it does"
   prose and its asset link). Fragments live under `site/includes/`
   because that is the content root's reserved tree: every `.md`
   elsewhere under `site/` is a published page, so a fragment at
   `site/apps/<slug>.story.md` would ship as a page of its own.
4. `themes/virelaios/layouts/apps.html` — the nested layout the publish
   and docs-gate commands select for `apps/*` pages via `--layout-rule`.
   Theme hrefs are static bytes Boris does not rewrite, so the curated
   header nav and footer need `../x.html` at `apps/` depth; this file is
   generated from `themes/virelaios/layouts/main.html` +
   `themes/virelaios/footer.html`, which stay the single sources.

Run with no arguments to rewrite the committed files in place (idempotent
when in sync); the render also removes generated app pages whose `app`
row is gone, so the directory can never lag the registry:

    python3 tools/render-component-site.py [--check]

--check exits 1 when any committed generated file differs from a fresh
render or the generated set is incomplete — that is what the docs-gate
freshness check calls.

No new toolchain: python3, same as every other gate script.
"""

import argparse
import importlib.util
import pathlib
import re
import sys

ROOT = pathlib.Path(__file__).resolve().parents[1]
SITE_PAGE = ROOT / "site" / "components.md"
APPS_DIR = ROOT / "site" / "apps"
STORIES_DIR = ROOT / "site" / "includes" / "apps"
DECISIONS_DIR = ROOT / "docs" / "decisions"
BLOB = "https://github.com/drawmeanelephant/VirelaiOS/blob/main/"


def _compiler():
    """The fragment compiler — the registry's single source of truth is
    docs/registry/<key>.md; render() returns the aggregate text."""
    spec = importlib.util.spec_from_file_location(
        "component_registry_compile",
        ROOT / "tools" / "component-registry" / "compile.py")
    mod = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(mod)
    return mod


def registry_text() -> str:
    """The aggregate text compiled from the committed fragments."""
    try:
        return _compiler().render(ROOT)
    except ValueError as e:
        raise SystemExit(f"docs/registry/: {e}")


BEGIN = "<!-- BEGIN generated component-registry table (docs/registry/*.md) -->"
END = "<!-- END generated component-registry table -->"

ADR_RE = re.compile(r"^ADR (\d{4})$")
PATHISH_RE = re.compile(r"^\S+/\S+$")
LAYOUT_MAIN = ROOT / "themes" / "virelaios" / "layouts" / "main.html"
LAYOUT_FOOTER = ROOT / "themes" / "virelaios" / "footer.html"
LAYOUT_APPS = ROOT / "themes" / "virelaios" / "layouts" / "apps.html"
# Bare local page routes in theme chrome (`href="name.html"`); external
# URLs, fragments, and {{asset-url}}/{{nav}}-emitted links never match.
LOCAL_HREF_RE = re.compile(r'href="([A-Za-z0-9._-]+\.html)"')


def row_cells(line: str):
    """Stripped cells for a six-cell registry data row, else None."""
    line = line.strip()
    if not line.startswith("|"):
        return None
    cells = [c.strip() for c in line.strip("|").split("|")]
    if len(cells) != 6:
        return None
    if all(set(c) <= {"-", ":", " "} and c for c in cells):
        return None  # separator row
    if cells[0] == "name":
        return None  # header row
    return cells


def app_row_line(line: str, cells: list) -> str:
    """An `app` row with its name cell wikilinked to the app's showcase
    page. `[[apps/slug|name]]` survives table-cell splitting in Boris —
    site/chronicle.md's index table is the landed proof."""
    cells = list(cells)
    cells[0] = f"[[apps/{slug(cells[2])}|{cells[0]}]]"
    tail = "\n" if line.endswith("\n") else ""
    return "| " + " | ".join(cells) + " |" + tail


def render_region() -> str:
    """The registry's table sections as the page's region body — verbatim
    except `app` rows, which carry the showcase-page link."""
    lines = registry_text().splitlines(keepends=True)
    sections, cur = [], None
    for line in lines:
        if line.startswith("## "):
            cur = [line]
            sections.append(cur)
        elif cur is not None:
            cur.append(line)
    sections = [s for s in sections if any(l.lstrip().startswith("|") for l in s)]
    out = [BEGIN + "\n"]
    for s in sections:
        for line in s:
            cells = row_cells(line)
            out.append(app_row_line(line, cells)
                       if cells is not None and cells[1] == "app" else line)
        if not out[-1].endswith("\n"):
            out[-1] += "\n"
        out.append("\n")
    out.append(END + "\n")
    return "".join(out)


def app_rows() -> list:
    """Every six-cell registry row whose kind is `app`, in table order."""
    rows = []
    for line in registry_text().splitlines():
        cells = row_cells(line)
        if cells is None or cells[1] != "app":
            continue
        rows.append({
            "name": cells[0],
            "binary": cells[2],
            "version": cells[3],
            "owners": cells[4],
            "state": cells[5],
        })
    return rows


def slug(binary: str) -> str:
    """`GOCALC.ELF` -> `gocalc`; `TABWM.BIN` -> `tabwm`."""
    if "." not in binary:
        raise SystemExit(
            f"docs/registry/: app row binary {binary!r} has no "
            "extension to slug from")
    return binary.rsplit(".", 1)[0].lower()


def adr_link(token: str) -> str:
    """`ADR 0034` -> a blob link to docs/decisions/0034-*.md when it resolves."""
    m = ADR_RE.match(token)
    if not m:
        return ""
    matches = sorted(DECISIONS_DIR.glob(m.group(1) + "-*.md"))
    if not matches:
        return ""
    return f"[{token}]({BLOB}{matches[0].relative_to(ROOT).as_posix()})"


def link_owners(cell: str) -> str:
    """An owning-spec cell's comma-separated tokens, as markdown links.

    Repo paths (any token containing a `/` and no space) and `ADR NNNN`
    tokens become blob links; anything else stays literal text.
    """
    parts = []
    for tok in cell.split(","):
        tok = tok.strip()
        link = adr_link(tok)
        if link:
            parts.append(link)
        elif PATHISH_RE.match(tok):
            parts.append(f"[`{tok}`]({BLOB}{tok})")
        else:
            parts.append(tok)
    return ", ".join(parts)


def story_path(slug_: str) -> pathlib.Path:
    return STORIES_DIR / f"{slug_}.story.md"


def orphan_stories(rows: list) -> list:
    """Story fragments whose slug no `app` row owns — dead prose."""
    slugs = {slug(r["binary"]) for r in rows}
    if not STORIES_DIR.is_dir():
        return []
    return sorted(p.name for p in STORIES_DIR.glob("*.story.md")
                  if p.name[:-len(".story.md")] not in slugs)


def render_app_page(row: dict) -> str:
    """One generated showcase page for one `app` row."""
    s = slug(row["binary"])
    story = story_path(s)
    out = [
        "---\n",
        f"title: {row['name']}\n",
        "parent: apps/index\n",
        "status: published\n",
        "tags: [apps, showcase]\n",
        "---\n\n",
        f"# {row['name']}\n\n",
        "| field | value |\n",
        "|-------|-------|\n",
        f"| binary | `{row['binary']}` |\n",
        f"| version | {row['version']} |\n",
        f"| owning spec / contract | {link_owners(row['owners'])} |\n",
        f"| state | {row['state']} |\n",
        "\n",
    ]
    if story.is_file():
        body = story.read_text()
        out.append(body if body.endswith("\n") else body + "\n")
        out.append("\n")
    out.append(
        "<!-- generated by tools/render-component-site.py from\n"
        "     docs/registry/*.md — edit the row's fragment, or\n"
        f"     site/includes/apps/{s}.story.md for showcase prose,\n"
        "     not this file -->\n")
    return "".join(out)


def render_index(rows: list) -> str:
    """site/apps/index.md — every app page linked in registry row order."""
    out = [
        "---\n",
        "title: App showcase\n",
        "parent: apps\n",
        "status: published\n",
        "tags: [apps, showcase, registry]\n",
        "---\n\n",
        "# App showcase\n\n",
        "Every `app` row in the [component\n",
        f"registry]({BLOB}docs/component-registry.md) gets one generated\n",
        "page, listed below in registry row order — the same render step\n",
        "that owns [[components]], so the showcase cannot drift from what\n",
        "ships. Where a page carries a showcase section it is a\n",
        "hand-written fragment inlined at render time; the rest of the\n",
        "page is registry fact.\n\n",
    ]
    for r in rows:
        s = slug(r["binary"])
        out.append(f"- [[apps/{s}|{r['name']}]] — `{r['binary']}`, {r['state']}\n")
    out.append(
        "\n<!-- generated by tools/render-component-site.py from\n"
        "     docs/registry/*.md — edit the row's fragment, not this file -->\n")
    return "".join(out)


def render_apps_layout() -> str:
    """The `apps/*` nested layout: main.html with the shared footer
    inlined and every hand-authored page href rebased one level up.

    Theme `href`/`src` bytes are static — Boris emits page-relative links
    only for its own slots ({{nav}}, {{breadcrumb}}, {{asset-url}}, …), so
    the curated header nav and the footer's license link must read
    `../x.html` for pages at `apps/` depth. Generating the variant keeps
    main.html and footer.html the single sources of truth.
    """
    layout = LAYOUT_MAIN.read_text()
    footer = LAYOUT_FOOTER.read_text().rstrip("\n")
    layout = layout.replace("{{footer}}", footer)
    layout = LOCAL_HREF_RE.sub(r'href="../\1"', layout)
    return (
        "<!-- generated by tools/render-component-site.py from\n"
        "     themes/virelaios/layouts/main.html + footer.html —\n"
        "     edit those, not this file -->\n" + layout)


def splice_region(text: str, begin: str, end: str, fresh: str) -> str:
    start = text.index(begin)
    stop = text.index(end) + len(end) + 1  # + trailing newline
    return text[:start] + fresh + text[stop:]


def expected_components() -> str:
    """The committed components.md with the generated region rendered."""
    return splice_region(SITE_PAGE.read_text(), BEGIN, END, render_region())


def expected_files(rows: list) -> dict:
    """repo-relative path -> committed content for every generated file."""
    files = {SITE_PAGE: expected_components()}
    files[LAYOUT_APPS] = render_apps_layout()
    files[APPS_DIR / "index.md"] = render_index(rows)
    for r in rows:
        files[APPS_DIR / (slug(r["binary"]) + ".md")] = render_app_page(r)
    return files


def generated_pages_on_disk() -> set:
    """site/apps/*.md currently committed or present — orphan detection."""
    if not APPS_DIR.is_dir():
        return set()
    return set(APPS_DIR.glob("*.md"))


def main() -> int:
    ap = argparse.ArgumentParser()
    ap.add_argument("--check", action="store_true",
                    help="exit 1 when any committed generated file is stale")
    args = ap.parse_args()
    rows = app_rows()
    for dead in orphan_stories(rows):
        print(f"warning: site/includes/apps/{dead} names no `app` row — ignored")

    try:
        expected = expected_files(rows)
    except (ValueError, OSError):
        print("components.md: generated region missing or malformed")
        return 1
    if args.check:
        stale = []
        for path, fresh in expected.items():
            try:
                committed = path.read_text()
            except OSError:
                stale.append(f"{path.relative_to(ROOT)}: missing")
                continue
            if committed != fresh:
                stale.append(f"{path.relative_to(ROOT)}: stale")
        for orphan in sorted(generated_pages_on_disk() - set(expected)):
            stale.append(f"{orphan.relative_to(ROOT)}: no `app` row backs it")
        if stale:
            print("\n".join(stale))
            print("run `python3 tools/render-component-site.py` "
                  "(M189c freshness guard)")
            return 1
        print("components.md: registry region in sync; "
              f"apps showcase in sync ({len(rows)} app pages)")
        return 0

    APPS_DIR.mkdir(parents=True, exist_ok=True)
    STORIES_DIR.mkdir(parents=True, exist_ok=True)
    for path, fresh in expected.items():
        path.write_text(fresh)
    for orphan in generated_pages_on_disk() - set(expected):
        orphan.unlink()
        print(f"removed {orphan.relative_to(ROOT)} — no `app` row backs it")
    print("components.md: rendered registry region; "
          f"site/apps/: index + {len(rows)} app pages "
          f"({sum(1 for r in rows if story_path(slug(r['binary'])).is_file())} "
          "with story fragments)")
    return 0


if __name__ == "__main__":
    sys.exit(main())
