// The M74c (issue #1646) help browser's state machine — deliberately pure:
// no Bubble Tea import, no guest syscall wrappers beyond the vi surface the
// host already stubs, so `go test` on the host pins the marker shapes, the
// filter's match counts and the modal flows. tea.go adapts it to the
// tea.Model contract; render.go draws it; main.go owns the bound-tty loop.
package main

import (
	"strings"

	"virelai/rss/keys"
	"virelai/shlib"
	"virelai/vi"
)

// docsDir is the in-guest docs bundle the `d` section browses — the share
// root's docs/ directory, the /host/docs path convention the tabwm nav
// tests already pin (user/src/tabwm.zig). Absent on a bare share: the
// section then honestly shows an empty list.
const docsDir = "/host/docs"

// maxDocBytes is the page the browser reads whole. A larger file is shown
// truncated and says so — it never pretends to have read the rest.
const maxDocBytes = 64 * 1024

// isDocName keeps the bundle to plain text/markdown pages.
func isDocName(name string) bool {
	l := strings.ToLower(name)
	return strings.HasSuffix(l, ".txt") || strings.HasSuffix(l, ".md")
}

// viewMode is the browser's modal state: browse is home; detail, docs and
// the doc reader each own the keyboard until they go back.
type viewMode uint8

const (
	modeBrowse  viewMode = iota
	modeDetail           // full-page detail of one catalog row
	modeDocs             // the /host/docs file list
	modeDocView          // one doc's text
)

type model struct {
	cmds     []shlib.HelpRow // the whole catalog, section-ordered
	sections []string        // display order (shlib.HelpSections)
	sel      int             // cursor into visible()

	filter    string // `/`-to-filter, matched case-folded against names
	filtering bool   // the filter line owns the keyboard

	mode   viewMode
	detail shlib.HelpRow

	docs    [vi.MaxDirEntries]vi.DirEntry
	docN    int
	docSel  int
	docText string
	docBig  bool // the page hit maxDocBytes: say truncated, don't lie
	docOf   string

	status string
	cols   int
	rows   int

	quit        bool
	detailBatch bool     // a detail opened this batch: main prints the settle marker
	pending     []string // serial markers, flushed by main AFTER the frame paints
}

// newModel takes the catalog and the first docs listing on a cols x rows
// grid; its boot markers ride pending for main to flush after the first
// paint, so even `catalog n=` describes a screen that exists.
func newModel(cols, rows int) model {
	m := model{cmds: shlib.HelpRows(), sections: shlib.HelpSections()}
	m.setSize(cols, rows)
	m.loadDocs()
	m.emit(markerCatalog + vi.Itoa64(int64(len(m.cmds))))
	m.emit(markerDocs + vi.Itoa64(int64(m.docN)))
	m.emitFocus()
	m.status = "ready"
	return m
}

// emit queues one serial marker; drain hands the batch to main.
func (m *model) emit(line string) { m.pending = append(m.pending, line) }

func (m *model) drain() []string {
	out := m.pending
	m.pending = nil
	return out
}

// setSize adopts a new grid after a window resize (M73j #1636: the window's
// px-derived grid — the gate runs at the native rect).
func (m *model) setSize(cols, rows int) {
	if cols < 16 {
		cols = 16
	}
	if rows < 8 {
		rows = 8
	}
	m.cols, m.rows = cols, rows
}

// bodyRows is the pane height: header + pane headers + status + hints.
func (m *model) bodyRows() int {
	b := m.rows - 4
	if b < 1 {
		return 1
	}
	return b
}

// loadDocs lists /host/docs, keeping plain text/markdown files sorted by
// name (GUIDE.TXT before NOTES.MD — the gate's row 0 is deterministic).
// An absent bundle is n=0, not an error.
func (m *model) loadDocs() {
	entries := [vi.MaxDirEntries]vi.DirEntry{}
	n, rc := vi.DirList(docsDir, entries[:])
	m.docN = 0
	if rc < 0 {
		return
	}
	kept := entries[:n]
	// Sort by name, insertion order irrelevant: a simple O(n²) selection
	// over at most MaxDirEntries keeps this dependency-free.
	for i := 0; i < len(kept); i++ {
		for j := i + 1; j < len(kept); j++ {
			if kept[j].NameString() < kept[i].NameString() {
				kept[i], kept[j] = kept[j], kept[i]
			}
		}
	}
	for _, e := range kept {
		if e.Dir() || !isDocName(e.NameString()) {
			continue
		}
		m.docs[m.docN] = e
		m.docN++
	}
}

// visible is the flat command list under the current filter: every catalog
// row whose NAME contains the filter (case-folded), section order intact.
func (m model) visible() []shlib.HelpRow {
	if m.filter == "" {
		return m.cmds
	}
	q := strings.ToLower(m.filter)
	out := make([]shlib.HelpRow, 0, len(m.cmds))
	for _, r := range m.cmds {
		if strings.Contains(strings.ToLower(r.Name), q) {
			out = append(out, r)
		}
	}
	return out
}

func (m *model) clampSel() {
	vis := m.visible()
	if m.sel < 0 {
		m.sel = 0
	}
	if m.sel >= len(vis) {
		m.sel = len(vis) - 1
	}
	if m.sel < 0 {
		m.sel = 0
	}
}

// emitFocus queues the focus marker for the selected row — the exact shape
// `gohelp: focus echo group=shell` the gate sequences on — and mirrors it
// into the status line.
func (m *model) emitFocus() {
	vis := m.visible()
	if len(vis) == 0 {
		m.emit(markerFocus + "none")
		m.status = "no matches"
		return
	}
	m.clampSel()
	r := vis[m.sel]
	m.emit(markerFocus + r.Name + " group=" + r.Group)
	m.status = r.Name + " · " + r.Group
}

// jumpGroup moves the cursor to the head of the neighbouring group — the
// "group list" face of the card: ←/→ hop sections without walking 41 rows.
// Left lands on the PREVIOUS group's first row (or the current group's head
// when inside one), right on the next group's first row; the ends stay put.
func (m *model) jumpGroup(dir int) {
	vis := m.visible()
	if len(vis) == 0 {
		return
	}
	g := vis[m.sel].Group
	head := m.sel
	for head > 0 && vis[head-1].Group == g {
		head--
	}
	if dir < 0 {
		if head == 0 {
			return // already at the very first row
		}
		pg := vis[head-1].Group
		p := head - 1
		for p > 0 && vis[p-1].Group == pg {
			p--
		}
		m.sel = p
		m.emitFocus()
		return
	}
	tail := m.sel
	for tail+1 < len(vis) && vis[tail+1].Group == g {
		tail++
	}
	if tail+1 < len(vis) {
		m.sel = tail + 1
		m.emitFocus()
	}
}

// openDetail flips to the full-page detail of one row. The detail marker is
// the gate's screenshot barrier, so it carries the usage line verbatim.
func (m *model) openDetail(r shlib.HelpRow) {
	m.detail = r
	m.mode = modeDetail
	m.detailBatch = true
	m.status = "detail: " + r.Name
	m.emit(markerDetail + r.Name + " usage=" + r.Usage)
}

// handleClick consumes the kernel's SGR cell coordinates (1-based over the
// client area): a click in the list selects that row, anywhere else is a
// no-op. Rows are cell-sized, so no scrolling arithmetic is involved.
func (m *model) handleClick(x, y int) {
	row := y - 1 // 0-based screen line
	i := row - 1 // body line i sits at screen line i+1 (0=header)
	if i < 0 {
		return
	}
	switch m.mode {
	case modeBrowse:
		if m.filtering {
			return
		}
		vis := m.visible()
		if i < len(vis) {
			m.sel = i
			m.emitFocus()
		}
	case modeDocs:
		if i < m.docN {
			m.docSel = i
			name := m.docs[m.docSel].NameString()
			m.status = name
			m.emit(markerDocFocus + name)
		}
	}
}

// handleKey runs one decoded key through the state machine. It is the whole
// flow: browse ←→ detail, browse ←→ docs ←→ doc, `/` filter with esc to
// clear, h/l or ←/→ group jumps, j/k or arrows to move, q to quit (browse only).
func (m *model) handleKey(ev keys.Event) {
	if ev.Key == keys.KeyCtrlC {
		m.quit = true
		return
	}
	switch m.mode {
	case modeBrowse:
		m.keyBrowse(ev)
	case modeDetail:
		switch ev.Key {
		case keys.KeyBackspace, keys.KeyEsc, keys.KeyEnter:
			m.mode = modeBrowse
			m.emit(markerBrowse)
			m.emitFocus()
		}
	case modeDocs:
		m.keyDocs(ev)
	case modeDocView:
		switch ev.Key {
		case keys.KeyBackspace, keys.KeyEsc:
			m.mode = modeDocs
			m.emit(markerDocsOpen)
		}
	}
}

func (m *model) keyBrowse(ev keys.Event) {
	if m.filtering {
		switch ev.Key {
		case keys.KeyRune:
			m.filter += string(ev.Rune)
			m.sel = 0
			m.clampSel()
			m.emit(markerFilter + m.filter + " n=" + vi.Itoa64(int64(len(m.visible()))))
			m.status = "filter: " + m.filter
		case keys.KeyBackspace:
			if m.filter != "" {
				r := []rune(m.filter)
				m.filter = string(r[:len(r)-1])
				m.sel = 0
				m.clampSel()
				m.emit(markerFilter + m.filter + " n=" + vi.Itoa64(int64(len(m.visible()))))
			}
		case keys.KeyEsc:
			m.filter = ""
			m.filtering = false
			m.sel = 0
			m.emit(markerFilterCleared + vi.Itoa64(int64(len(m.cmds))))
			m.emitFocus()
		case keys.KeyEnter:
			// Apply and leave the filter line; the filter itself stays on.
			m.filtering = false
			m.emitFocus()
		}
		return
	}

	// Nav keys — arrows or their vi-flavoured runes.
	switch ev.Key {
	case keys.KeyDown:
		m.move(+1)
		return
	case keys.KeyUp:
		m.move(-1)
		return
	case keys.KeyLeft:
		m.jumpGroup(-1)
		return
	case keys.KeyRight:
		m.jumpGroup(+1)
		return
	case keys.KeyEnter:
		vis := m.visible()
		m.clampSel()
		if len(vis) > 0 {
			m.openDetail(vis[m.sel])
		}
		return
	case keys.KeyEsc:
		return
	}

	if ev.Key != keys.KeyRune {
		return
	}
	switch ev.Rune {
	case 'j':
		m.move(+1)
	case 'k':
		m.move(-1)
	case 'h':
		m.jumpGroup(-1)
	case 'l':
		m.jumpGroup(+1)
	case '/':
		m.filtering = true
		m.status = "filter: "
		m.emit(markerFilterOn)
	case 'd':
		if m.mode == modeBrowse {
			m.mode = modeDocs
			m.docSel = clampDoc(m.docSel, m.docN)
			m.status = "docs"
			m.emit(markerDocsOpen)
			m.emitDocFocus()
		}
	case 'q':
		m.quit = true
	}
}

// move steps the cursor within the visible list and re-emits focus.
func (m *model) move(delta int) {
	vis := m.visible()
	if len(vis) == 0 {
		return
	}
	m.sel += delta
	m.clampSel()
	m.emitFocus()
}

func (m *model) keyDocs(ev keys.Event) {
	switch ev.Key {
	case keys.KeyDown:
		m.moveDoc(+1)
		return
	case keys.KeyUp:
		m.moveDoc(-1)
		return
	case keys.KeyEnter:
		m.openDoc()
		return
	case keys.KeyBackspace, keys.KeyEsc:
		m.mode = modeBrowse
		m.emit(markerBrowse)
		m.emitFocus()
		return
	}
	if ev.Key != keys.KeyRune {
		return
	}
	switch ev.Rune {
	case 'j':
		m.moveDoc(+1)
	case 'k':
		m.moveDoc(-1)
	}
}

func (m *model) moveDoc(delta int) {
	if m.docN == 0 {
		return
	}
	m.docSel += delta
	m.docSel = clampDoc(m.docSel, m.docN)
	m.emitDocFocus()
}

func (m *model) emitDocFocus() {
	if m.docN == 0 {
		m.emit(markerDocFocus + "none")
		m.status = "no docs"
		return
	}
	name := m.docs[m.docSel].NameString()
	m.emit(markerDocFocus + name)
	m.status = name
}

// openDoc reads the selected page whole and flips to the reader; the marker
// carries the byte count the gate pins against its fixture.
func (m *model) openDoc() {
	if m.docN == 0 {
		return
	}
	name := m.docs[m.docSel].NameString()
	data, rc := vi.ReadFileAll(docsDir+"/"+name, maxDocBytes)
	if rc < 0 {
		m.status = "read err"
		m.emit(markerDocErr + name + " rc=" + vi.Itoa64(rc))
		return
	}
	m.docOf = name
	m.docText = string(data)
	m.docBig = len(data) >= maxDocBytes
	m.mode = modeDocView
	m.status = "doc: " + name
	m.emit(markerDoc + name + " bytes=" + vi.Itoa64(int64(len(data))))
}

func clampDoc(i, n int) int {
	if n == 0 {
		return 0
	}
	if i < 0 {
		return 0
	}
	if i >= n {
		return n - 1
	}
	return i
}

// keyLabel names a decoded key the way the chord table spells it, so
// `gohelp: key return` / `key backspace` / `key escape` line up with the
// gate's CSV verbatim; a rune prints as itself.
func keyLabel(ev keys.Event) string {
	switch ev.Key {
	case keys.KeyRune:
		return string(ev.Rune)
	case keys.KeyEnter:
		return "return"
	case keys.KeyEsc:
		return "escape"
	case keys.KeyBackspace:
		return "backspace"
	case keys.KeyUp:
		return "up"
	case keys.KeyDown:
		return "down"
	case keys.KeyLeft:
		return "left"
	case keys.KeyRight:
		return "right"
	case keys.KeyTab:
		return "tab"
	case keys.KeyCtrlC:
		return "ctrl-c"
	default:
		return "key"
	}
}
