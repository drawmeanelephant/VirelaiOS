# M90c — Prove guest SVG pixels and the independent PDF-facing consumer contract

> **Held, not filed (grounding 2026-10-03, `7ead82ee`).** #1917 says M90b+ come from the R1; corpus, comparison, paths and gate are M90a outputs. `live-web.spec` does not cover SVG raster (its image rung lives in `live-web-ttf`, `live-web.spec:49`), so M90a names an existing spec that does or one new declarative spec. `user/render/` and `tests/consumers/` are outside both ADR-legal homes. File after M90a lands.

- **Parent index:** M90, [#1917](https://github.com/drawmeanelephant/VirelaiOS/issues/1917).
- **Owner:** Sol 6.1, vector acceptance implementer; owner review before merge.
- **Depends on:** merged M90a/b. Does not wait for M89 implementation.
- **Scope finalized by R1:** final scope set by R1 output. R1 defines the corpus, exact pixel comparison, raster artifact and independent consumer representation.
- **Size / scheduling:** 12–16 agent hours, deferred beyond week one.

## Deliverable

Land the minimal guest raster invocation/output proof, an independently checked SVG corpus and a consumer-side test of the M89-facing function contract. The independent consumer calls the vector interface directly, without SVG parser internals; it represents the downstream PDF use agreed in R1 but does not implement PDF. Prove accepted pixels, each refusal/degradation outcome and all budgets on the actual guest renderer. No SVG viewer, WEB feature expansion or Go/native FFI is included.

## Exclusive ownership

Proposed reservations, finalized by M90a:

- `user/render/svg-proof/*`: guest invocation/output adapter and tests.
- `tests/consumers/vector/*`: independent consumer fixture and tests against the published contract.
- `tools/svg-proof/*`: isolated proof build and independent output comparator/tests.
- `tests/fixtures/svg/acceptance/*`: declared corpus, provenance and small pinned references.
- `tools/gate/specs/live-web.spec`: sole editor for this breakdown.
- SVG/vector engine, WEB application, PDF and shared image/font/SDK files remain read-only.

## Verification

- Host consumer tests compile/call the documented interface and independently verify pixels, errors, buffer ownership and scratch/work limits. No importing private parser state.
- Extend `live-web.spec` with isolated guest raster/consumer boots, retaining its current HTML/HTTPS/corpus cases; run `just gate live-web`. The existing image-rendering spec is reused, not a mandate to teach WEB SVG.
- Independently compare every guest-written SVG bitmap with R1 reference output. Check unsupported/text/degradation cases exactly, maximum canvas and complexity limits, allocation recovery, renderer-added bytes and guest timing/memory receipts.
- A missing implementation, empty bitmap or timeout is a failure. Run host comparator tests, `bash tools/inventory-gates.sh --check`, `just verify-coordination` and `git diff --check`.
- Evidence: standard `artifacts/live-web-{report.txt,serial-*.log,run-*.txt}`, copied bitmaps/consumer receipts and actual corpus/budget results under `artifacts/m90-acceptance/`. No committed logs.

## Closes

The PR merging the complete on-guest corpus and independent consumer acceptance closes this issue, and its body says `leaves #1917 open`. AGENTS.md reserves `Closes #N` for the claimed card's own landing PR. The owner then closes M90's index (#1917) with a final evidence comment once every M90 card is closed, the way #1807, #1808, #1816 and #1880 were closed. M89 may still be open; the consumer contract is proved independently, so there is no circular index dependency.

## Sources read

#1917, four acceptance conditions and M89 downstream relationship; `tools/gate/specs/live-web.spec:1-10,33-60`; `tools/gate/SPEC.md:62-101,129-150`.
