# M89b — Land the bounded PDF parser and page-bitmap engine

- **Parent index:** M89, [#1916](https://github.com/drawmeanelephant/VirelaiOS/issues/1916).
- **Owner:** Sol 6.1, document-engine implementer; owner review before merge.
- **Depends on:** merged M89a; M90 vector implementation only if R1 explicitly selects that dependency.
- **Scope finalized by R1:** final scope set by R1 output. No parser/engine, PDF operator list, font strategy or M90 coupling is chosen by this shape.
- **Size / scheduling:** 24–32 agent hours, deferred. R1 must split this further if the selected complete deliverable will not fit that estimate; each replacement must close on its own complete contract.

## Deliverable

Land the whole R1-defined PDF engine interface from input bytes and page selection to a caller-visible bitmap or bounded error, including selected dependencies, parsing, page rendering, hostile-input accounting and cleanup. Implement every declared in-subset feature and every unsupported/malformed policy; do not label an object parser alone “PDF support.” Consume M90 only through its published contract if selected, with no edits to its files. Guest loading/output packaging and independent full-corpus acceptance belong to M89c.

## Exclusive ownership

Proposed reservations, finalized by M89a:

- `user/render/pdf/**`: PDF engine, adapters and engine-contract tests.
- `third_party/pdf/**`: selected dependency source/license/lock/patches, only if R1 selects one.
- `tools/pdf-engine/**`: isolated offline build/inspection recipe and tests.
- `tests/fixtures/pdf/engine/**`: small engine boundary vectors and fixtures.
- No M90/vector files, shared SDK, root build, app catalog, ADR, status or gate-spec edits. R1 must revise ownership explicitly if a shared-file change is necessary.

## Verification

- Host tests cover every declared feature and error class, truncation boundaries, cycles/depth, object/file limits, invalid dimensions/arithmetic, decompression/work ceilings and allocation failure.
- Pixel tests use independently produced pinned references under R1's exact comparison policy. Count peak allocations and work; a rejected input leaves no usable partial success or retained allocation.
- Offline freestanding build and ELF/dependency inspection establish the approved target and binary-added-byte budget without libc/POSIX leakage.
- Evidence: small pinned vectors/reference metadata committed; actual test, pixel-difference, peak-accounting and build/inspection results in `artifacts/m89-engine/`.
- M89c exclusively extends `live-image-viewer.spec` for guest proof. No new verification shell gate; read-only SDK/vector contract regression tests and `git diff --check` remain required.

## Closes

The PR merging the **complete declared engine and passing contract tests** closes this filed card. If R1 replaces it with separate parser/raster cards, their complete independent interfaces and closure bars replace this draft before filing, not after a partial PR. M89 stays open for on-guest corpus acceptance.

## Sources read

#1916, subset/refusal/engine survey and all acceptance conditions; `docs/ui-primitives.md:30-58,137-160`; `docs/decisions/0030-go-is-el0.md:34-59`.
