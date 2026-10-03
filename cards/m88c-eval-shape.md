# M88c — Execute JS files and bounded interactive eval on the guest

- **Parent index:** M88, [#1915](https://github.com/drawmeanelephant/VirelaiOS/issues/1915).
- **Owner:** Sol 6.1, runtime-product and acceptance implementer; owner review before merge.
- **Depends on:** merged M88a and M88b.
- **Scope finalized by R1:** final scope set by R1 output. R1 chooses the interactive mechanism, loading interface and guest artifact shape.
- **Size / scheduling:** 12–16 agent hours, deferred beyond week one.

## Deliverable

Land the R1-approved guest-facing script runner and REPL or equivalent interactive eval loop, plus the complete M88 acceptance evidence. Exercise the real engine and native host boundary, not a host JS interpreter or marker-only fixture. A file runs and interactive state behaves as declared within every R1 budget; denied features, memory exhaustion and interrupted work return bounded errors and leave the next eval usable. This is not a browser or a new shell/GUI.

## Exclusive ownership

Proposed reservations, finalized by M88a:

- `user/runtimes/js-client/**`: guest-facing adapter and its tests.
- `tools/js-client/**`: isolated product build/inspection recipe and tests.
- `tests/fixtures/js/**`: small pinned accepted/denied scripts and expected results.
- `tools/gate/specs/live-el0-exec.spec`: sole editor for this breakdown.
- Engine/shim files from M88b and shared SDK/shell files are read-only. No root build, manifest or boot-default edit.

## Verification

- Adapter tests pin file errors, output, interaction sequencing, exception recovery and the exact R1 host surface.
- Extend `live-el0-exec.spec`; run `just gate live-el0-exec`. Guest output/share comparisons prove a JS file's computed result and at least two interactive evaluations with the R1-declared state/reset behavior.
- Exercise every stub/refusal class and heap/context/interrupt boundaries with failures asserted. Record engine-added bytes, allocator peak and five cold first-eval timings against the exact R1 hardware/build definition. A timeout is a failure, not a refusal result.
- Preserve existing exec/SDK cases. Run `bash tools/inventory-gates.sh --check` and `just verify-coordination` before the future landing PR.
- Evidence: standard `artifacts/live-el0-exec-{report.txt,serial-*.log,run-*.txt}`, compared guest receipts and actual budget measurements under `artifacts/m88-acceptance/`. Commit only small pinned fixtures, no logs.

## Closes

The PR merging the complete runner/interactive loop and passing R1 acceptance closes this card's issue. That **same final acceptance PR** also closes **M88's index (#1915)**, only after M88a/b issues are closed and every index condition is evidenced. No standalone umbrella-closing action or prerequisite slice.

## Sources read

#1915, all three index acceptance conditions; `tools/gate/SPEC.md:62-101,129-150`; `tools/gate/specs/live-el0-exec.spec:1-36,116-170`.
