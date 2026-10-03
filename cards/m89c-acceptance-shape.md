# M89c — Prove guest PDF bitmaps and bounded corpus refusals

- **Parent index:** M89, [#1916](https://github.com/drawmeanelephant/VirelaiOS/issues/1916).
- **Owner:** Sol 6.1, document acceptance implementer; owner review before merge.
- **Depends on:** merged M89a/b and any M90 dependency explicitly selected by R1.
- **Scope finalized by R1:** final scope set by R1 output. R1 selects the bitmap/output artifact contract, corpus and exact comparison rule.
- **Size / scheduling:** 12–16 agent hours, deferred beyond week one.

## Deliverable

Land a minimal guest invocation/output adapter and independent acceptance corpus proving every declared PDF page renders correctly inside the R1 binary, time, memory and dimensions budgets, while malformed and out-of-subset inputs fail within all hostile-input limits. Produce bitmap artifacts, not a new viewer UI. Keep the invocation bounded and source-fresh; tests must exercise the actual guest engine rather than substitute a host PDF rasterizer.

## Exclusive ownership

Proposed reservations, finalized by M89a:

- `user/render/pdf-proof/**`: guest invocation/output adapter and tests.
- `tools/pdf-proof/**`: product build and independent artifact comparison tests.
- `tests/fixtures/pdf/acceptance/**`: full declared corpus, provenance and small pinned reference bitmaps.
- `tools/gate/specs/live-image-viewer.spec`: sole editor for this breakdown.
- PDF engine, M90, existing GOVIEW and shared image/SDK files remain read-only.

## Verification

- Extend `live-image-viewer.spec` with dedicated **bitmap-producer** boots; preserve its existing QOI, PNG-refusal and missing-file cases. Run `just gate live-image-viewer`.
- Compare each guest-written bitmap against the independently pinned R1 reference, not only a digest emitted by the renderer. Validate dimensions, pixel layout and completeness independently.
- Guest allocation/work/timing receipts and actual binary measurements must satisfy every R1 budget. Every rejected corpus entry has the declared error and completes before a finite gate timeout, with resource recovery and no OOM/hang. A host-killed timeout cannot count as a correct bounded refusal.
- Run host adapter/comparator tests, `bash tools/inventory-gates.sh --check`, `just verify-coordination` and `git diff --check`.
- Evidence: standard `artifacts/live-image-viewer-{report.txt,serial-*.log,run-*.txt}`, copied guest bitmaps/receipts, and corpus comparison/budget results under `artifacts/m89-acceptance/`. Commit only small references and source fixtures.

## Closes

The PR merging the complete guest proof and green declared corpus closes this issue. That **same final acceptance PR** also closes **M89's index (#1916)** after all earlier M89 cards are closed and every index criterion is satisfied. Neither engine-host tests alone nor a demonstration page closes the index.

## Sources read

#1916, all three acceptance conditions and no viewer UI; `tools/gate/specs/live-image-viewer.spec:10-29,38-56,78-127`; `tools/gate/SPEC.md:62-101`.
