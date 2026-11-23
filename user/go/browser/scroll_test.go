package main

import (
	"testing"

	"virelai/vi"
)

func TestWheelScrollMovesOffsetAndPageKeyDoesNotDoubleScroll(t *testing.T) {
	a := stateApp(t, `<p style="height:2000px">tall page</p>`)

	// Wheel down (+3 lines) moves scroll offset by 3 * 40 px.
	a.scrollEvent(vi.Event{Kind: vi.EvMouseScroll, Flags: vi.ScrollWheel, Arg0: 0x8003})
	if a.scroll != 120 {
		t.Fatalf("after wheel +3: scroll=%d want 120", a.scroll)
	}
	// Wheel up (-1 line) moves scroll offset back by 40 px.
	a.scrollEvent(vi.Event{Kind: vi.EvMouseScroll, Flags: vi.ScrollWheel, Arg0: 0x0001})
	if a.scroll != 80 {
		t.Fatalf("after wheel -1: scroll=%d want 80", a.scroll)
	}
	// Horizontal wheel is ignored.
	a.scrollEvent(vi.Event{Kind: vi.EvMouseScroll, Flags: vi.ScrollWheel, Arg0: 0xc002})
	if a.scroll != 80 {
		t.Fatalf("after horizontal wheel: scroll=%d want 80", a.scroll)
	}

	// PageDown arrives as KEY_DOWN + synthesized EvMouseScroll (Flags=0).
	// keyEvent scrolls by page; scrollEvent must ignore the synthesized event so there is no double scroll.
	before := a.scroll
	a.keyEvent(vi.Event{Kind: vi.EvKeyDown, Arg0: keyPageDown})
	afterKey := a.scroll
	if afterKey <= before {
		t.Fatalf("PageDown key did not scroll: before=%d after=%d", before, afterKey)
	}
	a.scrollEvent(vi.Event{Kind: vi.EvMouseScroll, Flags: 0, Arg0: 0x8001})
	if a.scroll != afterKey {
		t.Fatalf("synthesized PageDown EvMouseScroll double-scrolled: afterKey=%d afterScroll=%d", afterKey, a.scroll)
	}
}
