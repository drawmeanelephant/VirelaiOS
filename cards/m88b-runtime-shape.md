# M88b — Land the offline QuickJS port and bounded runtime contract

> **Held, not filed (grounding 2026-10-03, `7ead82ee`).** #1915 says M88b+ come from the R1 and are "not pre-committed here"; this shape's language, scope and paths are M88a outputs. Its `user/runtimes/` and `third_party/` reservations sit outside the ADR-legal native home (`user/src/` adapter plus `user/zig/` SDK, ADR 0038 §2) unless M88a's ADR 0030 amendment names them. File after M88a lands.

- **Parent index:** M88, [#1915](https://github.com/drawmeanelephant/VirelaiOS/issues/1915).
- **Owner:** Sol 6.1, runtime implementer; owner review before merge.
- **Depends on:** merged M88a.
- **Scope finalized by R1:** final scope set by R1 output. This is a followup shape, not authorization to choose shims, engine configuration or host APIs now.
- **Size / scheduling:** 16–24 agent hours, deferred beyond week one.

## Deliverable

Land the pinned, offline-buildable QuickJS engine port and its complete R1-defined runtime interface, including loading/evaluation entrypoints, bounded allocation, teardown, execution interruption, logging callbacks and all provide/stub/refuse dispositions. The deliverable is a reusable runtime with executable contract tests, not a partially ported engine that needs another PR to finish this card. The CLI/interactive product and real guest acceptance belong to M88c. No DOM, filesystem/socket/timer expansion, general libc, POSIX layer or Go FFI is implied.

## Exclusive ownership

Proposed reservations, absent today and ratified/replaced by M88a before claims:

- `third_party/quickjs/*`: pinned source, license, hashes and narrow patch metadata.
- `user/runtimes/quickjs/*`: engine adapter, shims and runtime tests.
- `tools/quickjs-runtime/*`: isolated offline build/inspection recipe and its tests.
- No root `build.zig`, shared `user/zig/*` SDK files, app manifest, CLI, status, ADR or gate-spec ownership. R1 must prove the isolated build can reuse the existing SDK without modifying it; a required shared-file change forces a reviewed ownership revision before claiming.

## Verification

- Tests instantiate the complete R1 interface and exercise every provided, stubbed and refused entry, normal and exceptional eval, heap boundary/over-bound allocation, context capacity, cancellation and repeated create/destroy.
- Build offline for the approved guest target. Inspect for unexpected unresolved platform symbols, dynamic interpreter, libc/POSIX linkage and unsupported loader shape; compare engine-added bytes against R1.
- Evidence: pinned source/lock metadata and small test vectors committed; actual test output, link/symbol inspection, binary-size calculation and allocation/refusal results under `artifacts/m88-runtime/`.
- Run existing SDK tests read-only and `git diff --check`. M88c owns and extends `tools/gate/specs/live-el0-exec.spec`; this card supplies callable fixtures, not a new shell gate. Runtime-library tests do not establish the interactive guest journey.

## Closes

The PR merging the **whole port, bounded interface and passing contract tests** closes this card's filed issue. No “shim prerequisite” PR closes it early or leaves its own issue open. M88 and M88c remain open for their distinct product acceptance.

## Sources read

#1915, offline pin, symbol dispositions and v1 non-goals; `docs/decisions/0038-zig-guest-target.md:74-108,137-145`; `tools/gate/specs/live-el0-exec.spec:19-36`. The proposed directory layout is a reservation, not observed repo state.
