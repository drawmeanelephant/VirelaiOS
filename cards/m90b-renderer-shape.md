# M90b — Land the declared SVG renderer and owned vector raster library

- **Parent index:** M90, [#1917](https://github.com/drawmeanelephant/VirelaiOS/issues/1917).
- **Owner:** Sol 6.1, vector-engine implementer; owner review before merge.
- **Depends on:** merged M90a. No M89 dependency.
- **Scope finalized by R1:** final scope set by R1 output. Engine, language, subset, text and vector representation are not selected by this shape.
- **Size / scheduling:** 16–24 agent hours, deferred. R1 replaces this with smaller complete contracts if the selected implementation exceeds that size.

## Deliverable

Land the full R1-declared SVG byte-to-bitmap renderer and its separately callable vector-raster interface, including all supported elements/attributes, exact unsupported/text policies, bounded allocation/work and selected offline dependencies. Implementing a path parser without the declared rendering contract does not finish this card. The vector interface supports consumer calls without SVG parsing; neither SVG scripting nor PDF internals are included.

## Exclusive ownership

Proposed reservations, finalized by M90a:

- `user/render/svg/**`: SVG front end, renderer adapter and contract tests.
- `user/render/vector/**`: shared vector raster implementation and tests; M89 consumers read/call it only.
- `third_party/svg/**`: selected dependency/license/lock/patches, only if needed.
- `tools/svg-engine/**`: isolated offline build/inspection recipe and tests.
- `tests/fixtures/svg/engine/**`: small engine-level feature and boundary vectors.
- No shared Go draw/font/SDK files, PDF files, root build, manifests, status, ADR or gate-spec edits. Any unavoidable shared-file change needs an R1 ownership revision before claims.

## Verification

- Feature and pixel tests cover every declared element/attribute, clipping/transform/fill edge, text decision, malformed input and each refusal/degradation class.
- Test source/depth/segment/work/dimension and allocation boundaries, including integer overflow and cleanup; compare pixels to R1's independent references and exact tolerance policy.
- Compile offline for the approved guest target; inspect dependencies/loader shape and renderer-added bytes. Count total allocations against R1, not just the final buffer.
- Evidence: small pinned vectors/references committed; test, comparison, allocator/work and build/inspection results in `artifacts/m90-engine/`.
- M90c owns the consumer-side and guest acceptance proof in `live-web.spec`; no new verification shell gate. Run read-only SDK regressions and `git diff --check`.

## Closes

The PR merging the **complete declared SVG and shared-vector interfaces with passing contract tests** closes this issue. If R1 splits it, each replacement lands its whole independent interface and closes its own issue. #1917 stays open for independent guest corpus and consumer proof.

## Sources read

#1917, renderer/subset/text/interface requirements; `docs/ui-primitives.md:30-58,137-160`; `docs/decisions/0030-go-is-el0.md:34-59`.
