package main

// M192g (#3252): the quick-open palette's adoption legs — ctrl+p summons
// over buffers + the envelope-seeded MRU + the bounded subtree walk,
// the incremental match re-filters per keystroke, Enter opens through
// the ring, Escape cancels, and M107c's prev-diagnostic rides ctrl+comma
// after the chord hand-off.

import (
	"testing"

	"virelai/vi"
)

// qoDirFixture substitutes the walk's directory seam with a fixed
// listing per directory, the errRead/bufRead pattern.
func qoDirFixture(t *testing.T, listing map[string][]vi.DirEntry) {
	t.Helper()
	old := qoDirList
	qoDirList = func(path string, buf []vi.DirEntry) (int, int64) {
		ents := listing[path]
		n := len(ents)
		if n > len(buf) {
			n = len(buf)
		}
		copy(buf, ents[:n])
		return n, int64(n)
	}
	t.Cleanup(func() { qoDirList = old })
}

func qoEntry(name string, isDir bool) vi.DirEntry {
	var e vi.DirEntry
	copy(e.Name[:], name)
	if isDir {
		e.IsDir = 1
	}
	return e
}

func TestCtrlPSummonsQuickOpen(t *testing.T) {
	qoDirFixture(t, map[string][]vi.DirEntry{
		"/host/EDIT": {qoEntry("SEED.TXT", false), qoEntry("OTHER.TXT", false)},
	})
	e := &editor{doc: doc{path: "/host/EDIT/SEED.TXT"}}
	e.handleKey(usDown(0x13, modCtrl, 'p'))
	if e.mode != modeQuickOpen {
		t.Fatalf("ctrl+p did not summon the palette: mode=%d", e.mode)
	}
	// The boot buffer plus the one extra subtree file — deduped.
	if len(e.qo) != 2 {
		t.Fatalf("candidates=%d want 2", len(e.qo))
	}
	if e.qo[0].tag != "buf" || e.qo[1].tag != "file" {
		t.Fatalf("tags %q/%q — the ring leads, the walk follows", e.qo[0].tag, e.qo[1].tag)
	}
	if len(e.qoMatch) != 2 {
		t.Fatalf("empty query must list every candidate: %d", len(e.qoMatch))
	}
}

func TestQuickOpenFilterIsIncremental(t *testing.T) {
	qoDirFixture(t, map[string][]vi.DirEntry{
		"/host/EDIT": {qoEntry("SEED.TXT", false), qoEntry("OTHER.TXT", false), qoEntry("SEXT.TXT", false)},
	})
	e := &editor{doc: doc{path: "/host/EDIT/SEED.TXT"}}
	e.handleKey(usDown(0x13, modCtrl, 'p'))
	// "se" matches SEED.TXT + SEXT.TXT, not OTHER.TXT — case-insensitive.
	e.handleKey(usDown(0x16, 0, 's'))
	e.handleKey(usDown(0x08, 0, 'e'))
	if len(e.qoMatch) != 2 {
		t.Fatalf("'se' matched %d want 2", len(e.qoMatch))
	}
	// "see" narrows to SEED.TXT alone.
	e.handleKey(usDown(0x08, 0, 'e'))
	if len(e.qoMatch) != 1 || baseOf(e.qo[e.qoMatch[0]].path) != "SEED.TXT" {
		t.Fatalf("'see' must keep only SEED.TXT: %v", e.qoMatch)
	}
	// Backspace widens again — the match re-runs per keystroke.
	e.handleKey(usDown(0x2a, 0, 0x08))
	if len(e.qoMatch) != 2 {
		t.Fatalf("backspace re-widened to %d want 2", len(e.qoMatch))
	}
}

func TestQuickOpenNoHitNamesZero(t *testing.T) {
	qoDirFixture(t, map[string][]vi.DirEntry{
		"/host/EDIT": {qoEntry("SEED.TXT", false)},
	})
	e := &editor{doc: doc{path: "/host/EDIT/SEED.TXT"}}
	e.handleKey(usDown(0x13, modCtrl, 'p'))
	for _, r := range "zzz" {
		e.handleKey(usDown(0, 0, uint32(r)))
	}
	if len(e.qoMatch) != 0 {
		t.Fatalf("'zzz' must hit nothing: %v", e.qoMatch)
	}
	// Enter on the empty list is the named refusal, not a crash.
	e.handleKey(usDown(0x28, 0, '\r'))
	if e.mode != modeQuickOpen {
		t.Fatalf("the no-hit Enter must keep the palette: mode=%d", e.mode)
	}
}

func TestQuickOpenEnterOpensThroughTheRing(t *testing.T) {
	read := ""
	old := bufRead
	bufRead = func(p string, max int) ([]byte, int64) {
		read = p
		return []byte("x"), 1
	}
	t.Cleanup(func() { bufRead = old })
	qoDirFixture(t, map[string][]vi.DirEntry{
		"/host/EDIT": {qoEntry("SEED.TXT", false), qoEntry("OTHER.TXT", false)},
	})
	e := &editor{doc: doc{path: "/host/EDIT/SEED.TXT"}}
	e.docs = []doc{e.doc}
	e.handleKey(usDown(0x13, modCtrl, 'p'))
	// "ot" matches only OTHER.TXT; Enter opens it in a NEW buffer.
	e.handleKey(usDown(0x12, 0, 'o'))
	e.handleKey(usDown(0x17, 0, 't'))
	e.handleKey(usDown(0x28, 0, '\r'))
	if e.mode != modeEdit {
		t.Fatalf("the pick must hand the keyboard back: mode=%d", e.mode)
	}
	if e.path != "/host/EDIT/OTHER.TXT" || read != e.path {
		t.Fatalf("pick landed path=%q read=%q", e.path, read)
	}
	if len(e.docList()) != 2 || e.active != 1 {
		t.Fatalf("the pick must take a ring slot: docs=%d active=%d", len(e.docList()), e.active)
	}
	if len(e.recents) == 0 || e.recents[0] != "/host/EDIT/OTHER.TXT" {
		t.Fatalf("the pick must feed the MRU: %v", e.recents)
	}
}

func TestQuickOpenBufferHitSwitchesNotReopens(t *testing.T) {
	qoDirFixture(t, map[string][]vi.DirEntry{
		"/host/EDIT": {qoEntry("SEED.TXT", false), qoEntry("OTHER.TXT", false)},
	})
	e := &editor{doc: doc{path: "/host/EDIT/SEED.TXT"}}
	e.docs = []doc{e.doc, {path: "/host/EDIT/OTHER.TXT"}}
	e.active = 1
	e.doc = e.docs[1]
	e.handleKey(usDown(0x13, modCtrl, 'p'))
	// The empty query lists both buffers; sel 0 is SEED.TXT.
	e.handleKey(usDown(0x28, 0, '\r'))
	if len(e.docList()) != 2 || e.active != 0 || e.path != "/host/EDIT/SEED.TXT" {
		t.Fatalf("a buffer hit switches, never re-opens: docs=%d active=%d path=%q",
			len(e.docList()), e.active, e.path)
	}
}

func TestQuickOpenEscapeCancels(t *testing.T) {
	qoDirFixture(t, map[string][]vi.DirEntry{"/host/EDIT": {qoEntry("SEED.TXT", false)}})
	e := &editor{doc: doc{path: "/host/EDIT/SEED.TXT"}}
	e.handleKey(usDown(0x13, modCtrl, 'p'))
	e.handleKey(usDown(0x29, 0, 0x1b))
	if e.mode != modeEdit || len(e.bar) != 0 {
		t.Fatalf("escape must cancel to the document: mode=%d bar=%q", e.mode, e.bar)
	}
}

func TestQuickOpenRecentOnlyHit(t *testing.T) {
	// A recent outside the walked subtree still matches — the MRU is the
	// pool, not the directory.
	qoDirFixture(t, map[string][]vi.DirEntry{
		"/host/EDIT": {qoEntry("SEED.TXT", false)},
	})
	e := &editor{doc: doc{path: "/host/EDIT/SEED.TXT"}}
	e.docs = []doc{e.doc}
	e.recents = []string{"/host/NOTES/OLD.TXT"}
	e.handleKey(usDown(0x13, modCtrl, 'p'))
	e.handleKey(usDown(0x12, 0, 'o'))
	e.handleKey(usDown(0x0f, 0, 'l'))
	e.handleKey(usDown(0x07, 0, 'd'))
	if len(e.qoMatch) != 1 || e.qo[e.qoMatch[0]].path != "/host/NOTES/OLD.TXT" {
		t.Fatalf("the MRU hit must list: %v", e.qoMatch)
	}
	if e.qo[e.qoMatch[0]].tag != "recent" {
		t.Fatalf("tag %q want recent", e.qo[e.qoMatch[0]].tag)
	}
}

func TestQuickOpenWalkBoundsNamed(t *testing.T) {
	// A directory deeper than qoMaxDepth is never listed; the walk names
	// the bound that stopped it.
	qoDirFixture(t, map[string][]vi.DirEntry{
		"/host/EDIT": {qoEntry("SEED.TXT", false), qoEntry("D1", true)},
		"/host/EDIT/D1": {qoEntry("D2", true)},
		"/host/EDIT/D1/D2": {qoEntry("D3", true)},
		"/host/EDIT/D1/D2/D3": {qoEntry("D4", true)},
		"/host/EDIT/D1/D2/D3/D4": {qoEntry("D5", true)},
	})
	e := &editor{doc: doc{path: "/host/EDIT/SEED.TXT"}}
	e.handleKey(usDown(0x13, modCtrl, 'p'))
	if e.qoTrunc != "depth" {
		t.Fatalf("trunc %q want depth", e.qoTrunc)
	}
	for _, it := range e.qo {
		if it.path == "/host/EDIT/D1/D2/D3/D4/D5" {
			t.Fatal("the beyond-depth directory was walked")
		}
	}
}

func TestRestoreSeedsTheRecents(t *testing.T) {
	// The envelope's record paths are the MRU's seed — active first.
	e := &editor{}
	e.pend = []*editState{
		{path: "/host/A.TXT"},
		{path: "/host/B.TXT"},
	}
	old := bufRead
	bufRead = func(p string, max int) ([]byte, int64) { return []byte("x"), 1 }
	t.Cleanup(func() { bufRead = old })
	e.docs = []doc{e.doc}
	e.applyRestored()
	if len(e.recents) != 2 || e.recents[0] != "/host/A.TXT" || e.recents[1] != "/host/B.TXT" {
		t.Fatalf("envelope seeding: %v", e.recents)
	}
}

func TestClosedBufferStaysRecent(t *testing.T) {
	e := &editor{doc: doc{path: "/host/A.TXT"}}
	e.docs = []doc{e.doc, {path: "/host/B.TXT"}}
	e.active = 1
	e.doc = e.docs[1]
	e.closeDoc()
	if len(e.recents) == 0 || e.recents[0] != "/host/B.TXT" {
		t.Fatalf("the closed buffer must stay recent: %v", e.recents)
	}
}

func TestCommaStepsPrevDiagnostic(t *testing.T) {
	// The M192g re-home: ctrl+comma is prev-error now that ctrl+p is the
	// palette's. With no list loaded and no diagnostics file, the step
	// answers the `none` refusal — the binding reached stepDiag.
	e := &editor{doc: doc{path: "/host/EDIT/SEED.TXT"}}
	old := errExists
	errExists = func(string) bool { return false }
	t.Cleanup(func() { errExists = old })
	e.handleKey(usDown(usageComma, modCtrl, ','))
	if e.mode == modeQuickOpen {
		t.Fatal("ctrl+comma must not summon the palette")
	}
	// And ctrl+p is the palette's even with a diagnostics list loaded —
	// the panel's own prev is the comma spelling now.
	e.diags = []diagRow{{file: "/host/EDIT/SEED.TXT", line: 1, col: 1}}
	e.diagsSel = 1
	e.handleKey(usDown(0x13, modCtrl, 'p'))
	if e.mode != modeQuickOpen {
		t.Fatalf("ctrl+p must summon the palette over a live list: mode=%d", e.mode)
	}
	if e.diagsSel != 1 {
		t.Fatalf("ctrl+p must not step the selection: sel=%d", e.diagsSel)
	}
}
