package main

import (
	"errors"
	"strings"
	"testing"

	"virelai/vi"
)

// The fakes: each seam is a var so the whole verb matrix runs on the
// host — same arrangement gotabwm's snapshot tests use.
type fakeEnv struct {
	logs     []string
	items    []vi.DirItem
	listErr  error
	exists   map[string]bool
	deleted  []string
	deleteRC int64
	// ctl records every issued session and answers with the queued
	// verdicts (0 = ok, >0 = code bytes written into the buf the kernel
	// would fill, <0 = errno like a host build's -ENOSYS).
	ctlCalls []string
	ctlRC    int64
	ctlCode  string
	// writes records every WriteFileSafe target (the pin verb's marker).
	writes  []string
	writeRC int64
}

func (f *fakeEnv) install(t *testing.T) {
	t.Helper()
	oldCtl, oldList, oldEx, oldDel, oldWr, oldLog := ctl, listDir, fileExists, fileDelete, fileWrite, logLine
	ctl = func(verb, name string, code []byte) int64 {
		f.ctlCalls = append(f.ctlCalls, verb+"/"+name)
		if f.ctlRC > 0 {
			n := int64(len(f.ctlCode))
			if n > int64(len(code)) {
				n = int64(len(code))
			}
			copy(code, f.ctlCode)
			return n
		}
		return f.ctlRC
	}
	listDir = func(string) ([]vi.DirItem, error) { return f.items, f.listErr }
	fileExists = func(p string) bool { return f.exists[p] }
	fileDelete = func(p string) int64 {
		f.deleted = append(f.deleted, p)
		return f.deleteRC
	}
	fileWrite = func(p string, _ []byte) int64 {
		f.writes = append(f.writes, p)
		return f.writeRC
	}
	logLine = func(s string) { f.logs = append(f.logs, s) }
	t.Cleanup(func() {
		ctl, listDir, fileExists, fileDelete, fileWrite, logLine = oldCtl, oldList, oldEx, oldDel, oldWr, oldLog
	})
}

func (f *fakeEnv) has(sub string) bool {
	for _, l := range f.logs {
		if strings.Contains(l, sub) {
			return true
		}
	}
	return false
}

func dirItem(name string, size uint64) vi.DirItem {
	var it vi.DirItem
	it.Name = name
	it.Size = size
	return it
}

// dirItemCT is dirItem with a ctime — prune's victim ordering reads it.
func dirItemCT(name string, size uint64, sec int64) vi.DirItem {
	it := dirItem(name, size)
	it.CtimeSec = sec
	return it
}

func TestLsEmptyWhenDirMissing(t *testing.T) {
	f := &fakeEnv{listErr: errors.New("ENOENT")}
	f.install(t)
	if rc := run([]string{"GOSNAP.ELF", "ls"}); rc != 0 {
		t.Fatalf("ls rc=%d", rc)
	}
	if !f.has("gosnap: ls n=0") {
		t.Fatalf("missing empty marker: %v", f.logs)
	}
}

func TestLsListsSnapshotsSortedAndSkipsStrays(t *testing.T) {
	f := &fakeEnv{items: []vi.DirItem{
		dirItem("SECOND.vzsave", 64),
		dirItem("FIRST.vzsave", 32),
		dirItem("FIRST.vzsave.part", 8), // an in-flight save is not a snapshot
		dirItem("README.TXT", 4),
	}}
	f.install(t)
	if rc := run([]string{"GOSNAP.ELF", "ls"}); rc != 0 {
		t.Fatalf("ls rc=%d", rc)
	}
	want := []string{
		"gosnap: snap name=FIRST bytes=32 pin=0",
		"gosnap: snap name=SECOND bytes=64 pin=0",
		"gosnap: ls n=2",
	}
	if len(f.logs) != len(want) {
		t.Fatalf("logs = %v, want %v", f.logs, want)
	}
	for i := range want {
		if f.logs[i] != want[i] {
			t.Fatalf("line %d = %q, want %q", i, f.logs[i], want[i])
		}
	}
}

func TestSaveRelaysTheCtlVerdict(t *testing.T) {
	f := &fakeEnv{}
	f.install(t)
	if rc := run([]string{"GOSNAP.ELF", "save", "BASE"}); rc != 0 {
		t.Fatalf("save rc=%d", rc)
	}
	if len(f.ctlCalls) != 1 || f.ctlCalls[0] != "snapshot/BASE" {
		t.Fatalf("ctl calls = %v", f.ctlCalls)
	}
	if !f.has("gosnap: save BASE ok") {
		t.Fatalf("missing ok marker: %v", f.logs)
	}
}

func TestSaveRelaysARefusalByName(t *testing.T) {
	f := &fakeEnv{ctlCode: "exists"}
	f.ctlRC = 1 // >0: code bytes returned
	f.install(t)
	if rc := run([]string{"GOSNAP.ELF", "save", "BASE"}); rc != exitRefused {
		t.Fatalf("save rc=%d", rc)
	}
	if !f.has("gosnap: save BASE refused exists") {
		t.Fatalf("missing refused marker: %v", f.logs)
	}
}

func TestSaveRejectsABadNameLocally(t *testing.T) {
	f := &fakeEnv{}
	f.install(t)
	if rc := run([]string{"GOSNAP.ELF", "save", ".."}); rc != exitRefused {
		t.Fatalf("save rc=%d", rc)
	}
	if len(f.ctlCalls) != 0 {
		t.Fatalf("a bad name must never reach the wire: %v", f.ctlCalls)
	}
	if !f.has("gosnap: save .. refused name_rejected") {
		t.Fatalf("missing name_rejected: %v", f.logs)
	}
}

func TestRmWithoutConfirmIsTheConsentAsk(t *testing.T) {
	f := &fakeEnv{exists: map[string]bool{"/host/SNAPSHOTS/BASE.vzsave": true}}
	f.install(t)
	if rc := run([]string{"GOSNAP.ELF", "rm", "BASE"}); rc != exitRefused {
		t.Fatalf("rm rc=%d", rc)
	}
	if len(f.deleted) != 0 {
		t.Fatalf("rm deleted without consent: %v", f.deleted)
	}
	if !f.has("gosnap: consent rm BASE") || !f.has("gosnap: rm BASE refused consent_required") {
		t.Fatalf("missing consent markers: %v", f.logs)
	}
}

func TestRmWithConfirmDeletesTheShareFile(t *testing.T) {
	f := &fakeEnv{exists: map[string]bool{"/host/SNAPSHOTS/BASE.vzsave": true}}
	f.install(t)
	if rc := run([]string{"GOSNAP.ELF", "rm", "BASE", "confirm"}); rc != 0 {
		t.Fatalf("rm rc=%d", rc)
	}
	if len(f.deleted) != 1 || f.deleted[0] != "/host/SNAPSHOTS/BASE.vzsave" {
		t.Fatalf("deleted = %v", f.deleted)
	}
	if !f.has("gosnap: rm BASE ok") {
		t.Fatalf("missing ok marker: %v", f.logs)
	}
}

func TestRmMissingIsNotFound(t *testing.T) {
	f := &fakeEnv{exists: map[string]bool{}}
	f.install(t)
	if rc := run([]string{"GOSNAP.ELF", "rm", "GONE", "confirm"}); rc != exitRefused {
		t.Fatalf("rm rc=%d", rc)
	}
	if len(f.deleted) != 0 {
		t.Fatalf("rm issued a delete for an absent file: %v", f.deleted)
	}
	if !f.has("gosnap: rm GONE refused not_found") {
		t.Fatalf("missing not_found: %v", f.logs)
	}
}

func TestRmCannotEscapeTheSnapshotsDir(t *testing.T) {
	f := &fakeEnv{exists: map[string]bool{"/host/SNAPSHOTS/../SETTINGS.TXT.vzsave": true}}
	f.install(t)
	// The name grammar refuses '/', so a traversal attempt never becomes
	// a path — the verb is refused before any file op.
	if rc := run([]string{"GOSNAP.ELF", "rm", "../SETTINGS.TXT", "confirm"}); rc != exitRefused {
		t.Fatalf("rm traversal rc=%d", rc)
	}
	if len(f.deleted) != 0 {
		t.Fatalf("rm deleted through a traversal name: %v", f.deleted)
	}
}

func TestRestoreWithoutConfirmWritesNothing(t *testing.T) {
	f := &fakeEnv{}
	f.install(t)
	if rc := run([]string{"GOSNAP.ELF", "restore", "BASE"}); rc != exitRefused {
		t.Fatalf("restore rc=%d", rc)
	}
	if len(f.ctlCalls) != 0 {
		t.Fatalf("restore staged without consent: %v", f.ctlCalls)
	}
	if !f.has("gosnap: consent restore BASE") || !f.has("gosnap: restore BASE refused consent_required") {
		t.Fatalf("missing consent markers: %v", f.logs)
	}
}

func TestRestoreWithConfirmStagesThroughCtl(t *testing.T) {
	f := &fakeEnv{}
	f.install(t)
	if rc := run([]string{"GOSNAP.ELF", "restore", "BASE", "confirm"}); rc != 0 {
		t.Fatalf("restore rc=%d", rc)
	}
	if len(f.ctlCalls) != 1 || f.ctlCalls[0] != "restore/BASE" {
		t.Fatalf("ctl calls = %v", f.ctlCalls)
	}
	if !f.has("gosnap: restore BASE staged") {
		t.Fatalf("missing staged marker: %v", f.logs)
	}
}

func TestRestoreRelaysNotFound(t *testing.T) {
	f := &fakeEnv{ctlCode: "not_found"}
	f.ctlRC = 1
	f.install(t)
	if rc := run([]string{"GOSNAP.ELF", "restore", "GONE", "confirm"}); rc != exitRefused {
		t.Fatalf("restore rc=%d", rc)
	}
	if !f.has("gosnap: restore GONE refused not_found") {
		t.Fatalf("missing not_found: %v", f.logs)
	}
}

// --- #3369: the capped store — pin markers, eviction surfacing, prune ---

func TestLsMarksPinnedRowsAndSkipsPinFiles(t *testing.T) {
	f := &fakeEnv{items: []vi.DirItem{
		dirItem("KEEP.vzsave", 64),
		dirItem("KEEP.pin", 4),
		dirItem("OPEN.vzsave", 32),
	}}
	f.install(t)
	if rc := run([]string{"GOSNAP.ELF", "ls"}); rc != 0 {
		t.Fatalf("ls rc=%d", rc)
	}
	want := []string{
		"gosnap: snap name=KEEP bytes=64 pin=1",
		"gosnap: snap name=OPEN bytes=32 pin=0",
		"gosnap: ls n=2", // the pin file never becomes a row
	}
	if len(f.logs) != len(want) {
		t.Fatalf("logs = %v, want %v", f.logs, want)
	}
	for i := range want {
		if f.logs[i] != want[i] {
			t.Fatalf("line %d = %q, want %q", i, f.logs[i], want[i])
		}
	}
}

func TestSaveSurfacesTheCapsEviction(t *testing.T) {
	f := &fakeEnv{items: []vi.DirItem{
		dirItem("OLDEST.vzsave", 64),
		dirItem("KEEP.vzsave", 32),
	}}
	f.install(t)
	// The host's cap admits the new name by evicting the oldest — model
	// the share truthfully: the victim file is gone after the save.
	ctl = func(verb, name string, code []byte) int64 {
		f.ctlCalls = append(f.ctlCalls, verb+"/"+name)
		f.items = []vi.DirItem{dirItem("KEEP.vzsave", 32), dirItem("NEW.vzsave", 96)}
		return 0
	}
	if rc := run([]string{"GOSNAP.ELF", "save", "NEW"}); rc != 0 {
		t.Fatalf("save rc=%d", rc)
	}
	if !f.has("gosnap: save NEW ok") {
		t.Fatalf("missing ok marker: %v", f.logs)
	}
	if !f.has("gosnap: evicted OLDEST") {
		t.Fatalf("the dropped row was not surfaced: %v", f.logs)
	}
	if f.has("gosnap: evicted KEEP") || f.has("gosnap: evicted NEW") {
		t.Fatalf("a surviving/new row was reported evicted: %v", f.logs)
	}
}

func TestSaveRelaysStoreFull(t *testing.T) {
	f := &fakeEnv{ctlCode: "store_full"}
	f.ctlRC = 1
	f.install(t)
	if rc := run([]string{"GOSNAP.ELF", "save", "ONE-MORE"}); rc != exitRefused {
		t.Fatalf("save rc=%d", rc)
	}
	if !f.has("gosnap: save ONE-MORE refused store_full") {
		t.Fatalf("missing store_full: %v", f.logs)
	}
}

func TestPinWritesTheMarkerBesideTheSnapshot(t *testing.T) {
	f := &fakeEnv{exists: map[string]bool{"/host/SNAPSHOTS/BASE.vzsave": true}}
	f.install(t)
	if rc := run([]string{"GOSNAP.ELF", "pin", "BASE"}); rc != 0 {
		t.Fatalf("pin rc=%d", rc)
	}
	if len(f.writes) != 1 || f.writes[0] != "/host/SNAPSHOTS/BASE.pin" {
		t.Fatalf("writes = %v", f.writes)
	}
	if !f.has("gosnap: pin BASE ok") {
		t.Fatalf("missing pin marker: %v", f.logs)
	}
	if len(f.ctlCalls) != 0 {
		t.Fatalf("pin must never reach the wire: %v", f.ctlCalls)
	}
}

func TestPinMissingSnapshotIsNotFound(t *testing.T) {
	f := &fakeEnv{exists: map[string]bool{}}
	f.install(t)
	if rc := run([]string{"GOSNAP.ELF", "pin", "GONE"}); rc != exitRefused {
		t.Fatalf("pin rc=%d", rc)
	}
	if len(f.writes) != 0 {
		t.Fatalf("pin wrote for an absent snapshot: %v", f.writes)
	}
	if !f.has("gosnap: pin GONE refused not_found") {
		t.Fatalf("missing not_found: %v", f.logs)
	}
}

func TestUnpinDeletesTheMarker(t *testing.T) {
	f := &fakeEnv{exists: map[string]bool{"/host/SNAPSHOTS/BASE.pin": true}}
	f.install(t)
	if rc := run([]string{"GOSNAP.ELF", "unpin", "BASE"}); rc != 0 {
		t.Fatalf("unpin rc=%d", rc)
	}
	if len(f.deleted) != 1 || f.deleted[0] != "/host/SNAPSHOTS/BASE.pin" {
		t.Fatalf("deleted = %v", f.deleted)
	}
	if !f.has("gosnap: unpin BASE ok") {
		t.Fatalf("missing unpin marker: %v", f.logs)
	}
}

func TestUnpinWithoutMarkerIsNotFound(t *testing.T) {
	f := &fakeEnv{exists: map[string]bool{"/host/SNAPSHOTS/BASE.vzsave": true}}
	f.install(t)
	if rc := run([]string{"GOSNAP.ELF", "unpin", "BASE"}); rc != exitRefused {
		t.Fatalf("unpin rc=%d", rc)
	}
	if !f.has("gosnap: unpin BASE refused not_found") {
		t.Fatalf("missing not_found: %v", f.logs)
	}
}

func TestPruneWithoutConfirmIsTheConsentAsk(t *testing.T) {
	f := &fakeEnv{items: []vi.DirItem{
		dirItemCT("OLD.vzsave", 64, 10),
		dirItemCT("NEW.vzsave", 64, 20),
	}}
	f.install(t)
	if rc := run([]string{"GOSNAP.ELF", "prune"}); rc != exitRefused {
		t.Fatalf("prune rc=%d", rc)
	}
	if len(f.deleted) != 0 {
		t.Fatalf("prune deleted without consent: %v", f.deleted)
	}
	if !f.has("gosnap: consent prune OLD") || !f.has("gosnap: prune refused consent_required") {
		t.Fatalf("missing consent markers: %v", f.logs)
	}
}

func TestPruneConfirmDeletesTheOldestUnpinned(t *testing.T) {
	// CRASH0 is the rotation's slot (older than everything) and KEEP is
	// pinned — the victim is the oldest unpinned NAMED row.
	f := &fakeEnv{
		items: []vi.DirItem{
			dirItemCT("CRASH0.vzsave", 64, 1),
			dirItemCT("KEEP.vzsave", 64, 5),
			dirItem("KEEP.pin", 4),
			dirItemCT("VICTIM.vzsave", 64, 10),
			dirItemCT("NEWER.vzsave", 64, 20),
		},
		exists: map[string]bool{"/host/SNAPSHOTS/VICTIM.vzsave": true},
	}
	f.install(t)
	if rc := run([]string{"GOSNAP.ELF", "prune", "confirm"}); rc != 0 {
		t.Fatalf("prune rc=%d", rc)
	}
	if len(f.deleted) != 1 || f.deleted[0] != "/host/SNAPSHOTS/VICTIM.vzsave" {
		t.Fatalf("deleted = %v — the crash slot or the pin was touched", f.deleted)
	}
	if !f.has("gosnap: prune VICTIM ok") {
		t.Fatalf("missing prune marker: %v", f.logs)
	}
}

func TestPruneNamesAllPinned(t *testing.T) {
	f := &fakeEnv{items: []vi.DirItem{
		dirItemCT("KEEP.vzsave", 64, 5),
		dirItem("KEEP.pin", 4),
		dirItemCT("CRASH0.vzsave", 64, 1),
	}}
	f.install(t)
	if rc := run([]string{"GOSNAP.ELF", "prune", "confirm"}); rc != exitRefused {
		t.Fatalf("prune rc=%d", rc)
	}
	if len(f.deleted) != 0 {
		t.Fatalf("a pinned or crash row was deleted: %v", f.deleted)
	}
	if !f.has("gosnap: prune refused all_pinned") {
		t.Fatalf("missing all_pinned: %v", f.logs)
	}
}

func TestPruneAnEmptyStoreIsNotFound(t *testing.T) {
	f := &fakeEnv{listErr: errors.New("ENOENT")}
	f.install(t)
	if rc := run([]string{"GOSNAP.ELF", "prune", "confirm"}); rc != exitRefused {
		t.Fatalf("prune rc=%d", rc)
	}
	if !f.has("gosnap: prune refused not_found") {
		t.Fatalf("missing not_found: %v", f.logs)
	}
}

func TestCtlNegativeReturnIsAnErrnoName(t *testing.T) {
	f := &fakeEnv{ctlRC: -4} // -ENOSYS, the host-build seam answer
	f.install(t)
	if rc := run([]string{"GOSNAP.ELF", "save", "BASE"}); rc != exitRefused {
		t.Fatalf("save rc=%d", rc)
	}
	if !f.has("gosnap: save BASE refused ENOSYS") {
		t.Fatalf("missing errno refusal: %v", f.logs)
	}
}

func TestUsageAndNoArgs(t *testing.T) {
	f := &fakeEnv{}
	f.install(t)
	// M120d: bare is the windowed face — on the host the win_open seam
	// is -ENOSYS, so the run refuses with the open error and still
	// prints the verbs (a serial-only caller learns the command line).
	if rc := run([]string{"GOSNAP.ELF"}); rc != exitRefused {
		t.Fatalf("bare rc=%d", rc)
	}
	if !f.has("gosnap: error open -1") {
		t.Fatalf("missing open refusal: %v", f.logs)
	}
	if rc := run([]string{"GOSNAP.ELF", "frobnicate"}); rc != exitUsage {
		t.Fatalf("bad verb rc=%d", rc)
	}
	if rc := run([]string{"GOSNAP.ELF", "ls", "extra"}); rc != exitUsage {
		t.Fatalf("ls extra rc=%d", rc)
	}
}
