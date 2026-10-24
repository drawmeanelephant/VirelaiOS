// quickopen.go — M192g (#3252): Ctrl+P opens the quick-open palette, the
// keyboard-first open GOEDIT was missing. The palette incrementally
// name-matches the typed query over two candidate pools, in this order:
//
//   - buffers + recents. The ring's open files first, then the session's
//     MRU — seeded from the state envelope's record paths (a restore's
//     buffers are the files the user had open) and fed by every open and
//     close, so a just-closed buffer answers the next summon.
//   - the open file's directory subtree, walked at summon under the same
//     budget the launcher's filename hits keep (depth 4, 256 visited,
//     64 hits): every bound reached is named in the marker, the hits
//     already collected are real.
//
// Matching is find.ContainsFold on the base name — the shared name
// matcher the launcher rides, not the find bar's byte-exact document
// scan. Enter opens the selected path through the ring (an open path
// switches to its buffer, a fresh one takes a slot, a full ring leaves
// the palette standing for another pick); Escape cancels; up/down move.
package main

import (
	"unicode/utf8"

	"virelai/find"
	"virelai/theme"
	"virelai/vi"
	"virelai/webrender/font"
)

const (
	// qoMaxDepth/qoMaxVisit/qoMaxFiles bound the subtree walk — the
	// launcher's own numbers (gotabwm/launcher.go), so the two name
	// walks spend the same budget.
	qoMaxDepth = 4
	qoMaxVisit = 256
	qoMaxFiles = 64

	// qoRowsMax bounds the visible list — the diagnostics panel's bound.
	qoRowsMax = 8
	// recentCap bounds the MRU — the newest entries are the ones a
	// user would miss (the picker's pickMaxRec).
	recentCap = 8
)

// qoItem is one candidate: the full path plus the pool it came from —
// "buf" (an open buffer), "recent" (the envelope-seeded MRU) or "file"
// (the subtree walk).
type qoItem struct {
	path string
	tag  string
}

// qoDirList is the walk's directory seam — vi.DirList in the guest,
// swapped by the host tests (the launchDirList/bufRead pattern).
var qoDirList = vi.DirList

// pushRecent records path in the MRU: an existing entry moves back to
// the front, the list never outgrows recentCap. The callers are the
// opens and closes — openBuf, the picker's open landing, closeDoc and
// the envelope's restore paths.
func (e *editor) pushRecent(path string) {
	if path == "" {
		return
	}
	for i, p := range e.recents {
		if p == path {
			copy(e.recents[i:], e.recents[i+1:])
			e.recents = e.recents[:len(e.recents)-1]
			break
		}
	}
	e.recents = append([]string{path}, e.recents...)
	if len(e.recents) > recentCap {
		e.recents = e.recents[:recentCap]
	}
}

// qoBuild assembles the candidate list at summon: the ring's paths
// first, then the MRU's, then the bounded subtree walk — deduped by
// path so a buffer's file never lists twice. The second return names
// the bound that stopped the walk, "" when none did.
func (e *editor) qoBuild() ([]qoItem, string) {
	seen := make(map[string]bool)
	var items []qoItem
	add := func(path, tag string) {
		if path == "" || seen[path] {
			return
		}
		seen[path] = true
		items = append(items, qoItem{path: path, tag: tag})
	}
	for _, d := range e.docList() {
		add(d.path, "buf")
	}
	for _, p := range e.recents {
		add(p, "recent")
	}
	root := dirOf(e.path)
	if root == "" {
		root = "/host"
	}
	w := &qoWalker{seen: seen}
	w.walk(root, 0, &items)
	return items, w.trunc
}

// qoWalker carries the walk's counters — visit counts every listed
// entry (files and directories), files the collected hits, trunc names
// whichever bound stopped the walk.
type qoWalker struct {
	seen  map[string]bool
	visit int
	files int
	trunc string
}

// walk lists one directory: matching files collect first (deduped into
// the candidate list), then subdirectories descend — the launcher and
// GOFILES' own order, so a too-deep directory cannot cut off same-depth
// files the bound still covers.
func (w *qoWalker) walk(dir string, depth int, items *[]qoItem) {
	var ents [vi.MaxDirEntries]vi.DirEntry
	n, rc := qoDirList(dir, ents[:])
	if rc < 0 {
		return // an unreadable dir contributes nothing — its parent still counts
	}
	for i := 0; i < n; i++ {
		if w.trunc != "" {
			return
		}
		if w.visit >= qoMaxVisit {
			w.trunc = "visited"
			return
		}
		w.visit++
		if ents[i].Dir() {
			continue
		}
		path := dir + "/" + ents[i].NameString()
		if w.seen[path] {
			continue
		}
		if w.files >= qoMaxFiles {
			w.trunc = "files"
			return
		}
		w.seen[path] = true
		w.files++
		*items = append(*items, qoItem{path: path, tag: "file"})
	}
	for i := 0; i < n; i++ {
		if w.trunc != "" {
			return
		}
		if !ents[i].Dir() {
			continue
		}
		if depth+1 > qoMaxDepth {
			w.trunc = "depth"
			return
		}
		name := ents[i].NameString()
		if name == "" || name == "." || name == ".." {
			continue
		}
		w.walk(dir+"/"+name, depth+1, items)
	}
}

// qoFilter rebuilds the match list for the bar's query — the incremental
// half: it runs on every query mutation. An empty query lists every
// candidate; a non-empty one keeps the items whose base name contains
// it under the ASCII fold, in candidate order (buffers, recents,
// subtree). The marker names the query and the hit count — n=0 is the
// no-hit state the card names.
func (e *editor) qoFilter() {
	q := e.bar
	needle := make([]byte, len(q))
	for i := range q {
		needle[i] = find.FoldByte(q[i])
	}
	e.qoMatch = e.qoMatch[:0]
	for i, it := range e.qo {
		if len(needle) == 0 || find.ContainsFold([]byte(baseOf(it.path)), needle) {
			e.qoMatch = append(e.qoMatch, i)
		}
	}
	if e.qoSel >= len(e.qoMatch) {
		e.qoSel = len(e.qoMatch) - 1
	}
	if e.qoSel < 0 {
		e.qoSel = 0
	}
	vi.ConsoleLine(markerQOpen + "'" + string(q) + "' n=" +
		vi.Itoa64(int64(len(e.qoMatch))))
}

// openQuick is the Ctrl+P chord — summoned like the bars, from any
// non-modal mode. The candidate build walks the subtree once; the
// filter re-runs per keystroke against the cached list.
func (e *editor) openQuick() bool {
	e.hist.breakRun()
	e.qo, e.qoTrunc = e.qoBuild()
	e.mode = modeQuickOpen
	e.bar = e.bar[:0]
	e.bar2 = e.bar2[:0]
	e.bar2At = false
	e.qoSel = 0
	// The empty-query match is the whole list — compute it without the
	// filter marker so the summon receipt is the one that prints.
	e.qoMatch = e.qoMatch[:0]
	for i := range e.qo {
		e.qoMatch = append(e.qoMatch, i)
	}
	line := markerQOpen + "open n=" + vi.Itoa64(int64(len(e.qo)))
	if e.qoTrunc != "" {
		line += " truncated=" + e.qoTrunc
	}
	vi.ConsoleLine(line)
	return true
}

// qoPick is Enter: open the selected path through the ring. An open
// path switches to its buffer, a fresh one takes a slot — the "opens in
// a tab" contract — and a full ring leaves the palette standing so
// another row can still land. An empty match list is the named refusal.
func (e *editor) qoPick() {
	if len(e.qoMatch) == 0 {
		vi.ConsoleLine(markerQOpen + "none")
		return
	}
	it := e.qo[e.qoMatch[e.qoSel]]
	vi.ConsoleLine(markerQOpen + "pick " + it.path)
	if e.openBuf(it.path) {
		e.closeBar()
	}
}

// qopenKey owns the keyboard while the palette stands: Escape cancels,
// Enter picks, up/down move the selection, backspace trims and any
// printable scalar appends — every query mutation re-filters.
func (e *editor) qopenKey(ev vi.Event) bool {
	if ev.Arg1 == codeEscape || ev.Arg0 == usageEsc {
		e.closeBar()
		vi.ConsoleLine(markerQOpen + "cancel")
		return true
	}
	if ev.Flags&modCtrl != 0 {
		return false
	}
	if ev.Arg1 == codeReturn || ev.Arg1 == codeNewline {
		e.qoPick()
		return true
	}
	switch ev.Arg0 {
	case usageUp:
		if e.qoSel > 0 {
			e.qoSel--
		}
		return true
	case usageDown:
		if e.qoSel+1 < len(e.qoMatch) {
			e.qoSel++
		}
		return true
	}
	if ev.Arg1 == codeBackspace || ev.Arg1 == codeDelete {
		if len(e.bar) == 0 {
			return false
		}
		_, size := utf8.DecodeLastRune(e.bar)
		e.bar = e.bar[:len(e.bar)-size]
		e.qoFilter()
		return true
	}
	if ch, ok := insertionFor(ev); ok {
		if e.barAppend(ch) {
			e.qoFilter()
			return true
		}
		return false
	}
	return false
}

// drawQuick paints the palette over the document: the match rows
// anchored above the bar row (the selected row under-painted, the
// diags panel's shape), a muted no-hit line when the query emptied the
// list, and the "Open: <q>" bar line itself.
func (e *editor) drawQuick(w, h int) {
	tok := theme.Current
	adv := font.Advance(1)
	cols := (w - 2*textOrigin) / adv
	barY := h - lineH - 2
	y := barY - lineH
	// Bottom-aligned like the diags panel: the window shows at most
	// qoRowsMax rows ending near the selection.
	first := e.qoSel - qoRowsMax + 1
	if first < 0 {
		first = 0
	}
	for i := first + qoRowsMax - 1; i >= first; i-- {
		if i < len(e.qoMatch) {
			it := e.qo[e.qoMatch[i]]
			text := baseOf(it.path) + "  " + it.tag + "  " + dirOf(it.path)
			if utf8.RuneCountInString(text) > cols && cols > 0 {
				cut := cols
				for cut > 0 && cut < len(text) && !utf8.RuneStart(text[cut]) {
					cut--
				}
				text = text[:cut]
			}
			if i == e.qoSel {
				e.fill(0, y, w, lineH, tok.Selection)
			}
			e.drawText(textOrigin, y, text, tok.Ink)
		}
		y -= lineH
	}
	if len(e.qoMatch) == 0 {
		e.drawText(textOrigin, y, "(no files match)", tok.InkMuted)
	}
	e.drawText(textOrigin, barY, "Open: "+string(e.bar), tok.InkMuted)
}
