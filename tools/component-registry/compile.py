#!/usr/bin/env python3
"""Fragment compiler for docs/component-registry.md (issue #3676).

docs/registry/<key>.md is the hand-edited source of truth — one file per
row key (binary stem for binaries, the service/tool/lib name otherwise).
docs/component-registry.md is generated output like site/components.md
and docs/gate-fleet-inventory.md: produced by this compiler, never
hand-edited, and exempt from the coordination gate's one-editor rule
(see is_generated in tools/status/verify-issue-coordination.sh).

Fragment grammar — a file is one or more blocks. A block is a
`---`-fenced front-matter followed by the row's table verbatim:

    ---
    key: gocalc
    section: apps
    order: 10
    ---

    | name | kind | binary | version | owning spec / contract | state |
    |------|------|--------|---------|------------------------|-------|
    | 64-bit Calc | app | GOCALC.ELF | appver 1, ver 1.0.0 | tools/gate/specs/go-calc.spec | shipped |

`key` must equal the filename stem; `section` is one of SECTIONS below;
`order` places the block inside the section (steps of 10 leave gaps for
inserts — pick any free number between neighbours). Header and
separator lines are carried for legibility and skipped on emit — the
template owns the canonical columns. Rows inside a block emit in file
order. A file may carry several blocks when rows share a key across
sections (the go-lib and zig-lib `flate`/`tabapp`/`tls` rows) and a
block may carry several rows when one key owns a row group (the seven
ART.TXT asset rows under `artmanifest`).

    python3 tools/component-registry/compile.py          # regenerate
    python3 tools/component-registry/compile.py --check  # fail on drift

Byte-deterministic: no dates, no host state; same fragments + template
produce the same aggregate in every environment. All file IO pins
encoding="utf-8" — the registry is UTF-8 (em-dashes, —) and Python's
locale-default codec would otherwise misread it under a C/POSIX locale
(the same class of bug the inventory script pins LC_ALL=C against).
"""

import re
import sys
from pathlib import Path

ROOT = Path(__file__).resolve().parents[2]
REGISTRY_DIR = ROOT / "docs" / "registry"
TEMPLATE = ROOT / "tools" / "component-registry" / "template.md"
AGGREGATE = ROOT / "docs" / "component-registry.md"

# Emission order of the table sections in the aggregate; each id has a
# {{rows:<id>}} marker in the template.
SECTIONS = [
    "apps",
    "seat-tools",
    "gate-fleet",
    "art",
    "go-libs",
    "zig-libs",
    "kernel-services",
    "host-tools",
]

MARKER_RE = re.compile(r"^\s*\{\{rows:([a-z-]+)\}\}\s*$")
ROW_RE = re.compile(r"^\s*\|.*\|\s*$")
FIELD_RE = re.compile(r"^([a-z]+):\s*(.*)$")


def is_data_row(line):
    """A `|` line that is a data row — not a header or separator."""
    if not ROW_RE.match(line):
        return False
    cells = [c.strip() for c in line.strip().strip("|").split("|")]
    if all(set(c) <= {"-", ":", " "} and c for c in cells):
        return False  # separator
    if cells[0] in ("name", ""):
        return False  # header
    return True


def parse_fragment(path, root=ROOT):
    """[(meta, [(lineno, row_line), ...])] — the blocks of one fragment.

    meta carries key/section/order. Malformed input raises ValueError
    naming the fragment-relative line.
    """
    rel = path.relative_to(root)
    lines = path.read_text(encoding="utf-8").splitlines()
    blocks = []
    i, n = 0, len(lines)
    while i < n:
        stripped = lines[i].strip()
        if not stripped:
            i += 1
            continue
        if stripped.startswith("<!--"):
            while i < n and "-->" not in lines[i]:
                i += 1
            i += 1
            continue
        if stripped != "---":
            raise ValueError(
                f"{rel}:{i + 1}: expected a --- front-matter fence, "
                f"got {lines[i].strip()!r}")
        i += 1
        meta = {}
        while i < n and lines[i].strip() != "---":
            m = FIELD_RE.match(lines[i].strip())
            if not m:
                raise ValueError(
                    f"{rel}:{i + 1}: bad front-matter line "
                    f"{lines[i].strip()!r} (want key: value)")
            meta[m.group(1)] = m.group(2).strip()
            i += 1
        if i >= n:
            raise ValueError(f"{rel}: unterminated front-matter block")
        i += 1
        rows = []
        while i < n:
            stripped = lines[i].strip()
            if not stripped:
                i += 1
                continue
            if stripped.startswith("<!--"):
                while i < n and "-->" not in lines[i]:
                    i += 1
                i += 1
                continue
            if stripped == "---":
                break
            if not ROW_RE.match(lines[i]):
                raise ValueError(
                    f"{rel}:{i + 1}: expected a table row, "
                    f"got {lines[i].strip()!r}")
            if is_data_row(lines[i]):
                rows.append((i + 1, lines[i].rstrip()))
            i += 1
        blocks.append((meta, rows))
    return blocks


def fragment_errors(path, root=ROOT):
    """All parse/validation errors for one fragment, as strings."""
    rel = path.relative_to(root)
    try:
        blocks = parse_fragment(path, root)
    except ValueError as e:
        return [str(e)]
    if not blocks:
        return [f"{rel}: no front-matter blocks"]
    errs = []
    for meta, rows in blocks:
        for field in ("key", "section", "order"):
            if field not in meta:
                errs.append(f"{rel}: block missing `{field}:` front-matter")
        key = meta.get("key", "")
        if key and key != path.stem:
            errs.append(
                f"{rel}: key {key!r} does not match the filename stem "
                f"{path.stem!r} — rename one")
        section = meta.get("section", "")
        if section and section not in SECTIONS:
            errs.append(
                f"{rel}: section {section!r} not one of "
                f"{', '.join(SECTIONS)}")
        order = meta.get("order", "")
        if order:
            try:
                int(order)
            except ValueError:
                errs.append(f"{rel}: order {order!r} is not an integer")
        if not rows:
            errs.append(f"{rel}: block carries no data rows")
    return errs


def collect_rows(root=ROOT):
    """section -> [(order, key, seq, path, block_lineno, rows)].

    `rows` is the block's [(lineno, row_line)] in file order; seq is the
    file-sorted block index so equal orders still emit deterministically.
    """
    registry_dir = root / "docs" / "registry"
    by_section = {s: [] for s in SECTIONS}
    errors = []
    seq = 0
    for path in sorted(registry_dir.glob("*.md")):
        errs = fragment_errors(path, root)
        if errs:
            errors.extend(errs)
            continue
        for meta, rows in parse_fragment(path, root):
            by_section[meta["section"]].append(
                (int(meta["order"]), meta["key"], seq, path, rows))
            seq += 1
    for section, blocks in by_section.items():
        blocks.sort(key=lambda t: (t[0], t[1], t[2]))
        orders = {}
        for order, key, _, path, _ in blocks:
            orders.setdefault(order, set()).add(key)
        for order, keys in orders.items():
            if len(keys) > 1:
                errors.append(
                    f"section {section}: order {order} claimed by "
                    f"{sorted(keys)} — pick a free number")
    return by_section, errors


def render(root=ROOT):
    """The aggregate text: template with every {{rows:<id>}} spliced."""
    by_section, errors = collect_rows(root)
    if errors:
        raise ValueError("fragment errors:\n  " + "\n  ".join(errors))
    template = (
        root / "tools" / "component-registry" / "template.md"
    ).read_text(encoding="utf-8")
    seen = set()
    out = []
    for line in template.splitlines(keepends=True):
        m = MARKER_RE.match(line)
        if not m:
            out.append(line)
            continue
        section = m.group(1)
        if section not in by_section:
            raise ValueError(
                f"template.md: {{{{rows:{section}}}}} is not a section")
        if section in seen:
            raise ValueError(
                f"template.md: {{{{rows:{section}}}}} appears twice")
        seen.add(section)
        for _, _, _, _, rows in by_section[section]:
            for _, row in rows:
                out.append(row + "\n")
    missing = set(by_section) - seen
    if missing:
        raise ValueError(
            "template.md: no {{rows:...}} marker for section(s) "
            + ", ".join(sorted(missing)))
    text = "".join(out)
    if not text.endswith("\n"):
        text += "\n"
    return text


def main():
    check = "--check" in sys.argv[1:]
    try:
        fresh = render()
    except ValueError as e:
        print(e, file=sys.stderr)
        return 1
    if check:
        try:
            committed = AGGREGATE.read_text(encoding="utf-8")
        except OSError:
            print(f"{AGGREGATE.relative_to(ROOT)}: missing — run "
                  "tools/component-registry/compile.py")
            return 1
        if committed != fresh:
            import difflib
            print(f"{AGGREGATE.relative_to(ROOT)}: stale — run "
                  "python3 tools/component-registry/compile.py")
            diff = difflib.unified_diff(
                committed.splitlines(keepends=True),
                fresh.splitlines(keepends=True),
                "committed", "fresh")
            print("".join(list(diff)[:40]))
            return 1
        print("component-registry.md: in sync with docs/registry/ fragments")
        return 0
    AGGREGATE.write_text(fresh, encoding="utf-8")
    rows = sum(1 for line in fresh.splitlines() if is_data_row(line))
    print(f"component-registry.md: regenerated ({rows} rows from "
          f"{len(list(REGISTRY_DIR.glob('*.md')))} fragments)")
    return 0


if __name__ == "__main__":
    sys.exit(main())
