package webrender

import "virelai/webrender/font"

// Surface is the pixel sink the renderer paints into. The guest browser
// implements it over the kernel's fill-batch syscall (slot 46); host tests
// implement it over a memory framebuffer. Fill is the required primitive;
// surfaces may also implement MaskSink.BlitMask for anti-aliased text and
// straight-alpha image coverage. The renderer applies its page clip before
// calling that optional capability. Fill-only surfaces remain supported.
type Surface interface {
	Fill(x, y, w, h int, rgb uint32)
}

// Clip is a half-open pixel rectangle used to keep page paint inside the
// content viewport (the chrome is drawn by the browser into the same window).
type Clip struct {
	X, Y, W, H int
}

// EmptyClip is a clip that rejects everything.
var EmptyClip = Clip{}

// Contains reports whether the pixel is inside the clip.
func (c Clip) Contains(x, y int) bool {
	return x >= c.X && y >= c.Y && x < c.X+c.W && y < c.Y+c.H
}

// Intersect returns the overlap of two clips.
func (c Clip) Intersect(o Clip) Clip {
	x0 := maxInt(c.X, o.X)
	y0 := maxInt(c.Y, o.Y)
	x1 := minInt(c.X+c.W, o.X+o.W)
	y1 := minInt(c.Y+c.H, o.Y+o.H)
	if x1 <= x0 || y1 <= y0 {
		return EmptyClip
	}
	return Clip{X: x0, Y: y0, W: x1 - x0, H: y1 - y0}
}

// DrawText paints text with the built-in 8x8 face as 1-pixel-high spans,
// clipped to c. It is the fixed-size painter the browser chrome uses (the page
// content goes through the renderer's TextEngine instead). Bold is a synthetic
// second strike offset by one pixel — the same trick the Zig userland renderer
// uses, because no bold face exists. It returns the advance width consumed.
func DrawText(s Surface, x, y int, text string, size int, bold bool, rgb uint32, c Clip) int {
	return Bitmap{}.Paint(s, x, y, text, Style{Size: size, Bold: bold}, rgb, c)
}

// bitmapGlyph is the 8x8 face's glyph for a rune.
func bitmapGlyph(ch rune) [8]byte { return font.Glyph8(ch) }

// bitmapGlyphRows emits one glyph's set pixels as spans.
func bitmapGlyphRows(s Surface, x, y int, rows [8]byte, size int, rgb uint32, c Clip) {
	for r := 0; r < 8; r++ {
		py := y + r*size
		if py >= c.Y+c.H || py+size <= c.Y {
			continue
		}
		row := rows[r]
		col := 0
		for col < 8 {
			if row&(1<<uint(col)) == 0 {
				col++
				continue
			}
			start := col
			for col < 8 && row&(1<<uint(col)) != 0 {
				col++
			}
			px := x + start*size
			pw := (col - start) * size
			if px < c.X {
				pw -= c.X - px
				px = c.X
			}
			if px+pw > c.X+c.W {
				pw = c.X + c.W - px
			}
			if pw > 0 {
				fillClipped(s, c, px, py, pw, size, rgb)
			}
		}
	}
}

// UpperASCII maps text into the glyphs the 8x8 face actually has, so unknown
// characters degrade to '?' instead of blank holes (ADR 0028 D5).
func UpperASCII(text string) string {
	b := make([]byte, 0, len(text))
	for _, ch := range text {
		switch {
		case ch == '\t':
			b = append(b, ' ')
		case ch == '\n' || ch == '\r':
			b = append(b, ' ')
		case ch >= 0x20 && ch <= 0x7e:
			b = append(b, byte(ch))
		default:
			b = append(b, '?')
		}
	}
	return string(b)
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}
