package draw

import (
	"math"
	"testing"

	"virelai/widgets"
)

func canvas(w, h int, bg uint32) *BufferCanvas {
	c := &BufferCanvas{Pix: make([]uint32, w*h), W: w, H: h}
	for i := range c.Pix {
		c.Pix[i] = bg
	}
	return c
}

func at(c *BufferCanvas, x, y int) uint32 { return c.Pix[y*c.W+x] }

func TestZigQuarterCircleReference(t *testing.T) {
	c := canvas(24, 24, 0xff000000)
	FillRoundedRect(c, widgets.Rect{W: 24, H: 24}, 8, 0xffffffff)
	if at(c, 12, 12) != 0xffffffff {
		t.Fatal("center not filled")
	}
	if at(c, 0, 0) != 0xff000000 {
		t.Fatal("outer corner not clipped")
	}
	corner := at(c, 2, 2)
	if corner <= 0xff000000 || corner >= 0xffffffff {
		t.Fatalf("corner does not have partial coverage: %08x", corner)
	}
	for _, p := range [][2]int{{21, 2}, {2, 21}, {21, 21}} {
		if got := at(c, p[0], p[1]); got != corner {
			t.Fatalf("quarter-circle symmetry %v: %08x != %08x", p, got, corner)
		}
	}
	// Zig computes its corner radius after clipping the rect to the buffer.
	// The contract clamps against the original geometry, not the visible
	// fragment, so a cropped corner must stay a cropped corner.
	c = canvas(6, 6, 0xff000000)
	FillRoundedRect(c, rect(-18, -18, 24, 24), 8, 0xffffffff)
	if at(c, 0, 0) != 0xffffffff {
		t.Fatal("clipping changed the shape's original radius")
	}
	c = canvas(24, 24, 0)
	FillRoundedRect(c, widgets.Rect{W: 24, H: 24}, 8, 0x80ffffff)
	if edge, center := at(c, 2, 2), at(c, 12, 12); edge>>24 == 0 || edge>>24 >= center>>24 || center != 0x80ffffff {
		t.Fatalf("coverage did not scale source alpha: edge %08x center %08x", edge, center)
	}
}

func TestRectAndAlpha(t *testing.T) {
	c := canvas(3, 3, 0)
	FillRect(c, rect(-1, -1, 3, 3), 0xff123456)
	if at(c, 1, 1) != 0xff123456 || at(c, 2, 0) != 0 {
		t.Fatal("rect clipping wrote outside its intersection")
	}
	FillRect(c, widgets.Rect{W: 3, H: 3}, 0x00112233)
	if at(c, 0, 0) != 0xff123456 {
		t.Fatal("alpha zero erased the canvas")
	}
	BlendPixel(c, 2, 2, 0x80ff0000)
	if got := at(c, 2, 2); got != 0x80ff0000 {
		t.Fatalf("straight alpha over transparency: %08x", got)
	}
	BlendPixel(c, 2, 2, 0x800000ff)
	if got := at(c, 2, 2); got != 0xc05500aa {
		t.Fatalf("straight alpha source-over: %08x", got)
	}
	BlendPixel(c, -1, 2, 0xffffffff)
	BlendPixel(c, 3, 2, 0xffffffff)
	if at(c, 0, 2) != 0 || at(c, 2, 2) != 0xc05500aa {
		t.Fatal("out-of-bounds blend changed a pixel")
	}
}

func TestClampsAndInnerStrokes(t *testing.T) {
	r := rect(3, 3, 20, 12)
	a := canvas(27, 19, 0xff203040)
	b := canvas(27, 19, 0xff203040)
	FillRoundedRect(a, r, -50, 0xffeeddcc)
	FillRect(b, r, 0xffeeddcc)
	for i := range a.Pix {
		if a.Pix[i] != b.Pix[i] {
			t.Fatalf("negative radius did not clamp to 0 at pixel %d", i)
		}
	}
	a = canvas(27, 19, 0xff203040)
	b = canvas(27, 19, 0xff203040)
	FillRoundedRect(a, r, 100, 0xffeeddcc)
	FillRoundedRect(b, r, 6, 0xffeeddcc)
	for i := range a.Pix {
		if a.Pix[i] != b.Pix[i] {
			t.Fatalf("radius did not clamp to half-height at pixel %d", i)
		}
	}
	a = canvas(27, 19, 0xff203040)
	StrokeRoundedRect(a, r, 5, 2, 0xffffc000)
	if at(a, 13, 9) != 0xff203040 || at(a, 13, 3) != 0xffffc000 {
		t.Fatal("rounded stroke should leave center and cover top edge")
	}
	if at(a, 2, 5) != 0xff203040 || at(a, 23, 9) != 0xff203040 {
		t.Fatal("rounded stroke spilled beyond the bounds")
	}
	b = canvas(27, 19, 0xff203040)
	StrokeRoundedRect(b, r, 5, 0, 0xffffffff)
	StrokeEllipse(b, r, -1, 0xffffffff)
	StrokeLine(b, 2, 3, 15, 3, 0, 0xffffffff)
	for i, p := range b.Pix {
		if p != 0xff203040 {
			t.Fatalf("weight zero must be a no-op at pixel %d", i)
		}
	}
	a = canvas(27, 19, 0xff203040)
	b = canvas(27, 19, 0xff203040)
	StrokeRoundedRect(a, r, 5, 100, 0xffffc000)
	FillRoundedRect(b, r, 5, 0xffffc000)
	for i := range a.Pix {
		if a.Pix[i] != b.Pix[i] {
			t.Fatalf("wide stroke must saturate to the fill at pixel %d", i)
		}
	}
}

func TestEllipseAndPolygon(t *testing.T) {
	r := rect(4, 4, 16, 16)
	c := canvas(24, 24, 0xff000000)
	FillEllipse(c, r, 0xffffffff)
	if at(c, 4, 4) != 0xff000000 || at(c, 12, 12) != 0xffffffff {
		t.Fatal("circle outer corner or center wrong")
	}
	if at(c, 6, 6) != at(c, 17, 6) || at(c, 6, 6) != at(c, 6, 17) {
		t.Fatal("circle lost four-way symmetry")
	}
	c = canvas(24, 24, 0xff000000)
	StrokeEllipse(c, r, 2, 0xffffffff)
	if at(c, 12, 12) != 0xff000000 || at(c, 12, 4) == 0xff000000 {
		t.Fatal("elliptical ring is not inside its rect")
	}
	pts := []Point{{2, 2}, {20, 2}, {20, 20}, {12, 12}, {2, 20}}
	rev := []Point{{2, 20}, {12, 12}, {20, 20}, {20, 2}, {2, 2}}
	c = canvas(24, 24, 0xff000000)
	d := canvas(24, 24, 0xff000000)
	FillPolygon(c, pts, 0xffabcdef)
	FillPolygon(d, rev, 0xffabcdef)
	for i := range c.Pix {
		if c.Pix[i] != d.Pix[i] {
			t.Fatalf("polygon winding reversed coverage at pixel %d", i)
		}
	}
	if at(c, 12, 7) != 0xffabcdef || at(c, 12, 19) != 0xff000000 {
		t.Fatal("concave polygon inside/outside wrong")
	}
}

func TestPolylineUnionAndBlitTinted(t *testing.T) {
	a := canvas(25, 25, 0)
	b := canvas(25, 25, 0)
	pts := []Point{{4, 4}, {12, 12}, {20, 4}}
	StrokePolyline(a, pts, 3, 0x80ff0000)
	StrokePolyline(b, []Point{pts[2], pts[1], pts[0]}, 3, 0x80ff0000)
	for i := range a.Pix {
		if a.Pix[i] != b.Pix[i] {
			t.Fatalf("reversed polyline differs at pixel %d", i)
		}
	}
	if got := at(a, 12, 12); got != 0x80ff0000 {
		t.Fatalf("translucent joint painted more than once: %08x", got)
	}
	a = canvas(5, 4, 0xff080808)
	src := &BufferCanvas{Pix: []uint32{0x00ffffff, 0x80ff00ff, 0xff000000, 0xffffffff}, W: 2, H: 2}
	BlitTinted(a, src, -1, 1, 0xffff8000)
	if got := at(a, 0, 1); got == 0xff080808 || got == 0x80ff00ff {
		t.Fatalf("blit did not clip and tint by alpha: %08x", got)
	}
	if at(a, 0, 2) != 0xffff8000 {
		t.Fatalf("opaque tint of white source alpha: %08x", at(a, 0, 2))
	}
}

func TestInvalidAndExtremeClips(t *testing.T) {
	c := canvas(2, 2, 0xff101010)
	FillRect(c, rect(math.MinInt, 0, math.MaxInt, 1), 0xffffffff)
	FillRect(c, rect(math.MaxInt, 0, 3, 3), 0xffffffff)
	FillPolygon(c, nil, 0xffffffff)
	FillPolygon(c, []Point{{math.MinInt, -1}, {math.MaxInt, -1}, {math.MaxInt, -2}}, 0xffffffff)
	FillRoundedRect(c, widgets.Rect{W: 0, H: 2}, 2, 0xffffffff)
	FillEllipse(c, widgets.Rect{W: -1, H: 2}, 0xffffffff)
	StrokeLine(c, math.MaxInt, 1, math.MaxInt, 2, 1, 0xffffffff)
	BlitTinted(c, &BufferCanvas{Pix: []uint32{0xffffffff}, W: 2, H: 2}, 0, 0, 0xffffffff)
	if at(c, 0, 0) != 0xff101010 || at(c, 1, 0) != 0xff101010 {
		t.Fatal("extreme off-screen rect or invalid canvas changed pixels")
	}
}

func TestHotPathsNoAllocations(t *testing.T) {
	c := canvas(64, 64, 0xff101010)
	r := rect(1, 1, 24, 20)
	line := [3]Point{{3, 3}, {20, 16}, {30, 4}}
	src := canvas(8, 8, 0x80ffffff)
	for name, fn := range map[string]func(){
		"rect":       func() { FillRect(c, r, 0xff102030) },
		"round":      func() { FillRoundedRect(c, r, 4, 0xff102030) },
		"roundRing":  func() { StrokeRoundedRect(c, r, 4, 2, 0xff102030) },
		"ellipse":    func() { FillEllipse(c, r, 0xff102030) },
		"ovalRing":   func() { StrokeEllipse(c, r, 2, 0xff102030) },
		"line":       func() { StrokeLine(c, 2, 3, 28, 17, 2, 0xff102030) },
		"polygon":    func() { FillPolygon(c, line[:], 0xff102030) },
		"polyline":   func() { StrokePolyline(c, line[:], 2, 0xff102030) },
		"blendPixel": func() { BlendPixel(c, 2, 3, 0x80102030) },
		"blitTinted": func() { BlitTinted(c, src, 2, 3, 0xff102030) },
	} {
		t.Run(name, func(t *testing.T) {
			if allocs := testing.AllocsPerRun(20, fn); allocs != 0 {
				t.Fatalf("hot path allocated: %.0f", allocs)
			}
		})
	}
}

func BenchmarkFullWindowFill(b *testing.B) {
	c := canvas(1280, 720, 0xff102030)
	r := widgets.Rect{W: 1280, H: 720}
	b.ReportAllocs()
	b.SetBytes(int64(c.W * c.H * 4))
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		FillRect(c, r, 0xff204060)
	}
}

func BenchmarkFullWindowRoundedFill(b *testing.B) {
	c := canvas(1280, 720, 0xff102030)
	r := widgets.Rect{W: 1280, H: 720}
	b.ReportAllocs()
	b.SetBytes(int64(c.W * c.H * 4))
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		FillRoundedRect(c, r, 8, 0xff204060)
	}
}
