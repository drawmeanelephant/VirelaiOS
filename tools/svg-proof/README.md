# Independent SVG acceptance

Offline guest builds use the local pinned Go 1.27.1 fork. Provision it with
`tools/go/apply.sh` plus `src/make.bash`, or set `GO_FORK_DIR`. No engine,
kernel, runtime, SDK, boot-default or UI changes belong to this tooling.

Host-only oracle preparation needs the exact environment in
`tests/fixtures/svg/acceptance/oracle-lock.json`. Install CairoSVG **2.8.2**
and the pinned dependencies in `artifacts/m90-acceptance/oracle/venv`,
retain the downloaded wheels in `oracle/packages`, and check out tag 2.8.2
at commit `9e8c6ede00dd1c4495fca4809b4cabd628a85eb9` in `oracle/source`.
Keep its LGPL-3.0-or-later notice and dependency notices there. The lock
records source, package/license, wheel and native-library hashes; there is
no substitute renderer or automatic installer. References never use fonts,
resources, the tested renderer, or out-of-subset inputs. The denying fetcher
and `unsafe=False` are mandatory. Exact RGBA-to-BGRA conversion and each
source/reference hash live in `manifest.json`.

For an owner-authorized external test environment, set `SVG_ORACLE_DIR` to
an absolute directory outside the repository containing `venv/` and `packages/`.
`SVG_ORACLE_PROVENANCE=wheel` verifies every frozen wheel, installed executable
payload, license, Python identity and native-library hash without a source
checkout. It explicitly omits the unobserved source commit and `setup.cfg`;
it never invents either, changes reference pixels or repins the original lock.
The small `oracle-wheel-overlay.json` binds that lock and the unchanged
reference manifest, and pins the locally upgraded libpng whose output was
revalidated byte for byte on all 16 frozen PNG/BGRA rows. All other executable
pins remain exact. This mode is verification-only. Cairo code and packages
stay outside the repository.

Use `pip download --require-hashes -r tools/svg-proof/requirements.txt`
for the wheel directory and install those wheels in the isolated venv with
the same `--require-hashes` requirements. CPython 3.14.7, Cairo 1.18.6 and
the native dependency chain must also match the lock. The source checkout
must be present: matching a version string alone is not verification.

```sh
source tools/env-check.sh
artifacts/m90-acceptance/oracle/venv/bin/python tools/svg-proof/oracle.py
bash tools/svg-proof/build.sh
go -C user/go test ./vectorconsumer ./svgproof
artifacts/m90-acceptance/oracle/venv/bin/python -m unittest discover \
  -s tools/svg-proof -p 'test_*.py'
just gate live-svg-raster
bash tools/inventory-gates.sh --check
just verify-coordination
git diff --check
```

`oracle.py --pin` and `corpus.pin()` are explicit corpus-authoring operations,
never invoked by a gate. Small exclusion sources are pinned as hex byte
fixtures in `negatives.json`, including malformed UTF-8; large numerical
boundaries are generated from recipes and checked against `boundaries.json`.
No large bitmap or source-cap fixture is committed.

Each boot invokes one program and holds its diagnostics script until the
primary-task reap line is observed. Process exit alone does not order the
kernel's deferred reap diagnostics. The final receipt must still prove all
tasks reaped, and the free-pool sample must follow those diagnostics.
Arena admission first uses `vi.MmapAnon`. If its unhinted placement returns
EINVAL, the adapter makes one bounded request through the existing
`vi.MmapHint` SDK at `0x80000000`, with the same private anonymous/populated
flags and 8 MiB size. The kernel checks collisions; this is not a pointer
cast, another retained arena, a cap increase or a runtime/SDK edit.
The program prints its completion marker only after all outputs close.
Receipts contain integer `vi.Nanos` durations, cumulative work, raster work,
edges, scratch high-water and zero allocation deltas. Icons have one cold
sample plus 20 repeats. Refusals publish no bitmap. Reuse performs 100
success/refusal cycles, forcing collection after each ten-cycle batch, with
all ten completions pinned in its reuse receipt. The direct consumer
authors its own commands; analytic rect/clip/alpha/page/padding expectations
and a separately authored CairoSVG filled-path reference verify its output.
`Mallocs` is cumulative. A zero delta across each icon's full call sequence
and each ten-cycle reuse batch proves zero allocations in every included
call, without inserting a stop-the-world diagnostic between each tiny call.
All public failure messages are exercised. Direct-call errors are triggered
through exported operations, without private test hooks. XML-only codes
belong to the SVG suite; MemoryLimit/TimeLimit are adapter admission errors.
CurveLimit and scanner-only unreachable attribute totals remain M90b's
parameterized counter tests, not invented successful public fixtures.

`ledger.py` pairs the M90d/M90f `procs receipt` true live-page high-water
with the retained populated 8 MiB arena and pinned sbrk mapping/touch paths,
including alignment padding and L2 metadata not present in MemStats. The 4,096-page field is an
inline record capacity, not a working-set ceiling. Require extensible tracking,
zero record failures/unrecorded pages and `reaped=1`. Runtime remains limited
to 1,536 pages/11 regions, arena plus runtime to 3,584 pages/12 regions.
The pinned source audit must prove fresh zero padding skips the byte sweep;
free-list header writes still touch pages, so guest high-water receipts remain required.
Static ELF pages are checked and reported separately, never subtracted.
Physical pages before exec and after final reap must match exactly. Failed
memory ledgers retain all raw counters and free-pool samples before failing.
Missing diagnostics or unproved source drift fails, not skips.
The final run requires M90d and M90f on main. Details live only under ignored
`artifacts/m90-acceptance/`; gate logs use `artifacts/live-svg-raster-*`.
