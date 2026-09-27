package main

import (
	"strings"
	"testing"
	"unsafe"

	"virelai/vi"
)

// watchFakeFS serves SlotDirList for the M81f watchStep tests: one mutable
// row set for the model's path, plus a fail switch for the path-gone shape.
// Everything else refuses with ENOSYS, so a test that strays off DirList
// fails loudly instead of silently.
type watchFakeFS struct {
	path  string
	rows  []vi.DirEntry
	fail  bool
	calls int
}

func (f *watchFakeFS) serve(num uintptr, a0, a1, a2, a3 uintptr) int64 {
	if num != vi.SlotDirList {
		return -vi.ErrENOSYS
	}
	f.calls++
	p := string(unsafe.Slice((*byte)(unsafe.Pointer(a0)), int(a1)))
	if p != f.path || f.fail {
		return vi.ErrFileNotFound // already negative: the HF not-found status
	}
	out := unsafe.Slice((*vi.DirEntry)(unsafe.Pointer(a2)), int(a3))
	n := 0
	for _, r := range f.rows {
		if n >= len(out) {
			break
		}
		out[n] = r
		n++
	}
	return int64(n)
}

func watchRow(name string, size uint32) vi.DirEntry {
	var e vi.DirEntry
	copy(e.Name[:], name)
	e.Size = size
	return e
}

func withFakeFS(t *testing.T, fs *watchFakeFS) {
	t.Helper()
	prev := vi.SetSyscallHookForTest(fs.serve)
	t.Cleanup(func() { vi.SetSyscallHookForTest(prev) })
}

func TestWatchArmsOnceThenSaysNothing(t *testing.T) {
	fs := &watchFakeFS{path: "/host/FM", rows: []vi.DirEntry{watchRow("BASE.TXT", 4)}}
	withFakeFS(t, fs)
	m := testModel()
	if ev := m.watchStep(); ev != 0 {
		t.Fatalf("first watchStep = %d, want 0 (armed)", ev)
	}
	if got := pendingJoined(&m); !strings.Contains(got, markerWatchArmed) {
		t.Fatalf("first batch missing the armed marker: %q", got)
	}
	if ev := m.watchStep(); ev != -1 {
		t.Fatalf("second watchStep = %d, want -1 (no change)", ev)
	}
	if got := pendingJoined(&m); got != "" {
		t.Fatalf("no-change poll queued markers: %q", got)
	}
}

func TestWatchReportsCreatedFileAndRelists(t *testing.T) {
	fs := &watchFakeFS{path: "/host/FM", rows: []vi.DirEntry{watchRow("BASE.TXT", 4)}}
	withFakeFS(t, fs)
	m := testModel()
	m.watchStep() // arm

	fs.rows = append(fs.rows, watchRow("LATE.TXT", 31))
	if ev := m.watchStep(); ev != 1 {
		t.Fatalf("watchStep = %d, want 1 event", ev)
	}
	got := pendingJoined(&m)
	for _, want := range []string{
		markerWatchCreated + "LATE.TXT",
		markerList + "/host/FM n=2",
		markerEntry + "LATE.TXT file size=31",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("batch %q missing %q", got, want)
		}
	}
	if m.n != 2 {
		t.Fatalf("listing after event n=%d, want 2", m.n)
	}
	// The event's refresh adopted the new baseline: the next poll is quiet.
	if ev := m.watchStep(); ev != -1 {
		t.Fatalf("poll after refresh = %d, want -1", ev)
	}
}

func TestWatchIgnoresWhatTheAppItselfChanged(t *testing.T) {
	fs := &watchFakeFS{path: "/host/FM", rows: []vi.DirEntry{watchRow("BASE.TXT", 4)}}
	withFakeFS(t, fs)
	m := testModel()
	m.watchStep() // arm
	m.drain()

	// The app's own action re-lists through refresh (a rename, a paste):
	// the feed must treat the result as the new unchanged baseline.
	fs.rows = []vi.DirEntry{watchRow("RENAMED.TXT", 4)}
	m.refresh()
	if got := pendingJoined(&m); strings.Contains(got, "watch ") {
		t.Fatalf("own refresh produced watch markers: %q", got)
	}
	if ev := m.watchStep(); ev != -1 {
		t.Fatalf("poll after own refresh = %d, want -1", ev)
	}
}

func TestWatchReportsRemovedChangedAndGone(t *testing.T) {
	fs := &watchFakeFS{path: "/host/FM", rows: []vi.DirEntry{watchRow("STAY.TXT", 4), watchRow("GONE.TXT", 5)}}
	withFakeFS(t, fs)
	m := testModel()
	m.watchStep() // arm

	fs.rows = []vi.DirEntry{watchRow("STAY.TXT", 9)}
	if ev := m.watchStep(); ev != 2 {
		t.Fatalf("watchStep = %d, want 2 events (changed + removed)", ev)
	}
	got := pendingJoined(&m)
	if !strings.Contains(got, markerWatchChanged+"STAY.TXT size=9") {
		t.Fatalf("batch %q missing the changed marker", got)
	}
	if !strings.Contains(got, markerWatchRemoved+"GONE.TXT") {
		t.Fatalf("batch %q missing the removed marker", got)
	}
	m.drain()

	// The directory itself stops listing: one path-gone event plus the
	// honest list-error refresh; its return diffs as Created.
	fs.fail = true
	if ev := m.watchStep(); ev != 1 {
		t.Fatalf("gone watchStep = %d, want 1", ev)
	}
	if got := pendingJoined(&m); !strings.Contains(got, markerWatchGone) ||
		!strings.Contains(got, markerListErr) {
		t.Fatalf("gone batch = %q, want %q + %q", got, markerWatchGone, markerListErr)
	}
	m.drain()

	fs.fail = false
	fs.rows = []vi.DirEntry{watchRow("STAY.TXT", 9)}
	if ev := m.watchStep(); ev != 1 {
		t.Fatalf("return watchStep = %d, want 1", ev)
	}
	if got := pendingJoined(&m); !strings.Contains(got, markerWatchCreated+"STAY.TXT") {
		t.Fatalf("return batch %q missing the created marker", got)
	}
}
