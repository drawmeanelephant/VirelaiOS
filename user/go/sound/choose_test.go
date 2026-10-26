// choose_test.go — host pins for the #3776 bare-launch face: the chooser
// arms record/meter through sel for main to dispatch, 'o' raises the
// shared FilePicker (which owns every key while it is up), Escape's
// cancel receipt leaves the chooser up, and a picked path probes exactly
// like an argv file — the verdict is the probe's, never faked.
package main

import (
	"testing"

	"virelai/vi"
)

func TestChooserArms(t *testing.T) {
	for _, tc := range []struct {
		ch   rune
		want chooserSel
	}{
		{'r', selRecord},
		{'m', selMeter},
	} {
		a := &app{st: stChoose}
		a.key(vi.Event{Kind: vi.EvKeyDown, Arg1: uint32(tc.ch)})
		if a.sel != tc.want {
			t.Fatalf("key %q: sel=%d, want %d", tc.ch, a.sel, tc.want)
		}
	}
}

// 'q' on the chooser is the parked quit every face already had — no arm.
func TestChooserQuit(t *testing.T) {
	a := &app{st: stChoose}
	a.key(vi.Event{Kind: vi.EvKeyDown, Arg0: keyQ})
	if !a.quit {
		t.Fatal("'q' did not arm quit on the chooser")
	}
	if a.sel != selNone {
		t.Fatalf("quit armed sel=%d", a.sel)
	}
}

func TestChooserPickCancel(t *testing.T) {
	a := &app{st: stChoose}
	if !a.key(vi.Event{Kind: vi.EvKeyDown, Arg1: 'o'}) || !a.pick.Open {
		t.Fatal("'o' did not raise the picker")
	}
	if !a.key(vi.Event{Kind: vi.EvKeyDown, Arg0: keyEscape}) {
		t.Fatal("escape was not the picker's")
	}
	if a.pick.Open || a.st != stChoose || a.sel != selNone {
		t.Fatalf("cancel left open=%v st=%d sel=%d", a.pick.Open, a.st, a.sel)
	}
}

// A picked path is probed like argv: a file the host cannot open refuses
// by name — the pick is not a promise.
func TestChooserPickProbes(t *testing.T) {
	a := &app{st: stChoose}
	a.pick.Result = "/host/nope.wav"
	a.pickerDone()
	if a.st != stRefused {
		t.Fatalf("missing file st=%d, want stRefused", a.st)
	}
	if a.path != "/host/nope.wav" || a.name != "nope.wav" {
		t.Fatalf("pick recorded path=%q name=%q", a.path, a.name)
	}
}
