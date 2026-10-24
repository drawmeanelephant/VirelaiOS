package shlib

import (
	"strings"
	"testing"
)

// The say builtin: `say TEXT` stages the utterance through the
// sayToHost seam — the seat's signed SPEAK record is what the host
// actually speaks.

func TestSayStagesUtterance(t *testing.T) {
	h := newFakeHost()
	var got []string
	prev := sayToHost
	defer func() { sayToHost = prev }()
	sayToHost = func(text string) string {
		got = append(got, text)
		return ""
	}
	sh := NewShell(h, &History{})
	status, _ := sh.RunLine("say hello world")
	if status != 0 {
		t.Fatalf("say status = %d, out=%q", status, h.outString())
	}
	if len(got) != 1 || got[0] != "hello world" {
		t.Fatalf("stage saw %v, want the joined args", got)
	}
	if !strings.Contains(h.outString(), "said to host") {
		t.Fatalf("output = %q", h.outString())
	}
	found := false
	for _, m := range h.markers {
		if m == "gosh: say queued len=11" {
			found = true
		}
	}
	if !found {
		t.Fatalf("markers = %v", h.markers)
	}
}

func TestSayRefusalsNamed(t *testing.T) {
	h := newFakeHost()
	prev := sayToHost
	defer func() { sayToHost = prev }()
	sayToHost = func(text string) string {
		return "lane_disabled"
	}
	sh := NewShell(h, &History{})
	status, _ := sh.RunLine("say hi")
	if status != 1 || !strings.Contains(h.outString(), "lane_disabled") {
		t.Fatalf("refusal: status=%d out=%q", status, h.outString())
	}
	found := false
	for _, m := range h.markers {
		if strings.Contains(m, "gosh: say refused reason=lane_disabled") {
			found = true
		}
	}
	if !found {
		t.Fatalf("markers = %v", h.markers)
	}
}

func TestSayNoArgsIsUsage(t *testing.T) {
	h := newFakeHost()
	prev := sayToHost
	defer func() { sayToHost = prev }()
	called := false
	sayToHost = func(text string) string { called = true; return "" }
	sh := NewShell(h, &History{})
	status, _ := sh.RunLine("say")
	if status != 2 || !strings.Contains(h.outString(), "gosh: say TEXT") {
		t.Fatalf("usage: status=%d out=%q", status, h.outString())
	}
	if called {
		t.Fatal("a bare say staged an empty utterance")
	}
}
