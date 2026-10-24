package appkit

import (
	"testing"

	"virelai/art"
	"virelai/draw"
	"virelai/widgets"
)

// An absent sheet reports zero frames and Draw is a no-op — the degrade
// contract, stated once so a future "fallback art" edit fails here.
func TestSpinnerNoSheetPaintsNothing(t *testing.T) {
	art.Reset()
	defer art.Reset()
	s := Spinner{Sheet: "NOPE_SHT", R: widgets.Rect{W: 14, H: 14}}
	if s.Frames() != 0 {
		t.Fatal("absent sheet must report 0 frames")
	}
	c := &draw.BufferCanvas{Pix: make([]uint32, 64*64), W: 64, H: 64}
	s.Draw(c)
	for _, p := range c.Pix {
		if p != 0 {
			t.Fatal("a missing sheet must paint nothing")
		}
	}
}

// A four-frame sheet wraps Frame at 4 and Draw paints the wrapped cell —
// Tick's cadence and the modulo are the whole machine.
func TestSpinnerFramesWrapAndPaint(t *testing.T) {
	art.Reset()
	defer art.Reset()
	n, h := 4, 4
	pix := make([]uint32, n*h*h)
	for f := 0; f < n; f++ {
		for y := 0; y < h; y++ {
			for x := 0; x < h; x++ {
				pix[y*n*h+f*h+x] = 0xff000000 | uint32(f+1)<<16
			}
		}
	}
	art.Stage("SPN_TST", &draw.BufferCanvas{Pix: pix, W: n * h, H: h})

	s := Spinner{Sheet: "SPN_TST", R: widgets.Rect{W: 8, H: 8}}
	if got := s.Frames(); got != n {
		t.Fatalf("frames = %d want %d", got, n)
	}
	s.Frame = 5 // wraps to cell 1
	c := &draw.BufferCanvas{Pix: make([]uint32, 32*32), W: 32, H: 32}
	s.Draw(c)
	if c.Pix[0] == 0 {
		t.Fatal("a staged sheet must paint inside R")
	}
	s.Frame = -1 // wraps to cell 3, never a negative index
	s.Draw(c)
	s.Frame = 3
	if got := s.Glyph(); got != '/' {
		t.Fatalf("glyph = %q want '/'", got)
	}
	s.Tick()
	if s.Frame != 4 {
		t.Fatalf("tick advanced to %d want 4", s.Frame)
	}
}
