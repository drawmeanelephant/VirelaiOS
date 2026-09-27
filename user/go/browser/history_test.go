package main

import (
	"strings"
	"testing"
)

func TestHistoryBackForward(t *testing.T) {
	h := newHistory()
	h.push(entry{Target: "/host/A.HTML"})
	h.push(entry{Target: "/host/B.HTML"})
	if !h.canBack() || h.canForward() {
		t.Fatal("expected back only")
	}
	e, ok := h.back()
	if !ok || e.Target != "/host/A.HTML" {
		t.Fatalf("back = %v %v", e, ok)
	}
	if h.canBack() {
		t.Fatal("no further back expected")
	}
	if !h.canForward() {
		t.Fatal("forward expected")
	}
	e, ok = h.forward()
	if !ok || e.Target != "/host/B.HTML" {
		t.Fatalf("forward = %v %v", e, ok)
	}
}

func TestHistoryTruncatesForwardTail(t *testing.T) {
	h := newHistory()
	h.push(entry{Target: "A"})
	h.push(entry{Target: "B"})
	h.push(entry{Target: "C"})
	h.back()
	h.back()
	if !h.canForward() {
		t.Fatal("forward expected after two backs")
	}
	h.push(entry{Target: "D"})
	if h.canForward() {
		t.Fatal("forward tail should be gone")
	}
	if h.len() != 2 {
		t.Fatalf("entries = %d want 2", h.len())
	}
	cur, _ := h.current()
	if cur.Target != "D" {
		t.Fatalf("current = %q", cur.Target)
	}
}

func TestHistoryDedupesConsecutiveSameTarget(t *testing.T) {
	h := newHistory()
	h.push(entry{Target: "A"})
	h.push(entry{Target: "A"})
	if h.len() != 1 {
		t.Fatalf("entries = %d want 1", h.len())
	}
	h.push(entry{Target: "A", Title: "Title"})
	cur, _ := h.current()
	if cur.Title != "Title" {
		t.Fatalf("title update lost: %+v", cur)
	}
}

func TestHistoryEmptyIsSafe(t *testing.T) {
	h := newHistory()
	if _, ok := h.back(); ok {
		t.Fatal("back on empty")
	}
	if _, ok := h.forward(); ok {
		t.Fatal("forward on empty")
	}
	if _, ok := h.current(); ok {
		t.Fatal("current on empty")
	}
	h.replace(entry{Target: "X"})
	if cur, ok := h.current(); !ok || cur.Target != "X" {
		t.Fatalf("replace on empty should push: %v %v", cur, ok)
	}
}

// The serial markers are the live gate's grep targets: pin the exact bytes
// here so a rename cannot silently disarm the gate.
func TestMarkerConstants(t *testing.T) {
	want := map[string]string{
		markerOpen:     "web: open id=",
		markerParse:    "web: parse nodes=",
		markerLayout:   "web: layout blocks=",
		markerURL:      "web: url ",
		markerNav:      "web: nav ",
		markerError:    "web: error ",
		markerPaint:    "web: paint items=",
		markerPollErr:  "web: poll err=",
		markerLoop:     "web: poll n=",
		markerEvent:    "web: ev ",
		markerRepaint:  "web: repaint items=",
		markerReady:    "web: ready",
		markerNavReady: "web: nav-ready",
		markerFetch:    "web: fetch ",
		markerRedirect: "web: redirect n=",
		markerSettled:  "web: settled",
	}
	for got, expect := range want {
		if got != expect {
			t.Fatalf("marker %q != %q", got, expect)
		}
	}
}

func TestResolveInput(t *testing.T) {
	cases := []struct{ in, want, kind string }{
		{"/host/PAGE.HTML", "/host/PAGE.HTML", "file"},
		{"NEXT.HTML", "/host/NEXT.HTML", "file"},
		{"http://10.0.0.2/x", "http://10.0.0.2/x", "http"},
		{"HTTP://10.0.0.2/", "HTTP://10.0.0.2/", "http"},
		{"https://example.com/", "https://example.com/", "http"}, // scheme-shaped; classifyTarget refuses it
		{"ftp://x/y", "ftp://x/y", "unsupported"},
		{"", "", "empty"},
	}
	for _, c := range cases {
		got, kind := resolveInput(c.in)
		if got != c.want || kind != c.kind {
			t.Fatalf("resolveInput(%q) = %q,%q want %q,%q", c.in, got, kind, c.want, c.kind)
		}
	}
}

func TestArgvTargetJoinsSlotsAndReadsHandoff(t *testing.T) {
	got, file := argvTarget([]string{"WEB.ELF", "http://10.0.0.2/"})
	if file || got != "http://10.0.0.2/" {
		t.Fatalf("single arg = %q file=%v", got, file)
	}
	// Slots at the kernel's 255-byte cap reassemble with no separator.
	long := "https://example.com/" + strings.Repeat("x", 540)
	var slots []string
	slots = append(slots, "WEB.ELF")
	for i := 0; i < len(long); i += 255 {
		j := i + 255
		if j > len(long) {
			j = len(long)
		}
		slots = append(slots, long[i:j])
	}
	got, file = argvTarget(slots)
	if file || got != long {
		t.Fatalf("joined = %q file=%v slots=%d", got, file, len(slots)-1)
	}
	path, file := argvTarget([]string{"WEB.ELF", "@/host/RSS.LINK"})
	if !file || path != "/host/RSS.LINK" {
		t.Fatalf("@path = %q file=%v", path, file)
	}
	if urlFromHandoff([]byte(long+"\n")) != long {
		t.Fatal("handoff file must yield the first line")
	}
}
