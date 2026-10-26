package main

import (
	"strings"
	"testing"

	"virelai/art"
)

// The gate's pixel assert counts this EXACT RGB in the real scanout — if a
// palette refactor ever moves it, this test is the class-A tripwire.
const testPreviewAccent = "\x1b[38;2;122;162;255m"

func renderModel() model {
	m := testModel(entry("SUB", true), entry("KNOWN.TXT", false))
	m.previewOf = "KNOWN.TXT"
	m.preview = "hello-from-gofiles\npreview-line-alpha"
	m.status = "listed"
	return m
}

// visibleLines splits a rendered frame into its lines with ANSI stripped.
func visibleLines(frame string) []string {
	body := strings.TrimPrefix(frame, framePrologue)
	raw := strings.Split(body, "\n")
	out := make([]string, 0, len(raw))
	var b strings.Builder
	inEsc := false
	for _, ln := range raw {
		b.Reset()
		for _, r := range ln {
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
			b.WriteRune(r)
		}
		out = append(out, b.String())
	}
	return out
}

func TestRenderCarriesProloguePreviewAccentAndSelection(t *testing.T) {
	frame := renderModel().render()
	if !strings.HasPrefix(frame, framePrologue) {
		t.Fatal("frame must open with the alt-screen prologue")
	}
	if strings.HasSuffix(frame, "\n") {
		t.Fatal("a trailing newline on the last row would scroll the frame")
	}
	if !strings.Contains(frame, testPreviewAccent+"hello-from-gofiles") {
		t.Fatalf("preview line must be painted in the gate accent %q", testPreviewAccent)
	}
	if !strings.Contains(frame, colSel) {
		t.Fatal("selected row styling missing")
	}
	if !strings.Contains(frame, " SUB/") || !strings.Contains(frame, "KNOWN.TXT") {
		t.Fatal("entry labels missing from the list pane")
	}
	if !strings.Contains(frame, "/host/FM") {
		t.Fatal("path bar missing")
	}
}

func TestRenderFitsTheGrid(t *testing.T) {
	m := renderModel()
	// A long path and a long preview line must be clipped, not wrapped.
	m.path = "/host/" + strings.Repeat("verylongdir/", 8)
	m.preview = strings.Repeat("PREVIEWWRAP ", 40)
	lines := visibleLines(m.render())
	if len(lines) != m.rows {
		t.Fatalf("frame has %d lines, grid rows=%d", len(lines), m.rows)
	}
	for i, ln := range lines {
		// The visible text is one byte-cell per rune here (ASCII), so its
		// rune count must fit the column budget.
		if len([]rune(ln)) > m.cols {
			t.Fatalf("line %d is %d cells, cols=%d: %q", i, len([]rune(ln)), m.cols, ln)
		}
	}
}

func TestRenderModalLines(t *testing.T) {
	m := renderModel()
	m.mode = modeRename
	m.input = "newname"
	if !strings.Contains(m.render(), " rename to: newname_") {
		t.Fatal("rename prompt missing from the frame")
	}
	m.mode = modeConfirmDelete
	m.status = "delete KNOWN.TXT? y/n"
	if !strings.Contains(m.render(), "delete KNOWN.TXT? y/n") {
		t.Fatal("delete confirm missing from the frame")
	}
}

func TestRenderEmptyPreview(t *testing.T) {
	m := testModel()
	m.preview = "(no selection)"
	if !strings.Contains(m.render(), "(no selection)") {
		t.Fatal("empty-preview placeholder missing")
	}
}

func TestListWidthKeepsBothPanes(t *testing.T) {
	for _, cols := range []int{16, 32, 64, 80, 200} {
		w := listWidth(cols)
		if w < 4 || cols-w-1 < 2 {
			t.Errorf("listWidth(%d)=%d leaves the preview pane dead", cols, w)
		}
	}
}

// #3348 (#1257): the staged filetype family paints a two-cell strip in
// the list rows — dirs the folder card, mapped families their raster,
// uncovered families the blank column — and the receipt counts the
// painted rows once per listing. An absent share keeps the text row and
// reserves nothing.
func TestListRowIcons(t *testing.T) {
	art.Reset()
	defer art.Reset()
	art.Stage(art.FiletypeFolder, stagedIconCanvas(200, 160, 40))
	art.Stage(art.FiletypeText, stagedIconCanvas(40, 90, 200))
	m := testModel(entry("SUB", true), entry("A.TXT", false), entry("S.JS", false))
	m.syncListIcons()
	if m.rowIconN != 2 {
		t.Fatalf("rowIconN = %d, want 2 (S.JS's script family is uncovered)", m.rowIconN)
	}
	if m.rowIconRef[0] == nil || m.rowIconRef[1] == nil || m.rowIconRef[2] != nil {
		t.Fatalf("rowIconRef = %v, want [icon icon nil]", m.rowIconRef)
	}
	if got := pendingJoined(&m); !strings.Contains(got, markerListIcons+"2") {
		t.Fatalf("icons receipt missing or wrong: %q", got)
	}
	// The strip is painted cells on every resolved row — the selected
	// row's chip keeps its own colours inside the selection band.
	if row := m.listRow(0, 28); !strings.Contains(row, "\x1b[48;2;200;160;40") {
		t.Fatalf("dir row must paint the folder strip: %q", row)
	} else if visLen(row) != 28 {
		t.Fatalf("dir row visLen = %d, want 28: %q", visLen(row), row)
	}
	if row := m.listRow(1, 28); !strings.Contains(row, "\x1b[48;2;40;90;200") {
		t.Fatalf("text row must paint the text strip: %q", row)
	}
	// The uncovered row keeps the column as blanks — aligned, typographic.
	if row := m.listRow(2, 28); strings.Contains(row, "\x1b[48;2;") {
		t.Fatalf("uncovered row must not paint cell colours: %q", row)
	} else if visLen(row) != 28 {
		t.Fatalf("uncovered row visLen = %d, want 28: %q", visLen(row), row)
	}
	// A re-sort remaps the table: name order puts A.TXT first, and its
	// strip follows the row, not the index.
	m.handleKey(runeKey('s'))
	if m.rowIconRef[0] == nil {
		t.Fatal("after re-sort A.TXT must still carry its strip")
	}
	// The honest degrade: no staged art at all — no column, no marker.
	art.Reset()
	m2 := testModel(entry("SUB", true), entry("A.TXT", false))
	m2.syncListIcons()
	if m2.rowIconN != 0 || len(m2.rowIconRef) != 2 {
		t.Fatalf("absent art must resolve nothing: n=%d ref=%v", m2.rowIconN, m2.rowIconRef)
	}
	if strings.Contains(pendingJoined(&m2), markerListIcons) {
		t.Fatal("icons receipt must stay silent without staged art")
	}
	if row := m2.listRow(1, 28); !strings.Contains(row, "  A.TXT") {
		t.Fatalf("no-art row keeps the plain layout: %q", row)
	}
}

func TestVisLenAndClip(t *testing.T) {
	s := colPrev + "abcde" + colReset
	if n := visLen(s); n != 5 {
		t.Errorf("visLen = %d, want 5", n)
	}
	if got := clipVis(s, 3); visLen(got) != 3 {
		t.Errorf("clipVis = %q (%d cells), want 3", got, visLen(got))
	}
	if got := clipVis(s, 10); !strings.HasSuffix(got, colReset) && len([]rune("abcde")) > 10 {
		t.Errorf("unclipped clipVis changed the text: %q", got)
	}
}
