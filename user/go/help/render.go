// The M74c frame: a grouped command list | live detail preview, plus the
// full-page detail and the /host/docs reader, drawn as truecolour ANSI for
// the kernel's window VT (M73h #1634: `38;2;r;g;b` paints at exact RGB — the
// go-help gate counts the DETAIL accent in the real scanout). Pure strings,
// no Bubble Tea import, so the host tests can pin the frame.
package main

import (
	"strconv"
	"strings"

	"virelai/shlib"
)

// Colours are truecolour on purpose: the class-B snapshot asserts the
// EXACT detail accent below, and no palette indirection may move it.
const (
	colReset = "\x1b[0m"

	colTitle  = "\x1b[38;2;90;205;230m"                // header bar
	colAccent = "\x1b[38;2;255;199;92m"                // DETAIL TEXT — gate accent (name/usage/blurb)
	colHead   = "\x1b[38;2;160;176;192m"               // pane headers
	colGroup  = "\x1b[38;2;90;205;230m"                // group tags in rows
	colRow    = "\x1b[38;2;216;224;232m"               // command rows
	colSel    = "\x1b[48;2;44;58;76;38;2;255;255;255m" // selected row
	colBody   = "\x1b[38;2;216;224;232m"               // doc text, notes
	colStat   = "\x1b[38;2;255;200;80m"                // status line
	colDim    = "\x1b[38;2;120;130;144m"               // hints
	colSep    = "\x1b[38;2;60;72;88m"                  // pane separator

	// The alt-screen prologue every frame starts with (M72b: the window VT
	// has ?1049/?25l/ED/CUP — each paint is a full, self-contained frame).
	framePrologue = "\x1b[?1049h\x1b[?25l\x1b[2J\x1b[H"
)

// listWidth is the left pane's width in cells: 45% of the grid, clamped so
// both panes stay usable on a narrow frame (the GOFILES layout).
func listWidth(cols int) int {
	w := cols * 45 / 100
	if w < 12 {
		w = 12
	}
	if w > cols-8 {
		w = cols - 8
	}
	if w < 4 {
		w = cols / 2
	}
	return w
}

// render is the whole frame for the bound tty: prologue + exactly rows
// lines, newline-joined with NO trailing newline on the last (a trailing
// newline on the final row would scroll the header out of the grid).
func (m model) render() string {
	lines := m.renderLines()
	return framePrologue + strings.Join(lines, "\n")
}

func (m model) renderLines() []string {
	switch m.mode {
	case modeDetail:
		return m.renderDetail()
	case modeDocs:
		return m.renderDocs()
	case modeDocView:
		return m.renderDocView()
	default:
		return m.renderBrowse()
	}
}

// header is row 0 in every mode: title, then the mode's own context.
func (m model) header(title string) string {
	return colTitle + " " + clipVis(title, m.cols-2) + colReset
}

func (m model) renderBrowse() []string {
	lw := listWidth(m.cols)
	pw := m.cols - lw - 1
	if pw < 8 {
		pw = 8
	}
	body := m.bodyRows()
	vis := m.visible()

	title := " GOHELP · GOSH command help"
	if m.filter != "" {
		title += "  [/" + m.filter + " → " + strconv.Itoa(len(vis)) + "]"
	}

	lines := make([]string, 0, m.rows)
	lines = append(lines, m.header(title))

	// Pane headers over their panes.
	si := clampDoc(m.sel, len(vis))
	detailHead := "(no matches)"
	if len(vis) > 0 {
		detailHead = vis[si].Name
	}
	lhead := clipVis(" commands "+strconv.Itoa(len(vis)), lw)
	rhead := clipVis(" detail: "+detailHead, pw)
	lines = append(lines,
		colHead+lhead+strings.Repeat(" ", lw-visLen(lhead))+colReset+
			colSep+"│"+colReset+
			colHead+rhead+strings.Repeat(" ", pw-visLen(rhead))+colReset)

	// Body: grouped command rows | separator | the selected row's detail.
	plines, accentN := m.previewLines(pw)
	for i := 0; i < body; i++ {
		lines = append(lines, m.listRow(i, lw, vis)+colSep+"│"+colReset+
			previewRow(i, plines, accentN, pw))
	}

	lines = append(lines, m.statusLine())
	hint := "j/k move · h/l group · enter detail · / filter · d docs · q quit"
	if m.filtering {
		hint = "type to filter · enter apply · esc clear · ↑↓ move"
	}
	lines = append(lines, colDim+" "+clipVis(hint, m.cols-2)+colReset)
	return lines
}

// listRow is one list line padded to w inside its style, so the selection
// background spans the full pane. Each row shows its group at the right
// edge — the catalog's grouping stays visible without header rows.
func (m model) listRow(i int, w int, vis []shlib.HelpRow) string {
	if i >= len(vis) {
		return strings.Repeat(" ", w)
	}
	r := vis[i]
	avail := w - 2 // "> " prefix
	grp := r.Group
	if len(grp) >= avail {
		grp = grp[:avail-1]
	}
	left := clipVis(r.Name, avail-len(grp)-1)
	fill := avail - visLen(left) - len(grp)
	if fill < 0 {
		fill = 0
	}
	if i == m.sel {
		return colSel + "> " + left + strings.Repeat(" ", fill) + grp + colReset
	}
	return colRow + "  " + left + strings.Repeat(" ", fill) + grp + colReset
}

// previewLines renders the selected row's detail for the right pane: name,
// usage and blurb in the gate's accent (accentN lines), notes in body.
func (m model) previewLines(w int) ([]string, int) {
	vis := m.visible()
	if len(vis) == 0 {
		return []string{"", "(no matches — esc clears the filter)"}, 0
	}
	return detailLines(vis[clampDoc(m.sel, len(vis))], w)
}

// detailLines is the shared detail composition (preview and full page):
// name, `usage: …`, blank, blurb, blank, notes — wrapping at w. The first
// accentN lines (through the blurb) render in the gate accent.
func detailLines(r shlib.HelpRow, w int) ([]string, int) {
	var out []string
	out = append(out, r.Name)
	usage := wrapText("usage: "+r.Usage, w)
	out = append(out, usage...)
	out = append(out, "")
	blurb := wrapText(r.Blurb, w)
	out = append(out, blurb...)
	accentN := 1 + len(usage) + 1 + len(blurb)
	if r.Notes != "" {
		out = append(out, "")
		out = append(out, wrapText(r.Notes, w)...)
	}
	return out, accentN
}

// previewRow is one detail line in accent or body, padded to w.
func previewRow(i int, plines []string, accentN, w int) string {
	if i >= len(plines) {
		return strings.Repeat(" ", w)
	}
	style := colBody
	if i < accentN {
		style = colAccent
	}
	vis := clipVis(plines[i], w)
	return style + vis + colReset + strings.Repeat(" ", w-visLen(vis))
}

// renderDetail is the full-page detail the card's gate pixel-asserts:
// accent name in the header, accent usage/blurb in the body, notes below.
func (m model) renderDetail() []string {
	body := m.pageRows()
	pw := m.cols - 2

	lines := make([]string, 0, m.rows)
	lines = append(lines, colAccent+" "+clipVis(m.detail.Name, m.cols-16)+
		colReset+colGroup+" · "+m.detail.Group+colReset)

	// The header already shows the name; the body starts at the usage line.
	plain, accentN := detailLines(m.detail, pw)
	src := plain[1:]
	accentBody := accentN - 1
	for i := 0; i < body; i++ {
		if i >= len(src) {
			lines = append(lines, "")
			continue
		}
		style := colBody
		if i < accentBody {
			style = colAccent
		}
		lines = append(lines, style+clipVis(src[i], pw)+colReset)
	}
	lines = append(lines, m.statusLine())
	lines = append(lines, colDim+" "+
		clipVis("backspace/esc list · d docs · / filter", m.cols-2)+colReset)
	return lines
}

func (m model) renderDocs() []string {
	body := m.pageRows()
	lines := make([]string, 0, m.rows)
	lines = append(lines, m.header(" docs — "+docsDir+" ("+strconv.Itoa(m.docN)+")"))

	for i := 0; i < body; i++ {
		if i >= m.docN {
			lines = append(lines, strings.Repeat(" ", m.cols))
			continue
		}
		e := m.docs[i]
		name := e.NameString()
		size := strconv.Itoa(int(e.Size))
		avail := m.cols - 2 - len(size)
		left := clipVis(name, avail)
		fill := avail - visLen(left)
		if fill < 0 {
			fill = 0
		}
		if i == m.docSel {
			lines = append(lines, colSel+"> "+left+strings.Repeat(" ", fill)+
				size+colReset)
		} else {
			lines = append(lines, colRow+"  "+left+strings.Repeat(" ", fill)+
				size+colReset)
		}
	}
	if m.docN == 0 {
		lines[1] = colDim + "  (no docs bundle — seed " + docsDir + ")" + colReset
	}
	lines = append(lines, m.statusLine())
	lines = append(lines, colDim+" "+
		clipVis("↑↓/jk move · enter open · backspace list", m.cols-2)+colReset)
	return lines
}

func (m model) renderDocView() []string {
	body := m.pageRows()
	pw := m.cols - 2
	lines := make([]string, 0, m.rows)
	lines = append(lines, m.header(" doc: "+m.docOf))

	text := m.docText
	if m.docBig {
		text += "\n\n(truncated at 64 KiB)"
	}
	ls := wrapText(text, pw)
	for i := 0; i < body; i++ {
		if i >= len(ls) {
			lines = append(lines, "")
			continue
		}
		lines = append(lines, colBody+clipVis(ls[i], pw)+colReset)
	}
	lines = append(lines, m.statusLine())
	lines = append(lines, colDim+" "+
		clipVis("backspace docs · esc list", m.cols-2)+colReset)
	return lines
}

func (m model) statusLine() string {
	return colStat + clipVis(" "+m.status, m.cols-2) + colReset
}

// pageRows is the body height for the single-column modes (detail, docs,
// reader): title + body + status + hints must total exactly m.rows — only
// the two-pane browse mode spends a row on pane headers.
func (m model) pageRows() int {
	r := m.rows - 3
	if r < 1 {
		r = 1
	}
	return r
}

// wrapText hard-wraps paragraphs on words at w cells (plain text only).
func wrapText(s string, w int) []string {
	if w < 4 {
		w = 4
	}
	var out []string
	for _, para := range strings.Split(s, "\n") {
		words := strings.Fields(para)
		if len(words) == 0 {
			out = append(out, "")
			continue
		}
		line := ""
		for _, wd := range words {
			if line == "" {
				line = wd
				continue
			}
			if len(line)+1+len(wd) <= w {
				line += " " + wd
			} else {
				out = append(out, line)
				line = wd
			}
		}
		out = append(out, line)
	}
	return out
}

// visLen is the printable width of s, skipping CSI escape sequences.
func visLen(s string) int {
	n := 0
	inEsc := false
	for _, r := range s {
		if inEsc {
			if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') {
				inEsc = false
			}
			continue
		}
		if r == 0x1b {
			inEsc = true
			continue
		}
		n++
	}
	return n
}

// clipVis truncates s to w printable cells (keeping any open ANSI prefix)
// and appends a reset when anything was cut.
func clipVis(s string, w int) string {
	if w <= 0 {
		return ""
	}
	var out strings.Builder
	vis := 0
	inEsc := false
	cut := false
	for _, r := range s {
		if inEsc {
			out.WriteRune(r)
			if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') {
				inEsc = false
			}
			continue
		}
		if r == 0x1b {
			inEsc = true
			out.WriteRune(r)
			continue
		}
		if vis >= w {
			cut = true
			break
		}
		out.WriteRune(r)
		vis++
	}
	if cut {
		out.WriteString(colReset)
	}
	return out.String()
}
