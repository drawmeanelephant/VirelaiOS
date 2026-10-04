# Authored M89b vectors

These small classic-xref PDF 1.4 documents are authored by the engine
tests, with no system fonts, external resources or third-party document data.
`empty`, `filled`, `cubic`, `type3` and `rgb` anchor white output, ordered
solid fills, cubic outlines, one uncolored Type 3 glyph and a 2×2 RGB image.
Analytic host tests check their semantics. They are not independently
oracle-reviewed acceptance goldens and are not the M89-PDF1 corpus.

The deterministic recipe is `TestEmitVectors` in `user/go/pdf/pdf_test.go`.
Regeneration is opt-in with `PDF_AUTHOR_VECTORS=1`; ordinary tests never
rewrite fixtures. Larger boundary inputs are generated only in host tests.

`time-fill-text-image` pins M89d's full-fill/Type 3/Flate-image workload;
`indirect-widths` pins scalar-width indirection and glyph advance.
Their opt-in recipe is `TestAcceptanceFixVectors` in
`user/go/pdf/acceptance_test.go`. The 8,192-object regression is generated
by the same test file and uses a forward-only 2,048-byte source seam,
including charged discard/reopen behavior, not a random-access source.
