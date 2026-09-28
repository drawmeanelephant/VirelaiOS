package widgets

import (
	"testing"

	"virelai/theme"
)

// recordingCanvas captures every fill so a test can prove Draw read the same
// rects HitTest uses.
type recordingCanvas struct {
	fills []struct {
		r   Rect
		rgb uint32
	}
}

func (c *recordingCanvas) FillRect(r Rect, rgb uint32) {
	c.fills = append(c.fills, struct {
		r   Rect
		rgb uint32
	}{r, rgb})
}

func (c *recordingCanvas) FillRoundedRect(r Rect, _ int, rgb uint32) {
	c.FillRect(r, rgb)
}

func (c *recordingCanvas) StrokeRoundedRect(r Rect, _, _ int, rgb uint32) {
	c.FillRect(r, rgb)
}

func (c *recordingCanvas) BlitTinted(_ []uint32, _, _, _, _ int, _ uint32) {}

// gridAgreement asserts HitTest(x,y) == Bounds().Contains(x,y) over a grid
// that brackets the widget, for every widget kind.
func gridAgreement(t *testing.T, w Widget, loX, loY, hiX, hiY int) {
	t.Helper()
	b := w.Bounds()
	for y := loY; y <= hiY; y++ {
		for x := loX; x <= hiX; x++ {
			if got, want := w.HitTest(x, y), b.Contains(x, y); got != want {
				t.Fatalf("HitTest(%d,%d)=%v want %v (bounds %+v)", x, y, got, want, b)
			}
		}
	}
}

func TestThreeWidgetsSatisfyInterface(t *testing.T) {
	var ws []Widget = []Widget{
		&Text{R: Rect{4, 4, 60, 16}, Label: "hi"},
		&Button{R: Rect{4, 40, 80, 24}, Label: "OK"},
		&List{R: Rect{4, 80, 120, 90}, Items: []string{"a", "b", "c"}, RowH: 30},
	}
	if len(ws) != 3 {
		t.Fatalf("expected exactly three widgets, got %d", len(ws))
	}
}

func TestTextHitTestAgreement(t *testing.T) {
	w := &Text{R: Rect{10, 10, 40, 12}, Label: "hi", Fg: 0xffffff, Bg: 0x101010}
	gridAgreement(t, w, 5, 5, 55, 30)
}

func TestButtonHitTestAgreement(t *testing.T) {
	w := &Button{R: Rect{20, 30, 60, 20}, Label: "OK", Face: 0x222222, Border: 0x888888}
	gridAgreement(t, w, 15, 25, 85, 55)
	// The border row is grabbable (HitTest is the OUTER rect).
	if !w.HitTest(20, 30) || !w.HitTest(79, 49) {
		t.Fatal("button corners should hit")
	}
	if w.HitTest(80, 30) {
		t.Fatal("right edge is exclusive")
	}
}

func TestButtonThemeStates(t *testing.T) {
	w := &Button{R: Rect{0, 0, 40, 16}, Label: "OK"}
	idleF, idleB, _ := w.colors()
	if idleF != theme.Current.BtnIdle || idleB != theme.Current.Border {
		t.Fatalf("idle face/border %#x/%#x", idleF, idleB)
	}
	w.Hovered = true
	_, hoverB, _ := w.colors()
	if hoverB != theme.Current.Accent {
		t.Fatalf("hover border %#x want accent", hoverB)
	}
	w.Pressed = true
	pressF, pressB, _ := w.colors()
	if pressF != theme.Current.BtnPressed || pressB != theme.Current.Accent {
		t.Fatalf("press %#x/%#x", pressF, pressB)
	}
	c := &recordingCanvas{}
	w.Draw(c)
	if len(c.fills) == 0 || c.fills[0].rgb != theme.Current.Accent {
		t.Fatalf("press draw border fill = %+v", c.fills)
	}
}

func TestButtonIdleOverrideKeepsHex(t *testing.T) {
	w := &Button{R: Rect{0, 0, 20, 10}, Face: 0x222222, Border: 0x888888, LabelRGB: 0xffffff}
	face, border, label := w.colors()
	if face != 0x222222 || border != 0x888888 || label != 0xffffff {
		t.Fatalf("idle override lost: %#x %#x %#x", face, border, label)
	}
}

func TestListHitTestAgreement(t *testing.T) {
	w := &List{R: Rect{0, 0, 100, 90}, Items: []string{"a", "b", "c"}, RowH: 30}
	gridAgreement(t, w, -2, -2, 102, 92)
}

// A point inside RowRect(i) must map to row i, and Draw must use the same
// rects — the CALC-drift guarantee.
func TestListRowRectMatchesItemAt(t *testing.T) {
	l := &List{R: Rect{5, 5, 100, 90}, Items: []string{"a", "b", "c"}, RowH: 30, Fg: 0xffffff, Bg: 0, SelBg: 0x333333}
	for i := range l.Items {
		rr := l.RowRect(i)
		// sample a point inside each row
		x, y := rr.X+1, rr.Y+1
		if got := l.ItemAt(x, y); got != i {
			t.Fatalf("ItemAt(%d,%d)=%d want %d (row rect %+v)", x, y, got, i, rr)
		}
		// the bottom-right interior point too
		x, y = rr.Right()-1, rr.Bottom()-1
		if got := l.ItemAt(x, y); got != i {
			t.Fatalf("ItemAt(%d,%d)=%d want %d", x, y, got, i)
		}
	}
	// Outside the list, and below the item count, is -1.
	if l.ItemAt(-1, 5) != -1 || l.ItemAt(5, 5+l.RowH*len(l.Items)) != -1 {
		t.Fatal("out-of-range points must be -1")
	}
}

// Draw must paint at least one fill whose rect is inside Bounds, and the
// selection row must be drawn using RowRect(Sel).
func TestListDrawUsesRowRects(t *testing.T) {
	l := &List{R: Rect{0, 0, 60, 60}, Items: []string{"a", "b"}, RowH: 30, Sel: 1, Bg: 0x000000, SelBg: 0xababab}
	c := &recordingCanvas{}
	l.Draw(c)
	if len(c.fills) == 0 {
		t.Fatal("list drew nothing")
	}
	selRect := l.RowRect(1)
	found := false
	for _, f := range c.fills {
		if f.r == selRect && f.rgb == l.SelBg {
			found = true
		}
	}
	if !found {
		t.Fatalf("selection row rect not drawn: want %+v", selRect)
	}
}

func TestVisibleRows(t *testing.T) {
	l := &List{R: Rect{0, 0, 10, 90}, RowH: 30}
	if got := l.VisibleRows(); got != 3 {
		t.Fatalf("VisibleRows = %d want 3", got)
	}
}

// Scale is the identity at the native canvas — the zero-regression fixed
// point — and maps proportionally otherwise.
func TestScaleIdentityAndMapping(t *testing.T) {
	r := Rect{8, 104, 56, 20}
	if got := Scale(r, 512, 340, 512, 340); got != r {
		t.Fatalf("identity scale changed %+v -> %+v", r, got)
	}
	got := Scale(r, 512, 340, 1024, 680)
	if got != (Rect{16, 208, 112, 40}) {
		t.Fatalf("scaled = %+v", got)
	}
	// Content never spills the target canvas.
	if got.X+got.W > 1024 || got.Y+got.H > 680 {
		t.Fatalf("scaled rect spills: %+v", got)
	}
	// A zero source is a no-op (guarded).
	if got := Scale(r, 0, 0, 100, 100); got != r {
		t.Fatalf("zero source should be a no-op: %+v", got)
	}
}

func TestLayoutHelpers(t *testing.T) {
	row := Row(0, 0, 10, 4, 2, 3)
	if len(row) != 3 || row[0] != (Rect{0, 0, 10, 4}) || row[2] != (Rect{24, 0, 10, 4}) {
		t.Fatalf("Row = %+v", row)
	}
	col := Column(0, 0, 10, 4, 2, 3)
	if len(col) != 3 || col[1] != (Rect{0, 6, 10, 4}) {
		t.Fatalf("Column = %+v", col)
	}
	if Row(0, 0, 1, 1, 0, 0) != nil {
		t.Fatal("zero-count Row should be nil")
	}
}

// 'H' is two stems and a crossbar (font8x8 0x33 / 0x3f). A solid cell per
// rune — the old placeholder — paints one tall rectangle and has no gap, so
// this fails if text goes back to being a horizontal bar.
func TestGlyphRunPaintsLettersNotBars(t *testing.T) {
	w := &Text{R: Rect{0, 0, 80, 24}, Label: "H", Fg: 0xffffff, Bg: 0x101010}
	c := &recordingCanvas{}
	w.Draw(c)
	var glyph []Rect
	for _, f := range c.fills {
		if f.rgb != w.Fg {
			continue
		}
		if f.r.H != 1 {
			t.Fatalf("glyph fill is %dpx tall: a bar, not a scanline %+v", f.r.H, f.r)
		}
		glyph = append(glyph, f.r)
	}
	if len(glyph) == 0 {
		t.Fatal("'H' painted no ink")
	}
	// First ink row of 'H' is 0x33: two stems, a gap between them.
	y0 := glyph[0].Y
	var runs int
	var prevRight int
	for _, r := range glyph {
		if r.Y != y0 {
			break
		}
		if runs > 0 && r.X < prevRight {
			t.Fatalf("row fills overlap: %+v", glyph)
		}
		runs++
		prevRight = r.X + r.W
	}
	if runs != 2 {
		t.Fatalf("'H' top row has %d run(s), want the two stems", runs)
	}
}

func TestRectHelpers(t *testing.T) {
	r := Rect{10, 10, 20, 20}
	if !r.Contains(10, 10) || r.Contains(30, 10) || r.Contains(10, 30) {
		t.Fatal("Contains edges wrong")
	}
	if r.Right() != 30 || r.Bottom() != 30 {
		t.Fatal("edges wrong")
	}
	if got := r.Inset(2); got != (Rect{12, 12, 16, 16}) {
		t.Fatalf("Inset = %+v", got)
	}
	if !(Rect{}).Empty() {
		t.Fatal("zero rect should be empty")
	}
}
