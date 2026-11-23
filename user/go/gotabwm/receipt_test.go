package main

import "testing"

// The receipt fires on the first pass that both painted and presented,
// exactly once per episode, and re-arms only through arm() — a painted
// frame that failed to present, or an unpainted pass, never claims it.
func TestPresentReceiptFiresOncePerEpisode(t *testing.T) {
	var r presentReceipt
	for _, p := range [][2]bool{{false, true}, {true, false}, {false, false}} {
		if r.feed(p[0], p[1]) {
			t.Fatalf("feed(%v,%v) claimed before a painted+presented pass", p[0], p[1])
		}
	}
	if !r.feed(true, true) {
		t.Fatal("the first painted+presented pass earned no receipt")
	}
	if r.feed(true, true) || r.feed(true, false) || r.feed(false, true) {
		t.Fatal("the receipt fired again inside one episode")
	}
	// A painted=false feed does NOT re-arm by itself: episode
	// boundaries are the caller's arm() calls, so a layer that flickers
	// cannot multiply markers.
	if r.feed(false, true) {
		t.Fatal("an unpainted pass claimed a receipt")
	}
	if r.feed(true, true) {
		t.Fatal("the receipt re-armed without arm()")
	}
	r.arm()
	if !r.feed(true, true) {
		t.Fatal("arm() did not re-arm the receipt")
	}
	if r.feed(true, true) {
		t.Fatal("the re-armed receipt fired twice")
	}
}
