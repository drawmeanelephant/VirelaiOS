package pdf

import (
	"strings"
	"testing"
)

func TestAnalyticTextSpacingAndIndependentMatrices(t *testing.T) {
	for _, test := range []struct {
		content   string
		positions []int
	}{
		{"BT /F1 3 Tf 3 Tc (AAA) Tj ET", []int{0, 8, 16}},
		{"BT /F1 3 Tf [(A) -1000 (A)] TJ ET", []int{0, 8}},
		{"BT /F1 3 Tf 200 Tz (AA) Tj ET", []int{0, 8}},
		{"BT /F1 3 Tf 1 0 0 1 6 0 Tm (A) Tj ET", []int{8}},
	} {
		src := fontDocument(test.content, "1 0 0 0 1 1 d1 0 0 1 1 re f", "")
		p, _, _ := rendered(t, src)
		for _, x := range test.positions {
			if p.Pix[62*64+x] != 0xff000000 {
				t.Fatal(test.content, x)
			}
		}
	}
	// Word spacing applies to byte 32 only, not to an empty outline whose
	// encoded code is different. All unused Widths remain type checked.
	objects := []string{
		"<< /Type /Catalog /Pages 2 0 R >>", "<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 48 48] /Resources << /Font << /F1 5 0 R >> >> /Contents 4 0 R >>",
		stream([]byte("BT /F1 3 Tf 3 Tw (A A) Tj ET"), ""),
		"<< /Type /Font /Subtype /Type3 /FontBBox [0 0 1 1] /FontMatrix [1 0 0 1 0 0] /CharProcs << /A 6 0 R /Space 7 0 R >> /Encoding << /Differences [32 /Space 65 /A] >> /FirstChar 32 /LastChar 65 /Widths [" + strings.Repeat("1 ", 34) + "] >>",
		stream([]byte("1 0 0 0 1 1 d1 0 0 1 1 re f"), ""), stream([]byte("1 0 0 0 0 0 d1"), ""),
	}
	p, _, _ := rendered(t, document(objects...))
	if p.Pix[62*64] != 0xff000000 || p.Pix[62*64+12] != 0xff000000 || p.Pix[62*64+8] != 0xffffffff {
		t.Fatal("word spacing")
	}
	// Glyph advance updates text matrix only. T* starts from the independent
	// line matrix, and q/Q restores parameters but not text positions.
	p, _, _ = rendered(t, fontDocument("BT /F1 3 Tf 3 TL 0 6 Td (A) Tj T* (A) Tj ET", "1 0 0 0 1 1 d1 0 0 1 1 re f", ""))
	if p.Pix[54*64] != 0xff000000 || p.Pix[58*64] != 0xff000000 {
		t.Fatal("line matrix")
	}
}
