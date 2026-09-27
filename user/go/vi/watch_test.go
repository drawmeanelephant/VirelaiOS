package vi

import (
	"strings"
	"testing"
	"unsafe"
)

// row builds one wire-shaped DirEntry for the diff tests.
func row(name string, isDir bool, size uint32) DirEntry {
	var e DirEntry
	copy(e.Name[:], name)
	if isDir {
		e.IsDir = 1
	}
	e.Size = size
	return e
}

func rows(rs ...DirEntry) DirSnapshot { return DirSnapshot{OK: true, Rows: rs} }

func evNames(evs []WatchEvent) string {
	out := make([]string, len(evs))
	for i, ev := range evs {
		out[i] = ev.Kind.String() + ":" + ev.Name
	}
	return strings.Join(out, ",")
}

// TestWatcherPrimesWithoutEvents pins the first-Observe contract: a watcher
// that has never seen the path cannot name a change against it.
func TestWatcherPrimesWithoutEvents(t *testing.T) {
	var w Watcher
	if evs := w.Observe(rows(row("A.TXT", false, 3))); evs != nil {
		t.Fatalf("first Observe returned %v, want nil (prime only)", evs)
	}
	if evs := w.Observe(rows(row("A.TXT", false, 3))); evs != nil {
		t.Fatalf("unchanged second Observe returned %v, want nil", evs)
	}
}

// TestWatcherDiffsCreatedRemovedChanged walks the three row-level events in
// one step: a created file, a resized file, a changed kind, and a removal.
func TestWatcherDiffsCreatedRemovedChanged(t *testing.T) {
	var w Watcher
	w.Observe(rows(row("Bdir", true, 0), row("A.TXT", false, 3), row("OLD", false, 9)))
	evs := w.Observe(rows(
		row("Bdir", true, 0),
		row("A.TXT", false, 7), // resize -> changed
		row("NEW", false, 1),   // absent before -> created
		row("OLD", true, 9),    // kind flip -> changed (a file becoming a dir is news)
	))
	// Order: next.Rows order for created/changed, then removals.
	if got, want := evNames(evs), "changed:A.TXT,created:NEW,changed:OLD"; got != want {
		t.Fatalf("diff events = %q, want %q", got, want)
	}
	// A shape that also removes a name:
	var w2 Watcher
	w2.Observe(rows(row("STAY", false, 3), row("GONE", false, 4), row("GROW", false, 5)))
	evs2 := w2.Observe(rows(row("STAY", false, 3), row("GROW", false, 9), row("FRESH", false, 1)))
	if got, want := evNames(evs2), "changed:GROW,created:FRESH,removed:GONE"; got != want {
		t.Fatalf("diff events = %q, want %q", got, want)
	}
}

// TestWatcherReportsPathGoneAndReturn pins the directory-level contract:
// a failed listing after a good one is WatchPathGone; the directory's
// return diffs as Created for everything listed.
func TestWatcherReportsPathGoneAndReturn(t *testing.T) {
	var w Watcher
	w.Observe(rows(row("A.TXT", false, 3)))
	if evs := w.Observe(DirSnapshot{OK: false}); len(evs) != 1 || evs[0].Kind != WatchPathGone {
		t.Fatalf("gone observe = %v, want one path-gone", evs)
	}
	if evs := w.Observe(DirSnapshot{OK: false}); evs != nil {
		t.Fatalf("still-gone observe = %v, want nil", evs)
	}
	evs := w.Observe(rows(row("A.TXT", false, 3), row("B.TXT", false, 4)))
	if got, want := evNames(evs), "created:A.TXT,created:B.TXT"; got != want {
		t.Fatalf("return events = %q, want %q", got, want)
	}
}

// TestWatcherAdoptSuppressesOwnChanges pins Adopt: a baseline the app took
// after its own mutation must not come back as events on the next poll.
func TestWatcherAdoptSuppressesOwnChanges(t *testing.T) {
	var w Watcher
	w.Observe(rows(row("A.TXT", false, 3)))
	w.Adopt(rows(row("A.TXT", false, 3), row("MADE-BY-APP.TXT", false, 5)))
	if evs := w.Observe(rows(row("A.TXT", false, 3), row("MADE-BY-APP.TXT", false, 5))); evs != nil {
		t.Fatalf("post-adopt Observe = %v, want nil", evs)
	}
	// A failed refresh adopts the zero snapshot; the directory's return is
	// Created events, not a path-gone echo.
	w.Adopt(DirSnapshot{OK: false})
	if got, want := evNames(w.Observe(rows(row("A.TXT", false, 3)))), "created:A.TXT"; got != want {
		t.Fatalf("post-failed-adopt return = %q, want %q", got, want)
	}
}

// TestSnapshotDirSortsAndCarriesSizes exercises the syscall half through the
// test hook: rows come back sorted dirs-first by name, and a failed listing
// is OK=false (never a panic, never a silent empty-but-ok snapshot).
func TestSnapshotDirSortsAndCarriesSizes(t *testing.T) {
	prev := SetSyscallHookForTest(func(num uintptr, a0, a1, a2, a3 uintptr) int64 {
		if num != SlotDirList {
			return -ErrENOSYS
		}
		out := unsafe.Slice((*DirEntry)(unsafe.Pointer(a2)), int(a3))
		fake := rows(row("zeta.txt", false, 26), row("Adir", true, 0), row("b.txt", false, 2))
		for i, r := range fake.Rows {
			if i >= len(out) {
				break
			}
			out[i] = r
		}
		return int64(len(fake.Rows))
	})
	defer SetSyscallHookForTest(prev)

	snap := SnapshotDir("/host/FM")
	if !snap.OK {
		t.Fatal("SnapshotDir reported !OK for a served listing")
	}
	if got, want := evNames(nil)+namesOf(snap.Rows), "Adir,b.txt,zeta.txt"; !strings.HasSuffix(got, want) {
		t.Fatalf("snapshot order = %q, want dirs-first %q", got, want)
	}
	if snap.Rows[2].Size != 26 {
		t.Fatalf("zeta.txt size = %d, want 26", snap.Rows[2].Size)
	}

	prev2 := SetSyscallHookForTest(func(num uintptr, a0, a1, a2, a3 uintptr) int64 {
		return ErrFileNotFound // already negative: the HF not-found status
	})
	defer SetSyscallHookForTest(prev2)
	if snap := SnapshotDir("/host/vanished"); snap.OK || len(snap.Rows) != 0 {
		t.Fatalf("failed listing = %+v, want OK=false with no rows", snap)
	}
}

func namesOf(rs []DirEntry) string {
	out := make([]string, len(rs))
	for i, r := range rs {
		out[i] = r.NameString()
	}
	return strings.Join(out, ",")
}
