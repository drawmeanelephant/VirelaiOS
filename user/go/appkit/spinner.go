package appkit

import (
	"virelai/art"
	"virelai/draw"
	"virelai/widgets"
)

// Spinner is the M203 (#3338, wired #3357) indeterminate indicator — one
// of the f-artcraft #1255 contact sheets staged under /host/ART/, played
// back as a uniform 1-row frame grid (every frame is HxH, so the sheet's
// W/H is its frame count). Sheet names intent, not pixels: art.SpinnerArc
// is the neutral chrome arc, art.SpinnerMascot the mascot pose.
//
// Like Progress the machine is pure: Tick advances Frame and Draw only
// reads it, so a heartbeat timer or the event loop's tick owns the
// cadence — or the caller derives Frame from a clock it already keeps,
// which is what the seat's notification row does (compositeClock % N).
// The degrade is the family contract: a share without the sheet, or a
// canvas that is not a pixel buffer, draws nothing — the caller's
// text/wording keeps answering; art is an upgrade, never a dependency.
type Spinner struct {
	R     widgets.Rect // the box the frame scales into
	Sheet string       // art.Load base name — art.SpinnerArc / art.SpinnerMascot
	Frame int          // advanced by Tick or set from a clock; wraps on Draw

	frames []*draw.BufferCanvas // decoded cell cache, built once per sheet
}

// Tick advances the loop one frame.
func (s *Spinner) Tick() {
	s.Frame++
}

// Glyph is the text form: the classic four-glyph cycle for a surface
// with no raster read (VT rows, the fill-only degrade). Frame picks the
// glyph, so the same tick drives both faces of the widget.
func (s *Spinner) Glyph() byte {
	return "-\\|/"[s.Frame&3]
}

// Frames reports the sheet's frame count, building the per-cell decode
// cache on first read; 0 when the sheet is absent. BufferCanvas has no
// stride field, so each cell is copied out of the sheet row once —
// 12 frames of 32x32 is 48 KiB for the whole SPN_ARC sheet.
func (s *Spinner) Frames() int {
	src := art.Load(s.Sheet)
	if src == nil || src.H <= 0 {
		return 0
	}
	n := src.W / src.H
	if n < 1 {
		return 0
	}
	if s.frames != nil && len(s.frames) == n {
		return n
	}
	fw := src.W / n
	s.frames = make([]*draw.BufferCanvas, n)
	for i := 0; i < n; i++ {
		pix := make([]uint32, fw*src.H)
		for y := 0; y < src.H; y++ {
			copy(pix[y*fw:(y+1)*fw], src.Pix[y*src.W+i*fw:y*src.W+(i+1)*fw])
		}
		s.frames[i] = &draw.BufferCanvas{Pix: pix, W: fw, H: src.H}
	}
	return n
}

// Draw paints the current frame scaled into R on a pixel canvas. It is a
// no-op everywhere else — the caller's text form is Glyph().
func (s *Spinner) Draw(c widgets.RectCanvas) {
	bc, ok := c.(*draw.BufferCanvas)
	if !ok {
		return
	}
	n := s.Frames()
	if n <= 0 || s.R.Empty() {
		return
	}
	f := s.Frame % n
	if f < 0 {
		f += n
	}
	draw.BlitScaled(bc, s.frames[f], s.R)
}
