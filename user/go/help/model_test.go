package main

import (
	"strings"
	"testing"

	"virelai/rss/keys"
	"virelai/vi"
)

// testModel builds a fresh 64x23 model and drops the boot markers (the
// host's vi layer is ENOSYS, so docs load as n=0 — the gate's n=2 comes
// from its seeded bundle).
func testModel() model {
	m := newModel(64, 23)
	m.drain()
	return m
}

func TestGridOfUsesKernelTerminalCell(t *testing.T) {
	cols, rows := gridOf(512, 384)
	if cols != 64 || rows != 23 {
		t.Fatalf("gridOf(512,384) = %dx%d, want 64x23 for the default 8x16 cell",
			cols, rows)
	}
}

func key(m *model, ev keys.Event) []string {
	m.handleKey(ev)
	return m.drain()
}

func runeKey(r rune) keys.Event { return keys.Event{Key: keys.KeyRune, Rune: r} }

var (
	kEnter     = keys.Event{Key: keys.KeyEnter}
	kEsc       = keys.Event{Key: keys.KeyEsc}
	kBackspace = keys.Event{Key: keys.KeyBackspace}
	kDown      = keys.Event{Key: keys.KeyDown}
	kUp        = keys.Event{Key: keys.KeyUp}
	kLeft      = keys.Event{Key: keys.KeyLeft}
	kRight     = keys.Event{Key: keys.KeyRight}
)

// hasLine reports whether the drained batch contains an exact marker.
func hasLine(lines []string, want string) bool {
	for _, l := range lines {
		if l == want {
			return true
		}
	}
	return false
}

func TestBootMarkers(t *testing.T) {
	m := newModel(64, 23)
	got := m.drain()
	// n=62 is the catalog row count (M115d #2463 adds `suspend`; M186d #3181 adds `say`;
	// card d #3441 adds the QJS.BIN externals row); a catalog change trips this
	// AND the class-B gate deliberately — both are the drift tripwire for shlib.HelpRows.
	if !hasLine(got, "gohelp: catalog n=62") {
		t.Errorf("boot markers missing catalog count: %v", got)
	}
	if !hasLine(got, "gohelp: docs n=0") {
		t.Errorf("boot markers missing docs count (host has no bundle): %v", got)
	}
	if !hasLine(got, "gohelp: focus alias group=shell") {
		t.Errorf("boot markers missing initial focus: %v", got)
	}
}

func TestGroupJumps(t *testing.T) {
	m := testModel()
	if got := key(&m, runeKey('l')); !hasLine(got, "gohelp: focus . group=files") {
		t.Errorf("l did not jump to the files group head: %v", got)
	}
	if got := key(&m, runeKey('h')); !hasLine(got, "gohelp: focus alias group=shell") {
		t.Errorf("h did not jump back to the shell group head: %v", got)
	}
}

func TestBrowseScrollKeepsFocusedRowVisible(t *testing.T) {
	m := testModel()
	for i := 0; i < 25; i++ {
		key(&m, kDown)
	}
	if m.sel != 25 || m.listTop <= 0 ||
		m.sel < m.listTop || m.sel >= m.listTop+m.bodyRows() {
		t.Fatalf("selection=%d listTop=%d bodyRows=%d; selection is not visible",
			m.sel, m.listTop, m.bodyRows())
	}
	lines := m.renderLines()
	screenRow := 2 + m.sel - m.listTop
	if screenRow >= len(lines) {
		t.Fatalf("focused row screen line %d exceeds rendered frame of %d lines",
			screenRow, len(lines))
	}
	if !strings.Contains(lines[screenRow], "> "+m.visible()[m.sel].Name) {
		t.Fatalf("focused row %q is not highlighted in line %d: %q",
			m.visible()[m.sel].Name, screenRow, lines[screenRow])
	}
}

func TestBrowseClickSelectsScrolledRow(t *testing.T) {
	m := testModel()
	m.listTop = 3
	want := m.visible()[m.listTop]
	m.handleClick(1, 3) // first list row follows title and pane header
	if m.sel != m.listTop || !hasLine(m.drain(),
		"gohelp: focus "+want.Name+" group="+want.Group) {
		t.Fatalf("first visible row selected index %d, want 3", m.sel)
	}
}

func TestBrowseResizeKeepsFocusedRowVisible(t *testing.T) {
	m := testModel()
	for i := 0; i < 26; i++ {
		key(&m, kDown)
	}
	m.setSize(64, 16)
	if m.sel != 26 || m.sel < m.listTop ||
		m.sel >= m.listTop+m.bodyRows() {
		t.Fatalf("after resize selection=%d listTop=%d bodyRows=%d",
			m.sel, m.listTop, m.bodyRows())
	}
	lines := m.renderLines()
	screenRow := 2 + m.sel - m.listTop
	if screenRow >= len(lines) {
		t.Fatalf("focused row screen line %d exceeds rendered frame of %d lines",
			screenRow, len(lines))
	}
	if !strings.Contains(lines[screenRow], "> "+m.visible()[m.sel].Name) {
		t.Fatalf("focused row not highlighted after resize: line %d = %q",
			screenRow, lines[screenRow])
	}
}

func TestFilterFlow(t *testing.T) {
	m := testModel()
	if got := key(&m, runeKey('/')); !hasLine(got, "gohelp: filter on") {
		t.Fatalf("/ did not open the filter: %v", got)
	}
	key(&m, runeKey('e'))
	got := key(&m, runeKey('c'))
	// #3767: the filter is full-text — `ec` leads with its three name hits
	// (echo, secrets, exec) then the rows whose help text carries the
	// substring (SECONDS, "checked", "record", "ICMP echo", …).
	if !hasLine(got, "gohelp: filter ec n=22") {
		t.Errorf("filter ec marker missing (want n=22): %v", got)
	}
	vis := m.visible()
	var names []string
	for _, r := range vis {
		names = append(names, r.Name)
	}
	if strings.Join(names[:3], ",") != "echo,secrets,exec" {
		t.Errorf("filter ec name hits lead with %v, want echo,secrets,exec", names[:3])
	}
	got = key(&m, kEsc)
	if !hasLine(got, "gohelp: filter cleared n=62") {
		t.Errorf("escape did not clear the filter: %v", got)
	}
	if m.filter != "" || m.filtering {
		t.Errorf("filter state after escape: filter=%q filtering=%v", m.filter, m.filtering)
	}
}

// TestFilterFullText is the #3767 leg's host half: a query no NAME contains
// still matches — every row comes from usage/blurb/notes, pinned exactly so
// a regression to name-only matching fails as n=0.
func TestFilterFullText(t *testing.T) {
	m := testModel()
	key(&m, runeKey('/'))
	var got []string
	for _, r := range "directory" {
		got = key(&m, runeKey(r))
	}
	vis := m.visible()
	var names []string
	for _, r := range vis {
		names = append(names, r.Name)
	}
	if strings.Join(names, ",") != "cd,dirs,popd,pushd,pwd" {
		t.Errorf("filter directory matched %v, want cd,dirs,popd,pushd,pwd", names)
	}
	if !hasLine(got, "gohelp: filter directory n=5") {
		t.Errorf("filter directory marker missing (want n=5): %v", got)
	}
}

// TestDeepLink is the #3767 argv hand-off's host half: an exact (case-folded)
// topic name opens its detail page and moves the cursor onto the row; a name
// the catalog does not have refuses on the status line and stays on the
// index — never a crash, never a pretend detail.
func TestDeepLink(t *testing.T) {
	m := testModel()
	m.deepLink("ECHO") // case-folded, like an exec'd hand-off spelling
	got := m.drain()
	if !hasLine(got, "gohelp: deeplink echo") ||
		!hasLine(got, "gohelp: detail echo usage=echo [ARG...]") {
		t.Errorf("deep-link hit markers: %v", got)
	}
	if m.mode != modeDetail || m.detail.Name != "echo" || !m.detailBatch {
		t.Errorf("state after deep-link hit: mode=%v detail=%q batch=%v",
			m.mode, m.detail.Name, m.detailBatch)
	}
	if m.visible()[m.sel].Name != "echo" {
		t.Errorf("cursor after deep-link hit: %q, want echo",
			m.visible()[m.sel].Name)
	}

	m2 := testModel()
	m2.deepLink("nosuchtopic")
	got = m2.drain()
	if !hasLine(got, "gohelp: deeplink miss nosuchtopic") {
		t.Errorf("deep-link miss marker: %v", got)
	}
	for _, l := range got {
		if strings.HasPrefix(l, "gohelp: detail ") {
			t.Errorf("miss emitted a detail marker: %v", got)
		}
	}
	if m2.mode != modeBrowse || m2.status != "no such topic: nosuchtopic" {
		t.Errorf("state after deep-link miss: mode=%v status=%q", m2.mode, m2.status)
	}
}

func TestDetailFlow(t *testing.T) {
	m := testModel()
	key(&m, runeKey('j')) // M98b's `alias` now leads the shell group
	key(&m, runeKey('j')) // date now sits between clear and echo
	if got := key(&m, runeKey('j')); !hasLine(got, "gohelp: focus echo group=shell") {
		t.Fatalf("second j did not focus echo: %v", got)
	}
	got := key(&m, kEnter)
	if !hasLine(got, "gohelp: detail echo usage=echo [ARG...]") {
		t.Errorf("detail marker missing: %v", got)
	}
	if m.mode != modeDetail || !m.detailBatch {
		t.Errorf("state after enter: mode=%v detailBatch=%v", m.mode, m.detailBatch)
	}
	got = key(&m, kBackspace)
	if !hasLine(got, "gohelp: browse") || m.mode != modeBrowse {
		t.Errorf("backspace did not return to browse: %v (mode=%v)", got, m.mode)
	}
}

func TestDocsFlowErrorsHonestly(t *testing.T) {
	m := testModel()
	// Inject the list the seeded gate would produce (host reads are ENOSYS).
	var e vi.DirEntry
	copy(e.Name[:], "GUIDE.TXT")
	e.Size = 26
	m.docs[0] = e
	m.docN = 1

	if got := key(&m, runeKey('d')); !hasLine(got, "gohelp: docs open") ||
		!hasLine(got, "gohelp: docfocus GUIDE.TXT") {
		t.Errorf("d did not open the docs section: %v", got)
	}
	if m.mode != modeDocs {
		t.Fatalf("mode after d: %v", m.mode)
	}
	got := key(&m, kEnter)
	// On the host the read fails (no guest fs): the error marker must be
	// the honest shape and the mode must stay on the list.
	if len(got) != 1 || !strings.HasPrefix(got[0], "gohelp: doc error GUIDE.TXT rc=") {
		t.Errorf("doc read error markers: %v", got)
	}
	if m.mode != modeDocs {
		t.Errorf("mode after failed read: %v", m.mode)
	}
	// Back to browse.
	if got = key(&m, kBackspace); !hasLine(got, "gohelp: browse") {
		t.Errorf("backspace did not leave the docs list: %v", got)
	}
}

func TestQuitOnlyInBrowse(t *testing.T) {
	m := testModel()
	key(&m, runeKey('d'))
	key(&m, runeKey('q'))
	if m.quit {
		t.Errorf("q quit from the docs section (quit is browse-only)")
	}
	key(&m, kBackspace)
	key(&m, runeKey('q'))
	if !m.quit {
		t.Errorf("q did not quit from browse")
	}
}

func TestKeyLabel(t *testing.T) {
	cases := map[keys.Event]string{
		kEnter: "return", kEsc: "escape", kBackspace: "backspace",
		kDown: "down", kUp: "up", kLeft: "left", kRight: "right",
		runeKey('/'): "/", runeKey('d'): "d",
	}
	for ev, want := range cases {
		if got := keyLabel(ev); got != want {
			t.Errorf("keyLabel(%v) = %q, want %q", ev, got, want)
		}
	}
}
