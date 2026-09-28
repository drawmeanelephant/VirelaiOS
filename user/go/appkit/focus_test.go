package appkit

import (
	"testing"

	"virelai/vi"
	"virelai/widgets"
)

type focusProbe struct {
	r       widgets.Rect
	keys    int
	clicks  int
	pressed bool
}

func (p *focusProbe) Bounds() widgets.Rect    { return p.r }
func (p *focusProbe) HitTest(x, y int) bool   { return p.r.Contains(x, y) }
func (p *focusProbe) Draw(widgets.RectCanvas) {}
func (p *focusProbe) OnKey(Key) bool          { p.keys++; return true }
func (p *focusProbe) OnClick(x, y int) bool {
	p.clicks++
	p.pressed = p.HitTest(x, y)
	return p.pressed
}
func (p *focusProbe) SetFocused(v bool) { p.pressed = v }

func keyEvent(usage, symbol uint32, flags uint16) vi.Event {
	return vi.Event{Kind: vi.EvKeyDown, Flags: flags, Arg0: usage, Arg1: symbol}
}

func TestNormalizeKeyAndFocusOrder(t *testing.T) {
	k, ok := NormalizeKey(keyEvent(0, 'x', 0))
	if !ok || k.Rune != 'x' || k.Named() != NamedNone {
		t.Fatalf("printable key = %+v ok=%v", k, ok)
	}
	k, ok = NormalizeKey(keyEvent(0, 0x00f6, 0))
	if !ok || k.Rune != 'ö' {
		t.Fatalf("Unicode key = %+v ok=%v", k, ok)
	}
	k, ok = NormalizeKey(keyEvent(0, 0, vi.ModCtrl))
	if !ok || k.Rune != 0 || k.Named() != NamedNone {
		t.Fatalf("ctrl printable = %+v ok=%v", k, ok)
	}
	k, ok = NormalizeKey(keyEvent(0, '\r', 0))
	if !ok || k.Named() != NamedEnter {
		t.Fatalf("enter = %+v ok=%v", k, ok)
	}

	a, b := &focusProbe{r: widgets.Rect{X: 0, Y: 0, W: 10, H: 10}}, &focusProbe{r: widgets.Rect{X: 20, Y: 0, W: 10, H: 10}}
	ring := NewFocusRing(a, b)
	if !ring.Next() || ring.index != 0 || !a.pressed {
		t.Fatalf("first focus = %+v", ring)
	}
	if !ring.Next() || ring.index != 1 || a.pressed || !b.pressed {
		t.Fatalf("next focus = %+v", ring)
	}
	if !ring.HandleKey(Key{Usage: 0x2b, Shift: true}) || ring.index != 0 || b.pressed {
		t.Fatalf("shift-tab = %+v", ring)
	}
	if !ring.HandleKey(Key{Usage: 0x2b}) || ring.index != 1 {
		t.Fatalf("tab = %+v", ring)
	}
	if !ring.HandleClick(1, 1) || ring.index != 0 || a.clicks != 1 {
		t.Fatalf("click = %+v a=%+v", ring, a)
	}
}

func TestListControllerScrollsSelectionIntoView(t *testing.T) {
	list := &widgets.List{R: widgets.Rect{X: 0, Y: 0, W: 100, H: 60}, Items: []string{"0", "1", "2", "3", "4"}, RowH: 20}
	listCtl := NewListController(list)
	if listCtl.HandleKey(Key{Usage: 0x52}) || list.Sel != 0 {
		t.Fatalf("up at start = %d", list.Sel)
	}
	for i := 0; i < 4; i++ {
		if !listCtl.HandleKey(Key{Usage: 0x51}) {
			t.Fatalf("down %d did not move", i)
		}
	}
	if list.Sel != 4 || list.ScrollTop != 2 || list.RowRect(4).Y != 40 {
		t.Fatalf("scrolled list = sel %d top %d row4 %+v", list.Sel, list.ScrollTop, list.RowRect(4))
	}
	if list.ItemAt(1, 41) != 4 || list.ItemAt(1, 19) != 2 {
		t.Fatalf("hit rows = %d, %d", list.ItemAt(1, 19), list.ItemAt(1, 41))
	}
}

func TestTextFieldEditsAtCaretAndHonorsBound(t *testing.T) {
	f := NewTextField(widgets.Rect{X: 0, Y: 0, W: 100, H: 20}, 4)
	for _, r := range []rune{'a', 'b', 'c'} {
		if !f.OnKey(Key{Rune: r}) {
			t.Fatalf("insert %c", r)
		}
	}
	if f.OnKey(Key{Rune: 'ö'}) || f.Value() != "abc" {
		t.Fatalf("byte field must refuse non-ASCII without truncating: %q", f.Value())
	}
	if f.Value() != "abc" || f.CaretPosition() != 3 {
		t.Fatalf("field = %q caret %d", f.Value(), f.CaretPosition())
	}
	if !f.OnKey(Key{Usage: 0x50}) || !f.OnKey(Key{Rune: 'X'}) || f.Value() != "abXc" {
		t.Fatalf("mid insert = %q", f.Value())
	}
	if !f.OnKey(Key{Usage: 0x2a}) || f.Value() != "abc" {
		t.Fatalf("backspace = %q", f.Value())
	}
	if !f.OnKey(Key{Usage: 0x4c}) || f.Value() != "ab" {
		t.Fatalf("delete = %q", f.Value())
	}
	if !f.OnKey(Key{Rune: 'd'}) || f.Value() != "abd" {
		t.Fatalf("insert at bound = %q", f.Value())
	}
	if !f.OnKey(Key{Rune: 'e'}) || f.Value() != "abde" {
		t.Fatalf("fill bound = %q", f.Value())
	}
	if f.OnKey(Key{Rune: 'f'}) || f.Value() != "abde" {
		t.Fatalf("bound = %q", f.Value())
	}
}
