package ttf

import "testing"

// M97f F5 (#2108): the composite resolution budget is one shared ledger —
// fan-out may not multiply the per-simple cap.

// fanoutFace builds a two-glyph glyf table in memory: glyph 0 is a
// composite with `fan` components all pointing at glyph 1, a simple glyph
// with `pts` on-curve points in one contour. Long loca, like the shipped
// faces.
func fanoutFace(fan, pts int) *Face {
	// Simple glyph: nc=1, ends=[pts-1], no instructions, one REPEAT flag
	// run (on-curve | x-short-pos | y-short-pos), pts x bytes, pts y bytes.
	simple := []byte{0, 1}                                 // numberOfContours = 1
	simple = append(simple, 0, 0, 0, 0, 0, 0, 0, 0)        // bbox
	simple = append(simple, byte((pts-1)>>8), byte(pts-1)) // endPts[0]
	simple = append(simple, 0, 0)                          // instLen
	simple = append(simple, 0x3f, byte(pts-1))             // flag + repeat
	for i := 0; i < pts; i++ {
		simple = append(simple, byte(i))
	}
	for i := 0; i < pts; i++ {
		simple = append(simple, byte(i))
	}

	// Composite glyph: nc=-1 header, then `fan` records of
	// flags|gid|dx|dy (words + XY args, MORE on all but the last).
	comp := []byte{0xff, 0xff} // numberOfContours = -1
	comp = append(comp, 0, 0, 0, 0, 0, 0, 0, 0)
	for i := 0; i < fan; i++ {
		flags := compArg1And2AreWords | compArgsAreXYValues
		if i+1 < fan {
			flags |= compMoreComponents
		}
		comp = append(comp, byte(flags>>8), byte(flags), 0, 1, 0, 0, 0, 0)
	}

	glyf := append(append([]byte(nil), comp...), simple...)
	loca := []byte{0, 0, 0, 0,
		byte(len(comp) >> 24), byte(len(comp) >> 16), byte(len(comp) >> 8), byte(len(comp)),
		byte(len(glyf) >> 24), byte(len(glyf) >> 16), byte(len(glyf) >> 8), byte(len(glyf))}
	return &Face{numGlyphs: 2, locFormat: 1, loca: loca, glyf: glyf}
}

func TestCompositeFanoutRefused(t *testing.T) {
	// 64 components x 100 points = 6400 resolved points > maxGlyphPoints.
	// Pre-fix this resolved happily (the audit's probe fanned the same
	// way to 8.4M); the shared ledger must refuse the whole glyph.
	f := fanoutFace(64, 100)
	got, err := f.glyphContours(0)
	if err == nil {
		n := 0
		for _, ct := range got {
			n += len(ct)
		}
		t.Fatalf("fan-out composite resolved %d points; want refusal past %d", n, maxGlyphPoints)
	}
	if got != nil {
		t.Fatal("refused glyph returned a partial outline")
	}
}

func TestCompositeUnderBudgetStillResolves(t *testing.T) {
	// 8 components x 100 points = 800 <= maxGlyphPoints: real composites
	// keep working — the ledger refuses overspend, not composition.
	f := fanoutFace(8, 100)
	got, err := f.glyphContours(0)
	if err != nil {
		t.Fatalf("in-budget composite refused: %v", err)
	}
	if len(got) != 8 || len(got[0]) != 100 {
		t.Fatalf("resolved %d contours of %d pts, want 8x100", len(got), len(got[0]))
	}
}
