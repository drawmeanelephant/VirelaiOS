"""Drift check for docs/component-registry.md (M189a, #3225).

The registry is the canonical component -> version-surface map; this test
is what keeps it from silently lying. It parses the six-cell table rows
and fails when:

  * an image/apps.txt executable has no `app` row, or an `app` row names
    a binary the manifest does not carry (both directions — coverage);
  * a non-app row's binary is not produced anywhere in the build/staging
    path (build.zig, image/, tools/*.sh, tools/*.py);
  * a cited tools/gate/specs/*.spec, `ADR NNNN`, `Release*` const, or
    repo path does not resolve;
  * an `appver N` cell disagrees with the `StateVersion` the app's
    package under user/go/ registers with the tabapp harness — or with
    the version a non-tabapp app stamps on the envelope itself via a
    direct `PostStateSave` call — in either direction, so a new
    registration showing `—` is drift too;
  * a `ver M.m.p` cell disagrees with the product-version const in the
    component's own source (`AppVersion`/`Version`/`pub const version`/
    `VER=`), in either direction — a const with no `ver` here is drift
    too (issue #3463); and
  * a cell carries a kind/state/version token outside the grammar the
    registry documents in its "Row grammar" section.

`--missing-ver` prints the non-exempt rows still lacking a `ver` token
and exits 0 — the advisory ledger the seeding cards burn down.
"""
import re
import sys
import unittest
from glob import glob
from pathlib import Path

ROOT = Path(__file__).resolve().parents[2]
REGISTRY = ROOT / "docs/component-registry.md"
APPS_TXT = ROOT / "image/apps.txt"
RELEASE_GO = ROOT / "user/go/store/release.go"
OSVER = ROOT / "kernel/src/osver.zig"

KINDS = {"app", "seat", "tool", "fixture", "lib", "kernel-service", "host-tool", "asset", "script"}
STATES = {"shipped", "staged", "fixture", "retained-fallback", "host"}

BINARY_RE = re.compile(r"^[A-Z][A-Z0-9_]{0,24}\.(?:ELF|BIN|SO|EFI)$")
ANY_BINARY_RE = re.compile(r"\b[A-Z][A-Z0-9_]{0,24}\.(?:ELF|BIN|SO|EFI)\b")
SPEC_RE = re.compile(r"^tools/gate/specs/[A-Za-z0-9._-]+\.spec$")
ADR_RE = re.compile(r"^ADR\s+(\d{4})$")
APPVER_RE = re.compile(r"^appver\s+(\d+)$")
VER_RE = re.compile(r"^ver\s+(\d+\.\d+\.\d+)$")
PIN_RE = re.compile(r"^pin\s+(\S+)$")
RELEASE_CONST_RE = re.compile(r"^Release[A-Z][A-Za-z0-9]*$")
STATEVERSION_RE = re.compile(r"StateVersion:\s*([A-Za-z_][A-Za-z0-9_]*|\d+)")
# An app outside the tabapp harness stamps the envelope itself; the
# version is PostStateSave's second argument. The trailing [,)] keeps a
# dotted receiver (t.StateVersion) from matching — that is a field read,
# not a literal the const pass can resolve.
POSTSTATESAVE_RE = re.compile(
    r"PostStateSave\([^,]+,\s*([A-Za-z_][A-Za-z0-9_]*|\d+)\s*[,)]"
)


def registry_rows():
    """(lineno, cells) for every `|` row in the file.

    Header and separator lines are excluded; a `|` line that is neither a
    header, a separator, nor a six-cell row is yielded as a one-cell row so
    the grammar test can flag it as malformed rather than skipping it.
    """
    rows = []
    for lineno, line in enumerate(REGISTRY.read_text().splitlines(), 1):
        line = line.strip()
        if not line.startswith("|"):
            continue
        cells = [c.strip() for c in line.strip("|").split("|")]
        if all(set(c) <= {"-", ":", " "} and c for c in cells):
            continue  # separator row
        if cells[0] in ("name", ""):
            continue  # header row
        rows.append((lineno, cells))
    return rows


def apps_txt_executables():
    names = set()
    for line in APPS_TXT.read_text().splitlines():
        line = line.strip()
        if not line or line.startswith("#") or "|" not in line:
            continue
        names.add(line.split("|", 1)[0].strip())
    return names


def produced_binaries():
    """Every NAME.ELF/BIN/SO/EFI the build or staging path can produce.

    Gate specs are part of the staging path: the M201a gate-fleet rows
    name binaries a build-*.sh emits under a computed output name, and
    the spec that copies it onto the gate share is where the literal
    staged name is written down.
    """
    corpus = set(apps_txt_executables())
    files = [ROOT / "build.zig"]
    files += glob(str(ROOT / "image/*"))
    files += glob(str(ROOT / "tools/**/*.sh"), recursive=True)
    files += glob(str(ROOT / "tools/**/*.py"), recursive=True)
    files += glob(str(ROOT / "tools/gate/specs/*.spec"))
    for f in files:
        p = Path(f)
        if p.is_file():
            corpus.update(ANY_BINARY_RE.findall(p.read_text(errors="replace")))
    for js in glob(str(ROOT / "image/scripts/*.js")):
        corpus.add("SCRIPTS/" + Path(js).name.upper())
    return corpus


def release_grammar_consts():
    """requiredConsts + optionalConsts in user/go/store/release.go."""
    text = RELEASE_GO.read_text()
    consts = set()
    for var in ("requiredConsts", "optionalConsts"):
        m = re.search(var + r"\s*=\s*\[\]string\{(.*?)\}", text, re.S)
        assert m, f"{var} not found in {RELEASE_GO}"
        consts.update(re.findall(r'"([A-Za-z]+)"', m.group(1)))
    return consts


def app_package_dir(binary):
    """GOCLOCK.ELF -> user/go/goclock; None for non-Go binaries."""
    stem = binary[:-4].lower() if binary.endswith(".ELF") else None
    if stem is None:
        return None
    candidates = [stem]
    if stem.startswith("go"):
        candidates.append(stem[2:])
    extra = {"web": "browser", "gostalgia": "gsport"}.get(stem)
    if extra:
        candidates.append(extra)
    for cand in candidates:
        if cand and (ROOT / "user/go" / cand).is_dir():
            return ROOT / "user/go" / cand
    return None


def registered_state_versions(pkg_dir):
    """The distinct integer versions a package stamps on state envelopes.

    Two surfaces feed it: `StateVersion:` field values (tabapp
    registrations) and the version argument of direct `PostStateSave`
    calls (apps that carry the envelope outside the harness, like
    browser's stateAppVer). Two passes: collect literal and const-token
    values across the package, then resolve const-valued ones against
    every non-test file (the registration and the const usually live in
    different files). A set is returned so more than one distinct value
    is visible to the caller — one registration is the contract, two is
    ambiguity the test flags.
    """
    texts = [
        go.read_text(errors="replace")
        for go in sorted(pkg_dir.glob("*.go"))
        if not go.name.endswith("_test.go")
    ]
    values = set()
    pending = []
    for text in texts:
        for pattern in (STATEVERSION_RE, POSTSTATESAVE_RE):
            for m in pattern.finditer(text):
                token = m.group(1)
                if token.isdigit():
                    values.add(int(token))
                else:
                    pending.append(token)
    for token in pending:
        pat = re.compile(
            r"(?:const\s+)?" + re.escape(token) + r"\s*(?:u?int?\d*\s*)?=\s*(\d+)\b"
        )
        for text in texts:
            cm = pat.search(text)
            if cm:
                values.add(int(cm.group(1)))
                break
    return values


# --- ver M.m.p resolution (issue #3463) ------------------------------------
#
# `ver` is the product version: one semver-shaped const in the component's
# own source, resolved per kind the same walk app_package_dir /
# registered_state_versions already make for `appver`. The shapes:
#
#   user/go package (.ELF binary)   -> const AppVersion = "M.m.p"
#   Zig source (.BIN/.EFI binary)   -> pub const version = "M.m.p"
#   SCRIPTS/*.JS + producer scripts -> VER="M.m.p" / VER=M.m.p
#   Go library dir (lib row)        -> const Version = "M.m.p"
#   Zig library unit (lib row)      -> pub const version = "M.m.p"
#   host-tool source root           -> const Version / let version
#
# Only a semver-valued const counts — `pub const version: u32 = 4`
# (handoff ABI) and "virelai-gate-roots-…" (vendored roots) are other
# meanings of "version" and must not resolve. kernel-service rows and
# `pin`-carrying rows are exempt: the ADR / the pin file is the surface.

# Zig guest binary -> the source file that carries its version const.
# The kernel image's is osver.zig — a version file, not main.zig, so the
# stamp and the product version share one read site.
ZIG_VER_SOURCES = {
    "KERNEL.BIN": "kernel/src/osver.zig",
    "BOOTAA64.EFI": "boot/src/main.zig",
    "USER.BIN": "user/src/main.zig",
    "TABWM.BIN": "user/src/tabwm.zig",
    "SEXIBURG.BIN": "user/src/sexiburger.zig",
    "WASM.BIN": "user/src/wasm.zig",
}

VER_CONST_RES = {
    "go-app": re.compile(r'\bAppVersion\s*=\s*"(\d+\.\d+\.\d+)"'),
    "go-lib": re.compile(r'\bVersion\s*=\s*"(\d+\.\d+\.\d+)"'),
    "zig": re.compile(r'pub\s+const\s+version\s*(?::\s*\S+\s*)?=\s*"(\d+\.\d+\.\d+)"'),
    "script": re.compile(r'^\s*VER\s*=\s*"?(\d+\.\d+\.\d+)"?\s*$', re.M),
    "swift": re.compile(r'\bversion\s*=\s*"(\d+\.\d+\.\d+)"'),
}


def ver_targets(name, kind, binary, owning):
    """(path, const-kind) pairs the row's `ver` may resolve against.

    A row that declares `ver` but yields no target cannot keep the
    promise — the check fails it. A target carrying a semver const the
    row does not declare is drift in the other direction.
    """
    targets = []
    if binary.endswith(".ELF"):
        pkg = app_package_dir(binary)
        if pkg:
            const_kind = "go-lib" if kind == "lib" else "go-app"
            targets += [
                (go, const_kind)
                for go in sorted(pkg.glob("*.go"))
                if not go.name.endswith("_test.go")
            ]
    if binary in ZIG_VER_SOURCES:
        targets.append((ROOT / ZIG_VER_SOURCES[binary], "zig"))
    if kind == "script" and binary.startswith("SCRIPTS/"):
        targets.append((ROOT / "image/scripts" / binary[8:].lower(), "script"))
    if kind == "lib":
        for unit in name.split("+"):
            unit = unit.strip()
            if unit.endswith("/"):
                lib_dir = ROOT / "user/src/lib" / unit
                targets += [(f, "zig") for f in sorted(lib_dir.glob("*.zig"))]
            elif unit.endswith(".zig"):
                f = ROOT / "user/src/lib" / unit
                if f.exists():
                    targets.append((f, "zig"))
            else:
                go_dir = ROOT / "user/go" / unit
                targets += [
                    (go, "go-lib")
                    for go in sorted(go_dir.glob("*.go"))
                    if not go.name.endswith("_test.go")
                ]
    if kind == "host-tool" and binary != "—":
        root = ROOT / binary
        if root.is_dir():
            targets += [(f, "go-lib") for f in sorted(root.rglob("*.go"))]
            targets += [(f, "swift") for f in sorted(root.rglob("*.swift"))]
    # A producer script named in the owning column carries VER= (the
    # build-go.sh fixture set, tools/mkdyn-elf.py, …).
    for token in [t.strip() for t in owning.split(",")]:
        if token.startswith("tools/") and token.endswith((".sh", ".py")):
            script = ROOT / token
            if script.is_file():
                targets.append((script, "script"))
    return targets


def source_versions(targets):
    """{semver: [files]} of every product-version const in the targets."""
    found = {}
    for path, const_kind in targets:
        for m in VER_CONST_RES[const_kind].finditer(
            path.read_text(errors="replace")
        ):
            found.setdefault(m.group(1), []).append(path)
    return found


def ver_exempt(kind, version_cell):
    """kernel-service rows keep the owning ADR; `pin` rows keep the pin."""
    if kind == "kernel-service":
        return True
    return any(t.strip().startswith("pin ") for t in version_cell.split(","))


def missing_ver_rows():
    """Non-exempt rows carrying no `ver` token — the seeding ledger."""
    out = []
    for lineno, cells in registry_rows():
        if len(cells) != 6 or ver_exempt(cells[1], cells[3]):
            continue
        if not any(VER_RE.match(t.strip()) for t in cells[3].split(",")):
            out.append((lineno, cells[0], cells[1]))
    return out


class TestComponentRegistry(unittest.TestCase):
    def test_row_grammar(self):
        fails = []
        for lineno, cells in registry_rows():
            where = f"component-registry.md:{lineno}"
            if len(cells) != 6:
                fails.append(
                    f"{where}: row has {len(cells)} cells, expected 6 — malformed row"
                )
                continue
            name, kind, binary, version, owning, state = cells
            if kind not in KINDS:
                fails.append(f"{where}: kind {kind!r} not in {sorted(KINDS)}")
            if state not in STATES:
                fails.append(f"{where}: state {state!r} not in {sorted(STATES)}")
            if binary != "—":
                if kind == "host-tool":
                    if "/" not in binary:
                        fails.append(
                            f"{where}: host-tool binary {binary!r} is not a repo path"
                        )
                    elif not (ROOT / binary).exists():
                        fails.append(f"{where}: host-tool root {binary!r} missing")
                elif kind == "script":
                    if not re.match(r"^SCRIPTS/[A-Z][A-Z0-9_]{0,24}\.JS$", binary):
                        fails.append(
                            f"{where}: script binary {binary!r} is not a SCRIPTS/*.JS name"
                        )
                    elif not (ROOT / "image/scripts" / binary[8:].lower()).exists():
                        fails.append(f"{where}: script source {binary!r} missing")
                elif not BINARY_RE.match(binary):
                    fails.append(f"{where}: binary {binary!r} is not a staged name")
            if kind == "lib" and binary != "—":
                fails.append(f"{where}: lib row carries binary {binary!r}")
            for token in [t.strip() for t in version.split(",")]:
                if (
                    token == "—"
                    or APPVER_RE.match(token)
                    or VER_RE.match(token)
                    or token == "osver"
                ):
                    continue
                am = ADR_RE.match(token)
                if am:
                    if not glob(str(ROOT / "docs/decisions" / f"{am.group(1)}-*.md")):
                        fails.append(f"{where}: {token!r} has no docs/decisions file")
                    continue
                if RELEASE_CONST_RE.match(token):
                    continue
                pm = PIN_RE.match(token)
                if pm:
                    if not (ROOT / pm.group(1)).exists():
                        fails.append(f"{where}: pin file {pm.group(1)!r} missing")
                    continue
                fails.append(f"{where}: version token {token!r} not in grammar")
            for token in [t.strip() for t in owning.split(",")]:
                if token == "—":
                    continue
                if SPEC_RE.match(token):
                    if not (ROOT / token).exists():
                        fails.append(f"{where}: spec {token!r} does not exist")
                    continue
                am = ADR_RE.match(token)
                if am:
                    if not glob(str(ROOT / "docs/decisions" / f"{am.group(1)}-*.md")):
                        fails.append(f"{where}: {token!r} has no docs/decisions file")
                    continue
                if (ROOT / token).exists():
                    continue
                fails.append(f"{where}: owning ref {token!r} does not resolve")
        self.assertEqual([], fails, "registry grammar violations:\n  " + "\n  ".join(fails))

    def test_apps_txt_coverage(self):
        manifest = apps_txt_executables()
        rows = registry_rows()
        app_bins = {cells[2] for _, cells in rows if len(cells) == 6 and cells[1] == "app"}
        fails = sorted(
            [f"apps.txt row {n} has no registry app row" for n in manifest - app_bins]
            + [f"registry app row {n} is not in apps.txt" for n in app_bins - manifest]
        )
        self.assertEqual([], fails, "apps.txt <-> registry drift:\n  " + "\n  ".join(fails))

    def test_binaries_are_produced(self):
        corpus = produced_binaries()
        fails = []
        for lineno, cells in registry_rows():
            if len(cells) != 6:
                continue
            name, kind, binary = cells[0], cells[1], cells[2]
            if kind in ("app", "lib", "host-tool") or binary == "—":
                continue
            if binary not in corpus:
                fails.append(
                    f"component-registry.md:{lineno}: {binary} ({name}) is not "
                    "produced by build.zig, image/, or tools/"
                )
        self.assertEqual([], fails, "unproduced binaries:\n  " + "\n  ".join(fails))

    def test_release_consts_in_grammar(self):
        grammar = release_grammar_consts()
        fails = []
        for lineno, cells in registry_rows():
            if len(cells) != 6:
                continue
            for cell in (cells[3], cells[4]):
                for token in [t.strip() for t in cell.split(",")]:
                    if RELEASE_CONST_RE.match(token) and token not in grammar:
                        fails.append(
                            f"component-registry.md:{lineno}: {token} is not in "
                            "the release grammar (user/go/store/release.go)"
                        )
        self.assertEqual([], fails, "const drift:\n  " + "\n  ".join(fails))

    def test_appver_matches_state_version(self):
        fails = []
        for lineno, cells in registry_rows():
            if len(cells) != 6:
                continue
            name, kind, binary, version = cells[0], cells[1], cells[2], cells[3]
            declared = [int(m.group(1)) for m in
                        (APPVER_RE.match(t.strip()) for t in version.split(","))
                        if m]
            pkg = app_package_dir(binary) if binary != "—" else None
            actual = registered_state_versions(pkg) if pkg else set()
            if len(actual) > 1:
                fails.append(
                    f"component-registry.md:{lineno}: {binary} registers "
                    f"ambiguous StateVersions {sorted(actual)}"
                )
                continue
            if declared:
                if len(declared) != 1:
                    fails.append(f"component-registry.md:{lineno}: multiple appver tokens")
                    continue
                if pkg is None:
                    fails.append(
                        f"component-registry.md:{lineno}: appver on {binary} but "
                        "no user/go package resolves"
                    )
                elif not actual:
                    fails.append(
                        f"component-registry.md:{lineno}: {binary} declares "
                        f"appver {declared[0]} but {pkg.relative_to(ROOT)} "
                        "registers no StateVersion"
                    )
                elif actual != {declared[0]}:
                    fails.append(
                        f"component-registry.md:{lineno}: {binary} declares "
                        f"appver {declared[0]} but the package registers {sorted(actual)[0]}"
                    )
            elif kind == "app" and actual:
                fails.append(
                    f"component-registry.md:{lineno}: {binary} registers "
                    f"StateVersion {sorted(actual)[0]} but the registry shows '—'"
                )
        self.assertEqual([], fails, "appver drift:\n  " + "\n  ".join(fails))

    def test_ver_matches_source_const(self):
        fails = []
        for lineno, cells in registry_rows():
            if len(cells) != 6:
                continue
            name, kind, binary, version, owning = cells[0], cells[1], cells[2], cells[3], cells[4]
            where = f"component-registry.md:{lineno}"
            declared = [
                m.group(1)
                for m in (VER_RE.match(t.strip()) for t in version.split(","))
                if m
            ]
            if len(declared) > 1:
                fails.append(f"{where}: multiple ver tokens {declared}")
                continue
            found = source_versions(ver_targets(name, kind, binary, owning))
            if len(found) > 1:
                paths = sorted({str(p.relative_to(ROOT)) for ps in found.values() for p in ps})
                fails.append(
                    f"{where}: ambiguous ver consts {sorted(found)} across {paths}"
                )
                continue
            if declared:
                if not found:
                    fails.append(
                        f"{where}: declares ver {declared[0]} but no "
                        "product-version const resolves in its source"
                    )
                elif declared[0] not in found:
                    paths = sorted(str(p.relative_to(ROOT)) for p in next(iter(found.values())))
                    fails.append(
                        f"{where}: declares ver {declared[0]} but the source "
                        f"carries {next(iter(found))} ({', '.join(paths)})"
                    )
            elif found:
                # Exemption lifts the requirement to carry `ver`, never
                # the agreement rule: a resolvable const the row does
                # not show is drift on any row.
                paths = sorted(str(p.relative_to(ROOT)) for p in next(iter(found.values())))
                fails.append(
                    f"{where}: source carries ver {next(iter(found))} "
                    f"({', '.join(paths)}) but the row shows no ver token"
                )
        self.assertEqual([], fails, "ver drift:\n  " + "\n  ".join(fails))

    def test_osver_surface_exists(self):
        rows = [c for _, c in registry_rows() if len(c) == 6 and "osver" in c[3]]
        if rows:
            self.assertTrue(OSVER.exists(), "osver cited but kernel/src/osver.zig is gone")
            self.assertIn("pub var label", OSVER.read_text())


if __name__ == "__main__":
    if "--missing-ver" in sys.argv:
        missing = missing_ver_rows()
        print(f"{len(missing)} registry rows carry no ver token "
              "(advisory — exempt: kernel-service, pin rows):")
        for lineno, name, kind in missing:
            print(f"  component-registry.md:{lineno}: {name} ({kind})")
        sys.exit(0)
    unittest.main()
