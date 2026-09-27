package draw

import (
	"bytes"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"testing"

	"virelai/widgets"
)

func rect(x, y, w, h int) widgets.Rect {
	return widgets.Rect{X: x, Y: y, W: w, H: h}
}

// A diff is never auto-accepted. DRAW_UPDATE_GOLDEN=1 explicitly regenerates
// the pinned images for review, as in webrender/golden_test.go.
func checkGolden(t *testing.T, name string, c *BufferCanvas) {
	t.Helper()
	img := image.NewNRGBA(image.Rect(0, 0, c.W, c.H))
	for i, px := range c.Pix {
		img.Pix[4*i+0] = byte(px >> 16)
		img.Pix[4*i+1] = byte(px >> 8)
		img.Pix[4*i+2] = byte(px)
		img.Pix[4*i+3] = byte(px >> 24)
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join("testdata", "golden", name+".png")
	if os.Getenv("DRAW_UPDATE_GOLDEN") == "1" {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, buf.Bytes(), 0o644); err != nil {
			t.Fatal(err)
		}
		t.Logf("golden updated: %s (%d bytes)", path, buf.Len())
		return
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("missing golden %s (run DRAW_UPDATE_GOLDEN=1 to create): %v", path, err)
	}
	pinned, err := png.Decode(bytes.NewReader(want))
	if err != nil {
		t.Fatalf("decode golden %s: %v", path, err)
	}
	if !pinned.Bounds().Eq(img.Bounds()) {
		t.Fatalf("golden bounds %v != %v", pinned.Bounds(), img.Bounds())
	}
	diffs, first := 0, ""
	for y := 0; y < c.H; y++ {
		for x := 0; x < c.W; x++ {
			a := color.NRGBAModel.Convert(pinned.At(x, y)).(color.NRGBA)
			b := img.NRGBAAt(x, y)
			if a != b {
				diffs++
				if first == "" {
					first = fmt.Sprintf("(%d,%d): %v != %v", x, y, a, b)
				}
			}
		}
	}
	if diffs != 0 {
		t.Fatalf("golden %s differs at %d pixels; first %s", name, diffs, first)
	}
}

func TestShapeGoldens(t *testing.T) {
	const bg, ink, accent = 0xff101b2b, 0xffe7f0ff, 0xff51adf6
	cases := []struct {
		name string
		w, h int
		draw func(*BufferCanvas)
	}{
		{"round", 106, 84, func(c *BufferCanvas) {
			FillRect(c, rect(3, 3, 24, 17), 0xff204060)
			FillRoundedRect(c, rect(30, 3, 24, 17), 1, accent)
			FillRoundedRect(c, rect(57, 3, 24, 17), 4, accent)
			FillRoundedRect(c, rect(84, 3, 24, 17), 30, accent)
			StrokeRoundedRect(c, rect(3, 25, 24, 17), 0, 1, ink)
			StrokeRoundedRect(c, rect(30, 25, 24, 17), 4, 1, ink)
			StrokeRoundedRect(c, rect(57, 25, 24, 17), 8, 2, ink)
			StrokeRoundedRect(c, rect(84, 25, 24, 17), 8, 5, ink)
			FillRoundedRect(c, rect(-6, 50, 40, 28), 12, 0x8060e080)
			StrokeRoundedRect(c, rect(44, 50, 44, 28), 12, 2, 0x80ffcc40)
		}},
		{"ellipse", 104, 82, func(c *BufferCanvas) {
			FillEllipse(c, rect(3, 3, 18, 18), ink)
			FillEllipse(c, rect(27, 3, 33, 18), accent)
			FillEllipse(c, rect(66, 3, 14, 30), accent)
			StrokeEllipse(c, rect(3, 39, 18, 18), 1, ink)
			StrokeEllipse(c, rect(27, 39, 33, 18), 2, ink)
			StrokeEllipse(c, rect(66, 39, 27, 32), 4, accent)
			StrokeEllipse(c, rect(84, 0, 25, 25), 8, 0x80ffcc40)
		}},
		{"polygon", 104, 78, func(c *BufferCanvas) {
			FillPolygon(c, []Point{{4, 3}, {42, 12}, {12, 37}}, ink)
			FillPolygon(c, []Point{{50, 4}, {97, 4}, {97, 37}, {78, 21}, {50, 37}}, accent)
			FillPolygon(c, []Point{{-7, 47}, {28, 42}, {40, 73}, {-7, 73}}, 0x80ffc044)
			FillPolygon(c, []Point{{51, 43}, {96, 71}, {51, 71}, {96, 43}}, 0x80ff5555)
		}},
		{"line", 105, 83, func(c *BufferCanvas) {
			StrokeLine(c, 4, 6, 97, 6, 1, ink)
			StrokeLine(c, 4, 16, 97, 16, 2, accent)
			StrokeLine(c, 4, 30, 97, 30, 5, 0x80ffcc40)
			StrokeLine(c, 5, 38, 42, 76, 1, ink)
			StrokeLine(c, 43, 76, 81, 38, 3, accent)
			StrokeLine(c, 88, 55, 88, 55, 7, ink)
			StrokePolyline(c, []Point{{-8, 75}, {15, 48}, {44, 75}, {68, 50}, {105, 80}}, 3, 0x80f06eec)
		}},
		{"composite", 52, 42, func(c *BufferCanvas) {
			FillRect(c, rect(2, 2, 28, 25), 0x809944cc)
			src := &BufferCanvas{W: 4, H: 2, Pix: []uint32{
				0x00000000, 0x40000000, 0x80000000, 0xff000000,
				0xffeeeeee, 0x80000000, 0x40000000, 0x00000000,
			}}
			BlitTinted(c, src, 25, 14, 0xc0ffc055)
			BlitTinted(c, src, -1, 32, 0xff51adf6)
			BlendPixel(c, 48, 38, 0x80ffffff)
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c := canvas(tc.w, tc.h, bg)
			tc.draw(c)
			checkGolden(t, tc.name, c)
		})
	}
}
