package pdf

import "testing"

// These are analytic subset/ADR expectations, not goldens from an oracle.
// In particular integer pixel cells are half-open; clipped-away cells and
// compound holes have zero coverage, not antialiasing bleed.
func TestAnalyticAcceptanceGeometry(t *testing.T) {
	for _, row := range []struct {
		content string
		pixel   func(int, int) uint32
	}{
		{"3 3 42 42 re 12 12 m 12 36 l 36 36 l 36 12 l h f", func(x, y int) uint32 {
			if x >= 4 && x < 60 && y >= 4 && y < 60 && !(x >= 16 && x < 48 && y >= 16 && y < 48) {
				return 0xff000000
			}
			return 0xffffffff
		}},
		{"3 3 30 30 re 15 15 30 30 re f*", func(x, y int) uint32 {
			a := x >= 4 && x < 44 && y >= 20 && y < 60
			b := x >= 20 && x < 60 && y >= 4 && y < 44
			if a != b {
				return 0xff000000
			}
			return 0xffffffff
		}},
		{"q 1 0 0 rg 0 0 24 48 re W f 0 0 1 rg 0 0 48 48 re f 0 0 12 48 re W* n 0 1 0 rg 0 0 48 48 re f Q", func(x, y int) uint32 {
			if x < 16 {
				return 0xff00ff00
			}
			if x < 32 {
				return 0xff0000ff
			}
			return 0xffffffff
		}},
	} {
		p, _, _ := rendered(t, simple(row.content, ""))
		checkAnalyticPage(t, p, row.pixel)
	}
	// A1.1 refuses the former operator r/g split at the later stream.
	src := document("<< /Type /Catalog /Pages 2 0 R >>", "<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 48 48] /Contents [4 0 R 5 0 R 6 0 R] >>",
		stream([]byte("1 0 0 r"), ""), stream([]byte("g 3 3 24 24 re q 0 "), ""), stream([]byte("1 0 rg Q f"), ""))
	refusal(t, src, MalformedContent)
	// Operands, the path and saved graphics state still cross legal cuts.
	src = document("<< /Type /Catalog /Pages 2 0 R >>", "<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 48 48] /Contents [4 0 R 5 0 R 6 0 R] >>",
		stream([]byte("1 0 0 "), ""), stream([]byte("rg 3 3 24 24 re q 0 "), ""), stream([]byte("1 0 rg Q f"), ""))
	p, _, _ := rendered(t, src)
	checkAnalyticPage(t, p, func(x, y int) uint32 {
		if x >= 4 && x < 36 && y >= 28 && y < 60 {
			return 0xffff0000
		}
		return 0xffffffff
	})
}

func checkAnalyticPage(t *testing.T, p Page, pixel func(int, int) uint32) {
	t.Helper()
	for y := 0; y < p.Height; y++ {
		for x := 0; x < p.Width; x++ {
			if want := pixel(x, y); p.Pix[y*p.Width+x] != want {
				t.Fatalf("%d,%d: %08x want %08x", x, y, p.Pix[y*p.Width+x], want)
			}
		}
	}
}

func TestAnalyticReflectedImageCells(t *testing.T) {
	p, _, _ := rendered(t, imageDocument("q 24 0 0 24 3 3 cm /I1 Do Q q -12 0 0 12 45 3 cm /I1 Do Q",
		[]byte{0, 85, 170, 255}, "/Width 2 /Height 2 /BitsPerComponent 8 /ColorSpace /DeviceGray"))
	checkAnalyticPage(t, p, func(x, y int) uint32 {
		v := uint32(255)
		if x >= 4 && x < 36 && y >= 28 && y < 60 {
			v = uint32(((y-28)/16*2 + (x-4)/16) * 85)
		}
		if x >= 44 && x < 60 && y >= 44 && y < 60 {
			v = uint32(((y-44)/8*2 + (59-x)/8) * 85)
		}
		return 0xff000000 | v*0x010101
	})
	p, _, _ = rendered(t, imageDocument("1 0 1 rg 0 0 48 48 re f q 3 3 36 36 re W n 24 0 0 -24 3 39 cm /I1 Do Q 0 0 0 rg 30 30 6 6 re f",
		[]byte{255, 0, 0, 0, 255, 0, 0, 0, 255, 255, 255, 0}, "/Width 2 /Height 2 /BitsPerComponent 8 /ColorSpace /DeviceRGB"))
	checkAnalyticPage(t, p, func(x, y int) uint32 {
		v := uint32(0xffff00ff)
		if x >= 4 && x < 36 && y >= 12 && y < 44 {
			colors := [4]uint32{0xff0000ff, 0xffffff00, 0xffff0000, 0xff00ff00}
			v = colors[(y-12)/16*2+(x-4)/16]
		}
		if x >= 40 && x < 48 && y >= 16 && y < 24 {
			v = 0xff000000
		}
		return v
	})
}

func TestAnalyticType3SpacingSubrows(t *testing.T) {
	// Seven triangle origins from the PDF text-space advances:
	// Tj: 3,16 pt; TJ: 21.5,28.125 pt; quote: 3 pt;
	// double quote: 3,8.5 pt. Size 4.5 pt maps to 6 px, rise 1.5
	// maps to 2 px. Baselines map to y=14,22,30 after the rise.
	font := "<< /Type /Font /Subtype /Type3 /FontBBox [0 0 1 1] /FontMatrix [1 0 0 1 0 0] /CharProcs << /A 6 0 R /Space 7 0 R >> /Encoding << /Differences [32 /Space 65 /A] >> /FirstChar 32 /LastChar 65 /Widths ["
	for i := 0; i < 34; i++ {
		font += "1 "
	}
	font += "] >>"
	src := document("<< /Type /Catalog /Pages 2 0 R >>", "<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 48 48] /Contents 4 0 R /Resources << /Font << /F1 5 0 R >> >> >>",
		stream([]byte("BT /F1 4.5 Tf 1 Tc 2 Tw 100 Tz 6 TL 1.5 Ts 0 Tr 1 0 0 1 3 36 Tm (A A) Tj [(A) -250 <41>] TJ (A) ' 1 1 (AA) \" ET"), ""),
		font, stream([]byte("1 0 0 0 1 1 d1 0 0 m 1 0 l .5 1 l h f"), ""), stream([]byte("1 0 0 0 0 0 d1"), ""))
	p, _, _ := rendered(t, src)
	// Exact rational interval overlap for four subrows, in 1/48-pixel units.
	// This is closed triangle geometry, independent of vector's edge walker.
	triangles := [][2]int{{192, 14 * 48}, {1024, 14 * 48}, {1376, 14 * 48}, {1800, 14 * 48},
		{192, 22 * 48}, {192, 30 * 48}, {544, 30 * 48}}
	checkAnalyticPage(t, p, func(x, y int) uint32 {
		v := 255
		for _, triangle := range triangles {
			coverage := 0
			for sub := 0; sub < 4; sub++ {
				sy := y*48 + 6 + sub*12
				if sy >= triangle[1]-288 && sy < triangle[1] {
					inset := (triangle[1] - sy) / 2
					coverage += max(0, min((x+1)*48, triangle[0]+288-inset)-max(x*48, triangle[0]+inset))
				}
			}
			alpha := (coverage*255 + 96) / 192
			v = (v*(255-alpha) + 127) / 255
		}
		return 0xff000000 | uint32(v)*0x010101
	})
}
