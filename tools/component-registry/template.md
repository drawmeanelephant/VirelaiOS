<!-- GENERATED FILE — do not hand-edit. Rows live in
     docs/registry/<key>.md (one fragment per row key); this file is
     compiled from the fragments + tools/component-registry/template.md
     by `python3 tools/component-registry/compile.py`. Edit the
     fragment, rerun the compiler, commit both. Declaring this file as
     a Touch is exempt (generated) — claim the fragment instead. -->

# Component registry (M189a, #3225)

One table of every shipped component: name, kind, binary, the one version
surface that answers "what version is it", the owning gate spec or contract,
and its state. The rows are authored as fragments —
`docs/registry/<key>.md` holds one row key (binary stem for binaries,
the component's name otherwise); this file is generated output compiled
by `tools/component-registry/compile.py`, never hand-edited. Checked by
`tools/tests/test_component_registry.py`, which reads the fragments and
runs in `just verify-portable` — a row that disagrees with
`image/apps.txt`, with a spec pin it cites, or with the release grammar
fails the build, and so does a committed aggregate that no longer
matches a fresh compile. The registry is also what the public site
renders: `site/components.md` carries a generated region (M189c, #3227)
produced by `tools/render-component-site.py` off the same fragments,
re-rendered at publish time, and the docs gate fails a PR that lets
`image/apps.txt`, the fragments, or the rendered page drift apart.

## The version rule

"Version" names exactly one contract surface per kind, and that surface
only. A shipped **app**'s version is its seat-stored state envelope
`appver` — the `StateVersion` the app registers with the tabapp harness
in its package under `user/go/`; an app that never saves state has no
version surface and shows `—`. A **store release**'s version is the
signed `ReleaseVersion` const in the head release manifest of its FLINT
stream (ADR 0055 D2) — that versions the release, never the app binary
itself. A **kernel service** or shared-format **contract**'s version is
the ADR number that pins it. The **kernel image**'s version is the
`osver` build stamp (`-Dos-version`, `kernel/src/osver.zig`). A
**shared library** has no version of its own unless it implements a
contract — then the contract's ADR is the surface. A pinned external
**host tool**'s version is its pin file.

Beside that contract surface, every component carries a product version
`ver M.m.p` — a semver-shaped const in the component's own source that
the drift check resolves the same way it resolves `StateVersion` today
(#3463): a `user/go` package carries `const AppVersion`, a Zig source
carries `pub const version`, a producer script carries `VER=`, a Go
library dir carries `const Version`, a host tool its own const. The
`ver` token sits comma-separated beside the kind's contract surface —
`appver`/`ADR`/`osver`/`pin` stay what they are; renaming them would
lie. Two exemptions: kernel-service rows, whose version IS the owning
ADR (the kernel image is the one exception — the product image itself
carries `ver` beside `osver`), and boris, whose pin file is the
surface.

Bump rule: everything seeds `ver 1.0.0` — no fake history; the refresh
ledger already carries touch dates. A PR that substantively touches a
component bumps patch; a feature-adding card bumps minor; a major bump
is an explicit owner decision.

Scope: every `image/apps.txt` row, the seat and the tools the session
stages beside it, the generated ELF fixtures, the shared libraries under
`user/go/` and `user/src/lib/`, the kernel services that own a contract,
the host tools the system cannot boot or publish without, and the gate
fleet — every `tools/go/build-*.sh` target that builds and is staged by
an owning class-B spec but never reaches the session share (the fleet
audit, issue #3397). What stays unlisted is what no `build-*.sh` target
produces: a gate-only Zig .BIN that `build.zig` emits for one spec's
share belongs to that spec, not to this table.

## Shipped apps (image/apps.txt)

Every row below is a manifest row — `app` means "has an `image/apps.txt`
entry", which is also what the drift check enforces in both directions.
All of them additionally answer to the schema-v2 manifest grammar
(`image/apps.txt` header).

| name | kind | binary | version | owning spec / contract | state |
|------|------|--------|---------|------------------------|-------|
{{rows:apps}}

## Seat and staged tools

The session share (`tools/session.sh`) stages these beside the manifest:
the Go seat that is the boot default, the persistence tools the app-bound
credentials name, the shell set `help` advertises plus the audit's other
named tools (issue #3397's table; staged by #3417), and the generated
fixtures the exec/dyn-link gates run. None is a catalog app, so none
carries an `appver` column entry — the seat owns the envelope, it does
not ride in one.

| name | kind | binary | version | owning spec / contract | state |
|------|------|--------|---------|------------------------|-------|
{{rows:seat-tools}}

## Gate fleet — built and gate-staged, not on the session share

Every `tools/go/build-*.sh` target that is neither an `image/apps.txt`
row nor staged by `tools/session.sh`, sorted by the fleet audit (issue
#3397 — the classification table lives there). `staged` on these rows
means the owning class-B spec copies the binary onto its gate share;
none of them rides the session share a daily boot sees. `tool` rows are
real programs driven by `exec`; `fixture` rows exist to serve their
spec. The build-go.sh family is one row — eighteen generated fixtures
sharing one builder and one naming map.

| name | kind | binary | version | owning spec / contract | state |
|------|------|--------|---------|------------------------|-------|
{{rows:gate-fleet}}

### The windowed-candidates verdict (issue #3455)

The audit's four `*`-marked candidates were judged on their landed,
gate-proven usage — one promotion, three named refusals:

- **GOPAINT.ELF — promoted.** Its v2 row is not new config: the spec's
  drop leg ran `GOPAINT.ELF | Paint | P | dock=false | v=2 | opens=image`
  gate-locally for every drag-admission leg, and the signed
  ReleaseManifestRow the store journey installs spells the same row. It
  is a real interactive windowed app (brush/line/rect/fill/erase tools,
  undo, QOI save + PNG export, drop-open, `appver 1` save_state), so the
  row moved to `## Shipped apps` and `image/apps.txt`; the spec's share
  setup now asserts the upstream row instead of appending it.
- **GOR3D.ELF — refused.** The window exists to own the golden-match
  viewport the spec snapshots (`tools/gate/specs/go-r3d.spec`): the
  program draws one fixed scene, then its event loop answers only close
  and the first resize (`user/go/r3d/main.go`). No argv, no `opens=`
  surface, no state. A desk tile would launch a static picture of the
  fill-batcher proof — the proof is already its honest row.
- **RSS.ELF — refused.** A bound-tty tea program (the bound-window tty
  shape): every proven launch is `exec RSS.ELF` on a share seeded with
  RSS.OPML/RSS.CACHE and a configured net (`tools/gate/specs/go-rss.spec`).
  A manifest row would promise a no-arg, unseeded desk launch no gate has
  ever run, and a TUI's honest desk face is `exec` inside a terminal,
  not a launcher tile.
- **GOGITUI.ELF — refused.** Same bound-tty shape, and its proven
  invocation is argv-bound — `exec GOGITUI.ELF /host/R`
  (`tools/gate/specs/go-gitui.spec`). A session share carries no
  guaranteed repository path, so the tile would open on a legible error
  rather than a desk face.

## Staged art — the f-artcraft conveyor

Signed f-artcraft drops adopted by `tools/art/adopt.py` into `image/art/`
and staged on the session share under `/host/ART/`. The manifest
(`image/art/ART.TXT`) is the canonical row set — it pins each asset's
source slug, share name, ship size, and both SHA-256s; rows here group
the manifest by family and say which seams consume them. Assets are not
binaries, so the binary column stays `—`.

| name | kind | binary | version | owning spec / contract | state |
|------|------|--------|---------|------------------------|-------|
{{rows:art}}

## Shared libraries — user/go

Importable packages under `user/go/` (every directory whose `.go` files
are not `package main`). No version surface of their own unless one is
listed; the contract column names the ADR or spec their behaviour answers
to where one exists.

| name | kind | binary | version | owning spec / contract | state |
|------|------|--------|---------|------------------------|-------|
{{rows:go-libs}}

## Shared libraries — user/src/lib

Zig library units under `user/src/lib/` (one row per top-level file or
subdirectory package).

| name | kind | binary | version | owning spec / contract | state |
|------|------|--------|---------|------------------------|-------|
{{rows:zig-libs}}

## Kernel services owning a contract

Kernel-resident services whose cross-component behaviour is pinned by an
ADR or a contract doc. The version surface is the contract itself: when
the ADR is amended, the number in this table is what changed.

| name | kind | binary | version | owning spec / contract | state |
|------|------|--------|---------|------------------------|-------|
{{rows:kernel-services}}

## Host tools

The host-side pieces the system cannot boot, image, or publish without.
`binary` names the in-repo source root.

| name | kind | binary | version | owning spec / contract | state |
|------|------|--------|---------|------------------------|-------|
{{rows:host-tools}}

## Row grammar (what the drift check reads)

Rows are authored one key per file under `docs/registry/`: a `---`-fenced
front-matter block (`key` — the filename stem; `section` — which table
below it lands in; `order` — its slot in that table) followed by the
row's table verbatim. Every `|`-delimited line with six cells in a
fragment block is a registry row; the check skips the header and
separator lines. Cells, in order:

- **name** — free text.
- **kind** — `app` (an `image/apps.txt` row), `seat`, `tool` (a staged
  guest binary), `fixture`, `lib`, `kernel-service`, `host-tool`.
- **binary** — the staged name (`NAME.ELF`/`NAME.BIN`/`NAME.SO`/`NAME.EFI`)
  or `—`. For `app` rows it must appear in `image/apps.txt`; for `seat`,
  `tool`, `fixture` and `kernel-service` rows it must be produced by the
  build or staging path (build.zig, `image/`, `tools/`); `lib` rows carry
  `—`; `host-tool` rows name their in-repo source root, which must exist.
- **version** — comma-separated tokens: `ver M.m.p` (the product
  version — the drift check resolves it against a const in the
  component's own source per "The version rule"; at most one per row,
  required on every row outside the named exemptions), `appver N` (the
  app's registered `StateVersion` under `user/go/`), a
  `Release`-prefixed const (one of the ADR 0055 grammar names in
  `user/go/store/release.go`), `ADR NNNN`, `osver`, `pin <path>`, or
  `—`.
- **owning spec / contract** — comma-separated refs: a
  `tools/gate/specs/*.spec` path, `ADR NNNN` (a file under
  `docs/decisions/` with that number must exist), another repo path that
  must exist, or `—`.
- **state** — `shipped`, `staged`, `fixture`, `retained-fallback`, `host`.

The check (`tools/tests/test_component_registry.py`, in
`just verify-portable`) fails when: an `image/apps.txt` row lacks a
registry `app` row or vice versa; a cited spec, ADR, const, or path does
not resolve; a binary is not in the produced set; an `appver` value
disagrees with the `StateVersion` the app's package registers (in either
direction — a new registration with a `—` here is drift too); a `ver`
value disagrees with the product-version const in the component's
source, or the source carries one the row does not show; a fragment is
malformed or its `key`/`section`/`order` disagrees with its name; the
committed aggregate differs from a fresh compile of the fragments; or a
cell uses a token outside this grammar. `--missing-ver` prints the rows
still unseeded and exits 0 — advisory until the seeding cards land.
