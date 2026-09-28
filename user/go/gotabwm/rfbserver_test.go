package main

import (
	"testing"

	"virelai/rfb"
	"virelai/vi"
)

func TestRFBRequiresExplicitOperatorArgument(t *testing.T) {
	for _, args := range [][]string{nil, {"GOTABWM.ELF"}, {"GOTABWM.ELF", "anything"},
		{"GOTABWM.ELF", "--rfb-hermetic", "extra"}} {
		if rfbRequested(args) {
			t.Fatalf("unexpected RFB listener with argv %q", args)
		}
	}
	if !rfbRequested([]string{"GOTABWM.ELF", "--rfb-hermetic"}) {
		t.Fatal("operator opt-in refused")
	}
	if rfbInputLimit > 16 || rfbWaitTicks <= 0 {
		t.Fatal("client queue/deadlines must be bounded")
	}
}

func TestRFBInputUsesWmEventWires(t *testing.T) {
	var m rfbModifiers
	// Modifier presses are state, not extra WM_KEY events. Unknown keysyms
	// from the codec cannot turn into a usage-zero seat chord.
	if _, ok := m.key(rfb.KeyEvent{Down: true, Usage: 0xe0, Known: true}); ok {
		t.Fatal("modifier generated a WM_KEY")
	}
	if _, ok := m.key(rfb.KeyEvent{Down: true, Keysym: 0x123456, Known: false}); ok {
		t.Fatal("unknown keysym generated a WM_KEY")
	}
	e, ok := m.key(rfb.KeyEvent{Down: true, Usage: 0x2c, Known: true})
	if !ok || e.Kind != vi.EvWmKey || e.Arg0 != 0x2c || e.Flags != vi.ModCtrl {
		t.Fatalf("ctrl-space WM_KEY = %+v, %t", e, ok)
	}
	if _, ok := m.key(rfb.KeyEvent{Down: true, Usage: 0x2c, Known: true}); ok {
		t.Fatal("held-key repeat generated a second WM_KEY")
	}
	if _, ok := m.key(rfb.KeyEvent{Down: false, Usage: 0x2c, Known: true}); ok {
		t.Fatal("release generated a WM_KEY")
	}
	m.key(rfb.KeyEvent{Down: false, Usage: 0xe0, Known: true})
	e, _ = m.key(rfb.KeyEvent{Down: true, Usage: 0x28, Known: true})
	if e.Flags != 0 {
		t.Fatalf("modifier stuck after release: %+v", e)
	}
}

func TestRFBButtonOrderingMatchesSeat(t *testing.T) {
	for _, c := range []struct{ wire, seat uint8 }{{0, 0}, {1, 1}, {2, 4}, {4, 2}, {7, 7}} {
		if got := rfbButtonsToSeat(c.wire); got != c.seat {
			t.Fatalf("RFB mask %d -> seat %d want %d", c.wire, got, c.seat)
		}
	}
}

func TestRFBInputQueueIsFinite(t *testing.T) {
	s := &rfbServer{input: make(chan remoteInput, rfbInputLimit)}
	for i := 0; i < rfbInputLimit; i++ {
		if !s.enqueue(remoteInput{kind: rfb.PointerInput}) {
			t.Fatalf("queue refused before capacity at %d", i)
		}
	}
	if s.enqueue(remoteInput{kind: rfb.PointerInput}) {
		t.Fatal("input flood was not refused")
	}
}

func TestRFBDeathReleasesHeldPointer(t *testing.T) {
	oldButtons := prevPtrButtons
	defer func() { prevPtrButtons = oldButtons }()
	prevPtrButtons = 1
	s := &rfbServer{input: make(chan remoteInput, 1), lastPoint: 100 | 600<<16, lastButtons: 1}
	s.closed.Store(true)
	if !s.finishInput() {
		t.Fatal("closed session not retired")
	}
	if prevPtrButtons != 0 {
		t.Fatal("held remote button survived viewer death")
	}
}
