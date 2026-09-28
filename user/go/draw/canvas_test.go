package draw

import (
	"testing"

	"virelai/vi"
	"virelai/widgets"
)

func TestBytesCanvasBorrowsAndClips(t *testing.T) {
	b := make([]byte, 4*6+7)
	c, err := BytesCanvas(b, 3, 2)
	if err != nil || len(c.Pix) != 6 {
		t.Fatalf("BytesCanvas = %v, %v", c, err)
	}
	c.FillRect(widgets.Rect{X: -1, Y: 0, W: 3, H: 1}, 0xff112233)
	if b[0] != 0x33 || b[1] != 0x22 || b[2] != 0x11 || b[3] != 0xff {
		t.Fatalf("not borrowed 0xAARRGGBB: %x", b[:4])
	}
	if b[8] != 0 || b[24] != 0 {
		t.Fatalf("clipping or tail touched: %x", b)
	}
	c.FillRoundedRect(widgets.Rect{X: 0, Y: 0, W: 3, H: 2}, 1, 0x00112233)
	if c.Pix[0] != 0xff112233 {
		t.Fatal("transparent draw erased the previous pixel")
	}
	for _, tc := range []struct {
		w, h int
		buf  []byte
	}{{0, 2, b}, {3, 0, b}, {4, 2, b}, {3, 3, b}, {1, 1, nil}} {
		if _, err := BytesCanvas(tc.buf, tc.w, tc.h); err == nil {
			t.Fatalf("accepted %dx%d in %d bytes", tc.w, tc.h, len(tc.buf))
		}
	}
}

func TestWidenedCanvasBlitsTintedAndWindowHostRefuses(t *testing.T) {
	var wc widgets.Canvas = &BufferCanvas{Pix: make([]uint32, 16), W: 4, H: 4}
	wc.BlitTinted([]uint32{0xff000000, 0}, 2, 1, 1, 1, 0xff00ff00)
	buf := wc.(*BufferCanvas)
	if buf.Pix[5] != 0xff00ff00 || buf.Pix[6] != 0 {
		t.Fatalf("raw sprite alpha ignored: %#x %#x", buf.Pix[5], buf.Pix[6])
	}
	wc.StrokeRoundedRect(widgets.Rect{W: 4, H: 4}, 1, 1, 0xff0000ff)
	if buf.Pix[1] != 0xff0000ff {
		t.Fatalf("stroke absent: %#x", buf.Pix[1])
	}
	if _, err := WindowCanvas(0, 2, 2); err == nil {
		t.Fatal("zero window id accepted")
	}
	if _, err := WindowCanvas(1, 2, 2); err == nil {
		t.Fatal("host mapped a guest-owned window")
	}
}

func TestLegacyCanvasBadGeometryIsSafe(t *testing.T) {
	var f vi.Filler
	c := FillerCanvas{Filler: &f, WindowID: 1}
	c.FillRect(widgets.Rect{X: -10, Y: -10, W: 2, H: 2}, 0xffffffff)
	c.FillRoundedRect(widgets.Rect{W: -1, H: 2}, 4, 0xffffffff)
	c.StrokeRoundedRect(widgets.Rect{W: 4, H: 4}, 4, 0, 0xffffffff)
	c.BlitTinted([]uint32{1}, 2, 1, 0, 0, 0xffffffff)
	// Host syscalls are unavailable, so this pins safe degradation rather
	// than claiming the kernel processed any batch.
	f.Flush()
}

func TestReadFontBoundAndHostRefusal(t *testing.T) {
	for _, maxBytes := range []int{0, -1, 1024*1024 + 1} {
		if _, err := ReadFont("/host/INTER.TTF", maxBytes); err == nil {
			t.Fatalf("ReadFont accepted invalid bound %d", maxBytes)
		}
	}
	if _, err := ReadFont("/host/INTER.TTF", 500000); err == nil {
		t.Fatal("host stub opened a guest font")
	}
}
