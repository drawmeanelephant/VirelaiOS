"""Drift check + staleness report for docs/app-refresh.md (M192a, #3246).

The refresh ledger is the rotation index for refresh waves. The first
table carries one row per image/apps.txt entry with the card that last
touched the app and the ISO-8601 date; the "Non-app staleness" table
extends the same convention to the registry's other components — one
row per docs/component-registry.md row whose kind is seat, tool,
script, lib, kernel-service or host-tool and whose state is not
fixture, in registry order (issue #3665). As a unittest (it rides
`just verify-portable` beside test_component_registry.py) it fails
when:

  * a row is not exactly five cells (`app | binary | last refresh |
    date | notes`) in the app table, or six cells (`component | kind |
    seed path | last refresh | date | notes`) in the non-app table;
  * an apps.txt row has no ledger row or more than one, a ledger row
    names a binary the manifest does not carry, or the ledger's row
    order drifts from manifest order (coverage, both directions);
  * an `app` cell disagrees with the manifest display name on the same
    row;
  * an in-scope registry row has no non-app row or more than one, a
    non-app row names a component or kind the registry does not carry
    on that row, or the non-app row order drifts from registry order;
  * a `kind` cell is not one of the non-app kinds, or a `seed path`
    token does not exist in the repo;
  * a `date` cell is not ISO-8601 `YYYY-MM-DD` (`—` is allowed, but only
    when `last refresh` is `—` too — an untracked row);
  * a `last refresh` cell is neither `—` nor whitespace-separated card
    tokens (`Mnnnx` or `#issue` each).

As a script — `python3 tools/tests/test_app_refresh.py --stale` — it
prints the tables oldest-first for wave filers (`—` dates sort
oldest): the app table, then the non-app table split by wave verb
(#3322) — seat/tool/script/lib rows are the wave tranche surface,
while kernel-service and host-tool rows are audit-only (ADR-amendment
cards plus a yearly sweep, never the tranche) and print in their own
table. That mode is advisory only: staleness is never a gate, time is
not a build input, and --stale exits 0 regardless.
"""
import re
import sys
import unittest
from datetime import date
from pathlib import Path

ROOT = Path(__file__).resolve().parents[2]
LEDGER = ROOT / "docs/app-refresh.md"
APPS_TXT = ROOT / "image/apps.txt"
REGISTRY = ROOT / "docs/component-registry.md"

APP_HEADING = "The ledger"
NONAPP_HEADING = "Non-app staleness"
NONAPP_KINDS = {"seat", "tool", "script", "lib", "kernel-service", "host-tool"}
AUDIT_ONLY_KINDS = {"kernel-service", "host-tool"}

REF_TOKEN_RE = re.compile(r"^(?:M\d+[a-z]|#\d+)$")
EPOCH = date.min


def ledger_sections():
    """{heading: [(lineno, cells)]} — every `|` row under its `## ` section.

    Header and separator lines are excluded; a `|` line that is neither
    is yielded verbatim so the grammar tests flag a malformed row rather
    than skipping it.
    """
    sections = {}
    heading = ""
    for lineno, line in enumerate(LEDGER.read_text().splitlines(), 1):
        line = line.strip()
        if line.startswith("## "):
            heading = line[3:].strip()
            continue
        if not line.startswith("|"):
            continue
        cells = [c.strip() for c in line.strip("|").split("|")]
        if all(set(c) <= {"-", ":", " "} and c for c in cells):
            continue  # separator row
        if cells[0] in ("app", "component", ""):
            continue  # header row
        sections.setdefault(heading, []).append((lineno, cells))
    return sections


def ledger_rows():
    return ledger_sections().get(APP_HEADING, [])


def nonapp_rows():
    return ledger_sections().get(NONAPP_HEADING, [])


def apps_txt_rows():
    """(binary, display name) for each image/apps.txt row, in order."""
    rows = []
    for line in APPS_TXT.read_text().splitlines():
        line = line.strip()
        if not line or line.startswith("#") or "|" not in line:
            continue
        fields = [f.strip() for f in line.split("|")]
        rows.append((fields[0], fields[1]))
    return rows


def registry_rows():
    """(name, kind, state) per six-cell registry row, in file order."""
    rows = []
    for line in REGISTRY.read_text().splitlines():
        line = line.strip()
        if not line.startswith("|"):
            continue
        cells = [c.strip() for c in line.strip("|").split("|")]
        if len(cells) != 6:
            continue
        if all(set(c) <= {"-", ":", " "} and c for c in cells):
            continue  # separator row
        if cells[0] == "name":
            continue  # header row
        rows.append((cells[0], cells[1], cells[5]))
    return rows


def nonapp_scope():
    """Registry rows the non-app table tracks, in registry order."""
    return [
        (name, kind)
        for name, kind, state in registry_rows()
        if kind in NONAPP_KINDS and state != "fixture"
    ]


def parse_date(cell):
    """date for an ISO-8601 cell, EPOCH for `—`, else None."""
    if cell == "—":
        return EPOCH
    try:
        return date.fromisoformat(cell)
    except ValueError:
        return None


def check_ref_and_date(fails, where, refresh, dt):
    if refresh != "—":
        tokens = refresh.split()
        bad = [t for t in tokens if not REF_TOKEN_RE.match(t)]
        if bad:
            fails.append(
                f"{where}: last-refresh token(s) {bad} not in "
                "grammar (Mnnnx or #issue)"
            )
    d = parse_date(dt)
    if d is None:
        fails.append(f"{where}: date {dt!r} is not ISO-8601 YYYY-MM-DD")
    elif d is EPOCH and refresh != "—":
        fails.append(f"{where}: date '—' but last refresh names {refresh!r}")


class TestAppRefresh(unittest.TestCase):
    def test_row_grammar(self):
        fails = []
        for lineno, cells in ledger_rows():
            where = f"app-refresh.md:{lineno}"
            if len(cells) != 5:
                fails.append(
                    f"{where}: row has {len(cells)} cells, expected 5 — malformed row"
                )
                continue
            app, binary, refresh, dt, notes = cells
            if not app or not binary or not notes:
                fails.append(f"{where}: app, binary and notes must be non-empty")
            check_ref_and_date(fails, where, refresh, dt)
        self.assertEqual([], fails, "ledger grammar violations:\n  " + "\n  ".join(fails))

    def test_manifest_coverage_and_order(self):
        manifest = apps_txt_rows()
        ledger = [c for _, c in ledger_rows() if len(c) == 5]
        fails = []
        seen = {}
        for lineno, cells in ledger_rows():
            if len(cells) != 5:
                continue
            if cells[1] in seen:
                fails.append(
                    f"app-refresh.md:{lineno}: duplicate ledger row for {cells[1]} "
                    f"(first at line {seen[cells[1]]})"
                )
            seen[cells[1]] = lineno
        manifest_bins = [b for b, _ in manifest]
        ledger_bins = [c[1] for c in ledger]
        for b in sorted(set(manifest_bins) - set(ledger_bins)):
            fails.append(f"apps.txt row {b} has no ledger row")
        for b in sorted(set(ledger_bins) - set(manifest_bins)):
            fails.append(f"ledger row {b} is not in apps.txt")
        if ledger_bins and manifest_bins and ledger_bins != manifest_bins:
            fails.append("ledger row order differs from apps.txt order")
        names = dict(manifest)
        for lineno, cells in ledger_rows():
            if len(cells) == 5 and cells[1] in names and cells[0] != names[cells[1]]:
                fails.append(
                    f"app-refresh.md:{lineno}: app {cells[0]!r} disagrees with "
                    f"manifest display name {names[cells[1]]!r}"
                )
        self.assertEqual([], fails, "apps.txt <-> ledger drift:\n  " + "\n  ".join(fails))

    def test_nonapp_row_grammar(self):
        fails = []
        for lineno, cells in nonapp_rows():
            where = f"app-refresh.md:{lineno}"
            if len(cells) != 6:
                fails.append(
                    f"{where}: non-app row has {len(cells)} cells, "
                    "expected 6 — malformed row"
                )
                continue
            component, kind, paths, refresh, dt, notes = cells
            if not component or not kind or not paths or not notes:
                fails.append(
                    f"{where}: component, kind, seed path and notes "
                    "must be non-empty"
                )
            if kind and kind not in NONAPP_KINDS:
                fails.append(f"{where}: kind {kind!r} is not a non-app kind")
            for p in paths.split():
                if not (ROOT / p).exists():
                    fails.append(f"{where}: seed path {p!r} does not exist")
            check_ref_and_date(fails, where, refresh, dt)
        self.assertEqual(
            [], fails, "non-app ledger grammar violations:\n  " + "\n  ".join(fails)
        )

    def test_nonapp_registry_coverage_and_order(self):
        scope = nonapp_scope()
        ledger = [c for _, c in nonapp_rows() if len(c) == 6]
        fails = []
        seen = {}
        for lineno, cells in nonapp_rows():
            if len(cells) != 6:
                continue
            if cells[0] in seen:
                fails.append(
                    f"app-refresh.md:{lineno}: duplicate non-app row for "
                    f"{cells[0]!r} (first at line {seen[cells[0]]})"
                )
            seen[cells[0]] = lineno
        scope_names = [n for n, _ in scope]
        ledger_names = [c[0] for c in ledger]
        for n in sorted(set(scope_names) - set(ledger_names)):
            fails.append(f"registry row {n!r} has no non-app ledger row")
        for n in sorted(set(ledger_names) - set(scope_names)):
            fails.append(f"non-app ledger row {n!r} is not an in-scope registry row")
        if ledger_names and scope_names and ledger_names != scope_names:
            fails.append("non-app row order differs from registry order")
        kinds = dict(scope)
        for lineno, cells in nonapp_rows():
            if (
                len(cells) == 6
                and cells[0] in kinds
                and cells[1] != kinds[cells[0]]
            ):
                fails.append(
                    f"app-refresh.md:{lineno}: kind {cells[1]!r} disagrees "
                    f"with registry kind {kinds[cells[0]]!r} for {cells[0]!r}"
                )
        self.assertEqual(
            [], fails, "registry <-> non-app ledger drift:\n  " + "\n  ".join(fails)
        )


def stale_table(rows, columns):
    """One staleness table, oldest row first (`—` dates sort oldest).

    `rows` is `(order_key, cells)` pairs; ties on date resolve by the
    manifest/registry position the caller computed into `order_key`.
    """
    dated = [(parse_date(cells[-2]) or EPOCH, key, cells) for key, cells in rows]
    dated.sort(key=lambda r: (r[0], r[1]))
    table = [columns] + [tuple(c[: len(columns)]) for _, _, c in dated]
    widths = [max(len(row[i]) for row in table) for i in range(len(columns))]
    out = []
    for i, row in enumerate(table):
        line = "  ".join(cell.ljust(widths[j]) for j, cell in enumerate(row))
        out.append(line.rstrip())
        if i == 0:
            out.append("  ".join("-" * w for w in widths))
    return "\n".join(out)


def stale_report():
    """Ledger tables, oldest row first in each.

    The non-app ledger prints as two tables (#3322): the tranche
    surface (seat/tool/script/lib — a wave's ~3-row non-app pick comes
    from the top of this table) and the audit-only rows
    (kernel-service/host-tool — yearly sweep, never tranche-picked).
    """
    app_order = {b: i for i, (b, _) in enumerate(apps_txt_rows())}
    apps = stale_table(
        [
            (app_order.get(c[1], lineno), c)
            for lineno, c in ledger_rows()
            if len(c) == 5
        ],
        ("APP", "BINARY", "LAST REFRESH", "DATE"),
    )
    nonapp_order = {n: i for i, (n, _) in enumerate(nonapp_scope())}
    columns = ("COMPONENT", "KIND", "LAST REFRESH", "DATE")
    rows = [
        (nonapp_order.get(c[0], lineno), c[:2] + c[3:])
        for lineno, c in nonapp_rows()
        if len(c) == 6
    ]
    tranche = stale_table([r for r in rows if r[1][1] not in AUDIT_ONLY_KINDS], columns)
    audit = stale_table([r for r in rows if r[1][1] in AUDIT_ONLY_KINDS], columns)
    audit_heading = "Audit-only staleness (kernel-service, host-tool — yearly sweep, not the tranche)"
    return (
        f"{apps}\n\n{NONAPP_HEADING}\n{'-' * len(NONAPP_HEADING)}\n{tranche}"
        f"\n\n{audit_heading}\n{'-' * len(audit_heading)}\n{audit}"
    )


if __name__ == "__main__":
    if "--stale" in sys.argv:
        print(stale_report())
    else:
        unittest.main()
