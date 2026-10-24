package main

import (
	"errors"
	"testing"

	"virelai/tabapp"
	"virelai/vi"
)

// view_test.go — M120d (#2561): class-A tests for the windowed face's
// model. The whole state machine — listing, selection, the two-step
// confirm, the focus ring — runs on the host against the same fakeEnv
// seams the argv tests use; only the paint itself stays guest-only.
//
// newSnapView takes a nil TabApp here deliberately: nothing in the
// model path touches the window. (draw()/present() do, and are covered
// by the go-snap spec's windowed leg instead.)

func newTestView() *snapView { return newSnapView(nil) }

func keyEvent(usage uint32, ch byte) vi.Event {
	return vi.Event{Kind: vi.EvKeyDown, Arg0: usage, Arg1: uint32(ch)}
}

func TestViewRefreshListsSortedRows(t *testing.T) {
	f := &fakeEnv{items: []vi.DirItem{
		dirItem("SECOND.vzsave", 64),
		dirItem("FIRST.vzsave", 32),
		dirItem("FIRST.vzsave.part", 8),
	}}
	f.install(t)
	v := newTestView()
	v.refresh()
	if len(v.rows) != 2 || v.rows[0].name != "FIRST" || v.rows[1].name != "SECOND" {
		t.Fatalf("rows = %v", v.rows)
	}
	if len(v.list.Items) != 2 {
		t.Fatalf("items = %v", v.list.Items)
	}
	if !f.has("gosnap: snap name=FIRST bytes=32") || !f.has("gosnap: ls n=2") {
		t.Fatalf("missing ls markers: %v", f.logs)
	}
}

func TestViewRefreshMissingDirIsEmpty(t *testing.T) {
	f := &fakeEnv{listErr: errors.New("ENOENT")}
	f.install(t)
	v := newTestView()
	v.refresh()
	if len(v.rows) != 0 || v.sel != -1 {
		t.Fatalf("rows=%v sel=%d", v.rows, v.sel)
	}
	if !f.has("gosnap: ls n=0") {
		t.Fatalf("missing empty marker: %v", f.logs)
	}
	if _, ok := v.selName(); ok {
		t.Fatal("empty list must not have a selection name")
	}
	if !v.restBtn.Disabled || !v.delBtn.Disabled {
		t.Fatal("row verbs must be disabled with no selection")
	}
}

func TestViewSaveRunsTheVerbAndRelists(t *testing.T) {
	f := &fakeEnv{}
	f.install(t)
	v := newTestView()
	v.field.SetValue("BASE")
	v.save()
	if len(f.ctlCalls) != 1 || f.ctlCalls[0] != "snapshot/BASE" {
		t.Fatalf("ctl calls = %v", f.ctlCalls)
	}
	if !f.has("gosnap: save BASE ok") {
		t.Fatalf("missing save marker: %v", f.logs)
	}
	if v.field.Value() != "" {
		t.Fatalf("field not cleared: %q", v.field.Value())
	}
	// The re-list ran: the ls marker followed the save marker.
	if !f.has("gosnap: ls n=0") {
		t.Fatalf("missing refresh marker: %v", f.logs)
	}
}

func TestViewSaveEmptyFieldIsAHintNotAVerb(t *testing.T) {
	f := &fakeEnv{}
	f.install(t)
	v := newTestView()
	v.save()
	if len(f.ctlCalls) != 0 {
		t.Fatalf("an empty name must never reach the wire: %v", f.ctlCalls)
	}
	if v.status == "" {
		t.Fatal("no status hint for the empty name")
	}
}

func TestViewRestoreNeedsTwoGestures(t *testing.T) {
	f := &fakeEnv{items: []vi.DirItem{dirItem("BASE.vzsave", 64)}}
	f.install(t)
	v := newTestView()
	v.refresh()
	v.sel, v.list.Sel = 0, 0
	// First gesture: the consent ask, the Confirm? face, no wire call.
	v.activate("restore")
	if v.armed != "restore" || v.armedName != "BASE" {
		t.Fatalf("armed=%q %q", v.armed, v.armedName)
	}
	if len(f.ctlCalls) != 0 {
		t.Fatalf("restore ran on the first gesture: %v", f.ctlCalls)
	}
	if !f.has("gosnap: consent restore BASE") {
		t.Fatalf("missing consent marker: %v", f.logs)
	}
	if v.restBtn.Label != "Confirm?" {
		t.Fatalf("armed label = %q", v.restBtn.Label)
	}
	// Second gesture: the verb, the staged marker, disarmed.
	v.activate("restore")
	if len(f.ctlCalls) != 1 || f.ctlCalls[0] != "restore/BASE" {
		t.Fatalf("ctl calls = %v", f.ctlCalls)
	}
	if !f.has("gosnap: restore BASE staged") {
		t.Fatalf("missing staged marker: %v", f.logs)
	}
	if v.armed != "" || v.restBtn.Label != "Restore" {
		t.Fatalf("still armed after confirm: %q %q", v.armed, v.restBtn.Label)
	}
}

func TestViewDeleteNeedsTwoGestures(t *testing.T) {
	f := &fakeEnv{
		items:  []vi.DirItem{dirItem("BASE.vzsave", 64)},
		exists: map[string]bool{"/host/SNAPSHOTS/BASE.vzsave": true},
	}
	f.install(t)
	// Model the delete against the listing too — a faithful share drops
	// the file, so the post-rm refresh sees it gone.
	fileDelete = func(p string) int64 {
		f.deleted = append(f.deleted, p)
		var kept []vi.DirItem
		for _, it := range f.items {
			if snapsDir+"/"+it.Name != p {
				kept = append(kept, it)
			}
		}
		f.items = kept
		return 0
	}
	v := newTestView()
	v.refresh()
	v.sel, v.list.Sel = 0, 0
	v.activate("rm")
	if len(f.deleted) != 0 {
		t.Fatalf("rm ran on the first gesture: %v", f.deleted)
	}
	v.activate("rm")
	if len(f.deleted) != 1 || f.deleted[0] != "/host/SNAPSHOTS/BASE.vzsave" {
		t.Fatalf("deleted = %v", f.deleted)
	}
	if !f.has("gosnap: rm BASE ok") {
		t.Fatalf("missing rm marker: %v", f.logs)
	}
	// The row left the list — the refresh re-read the share.
	if len(v.rows) != 0 || v.sel != -1 {
		t.Fatalf("rows=%v sel=%d after rm", v.rows, v.sel)
	}
}

func TestViewSwitchingRowsDisarms(t *testing.T) {
	f := &fakeEnv{items: []vi.DirItem{
		dirItem("FIRST.vzsave", 32),
		dirItem("SECOND.vzsave", 64),
	}}
	f.install(t)
	v := newTestView()
	v.refresh()
	v.sel, v.list.Sel = 0, 0
	v.activate("restore")
	// A moved selection drops the arm — the confirm named a row.
	v.selTo(1)
	if v.armed != "" {
		t.Fatalf("arm survived a selection change: %q", v.armed)
	}
	if len(f.ctlCalls) != 0 {
		t.Fatalf("a wire verb ran: %v", f.ctlCalls)
	}
}

func TestViewEscapeDisarmsBeforeQuitting(t *testing.T) {
	f := &fakeEnv{items: []vi.DirItem{dirItem("BASE.vzsave", 64)}}
	f.install(t)
	v := newTestView()
	v.refresh()
	v.sel, v.list.Sel = 0, 0
	v.activate("rm")
	if !v.handle(keyEvent(keyEscape, 0)) {
		t.Fatal("Esc did not report a frame change")
	}
	if v.armed != "" {
		t.Fatal("Esc did not disarm")
	}
	if v.quit {
		t.Fatal("the cancel Esc also quit")
	}
	if len(f.deleted) != 0 || len(f.ctlCalls) != 0 {
		t.Fatal("a verb ran without the second gesture")
	}
	// Disarmed, the next Esc is the quit gesture.
	if !v.handle(keyEvent(keyEscape, 0)) || !v.quit {
		t.Fatal("second Esc did not quit")
	}
}

func TestViewArmingADifferentVerbReselects(t *testing.T) {
	f := &fakeEnv{items: []vi.DirItem{dirItem("BASE.vzsave", 64)}}
	f.install(t)
	v := newTestView()
	v.refresh()
	v.sel, v.list.Sel = 0, 0
	v.activate("restore")
	v.activate("rm") // arm switches verbs, still first-gesture
	if v.armed != "rm" {
		t.Fatalf("armed=%q", v.armed)
	}
	if len(f.ctlCalls) != 0 || len(f.deleted) != 0 {
		t.Fatal("a verb ran on a re-arm")
	}
}

func TestViewListKeysAndFocusRing(t *testing.T) {
	f := &fakeEnv{items: []vi.DirItem{
		dirItem("A.vzsave", 1),
		dirItem("B.vzsave", 2),
		dirItem("C.vzsave", 3),
	}}
	f.install(t)
	v := newTestView()
	v.refresh()
	// Up/Down move the list from every focus slot (the field included).
	v.handle(keyEvent(keyDown, 0))
	if v.sel != 0 {
		t.Fatalf("sel=%d", v.sel)
	}
	v.handle(keyEvent(keyDown, 0))
	v.handle(keyEvent(keyDown, 0))
	if v.sel != 2 {
		t.Fatalf("sel=%d", v.sel)
	}
	v.handle(keyEvent(keyDown, 0))
	if v.sel != 2 {
		t.Fatalf("sel ran past the end: %d", v.sel)
	}
	// Tab walks the ring field -> save -> restore -> delete -> pin ->
	// prune -> list.
	v.handle(keyEvent(keyTab, 0))
	if v.focus != fSave {
		t.Fatalf("focus=%d", v.focus)
	}
	v.handle(keyEvent(keyTab, 0))
	v.handle(keyEvent(keyTab, 0))
	if v.focus != fDel {
		t.Fatalf("focus=%d", v.focus)
	}
	v.handle(keyEvent(keyTab, 0))
	if v.focus != fPin {
		t.Fatalf("focus=%d", v.focus)
	}
	v.handle(keyEvent(keyTab, 0))
	if v.focus != fPrune {
		t.Fatalf("focus=%d", v.focus)
	}
	v.handle(keyEvent(keyTab, 0))
	if v.focus != fList {
		t.Fatalf("focus=%d (ring did not wrap)", v.focus)
	}
}

// --- #3369: pin + prune controls -------------------------------------

func TestViewPinTogglesOnTheSelectedRow(t *testing.T) {
	f := &fakeEnv{
		items:  []vi.DirItem{dirItem("BASE.vzsave", 64)},
		exists: map[string]bool{"/host/SNAPSHOTS/BASE.vzsave": true},
	}
	f.install(t)
	v := newTestView()
	v.refresh()
	v.sel, v.list.Sel = 0, 0
	// One gesture — pin is additive, never destructive.
	v.togglePin()
	if len(f.writes) != 1 || f.writes[0] != "/host/SNAPSHOTS/BASE.pin" {
		t.Fatalf("writes = %v", f.writes)
	}
	if !f.has("gosnap: pin BASE ok") {
		t.Fatalf("missing pin marker: %v", f.logs)
	}
	// A pinned row's button reads Unpin after the re-list picks the
	// marker up — model the share: the pin file now lists and exists.
	f.items = append(f.items, dirItem("BASE.pin", 4))
	f.exists["/host/SNAPSHOTS/BASE.pin"] = true
	v.refresh()
	if v.pinBtn.Label != "Unpin" {
		t.Fatalf("pinned row's label = %q", v.pinBtn.Label)
	}
	// Unpin deletes the marker — again one gesture.
	v.togglePin()
	if len(f.deleted) != 1 || f.deleted[0] != "/host/SNAPSHOTS/BASE.pin" {
		t.Fatalf("deleted = %v", f.deleted)
	}
	if !f.has("gosnap: unpin BASE ok") {
		t.Fatalf("missing unpin marker: %v", f.logs)
	}
}

func TestViewPruneArmsOnTheComputedVictim(t *testing.T) {
	f := &fakeEnv{
		items: []vi.DirItem{
			dirItemCT("OLD.vzsave", 64, 10),
			dirItemCT("NEW.vzsave", 64, 20),
		},
		exists: map[string]bool{"/host/SNAPSHOTS/OLD.vzsave": true},
	}
	f.install(t)
	// Model the delete against the listing so the post-prune refresh
	// sees the file gone.
	fileDelete = func(p string) int64 {
		f.deleted = append(f.deleted, p)
		var kept []vi.DirItem
		for _, it := range f.items {
			if snapsDir+"/"+it.Name != p {
				kept = append(kept, it)
			}
		}
		f.items = kept
		return 0
	}
	v := newTestView()
	v.refresh()
	// First gesture arms on the victim the host's cap would evict — the
	// selected row is irrelevant to prune.
	v.sel, v.list.Sel = 1, 1
	v.activatePrune()
	if v.armed != "prune" || v.armedName != "OLD" {
		t.Fatalf("armed=%q %q", v.armed, v.armedName)
	}
	if len(f.deleted) != 0 {
		t.Fatalf("prune ran on the first gesture: %v", f.deleted)
	}
	if !f.has("gosnap: consent prune OLD") {
		t.Fatalf("missing consent marker: %v", f.logs)
	}
	if v.pruneBtn.Label != "Confirm?" {
		t.Fatalf("armed label = %q", v.pruneBtn.Label)
	}
	// Second gesture deletes exactly the victim.
	v.activatePrune()
	if len(f.deleted) != 1 || f.deleted[0] != "/host/SNAPSHOTS/OLD.vzsave" {
		t.Fatalf("deleted = %v", f.deleted)
	}
	if !f.has("gosnap: prune OLD ok") {
		t.Fatalf("missing prune marker: %v", f.logs)
	}
	if len(v.rows) != 1 || v.rows[0].name != "NEW" {
		t.Fatalf("rows = %v", v.rows)
	}
}

func TestViewPruneDisabledWithNoVictim(t *testing.T) {
	// A store of only pins + crash slots has nothing prune may take.
	f := &fakeEnv{items: []vi.DirItem{
		dirItem("KEEP.vzsave", 64),
		dirItem("KEEP.pin", 4),
		dirItem("CRASH0.vzsave", 32),
	}}
	f.install(t)
	v := newTestView()
	v.refresh()
	if !v.pruneBtn.Disabled {
		t.Fatal("prune enabled with no unpinned named row")
	}
	v.activatePrune()
	if v.armed != "" || len(f.deleted) != 0 {
		t.Fatal("prune armed or deleted with no victim")
	}
	if !f.has("gosnap: snap name=KEEP bytes=64 pin=1") {
		t.Fatalf("pin flag missing from the ls markers: %v", f.logs)
	}
}

func TestViewPollRepaintsOnlyOnChange(t *testing.T) {
	f := &fakeEnv{items: []vi.DirItem{dirItem("A.vzsave", 1)}}
	f.install(t)
	v := newTestView()
	v.refresh()
	logs := len(f.logs)
	// Same listing: no repaint, no new marker lines.
	if v.poll(v.pollAt + pollNanos) {
		t.Fatal("unchanged listing repainted")
	}
	if len(f.logs) != logs {
		t.Fatalf("unchanged listing printed: %v", f.logs[logs:])
	}
	// A snapshot minted out of band arrives on the next poll.
	f.items = append(f.items, dirItem("MENU-1.vzsave", 9))
	if !v.poll(v.pollAt + pollNanos) {
		t.Fatal("changed listing did not repaint")
	}
	if len(v.rows) != 2 || v.rows[1].name != "MENU-1" {
		t.Fatalf("rows = %v", v.rows)
	}
	if !f.has("gosnap: snap name=MENU-1 bytes=9") {
		t.Fatalf("missing row marker: %v", f.logs)
	}
	// Under the cadence nothing polls early.
	if v.poll(v.pollAt + pollNanos/2) {
		t.Fatal("poll fired early")
	}
}

func TestViewPollChangeDisarms(t *testing.T) {
	f := &fakeEnv{items: []vi.DirItem{dirItem("BASE.vzsave", 64)}}
	f.install(t)
	v := newTestView()
	v.refresh()
	v.sel, v.list.Sel = 0, 0
	v.activate("rm")
	f.items = append(f.items, dirItem("OTHER.vzsave", 1))
	v.poll(v.pollAt + pollNanos)
	if v.armed != "" {
		t.Fatal("a listing change left the arm live")
	}
	if len(f.deleted) != 0 {
		t.Fatal("a verb ran")
	}
}

// --- M195c (#3308): the appver-1 state envelope -------------------------

// The envelope round-trips the face's durable state — the in-progress
// field text and the selected row's NAME (never its index, so a
// re-ordered share cannot mispoint the restore). The arm never rides:
// a confirm gesture is re-armed by the person, not restored.
func TestSnapEnvelopeRoundTrip(t *testing.T) {
	f := &fakeEnv{items: []vi.DirItem{
		dirItem("FIRST.vzsave", 32),
		dirItem("SECOND.vzsave", 64),
	}}
	f.install(t)
	v := newTestView()
	v.refresh()
	v.sel, v.list.Sel = 1, 1
	v.field.SetValue("draft-name")
	st, ok := decodeSnapState(v.encode())
	if !ok || st.field != "draft-name" || st.sel != "SECOND" {
		t.Fatalf("round-trip = %+v ok=%v", st, ok)
	}
	// An armed confirm does not enter the blob.
	v.armed, v.armedName = "rm", "SECOND"
	st, ok = decodeSnapState(v.encode())
	if !ok || st.sel != "SECOND" {
		t.Fatalf("armed round-trip = %+v ok=%v", st, ok)
	}
	// Fresh view: both pieces empty, still well-formed.
	v2 := newTestView()
	st, ok = decodeSnapState(v2.encode())
	if !ok || st.field != "" || st.sel != "" {
		t.Fatalf("fresh round-trip = %+v ok=%v", st, ok)
	}
}

// Decode is total: a bad length, foreign fmtVer, nonzero flags,
// truncated or trailing bytes, and non-printable text all refuse the
// whole blob.
func TestSnapEnvelopeRefusals(t *testing.T) {
	good := []byte{stateFmtVer, 0, 3, 'w', 'i', 'n', 4, 'B', 'A', 'S', 'E'}
	if _, ok := decodeSnapState(good); !ok {
		t.Fatalf("well-formed blob refused: %x", good)
	}
	for i, b := range [][]byte{
		{},                                    // empty
		{stateFmtVer, 0, 0},                   // short: no sel section
		{2, 0, 0, 0},                          // foreign fmtver
		{stateFmtVer, 1, 0, 0},                // nonzero flags
		{stateFmtVer, 0, 3, 'w', 'i'},         // truncated field
		{stateFmtVer, 0, 3, 'w', 'i', 'n'},    // missing sel length
		{stateFmtVer, 0, 0, 4, 'B', 'A', 'S'}, // truncated sel
		{stateFmtVer, 0, 0, 0, 0},             // trailing byte
		{stateFmtVer, 0, 1, 0x1b, 0},          // non-printable field
		{stateFmtVer, 0, 0, 1, 0x7f},          // non-printable sel
		{stateFmtVer, 0, 1, 'x', 1, 'y', 9},   // trailing garbage
	} {
		if _, ok := decodeSnapState(b); ok {
			t.Fatalf("blob %d decoded: %x", i, b)
		}
	}
}

// The apply side: a restored blob re-selects its row by name wherever
// the fresh listing sorted it and re-fills the field; a stored name the
// share has since dropped degrades to `gone`, and every refusal names
// itself. No blob waiting is a silent fresh start.
func TestApplyRestored(t *testing.T) {
	f := &fakeEnv{items: []vi.DirItem{
		dirItem("FIRST.vzsave", 32),
		dirItem("SECOND.vzsave", 64),
	}}
	f.install(t)
	// No stored envelope: nothing to report, nothing applied.
	v := newTestView()
	if line := v.applyRestored(snapState{}, false); line != "" {
		t.Fatalf("no-restore marker = %q", line)
	}
	// SECOND sorted second; the blob names it, not index 1.
	v = newTestView()
	v.ta = &tabapp.TabApp{Restored: true, RestoredVersion: stateAppVer}
	v.refresh()
	line := v.applyRestored(snapState{field: "draft", sel: "SECOND"}, true)
	if v.sel != 1 || v.field.Value() != "draft" {
		t.Fatalf("applied = sel %d field %q", v.sel, v.field.Value())
	}
	if line != "gosnap: state restored sel=SECOND field=draft" {
		t.Fatalf("restored marker = %q", line)
	}
	if v.armed != "" {
		t.Fatalf("a restore left an arm live: %q", v.armed)
	}
	// A stored name the share dropped reports gone, no selection.
	v2 := newTestView()
	v2.ta = &tabapp.TabApp{Restored: true, RestoredVersion: stateAppVer}
	v2.refresh()
	line = v2.applyRestored(snapState{sel: "DELETED"}, true)
	if v2.sel != -1 {
		t.Fatalf("gone sel applied = %d", v2.sel)
	}
	if line != "gosnap: state restored sel=gone field=-" {
		t.Fatalf("gone marker = %q", line)
	}
	// The refusals, each named: foreign appver, torn blob.
	v3 := newTestView()
	v3.ta = &tabapp.TabApp{Restored: true, RestoredVersion: 9}
	if line := v3.applyRestored(snapState{}, true); line != "gosnap: state refused ver=9" {
		t.Fatalf("ver refusal = %q", line)
	}
	v3.ta.RestoredVersion = stateAppVer
	if line := v3.applyRestored(snapState{}, false); line != "gosnap: state refused bad" {
		t.Fatalf("bad refusal = %q", line)
	}
	if v3.sel != -1 || v3.field.Value() != "" {
		t.Fatalf("a refused blob still applied: sel %d field %q", v3.sel, v3.field.Value())
	}
}
