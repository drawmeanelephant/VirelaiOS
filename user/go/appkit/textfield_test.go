package appkit

import (
	"testing"

	"virelai/layout"
	"virelai/vi"
	"virelai/widgets"
)

// keyUnder is the Key a hosted app receives for a physical key under a layout:
// the kernel's usage and translated symbol, normalized the way the app's event
// loop does. A dead key has no symbol, so its Rune is 0.
func keyUnder(id layout.ID, usage uint8, shift bool) Key {
	var sym uint32
	if r, ok := layout.Translate(id, usage, shift); ok {
		sym = uint32(r)
	}
	var flags uint16
	if shift {
		flags = vi.ModShift
	}
	k, _ := NormalizeKey(keyEvent(uint32(usage), sym, flags))
	return k
}

const (
	usageE     = 0x08
	usageX     = 0x1b
	usageAcute = 0x2e // DE dead acute; Shift is dead grave
	usageCirc  = 0x35 // DE dead circumflex
	usageEnter = 0x28
	usageEsc   = 0x29
	usageBksp  = 0x2a
	usageSpace = 0x2c
	usageLeft  = 0x50
)

func deField(max int) *TextField {
	f := NewTextField(widgets.Rect{X: 0, Y: 0, W: 200, H: 20}, max)
	f.SetLayout(layout.DE)
	return &f
}

func TestTextFieldComposesADeadKeyAndShowsItPending(t *testing.T) {
	f := deField(16)
	f.OnKey(keyUnder(layout.DE, usageX, false))
	if !f.OnKey(keyUnder(layout.DE, usageAcute, false)) {
		t.Fatal("staging a dead key changes what the field shows, so it must report a change")
	}
	if f.Value() != "x" || f.Preedit() != "'" {
		t.Fatalf("after the dead key value=%q preedit=%q, want the accent pending and not yet typed", f.Value(), f.Preedit())
	}
	if !f.OnKey(keyUnder(layout.DE, usageE, false)) {
		t.Fatal("composing must report a change")
	}
	if f.Value() != "xé" || f.Preedit() != "" || f.CaretPosition() != len("xé") {
		t.Fatalf("value=%q preedit=%q caret=%d, want the composed é typed at the caret", f.Value(), f.Preedit(), f.CaretPosition())
	}
	// Shift on the dead key is the grave; the circumflex key is dead even
	// though the kernel translated it to a literal '^'.
	f.OnKey(keyUnder(layout.DE, usageAcute, true))
	f.OnKey(keyUnder(layout.DE, usageE, false))
	f.OnKey(keyUnder(layout.DE, usageCirc, false))
	f.OnKey(keyUnder(layout.DE, 0x12, false))
	if f.Value() != "xéèô" {
		t.Fatalf("value = %q, want xéèô", f.Value())
	}
}

func TestTextFieldStrayCombinationTypesBothCharacters(t *testing.T) {
	f := deField(16)
	f.OnKey(keyUnder(layout.DE, usageAcute, false))
	if !f.OnKey(keyUnder(layout.DE, usageX, false)) || f.Value() != "´x" {
		t.Fatalf("value = %q, want the literal accent then the x", f.Value())
	}
	f.OnKey(keyUnder(layout.DE, usageAcute, false))
	f.OnKey(keyUnder(layout.DE, usageSpace, false))
	f.OnKey(keyUnder(layout.DE, usageAcute, false))
	f.OnKey(keyUnder(layout.DE, usageAcute, false))
	if f.Value() != "´x´´" {
		t.Fatalf("value = %q, want the bare accent from dead+space and dead+dead", f.Value())
	}
}

func TestTextFieldKeysThatEndASequenceStillDoTheirJob(t *testing.T) {
	f := deField(16)
	f.SetValue("ac")
	f.OnKey(Key{Usage: usageLeft})
	f.OnKey(keyUnder(layout.DE, usageAcute, false))
	if !f.OnKey(keyUnder(layout.DE, usageLeft, false)) {
		t.Fatal("the arrow is the field's own key, so it is consumed")
	}
	if f.Value() != "a´c" || f.CaretPosition() != 1 {
		t.Fatalf("value=%q caret=%d, want the accent typed at the old caret and the caret one rune left of it", f.Value(), f.CaretPosition())
	}

	// Enter and Escape are the caller's keys: the accent is typed, and the
	// field reports the key unconsumed so the caller can act on it.
	for _, u := range []uint8{usageEnter, usageEsc} {
		g := deField(16)
		g.OnKey(keyUnder(layout.DE, usageAcute, false))
		if g.OnKey(keyUnder(layout.DE, u, false)) {
			t.Errorf("usage %#x must not be consumed", u)
		}
		if g.Value() != "´" || g.Preedit() != "" {
			t.Errorf("usage %#x: value=%q preedit=%q, want the literal typed", u, g.Value(), g.Preedit())
		}
	}
}

func TestTextFieldBackspaceCancelsTheStagedAccent(t *testing.T) {
	f := deField(16)
	f.SetValue("ab")
	f.OnKey(keyUnder(layout.DE, usageAcute, false))
	if !f.OnKey(keyUnder(layout.DE, usageBksp, false)) {
		t.Fatal("dropping the staged accent is a visible change")
	}
	if f.Value() != "ab" || f.Preedit() != "" {
		t.Fatalf("value=%q preedit=%q, want the text untouched and nothing pending", f.Value(), f.Preedit())
	}
	// The next Backspace is an ordinary one.
	f.OnKey(keyUnder(layout.DE, usageBksp, false))
	if f.Value() != "a" {
		t.Fatalf("value = %q", f.Value())
	}
}

func TestTextFieldLosingFocusOrClickTypesTheStagedAccent(t *testing.T) {
	f := deField(16)
	f.SetFocused(true)
	f.OnKey(keyUnder(layout.DE, usageAcute, false))
	f.SetFocused(false)
	if f.Value() != "´" || f.Preedit() != "" {
		t.Fatalf("blur: value=%q preedit=%q", f.Value(), f.Preedit())
	}

	f.OnKey(keyUnder(layout.DE, usageAcute, true))
	if !f.OnClick(1, 1) || f.Value() != "´`" {
		t.Fatalf("click: value=%q, want the grave typed", f.Value())
	}

	// Tab moves focus through the ring, which blurs the field: the same rule.
	a, b := deField(16), deField(16)
	ring := NewFocusRing(a, b)
	ring.Next()
	a.OnKey(keyUnder(layout.DE, usageCirc, false))
	if !ring.HandleKey(Key{Usage: 0x2b}) || a.Value() != "^" {
		t.Fatalf("tab away: value=%q, want the circumflex typed", a.Value())
	}

	// Programmatic edits drop a stage that no longer follows the text.
	c := deField(16)
	c.OnKey(keyUnder(layout.DE, usageAcute, false))
	c.SetValue("new")
	if c.Preedit() != "" || c.Value() != "new" {
		t.Fatalf("SetValue: value=%q preedit=%q", c.Value(), c.Preedit())
	}
	c.OnKey(keyUnder(layout.DE, usageAcute, false))
	c.Clear()
	if c.Preedit() != "" || c.Value() != "" {
		t.Fatalf("Clear: value=%q preedit=%q", c.Value(), c.Preedit())
	}
}

func TestTextFieldWithoutDeadKeysIsUnchanged(t *testing.T) {
	// Under US the same physical key is '=', and it must simply type.
	for _, id := range []layout.ID{"", layout.US} {
		f := NewTextField(widgets.Rect{W: 100, H: 20}, 8)
		f.SetLayout(id)
		f.OnKey(keyUnder(layout.US, usageAcute, false))
		f.OnKey(keyUnder(layout.US, usageE, false))
		if f.Value() != "=e" || f.Preedit() != "" {
			t.Errorf("layout %q: value=%q preedit=%q, want =e", id, f.Value(), f.Preedit())
		}
	}
	// A layout key the kernel already translated (DE's ö) is plain text.
	f := deField(8)
	f.OnKey(keyUnder(layout.DE, 0x33, false))
	if f.Value() != "ö" {
		t.Fatalf("value = %q", f.Value())
	}
}

func TestTextFieldHoldsUTF8WithoutSplittingARune(t *testing.T) {
	f := deField(5)
	for _, r := range "aéb" {
		if !f.OnKey(Key{Rune: r}) {
			t.Fatalf("insert %q", r)
		}
	}
	if f.Value() != "aéb" || f.CaretPosition() != 4 {
		t.Fatalf("value=%q caret=%d", f.Value(), f.CaretPosition())
	}
	// Left, Right, Delete and Backspace all move by whole runes.
	f.OnKey(Key{Usage: 0x50})
	f.OnKey(Key{Usage: 0x50})
	if f.CaretPosition() != 1 {
		t.Fatalf("two lefts from the end: caret=%d, want 1 (before é)", f.CaretPosition())
	}
	f.OnKey(Key{Usage: 0x4f})
	if f.CaretPosition() != 3 {
		t.Fatalf("right over é: caret=%d, want 3", f.CaretPosition())
	}
	f.OnKey(Key{Usage: 0x2a})
	if f.Value() != "ab" || f.CaretPosition() != 1 {
		t.Fatalf("backspace over é: value=%q caret=%d", f.Value(), f.CaretPosition())
	}
	f.SetValue("aéb")
	f.OnKey(Key{Usage: 0x4a})
	f.OnKey(Key{Usage: 0x4f})
	f.OnKey(Key{Usage: 0x4c})
	if f.Value() != "ab" {
		t.Fatalf("delete over é: value=%q", f.Value())
	}

	// The byte bound refuses a rune that does not fit, whole.
	g := deField(4)
	g.SetValue("abc")
	if g.OnKey(Key{Rune: 'é'}) || g.Value() != "abc" || g.CaretPosition() != 3 {
		t.Fatalf("value=%q caret=%d, want é refused with the field untouched", g.Value(), g.CaretPosition())
	}
	// SetValue clips to a rune boundary, never through a rune.
	g.SetValue("abé")
	if g.Value() != "abé" {
		t.Fatalf("value = %q", g.Value())
	}
	g.SetValue("aébé")
	if g.Value() != "aéb" {
		t.Fatalf("clipped value = %q, want aéb (é would have been cut in half)", g.Value())
	}
	// Control codes and C1 controls are not text.
	if g.OnKey(Key{Rune: 0x85}) || g.OnKey(Key{Rune: 0x1b}) {
		t.Fatal("control runes must not be typed")
	}
}

func TestTextFieldComposedCharacterThatDoesNotFitIsRefusedNotTruncated(t *testing.T) {
	f := deField(2)
	f.SetValue("ab")
	f.OnKey(keyUnder(layout.DE, usageAcute, false))
	f.OnKey(keyUnder(layout.DE, usageE, false))
	if f.Value() != "ab" || f.Preedit() != "" {
		t.Fatalf("value=%q preedit=%q, want a full field left as it was", f.Value(), f.Preedit())
	}
}

type fill struct {
	r   widgets.Rect
	rgb uint32
}

type recCanvas struct{ fills []fill }

func (c *recCanvas) FillRect(r widgets.Rect, rgb uint32) { c.fills = append(c.fills, fill{r, rgb}) }

func (c *recCanvas) within(rgb uint32, x0, x1, y0, y1 int) int {
	n := 0
	for _, f := range c.fills {
		if f.rgb == rgb && f.r.X >= x0 && f.r.X < x1 && f.r.Y >= y0 && f.r.Y < y1 {
			n++
		}
	}
	return n
}

func (c *recCanvas) has(r widgets.Rect, rgb uint32) bool {
	for _, f := range c.fills {
		if f.r == r && f.rgb == rgb {
			return true
		}
	}
	return false
}

func TestTextFieldPaintsThePendingAccentAtTheCaret(t *testing.T) {
	const fg, accent = 0xe6edf3, 0xffd75f
	f := deField(16)
	f.Fg, f.Caret = fg, accent
	f.SetFocused(true)
	f.SetValue("ab")
	f.OnKey(Key{Usage: usageLeft})

	// Without a pending accent the caret sits after the "a" (cell 1), where the
	// buffer's caret is, and nothing is drawn in the accent colour but it.
	var c recCanvas
	f.Draw(&c)
	if !c.has(widgets.Rect{X: 2 + 8, Y: 2, W: 2, H: 16}, accent) {
		t.Fatalf("caret not at cell 1: %+v", c.fills)
	}
	if n := c.within(accent, 0, 200, 0, 20); n != 1 {
		t.Fatalf("%d accent-coloured fills with nothing pending, want only the caret", n)
	}

	f.OnKey(keyUnder(layout.DE, usageAcute, false))
	c = recCanvas{}
	f.Draw(&c)
	// Cell 1 holds the mark: accent glyph pixels and an underline one row under
	// the glyph (centred in the inset plate: glyph rows 6..13, underline row 14).
	if c.within(accent, 10, 18, 6, 14) == 0 {
		t.Fatal("no accent-coloured glyph pixels in the pending cell")
	}
	if !c.has(widgets.Rect{X: 10, Y: 14, W: 8, H: 1}, accent) {
		t.Fatalf("no underline under the pending cell: %+v", c.fills)
	}
	// The text before stays in cell 0, and the "b" has moved right to cell 2.
	if c.within(fg, 2, 10, 0, 20) == 0 || c.within(fg, 18, 26, 0, 20) == 0 {
		t.Fatal("text before or after the accent is missing")
	}
	if c.within(fg, 10, 18, 0, 20) != 0 {
		t.Fatal("text was painted over the pending cell")
	}
	// The caret follows the pending cell.
	if !c.has(widgets.Rect{X: 2 + 16, Y: 2, W: 2, H: 16}, accent) {
		t.Fatalf("caret did not move past the pending accent: %+v", c.fills)
	}

	// Composing removes the mark and types the character in its place.
	f.OnKey(keyUnder(layout.DE, usageE, false))
	c = recCanvas{}
	f.Draw(&c)
	if c.has(widgets.Rect{X: 10, Y: 14, W: 8, H: 1}, accent) {
		t.Fatal("underline outlived the pending accent")
	}
}

func TestTextFieldPlaceholderYieldsToAPendingAccent(t *testing.T) {
	const fg, accent = 0xe6edf3, 0xffd75f
	f := deField(16)
	f.Fg, f.Caret, f.Placeholder = fg, accent, "type here"
	var c recCanvas
	f.Draw(&c)
	if c.within(fg, 2, 200, 0, 20) == 0 {
		t.Fatal("empty field must paint its placeholder")
	}
	f.OnKey(keyUnder(layout.DE, usageAcute, false))
	c = recCanvas{}
	f.Draw(&c)
	if c.within(fg, 0, 200, 0, 20) != 0 {
		t.Fatal("the placeholder must not paint next to a pending accent")
	}
	if !c.has(widgets.Rect{X: 2, Y: 14, W: 8, H: 1}, accent) {
		t.Fatalf("no underline for the pending accent in an empty field: %+v", c.fills)
	}
}
