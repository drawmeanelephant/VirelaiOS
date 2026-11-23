package main

import (
	"fmt"
	"testing"

	"virelai/rss/keys"
	"virelai/vi"
)

func TestNavKeysMoveSelection(t *testing.T) {
	m := testModel(entry("SUB", true), entry("KNOWN.TXT", false))
	m.handleKey(keys.Event{Key: keys.KeyDown})
	if m.sel != 1 {
		t.Fatalf("down moved sel to %d, want 1", m.sel)
	}
	m.handleKey(runeKey('j'))
	if m.sel != 1 {
		t.Fatalf("j past the end: sel=%d", m.sel)
	}
	m.handleKey(keys.Event{Key: keys.KeyUp})
	m.handleKey(runeKey('k'))
	if m.sel != 0 {
		t.Fatalf("up/k clamps at 0, got %d", m.sel)
	}
	m.handleKey(keys.Event{Key: keys.KeyEnd})
	if m.sel != 1 {
		t.Fatalf("end = %d, want 1", m.sel)
	}
	m.handleKey(keys.Event{Key: keys.KeyHome})
	if m.sel != 0 {
		t.Fatalf("home = %d, want 0", m.sel)
	}
}

func TestQuitKeys(t *testing.T) {
	m := testModel(entry("KNOWN.TXT", false))
	m.handleKey(runeKey('q'))
	if !m.quit {
		t.Fatal("q must quit in normal mode")
	}
	m2 := testModel(entry("KNOWN.TXT", false))
	m2.handleKey(keys.Event{Key: keys.KeyCtrlC})
	if !m2.quit {
		t.Fatal("ctrl-c must quit")
	}
}

func TestClickSelectsThenOpens(t *testing.T) {
	m := testModel(entry("SUB", true), entry("KNOWN.TXT", false))
	// SGR cells are 1-based over the client area; list rows start at row 2
	// (row 0 = path bar, row 1 = pane headers).
	m.handleClick(3, 4) // col 2, row 3 (0-based) -> index 1
	if m.sel != 1 {
		t.Fatalf("click selected %d, want 1", m.sel)
	}
	// Second click on the selected row opens it (cd into SUB is index 0 —
	// so click index 0 first, then again). M124a-2: the open settles on
	// the RELEASE edge — a press alone pends it so a held move can arm a
	// drag instead.
	m.handleClick(3, 3) // index 0 = SUB
	if m.sel != 0 {
		t.Fatalf("click selected %d, want 0", m.sel)
	}
	m.handleClick(3, 3)
	if m.path != "/host/FM" {
		t.Fatalf("press alone must pend the open, path=%q", m.path)
	}
	m.releaseClick()
	if m.path != "/host/FM/SUB" {
		t.Fatalf("second click must open the dir, path=%q", m.path)
	}
	// Clicks in the preview pane or chrome are inert.
	m2 := testModel(entry("SUB", true), entry("KNOWN.TXT", false))
	m2.handleClick(listWidth(64)+3, 4) // preview side
	if m2.sel != 0 {
		t.Fatalf("preview-pane click moved sel to %d", m2.sel)
	}
	m2.handleClick(3, 1) // header row
	if m2.sel != 0 {
		t.Fatalf("header click moved sel to %d", m2.sel)
	}
}

func TestKeyLabelsUseChordVocabulary(t *testing.T) {
	cases := map[keys.Event]string{
		{Key: keys.KeyEnter}:           "return",
		{Key: keys.KeyBackspace}:       "backspace",
		{Key: keys.KeyDown}:            "down",
		{Key: keys.KeyEsc}:             "escape",
		{Key: keys.KeyRune, Rune: ' '}: "space",
		{Key: keys.KeyRune, Rune: 'r'}: "r",
	}
	for ev, want := range cases {
		if got := keyLabel(ev); got != want {
			t.Errorf("keyLabel(%v) = %q, want %q", ev, got, want)
		}
	}
}

func TestListWheelScrollAndPageKeyNoDoubleScroll(t *testing.T) {
	entries := make([]vi.DirEntry, 35)
	for i := range entries {
		entries[i] = entry(fmt.Sprintf("F%02d.TXT", i), false)
	}
	m := testModel(entries...)
	m.setSize(64, 14) // bodyRows() == 10

	// Wheel down (+12 lines) moves selection and scrolls top past 0.
	if !m.handleScroll(vi.Event{Kind: vi.EvMouseScroll, Flags: vi.ScrollWheel, Arg0: 0x800c}) {
		t.Fatal("wheel down must scroll list")
	}
	if m.sel != 12 || m.top == 0 {
		t.Fatalf("after wheel +12: sel=%d top=%d want sel=12 top>0", m.sel, m.top)
	}
	// Wheel up (-3 lines) moves back up.
	if !m.handleScroll(vi.Event{Kind: vi.EvMouseScroll, Flags: vi.ScrollWheel, Arg0: 0x0003}) {
		t.Fatal("wheel up must scroll list")
	}
	if m.sel != 9 {
		t.Fatalf("after wheel -3: sel=%d want 9", m.sel)
	}
	// Horizontal wheel is ignored.
	if m.handleScroll(vi.Event{Kind: vi.EvMouseScroll, Flags: vi.ScrollWheel, Arg0: 0xc002}) {
		t.Fatal("horizontal wheel must be ignored")
	}

	// PageDown key + synthesized EvMouseScroll (Flags=0): no double scroll.
	beforeSel, beforeTop := m.sel, m.top
	m.handleKey(keys.Event{Key: keys.KeyPageDown})
	afterSel, afterTop := m.sel, m.top
	if afterSel <= beforeSel || afterTop <= beforeTop {
		t.Fatalf("PageDown did not scroll: before=(%d,%d) after=(%d,%d)", beforeSel, beforeTop, afterSel, afterTop)
	}
	if m.handleScroll(vi.Event{Kind: vi.EvMouseScroll, Flags: 0, Arg0: 0x8001}) {
		t.Fatal("synthesized PageDown EvMouseScroll must be ignored")
	}
	if m.sel != afterSel || m.top != afterTop {
		t.Fatalf("synthesized EvMouseScroll double-scrolled: got (%d,%d) want (%d,%d)", m.sel, m.top, afterSel, afterTop)
	}
}
