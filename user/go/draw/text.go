package draw

import (
	"errors"

	"virelai/ttf"
	"virelai/vi"
	"virelai/widgets"
)

var errFontFile = errors.New("draw: font file unavailable or too large")

// ReadFont parses a guest share font for chrome labels or glyphs. Unlike
// vi.ReadFileAll it can read the 411 KiB Inter UI face (that helper caps at
// 256 KiB). The explicit bound also refuses a truncated font before Parse.
func ReadFont(path string, maxBytes int) (*ttf.Face, error) {
	if maxBytes <= 0 || maxBytes > 1024*1024 {
		return nil, errFontFile
	}
	h, rc := vi.FileOpen(path, vi.ModeRead)
	if rc < 0 {
		return nil, errFontFile
	}
	defer vi.FileClose(uint32(h))
	out := make([]byte, 0, min(8192, maxBytes))
	buf := make([]byte, 4096)
	for {
		n, rr := vi.FileRead(uint32(h), buf)
		if rr < 0 || len(out)+n > maxBytes {
			return nil, errFontFile
		}
		if n == 0 {
			break
		}
		out = append(out, buf[:n]...)
	}
	return ttf.Parse(out)
}

// Text draws a clipped TrueType run through Canvas's tinted-mask path. The
// caller supplies a parsed face, so host tests and guest apps use the same
// rasterizer. The pen is at x and the baseline at y.
func Text(c widgets.Canvas, face *ttf.Face, clip widgets.Rect, x, y, size int, text string, rgb uint32) {
	if c == nil || face == nil || clip.Empty() {
		return
	}
	for _, ch := range text {
		g, adv, err := face.Glyph(face.GlyphIndex(ch), size)
		if err == nil && !g.Empty() {
			blitMask(c, g, clip, x+g.BearingX, y-g.BearingY, rgb)
		}
		x += adv
		if x >= clip.Right() {
			break
		}
	}
}

// Glyph draws one PUA chrome icon with the same mask/AA compositing as Text.
func Glyph(c widgets.Canvas, face *ttf.Face, clip widgets.Rect, x, baseline, size int, cp rune, rgb uint32) {
	if c == nil || face == nil || clip.Empty() {
		return
	}
	g, _, err := face.Glyph(face.GlyphIndex(cp), size)
	if err == nil && !g.Empty() {
		blitMask(c, g, clip, x+g.BearingX, baseline-g.BearingY, rgb)
	}
}

func blitMask(c widgets.Canvas, mask *ttf.Mask, clip widgets.Rect, x, y int, rgb uint32) {
	// Only visible mask bytes become pixels. The allocation is bounded by
	// one glyph bitmap; shape hot paths in draw.go remain allocation-free.
	x0, x1 := max(x, clip.X), min(x+mask.Width, clip.Right())
	y0, y1 := max(y, clip.Y), min(y+mask.Height, clip.Bottom())
	if x0 >= x1 || y0 >= y1 {
		return
	}
	w, h := x1-x0, y1-y0
	pix := make([]uint32, w*h)
	for row := 0; row < h; row++ {
		for col := 0; col < w; col++ {
			pix[row*w+col] = uint32(mask.Alpha[(y0-y+row)*mask.Width+x0-x+col]) << 24
		}
	}
	c.BlitTinted(pix, w, h, x0, y0, rgb)
}
