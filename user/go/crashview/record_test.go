package main

import (
	"os"
	"strconv"
	"strings"
	"testing"

	"virelai/vi"
)

func fixture(t *testing.T, name string) []byte {
	t.Helper()
	body, err := os.ReadFile("testdata/" + name)
	if err != nil {
		t.Fatal(err)
	}
	return body
}

func TestClassifyBothFormats(t *testing.T) {
	for _, test := range []struct {
		name string
		kind entryKind
		app  string
	}{
		{"GOSELF.ELF.TXT", receiptEntry, "GOSELF.ELF"},
		{"GOSELF.ELF.STK", stackEntry, "GOSELF.ELF"},
		{"1-CRASH.ELF.txt", kernelEntry, "CRASH.ELF"},
		{"1-APP.ELF.TXT", receiptEntry, "1-APP.ELF"},
		{"GOSH.ELF.txt", receiptEntry, "GOSH.ELF"},
		{"../GOSELF.ELF.TXT", unknownEntry, ""},
		{"GOSELF.ELF.TXT~", unknownEntry, ""},
		{"notes.log", unknownEntry, ""},
	} {
		kind, app := classify(test.name)
		if kind != test.kind || app != test.app {
			t.Fatalf("%s: (%v,%q)", test.name, kind, app)
		}
	}
}

func TestRealReceiptAndTombstone(t *testing.T) {
	body := fixture(t, "goself.txt")
	r, ok := parseRecord("GOSELF.ELF.TXT", body, nil)
	if !ok || r.app != "GOSELF.ELF" || r.kind != "go" || len(r.frames) != 0 ||
		r.log != "started\nfixture: before panic\npanic: M82e fixture panic\n" {
		t.Fatalf("receipt=%+v ok=%v", r, ok)
	}
	body = fixture(t, "kernel.txt")
	r, ok = parseRecord("1-CRASH.ELF.txt", body, nil)
	if !ok || r.kind != "kernel" || r.pid != 1 || r.tick != 4 ||
		len(r.frames) != 1 || r.frames[0].function != "(in crasher+0x4)" ||
		!strings.Contains(r.serial, "tasks worker advances=") ||
		r.stackNote() != "stack: one recorded PC (not an unwound stack)" {
		t.Fatalf("kernel=%+v ok=%v", r, ok)
	}
	// Start with the actual gate output, replace only the bounded serial tail,
	// and cut at the tombstone's 1024-byte capacity, mid-line and without footer.
	head, _, _ := strings.Cut(string(body), "\n--- Last Serial Output ---\n")
	truncated := []byte(head + "\n--- Last Serial Output ---\n" +
		strings.Repeat("tasks worker advances=5312\n", 64))
	truncated = truncated[:kernelLimit]
	r, ok = parseRecord("1-CRASH.ELF.txt", truncated, nil)
	if !ok || !r.truncated || len(r.frames) != 1 ||
		len(r.serial) != kernelLimit-len(head)-len("\n--- Last Serial Output ---\n") {
		t.Fatalf("truncated=%+v ok=%v", r, ok)
	}
}

func TestRealGuardStack(t *testing.T) {
	r, ok := parseRecord("CRASHFIX.ELF.TXT", fixture(t, "crashfix.txt"), fixture(t, "crashfix.stk"))
	if !ok || r.kind != "go" || len(r.frames) < 3 || r.truncated || r.generation <= 0 {
		t.Fatalf("real guarded panic=%+v ok=%v", r, ok)
	}
	found := false
	for _, f := range r.frames {
		if f.function == "main.main()" {
			found = true
		}
	}
	if !found {
		t.Fatal("real panic stack has no main.main frame")
	}
}

func sidecar(body []byte, generation int64) []byte {
	return []byte("VCRASH1 receipt=" + strconv.FormatUint(hashBytes(body), 16) +
		" nanos=" + strconv.FormatInt(generation, 10) + " truncated=false\n" +
		"goroutine 1 [running]:\n" +
		"virelai/vi.CrashGuard({0x1, 0x2})\n\tvi/log.go:100 +0x40\n" +
		"panic({0x1, 0x2})\n\truntime/panic.go:783 +0x100\n" +
		"main.third(...)\n\tcrashfixture/main.go:30 +0x20\n")
}

func TestStackMatchingAndNoInventedFrames(t *testing.T) {
	body := fixture(t, "goself.txt")
	stack := sidecar(body, 123)
	r, ok := parseRecord("GOSELF.ELF.TXT", body, stack)
	if !ok || len(r.frames) != 3 || r.generation != 123 ||
		r.frames[2].function != "main.third(...)" {
		t.Fatalf("stack=%+v ok=%v", r, ok)
	}
	// An incomplete final source row is not a frame.
	partial := stack[:len(stack)-1]
	r, ok = parseRecord("GOSELF.ELF.TXT", body, partial)
	if !ok || len(r.frames) != 2 {
		t.Fatalf("partial=%+v ok=%v", r, ok)
	}
	for _, bad := range [][]byte{
		sidecar([]byte("a different receipt"), 123),
		[]byte(strings.Replace(string(stack), "nanos=123", "nanos=-1", 1)),
		[]byte(strings.Replace(string(stack), "truncated=false", "truncated=maybe", 1)),
		[]byte("unversioned stack\n"),
	} {
		r, ok := parseRecord("GOSELF.ELF.TXT", body, bad)
		if !ok || len(r.frames) != 0 || r.generation != 0 {
			t.Fatalf("accepted stale/malformed stack=%+v", r)
		}
	}
	exit := []byte("app=GOSH.ELF\noutcome=exit=2\nlast-log:\nchild stopped\n")
	r, ok = parseRecord("GOSH.ELF.TXT", exit, sidecar(exit, 123))
	if !ok || r.kind != "exit" || len(r.frames) != 0 ||
		r.stackNote() != "stack: none (exit status only)" {
		t.Fatalf("exit invented stack=%+v", r)
	}
}

func TestSameSizeAndIdenticalReceiptChanges(t *testing.T) {
	v := newViewer()
	first := fixture(t, "goself.txt")
	second := []byte(strings.Replace(string(first), "fixture panic", "another panic", 1))
	if len(first) != len(second) {
		t.Fatal("test must defeat name+size detection")
	}
	const path = "/host/CRASH/GOSELF.ELF.TXT"
	if _, changed := v.observe(path, "GOSELF.ELF.TXT", first, sidecar(first, 123)); !changed {
		t.Fatal("first crash not shown")
	}
	if _, changed := v.observe(path, "GOSELF.ELF.TXT", first, sidecar(first, 123)); changed {
		t.Fatal("unchanged receipt shown twice")
	}
	if _, changed := v.observe(path, "GOSELF.ELF.TXT", second, sidecar(second, 124)); !changed {
		t.Fatal("same-size second crash not shown")
	}
	if _, changed := v.observe(path, "GOSELF.ELF.TXT", second, sidecar(second, 125)); !changed {
		t.Fatal("identical receipt with new stack generation not shown")
	}
	kernel := fixture(t, "kernel.txt")
	v.observe("/host/crash/1-CRASH.ELF.txt", "1-CRASH.ELF.txt", kernel, nil)
	if len(v.records) != 2 || v.records[0].kind != "kernel" {
		t.Fatalf("newest observed first=%+v", v.records)
	}
}

func TestNameSizeWatchMissesTheSameSizeCrash(t *testing.T) {
	first := fixture(t, "goself.txt")
	second := []byte(strings.Replace(string(first), "fixture panic", "another panic", 1))
	var receipt, stack vi.DirEntry
	copy(receipt.Name[:], "GOSELF.ELF.TXT")
	copy(stack.Name[:], "GOSELF.ELF.STK")
	receipt.Size = uint32(len(first))
	stack.Size = uint32(len(sidecar(first, 123)))
	snap := vi.DirSnapshot{OK: true, Rows: []vi.DirEntry{receipt, stack}}
	var watch vi.Watcher
	watch.Adopt(snap)
	if events := watch.Observe(snap); len(events) != 0 {
		t.Fatalf("Watch must miss equal-size replacement: %v", events)
	}
	if fingerprint(first, sidecar(first, 123)) == fingerprint(second, sidecar(second, 124)) {
		t.Fatal("content hash missed the same-size replacement")
	}
}

func TestParserBoundsAndMalformedRecords(t *testing.T) {
	for _, test := range []struct {
		name, body string
	}{
		{"GOSELF.ELF.TXT", strings.Repeat("x", receiptLimit+1)},
		{"1-CRASH.ELF.txt", strings.Repeat("x", kernelLimit+1)},
		{"GOSELF.ELF.TXT", "app=GOSH.ELF\noutcome=exit=2\nlast-log:\n"},
		{"GOSELF.ELF.STK", "not a receipt"},
		{"1-CRASH.ELF.txt", "VirelaiOS Crash Tombstone\n========================\nProcess: CRASH.ELF\nPID: 2\n"},
	} {
		if _, ok := parseRecord(test.name, []byte(test.body), nil); ok {
			t.Fatalf("accepted malformed %q", test)
		}
	}
}

func TestOptions(t *testing.T) {
	if o, ok := parseOptions([]string{"-text", "-polls", "3", "-exercise", "twice"}); !ok ||
		!o.text || o.polls != 3 || o.exercise != "twice" {
		t.Fatalf("options=%+v ok=%v", o, ok)
	}
	for _, args := range [][]string{{"-polls"}, {"-polls", "0"}, {"-polls", "121"},
		{"-exercise", "unknown"}, {"-unknown"}} {
		if _, ok := parseOptions(args); ok {
			t.Fatalf("accepted %v", args)
		}
	}
}
