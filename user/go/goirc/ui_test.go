package main

import (
	"testing"

	"virelai/tabapp"
	"virelai/vi"
)

func TestWheelScrollMovesBacklogAndPageKeyDoesNotDoubleScroll(t *testing.T) {
	cli := NewClient(Config{Nick: "alice"}, nil, nil)
	for i := 0; i < 60; i++ {
		cli.Backlog("#dev").Add(Entry{Kind: EntryMsg, Channel: "#dev", From: "bob", Text: "msg"})
	}
	u := &uiApp{
		ta:     &tabapp.TabApp{W: uiNatW, H: uiNatH},
		cli:    cli,
		bufs:   []string{"#dev"},
		focus:  0,
		scroll: map[string]int{},
	}

	// Wheel up (-4 lines) scrolls 4 lines back into history (u.scroll increases).
	u.handleEvent(vi.Event{Kind: vi.EvMouseScroll, Flags: vi.ScrollWheel, Arg0: 0x0004})
	if got := u.scroll["#dev"]; got != 4 {
		t.Fatalf("after wheel -4: scroll=%d want 4", got)
	}
	// Wheel down (+2 lines) scrolls 2 lines toward newest (u.scroll decreases).
	u.handleEvent(vi.Event{Kind: vi.EvMouseScroll, Flags: vi.ScrollWheel, Arg0: 0x8002})
	if got := u.scroll["#dev"]; got != 2 {
		t.Fatalf("after wheel +2: scroll=%d want 2", got)
	}
	// Horizontal wheel is ignored.
	u.handleEvent(vi.Event{Kind: vi.EvMouseScroll, Flags: vi.ScrollWheel, Arg0: 0xc003})
	if got := u.scroll["#dev"]; got != 2 {
		t.Fatalf("after horizontal wheel: scroll=%d want 2", got)
	}

	// PageUp arrives as KEY_DOWN + synthesized EvMouseScroll (Flags=0).
	// handleEvent scrolls once on keyPgUp and ignores the synthesized scroll event.
	before := u.scroll["#dev"]
	u.handleEvent(vi.Event{Kind: vi.EvKeyDown, Arg0: keyPgUp})
	afterKey := u.scroll["#dev"]
	if afterKey <= before {
		t.Fatalf("PageUp key did not scroll: before=%d after=%d", before, afterKey)
	}
	u.handleEvent(vi.Event{Kind: vi.EvMouseScroll, Flags: 0, Arg0: 0x0001})
	if got := u.scroll["#dev"]; got != afterKey {
		t.Fatalf("synthesized PageUp EvMouseScroll double-scrolled: afterKey=%d afterScroll=%d", afterKey, got)
	}
}
