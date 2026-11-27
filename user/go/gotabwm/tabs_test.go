package main

import (
	"strings"
	"testing"

	"virelai/art"
	"virelai/draw"
	"virelai/theme"
	"virelai/vi"
)

func TestTabMarkerShapes(t *testing.T) {
	resetSeatForTest(t)
	cases := []struct{ got, want string }{
		{MarkerTabOpen, "gotabwm: tab open id="},
		{MarkerTabFocus, "gotabwm: tab focus id="},
		{MarkerTabClose, "gotabwm: tab close id="},
		{MarkerRail, "gotabwm: rail "},
		{MarkerRailPaint, "gotabwm: rail paint "},
		{MarkerTabsEmpty, "gotabwm: tabs empty"},
		{MarkerSplit, "gotabwm: split "},
		{MarkerUnsplit, "gotabwm: unsplit"},
		{MarkerLayout, "gotabwm: layout "},
		{MarkerLayoutFile, "gotabwm: layout file="},
		{MarkerPane, "gotabwm: pane "},
		{MarkerPin, "gotabwm: pin "},
		{MarkerReorder, "gotabwm: reorder "},
		{MarkerOrder, "gotabwm: order "},
		{MarkerSash, "gotabwm: sash "},
		{MarkerSessionWrite, "gotabwm: session write n="},
		{MarkerSessionLoad, "gotabwm: session load n="},
		{MarkerSessionTitles, "gotabwm: session titles="},
		{MarkerSessionBad, "gotabwm: session bad"},
	}
	for _, c := range cases {
		if c.got != c.want {
			t.Fatalf("marker = %q want %q", c.got, c.want)
		}
	}
}

func TestGuessBinShippingTitles(t *testing.T) {
	resetSeatForTest(t)
	cases := []struct{ title, want string }{
		{"Calc", "GOCALC.ELF"},
		{"Notepad", "NOTE.ELF"},
		{"Edit", "GOEDIT.ELF"},
		{"Files", "GOFILES.ELF"},
		{"Image Viewer", "GOVIEW.ELF"},
		{"Term", "GOTERM.ELF"},
		{"Web", "WEB.ELF"},
		{"RSS Reader", "RSS.ELF"},
		{"Sh", "GOSH.ELF"},
		{"Vault", "GOVAULT.ELF"},
		{"Settings", "GOSET.ELF"},
		{"Welcome", "GOWELCOME.ELF"},
		{"LLM Infer", "GOINFER.ELF"},
		{"Recorder", "GOREC.ELF"},
		{"Store", "GOSTORE.ELF"},
		{"Gostalgia", "GOSTALGIA.ELF"},
		{"Sound Player", "GOSOUND.ELF"},
		{"Sound Player: TONE.WAV", "GOSOUND.ELF"},
		{"Sound Recorder", "GOSOUND.ELF"},
		{"Mic Meter", "GOSOUND.ELF"},
		{"A11y", "GOA11Y.ELF"},
		{"Help", "GOHELP.ELF"},
		{"Other", "Other"},
	}
	for _, c := range cases {
		if got := guessBin(c.title); got != c.want {
			t.Fatalf("guessBin(%q) = %q want %q", c.title, got, c.want)
		}
	}
	var s TabStrip
	if !s.OpenTab(3, "Edit") || !s.OpenTab(4, "Notepad") {
		t.Fatal("OpenTab")
	}
	if s.At(0).Bin != "GOEDIT.ELF" || s.At(1).Bin != "NOTE.ELF" {
		t.Fatalf("bins %q %q", s.At(0).Bin, s.At(1).Bin)
	}
	body := layoutFileBody(&s, 1280, 720)
	got := string(body)
	if !strings.Contains(got, "bin=GOEDIT.ELF") || !strings.Contains(got, "bin=NOTE.ELF") {
		t.Fatalf("LAYOUT body missing live bins: %q", body)
	}
}

// #3053: the title is app-controlled text but lands in the gate-parsed
// `bin=` token — a space, '=' or newline would inject extra key=value
// fields into the LAYOUT.txt / serial dump line.
func TestBinTokenSanitizesTitle(t *testing.T) {
	resetSeatForTest(t)
	cases := []struct{ in, want string }{
		{"GOCALC.ELF", "GOCALC.ELF"}, // filename set passes through
		{"evil focus=1", "evil_focus_1"},
		{"x=1 y=2", "x_1_y_2"},
		{"a\nb", "a_b"},
		{"héllo", "h__llo"}, // é is two UTF-8 bytes
		{"", ""},
	}
	for _, c := range cases {
		if got := binToken(c.in); got != c.want {
			t.Fatalf("binToken(%q) = %q want %q", c.in, got, c.want)
		}
	}

	var s TabStrip
	if !s.OpenTab(3, "evil focus=1") {
		t.Fatal("OpenTab")
	}
	if s.At(0).Bin != "evil_focus_1" {
		t.Fatalf("stored bin = %q", s.At(0).Bin)
	}
	body := layoutFileBody(&s, 1280, 720)
	want := "tab=3 bin=evil_focus_1 x=0 y=0 w=1280 h=720 focus=1 split=none\n"
	if string(body) != want {
		t.Fatalf("LAYOUT body = %q want %q", body, want)
	}
}

// The emit surface re-sanitizes: a bin planted by a crafted .tabs record
// never reaches the gate line raw.
func TestLayoutLineSanitizesBin(t *testing.T) {
	resetSeatForTest(t)
	line := layoutLine(5, "evil focus=1", Rect{640, 0, 640, 720}, true, SplitVert)
	want := "tab=5 bin=evil_focus_1 x=640 y=0 w=640 h=720 focus=1 split=v"
	if line != want {
		t.Fatalf("layoutLine = %q want %q", line, want)
	}
}

func TestSetTitlePreservesBinAndSession(t *testing.T) {
	resetSeatForTest(t)
	var live TabStrip
	if !live.OpenTab(4, "Notepad") {
		t.Fatal("OpenTab")
	}
	if !live.SetTitle(4, "notes.txt") {
		t.Fatal("SetTitle existing tab")
	}
	if live.At(0).Title != "notes.txt" || live.At(0).Bin != "NOTE.ELF" {
		t.Fatalf("retitled tab = %+v", live.At(0))
	}
	if live.SetTitle(99, "other.txt") {
		t.Fatal("SetTitle accepted an unknown id")
	}
	if live.SetTitle(4, "") {
		t.Fatal("SetTitle accepted an empty title")
	}

	raw, _, ok := live.encodeTabsV2(1)
	if !ok {
		t.Fatal("encodeTabsV2")
	}
	var restored TabStrip
	if _, ok := restored.applyTabsV2(raw); !ok {
		t.Fatal("applyTabsV2")
	}
	if restored.At(0).Title != "notes.txt" || restored.At(0).Bin != "NOTE.ELF" {
		t.Fatalf("restored title/bin = %+v", restored.At(0))
	}
}

func TestOpenCloseFocusMachine(t *testing.T) {
	resetSeatForTest(t)
	var s TabStrip
	if s.Count() != 0 {
		t.Fatalf("zero strip count = %d", s.Count())
	}
	if _, ok := s.Focused(); ok {
		t.Fatal("zero strip must have no focus")
	}
	if s.OpenTab(0, "x") {
		t.Fatal("id 0 must be refused")
	}
	if !s.OpenTab(4, "Calc") {
		t.Fatal("first OpenTab")
	}
	if s.Count() != 1 {
		t.Fatalf("count = %d want 1", s.Count())
	}
	if id, ok := s.Focused(); !ok || id != 4 {
		t.Fatalf("first tab focus = %d ok=%v want 4", id, ok)
	}
	if s.OpenTab(4, "Calc2") {
		t.Fatal("duplicate OpenTab must not count as added")
	}
	if s.Count() != 1 || s.At(0).Title != "Calc2" {
		t.Fatalf("duplicate should update title, count=%d title=%q", s.Count(), s.At(0).Title)
	}
	if !s.OpenTab(5, "Notepad") {
		t.Fatal("second OpenTab")
	}
	if s.Count() != 2 {
		t.Fatalf("count = %d want 2", s.Count())
	}
	if id, ok := s.Focused(); !ok || id != 4 {
		t.Fatalf("second OpenTab stole focus: %d ok=%v", id, ok)
	}
	if !s.FocusTab(5) {
		t.Fatal("FocusTab 5")
	}
	if id, ok := s.Focused(); !ok || id != 5 {
		t.Fatalf("focus = %d want 5", id)
	}
	if s.FocusTab(99) {
		t.Fatal("FocusTab unknown")
	}
	// Close the focused tab: the remaining one becomes focused.
	if !s.CloseTab(5) {
		t.Fatal("CloseTab focused")
	}
	if s.Count() != 1 {
		t.Fatalf("count after close focused = %d want 1", s.Count())
	}
	if id, ok := s.Focused(); !ok || id != 4 {
		t.Fatalf("remaining focus = %d ok=%v want 4", id, ok)
	}
	if !s.CloseTab(4) {
		t.Fatal("CloseTab last")
	}
	if s.Count() != 0 {
		t.Fatalf("empty count = %d", s.Count())
	}
	if _, ok := s.Focused(); ok {
		t.Fatal("empty strip still focused")
	}
	if s.CloseTab(4) {
		t.Fatal("CloseTab missing")
	}
}

func TestCloseNonFocusedKeepsFocus(t *testing.T) {
	resetSeatForTest(t)
	var s TabStrip
	s.OpenTab(1, "a")
	s.OpenTab(2, "b")
	s.FocusTab(1)
	if !s.CloseTab(2) {
		t.Fatal("close non-focused")
	}
	if id, ok := s.Focused(); !ok || id != 1 {
		t.Fatalf("focus moved off the surviving tab: %d ok=%v", id, ok)
	}
}

func TestCloseFirstOfTwoFocusesNeighbour(t *testing.T) {
	resetSeatForTest(t)
	var s TabStrip
	s.OpenTab(1, "a")
	s.OpenTab(2, "b")
	s.FocusTab(1)
	if !s.CloseTab(1) {
		t.Fatal("close first")
	}
	if id, ok := s.Focused(); !ok || id != 2 {
		t.Fatalf("neighbour focus = %d ok=%v want 2", id, ok)
	}
}

func TestMaxTabsCap(t *testing.T) {
	resetSeatForTest(t)
	var s TabStrip
	for i := 1; i <= MaxTabs; i++ {
		if !s.OpenTab(uint32(i), "t") {
			t.Fatalf("OpenTab %d failed under cap", i)
		}
	}
	if s.OpenTab(uint32(MaxTabs+1), "overflow") {
		t.Fatal("OpenTab past MaxTabs")
	}
	if s.Count() != MaxTabs {
		t.Fatalf("count = %d want %d", s.Count(), MaxTabs)
	}
}

func TestReopenRingRecordsAndReopensLIFO(t *testing.T) {
	resetSeatForTest(t)
	var s TabStrip
	if _, ok := s.ReopenLastClosed(); ok {
		t.Fatal("an empty ring must not reopen")
	}
	if !s.OpenTab(3, "Calc") || !s.OpenTab(4, "Edit") {
		t.Fatal("OpenTab")
	}
	if !s.CloseTab(3) {
		t.Fatal("CloseTab Calc")
	}
	c, ok := s.RecentlyClosed(0)
	if !ok || c.Bin != "GOCALC.ELF" || c.Title != "Calc" {
		t.Fatalf("ring top = %+v ok=%v want GOCALC.ELF/Calc", c, ok)
	}
	if !s.CloseTab(4) {
		t.Fatal("CloseTab Edit")
	}
	// LIFO: the most recent close comes back first.
	top, ok := s.ReopenLastClosed()
	if !ok || top.Bin != "GOEDIT.ELF" {
		t.Fatalf("reopen #1 = %+v ok=%v want GOEDIT.ELF", top, ok)
	}
	next, ok := s.ReopenLastClosed()
	if !ok || next.Bin != "GOCALC.ELF" {
		t.Fatalf("reopen #2 = %+v ok=%v want GOCALC.ELF", next, ok)
	}
	if _, ok := s.ReopenLastClosed(); ok {
		t.Fatal("ring must be drained after two reopens")
	}
	// Reopen is a re-exec, not a re-open: the strip stays empty until the
	// re-exec'd window declares and joins as a fresh tab (D1).
	if s.Count() != 0 {
		t.Fatalf("strip count = %d want 0", s.Count())
	}
}

func TestReopenRingIsBounded(t *testing.T) {
	resetSeatForTest(t)
	var s TabStrip
	for i := 0; i < MaxTabs+3; i++ {
		id := uint32(100 + i)
		if !s.OpenTab(id, "Edit") {
			t.Fatalf("OpenTab %d", id)
		}
		if !s.CloseTab(id) {
			t.Fatalf("CloseTab %d", id)
		}
	}
	// The counter is monotonic (Zig closed_count) but the ring only exposes
	// MaxTabs entries: the oldest three were overwritten.
	if s.closedCount != MaxTabs+3 {
		t.Fatalf("closedCount = %d want %d", s.closedCount, MaxTabs+3)
	}
	if _, ok := s.RecentlyClosed(MaxTabs); ok {
		t.Fatal("ring must expose at most MaxTabs entries")
	}
	if c, ok := s.RecentlyClosed(MaxTabs - 1); !ok || c.Bin != "GOEDIT.ELF" {
		t.Fatalf("oldest live entry = %+v ok=%v", c, ok)
	}
	n := 0
	for {
		if _, ok := s.ReopenLastClosed(); !ok {
			break
		}
		n++
		if n > MaxTabs+1 {
			t.Fatal("reopen never drained: bounded ring leaked")
		}
	}
	if n != MaxTabs {
		t.Fatalf("reopened %d entries want %d (bounded ring)", n, MaxTabs)
	}
}

func TestReopenSkipsUnreopenableEntries(t *testing.T) {
	resetSeatForTest(t)
	var s TabStrip
	// OpenTab with an empty title records no bin (guessBin("") == ""), which
	// is Zig's "a tab this WM never spawned" entry.
	if !s.OpenTab(7, "") || !s.OpenTab(8, "Calc") {
		t.Fatal("OpenTab")
	}
	if !s.CloseTab(7) || !s.CloseTab(8) {
		t.Fatal("closes")
	}
	c, ok := s.ReopenLastClosed()
	if !ok || c.Bin != "GOCALC.ELF" {
		t.Fatalf("reopen = %+v ok=%v want GOCALC.ELF", c, ok)
	}
	// The bin-less entry was popped on the way, not returned.
	if _, ok := s.ReopenLastClosed(); ok {
		t.Fatal("unreopenable entry must be popped, not returned")
	}
}

func TestDuplicateFocusedHonestNoop(t *testing.T) {
	resetSeatForTest(t)
	var s TabStrip
	if _, ok := s.DuplicateFocused(); ok {
		t.Fatal("an empty strip must not duplicate")
	}
	if !s.OpenTab(3, "Calc") || !s.OpenTab(4, "Edit") {
		t.Fatal("OpenTab")
	}
	if !s.FocusTab(4) {
		t.Fatal("FocusTab")
	}
	bin, ok := s.DuplicateFocused()
	if !ok || bin != "GOEDIT.ELF" {
		t.Fatalf("duplicate = %q ok=%v want GOEDIT.ELF", bin, ok)
	}
	// Duplicate is a re-exec: the strip is unchanged until the clone declares.
	if s.Count() != 2 {
		t.Fatalf("count = %d want 2", s.Count())
	}
	var e TabStrip
	if !e.OpenTab(9, "") {
		t.Fatal("OpenTab empty title")
	}
	if _, ok := e.DuplicateFocused(); ok {
		t.Fatal("a tab without a recorded bin must not duplicate")
	}
}

func TestFreezeMarkerShapes(t *testing.T) {
	resetSeatForTest(t)
	cases := []struct{ got, want string }{
		{MarkerFreeze, "gotabwm: freeze id="},
		{MarkerThaw, "gotabwm: thaw id="},
		{MarkerSessionFreeze, "gotabwm: session freeze n="},
	}
	for _, c := range cases {
		if c.got != c.want {
			t.Fatalf("marker = %q want %q", c.got, c.want)
		}
	}
}

// M71e (#1564): Freeze/Thaw are a flag machine on an existing tab (D1), and
// deliberately NOT a lock — Zig checks `frozen` nowhere in its close path.
func TestFreezeThawMachine(t *testing.T) {
	resetSeatForTest(t)
	var s TabStrip
	if s.Freeze(1) {
		t.Fatal("Freeze on an empty strip must be false")
	}
	if s.Thaw(1) {
		t.Fatal("Thaw on an empty strip must be false")
	}
	if !s.OpenTab(3, "Calc") || !s.OpenTab(4, "Edit") {
		t.Fatal("OpenTab")
	}
	if !s.Freeze(4) {
		t.Fatal("Freeze Edit")
	}
	if !s.At(1).Frozen || s.At(0).Frozen {
		t.Fatalf("wrong tab frozen: %+v %+v", s.At(0), s.At(1))
	}
	if s.Freeze(4) {
		t.Fatal("Freeze is idempotent: a second call must report no change")
	}
	if s.Freeze(99) {
		t.Fatal("Freeze of a missing id must be false")
	}
	if n := s.FrozenCount(); n != 1 {
		t.Fatalf("FrozenCount = %d want 1", n)
	}
	if !s.Thaw(4) {
		t.Fatal("Thaw Edit")
	}
	if s.Thaw(4) {
		t.Fatal("Thaw is idempotent")
	}
	if n := s.FrozenCount(); n != 0 {
		t.Fatalf("FrozenCount = %d want 0", n)
	}
	// Freeze must not disturb pin, focus, order, or closability.
	s.Freeze(3)
	if !s.FocusTab(3) {
		t.Fatal("FocusTab")
	}
	if id, ok := s.Focused(); !ok || id != 3 {
		t.Fatalf("focus = %d ok=%v", id, ok)
	}
	if !s.CloseTab(3) {
		t.Fatal("a frozen tab must still close (badge, not lock)")
	}
}

// The frozen badge rides the rail: the frozen cell carries Warning pixels and
// a thawed one carries none.
func TestPaintRailFrozenBadge(t *testing.T) {
	resetSeatForTest(t)
	const w, h = 200, 40
	scan := make([]byte, w*h*4)
	pix := asUint32(scan)
	var s TabStrip
	if !s.OpenTab(3, "Calc") {
		t.Fatal("OpenTab")
	}
	thawed := paintRail(scan, w, h, RailHeight, &s, -1, -1)
	if n := countRGB(pix, w, RailHeight, railFrozenRGB()); n != 0 {
		t.Fatalf("a thawed rail painted %d badge pixels", n)
	}
	// Clear and re-paint with the badge on.
	for i := range pix {
		pix[i] = 0
	}
	if !s.Freeze(3) {
		t.Fatal("Freeze")
	}
	frozen := paintRail(scan, w, h, RailHeight, &s, -1, -1)
	badge := countRGB(pix, w, RailHeight, railFrozenRGB())
	if badge == 0 {
		t.Fatal("a frozen rail painted no Warning badge pixels")
	}
	if frozen <= thawed {
		t.Fatalf("frozen rail wrote %d pixels, thawed %d — the badge must add pixels", frozen, thawed)
	}
	// The badge is inset from the band's top and bottom, so it never touches
	// the rail's edges (it reads on both idle and focused cells).
	for row := 0; row < h; row++ {
		for col := 0; col < w; col++ {
			if pix[row*w+col]&0xffffff != railFrozenRGB() {
				continue
			}
			if row == 0 || row == RailHeight-1 {
				t.Fatalf("badge pixel at rail edge row=%d", row)
			}
		}
	}
}

// countRGB counts pixels matching rgb across the top `rows` rows of a
// width-strided word buffer.
func countRGB(pix []uint32, width, rows int, rgb uint32) int {
	n := 0
	for row := 0; row < rows; row++ {
		for col := 0; col < width; col++ {
			if row*width+col < len(pix) && pix[row*width+col]&0xffffff == rgb {
				n++
			}
		}
	}
	return n
}

// M79b (#1705): the hovered cell tints (theme BtnHover, distinct from idle
// and focus), focus wins over hover, and every cell carries the close-x glyph
// in its close zone.
func TestPaintRailHoverTintAndCloseGlyph(t *testing.T) {
	resetSeatForTest(t)
	const w, h = 256, 32
	buf := make([]byte, w*h*4)
	pix := asUint32(buf)
	var s TabStrip
	if !s.OpenTab(3, "A") || !s.OpenTab(4, "B") {
		t.Fatal("OpenTab")
	}
	_ = s.FocusTab(4) // cell 1 focused
	// Hover the IDLE cell: it tints and no idle pixels remain there.
	paintRail(buf, w, h, RailHeight, &s, 0, -1)
	if n := countRGB(pix, w, RailHeight, railHoverRGB()); n == 0 {
		t.Fatal("hovering an idle cell painted no hover tint")
	}
	if n := countRGB(pix, w, RailHeight, railIdleRGB()); n != 0 {
		t.Fatalf("the hovered idle cell left %d idle pixels", n)
	}
	// Hover the FOCUSED cell: focus wins, no hover tint anywhere.
	for i := range pix {
		pix[i] = 0
	}
	paintRail(buf, w, h, RailHeight, &s, 1, -1)
	if n := countRGB(pix, w, RailHeight, railHoverRGB()); n != 0 {
		t.Fatalf("hover painted %d tint pixels over the focused cell", n)
	}
	// The focused cell is the §2.3.6 treatment: Selection fill plus the
	// 3px Accent marker (railFocusRGB is the token's Accent).
	if n := countRGB(pix, w, RailHeight, theme.Current.Selection); n == 0 {
		t.Fatal("the focused cell lost its Selection fill")
	}
	if n := countRGB(pix, w, RailHeight, railFocusRGB()); n == 0 {
		t.Fatal("the focused cell lost its Accent marker")
	}
	// No hover: the close glyph (muted ink) sits in each cell's zone.
	// Cell 0's zone at w=256 n=2 (cellW=128) is [112,128).
	for i := range pix {
		pix[i] = 0
	}
	paintRail(buf, w, h, RailHeight, &s, -1, -1)
	if n := countRGB(pix, w, RailHeight, railCloseRGB()); n == 0 {
		t.Fatal("no close-x glyph pixels on the rail")
	}
	found := false
	for row := 0; row < RailHeight; row++ {
		for col := 112; col < 128; col++ {
			if pix[row*w+col]&0xffffff == railCloseRGB() {
				found = true
			}
		}
	}
	if !found {
		t.Fatal("cell 0's close zone holds no glyph pixels")
	}
	// §2.4: the close zone under the pointer tints its glyph Danger —
	// cell 0's zone only, and nowhere else on the rail.
	for i := range pix {
		pix[i] = 0
	}
	paintRail(buf, w, h, RailHeight, &s, -1, 0)
	if n := countRGB(pix, w, RailHeight, theme.Current.Danger); n == 0 {
		t.Fatal("the hovered close zone painted no Danger glyph")
	}
	for row := 0; row < RailHeight; row++ {
		for col := 128; col < w; col++ {
			if pix[row*w+col]&0xffffff == theme.Current.Danger {
				t.Fatalf("Danger close glyph outside cell 0's zone at %d,%d", col, row)
			}
		}
	}
}

func TestNextIDWraps(t *testing.T) {
	resetSeatForTest(t)
	var s TabStrip
	if _, ok := s.NextID(); ok {
		t.Fatal("empty NextID")
	}
	s.OpenTab(1, "a")
	s.OpenTab(2, "b")
	s.FocusTab(1)
	if id, ok := s.NextID(); !ok || id != 2 {
		t.Fatalf("next from 1 = %d ok=%v want 2", id, ok)
	}
	s.FocusTab(2)
	if id, ok := s.NextID(); !ok || id != 1 {
		t.Fatalf("next from 2 = %d ok=%v want 1", id, ok)
	}
}

func TestMaxTabsMatchesSessionCap(t *testing.T) {
	resetSeatForTest(t)
	if MaxTabs != 16 {
		t.Fatalf("MaxTabs = %d want 16 (ADR 0033 / .tabs v2)", MaxTabs)
	}
	if RailHeight != 22 {
		t.Fatalf("RailHeight = %d want 22 (kernel tab_bar_height)", RailHeight)
	}
}

func TestPaintRailFillsStripOnly(t *testing.T) {
	resetSeatForTest(t)
	const w, h, strip = 256, 32, 8
	buf := make([]byte, w*h*4)
	var s TabStrip
	if n := paintRail(buf, w, h, strip, &s, -1, -1); n != 0 {
		t.Fatalf("empty strip wrote %d pixels", n)
	}
	s.OpenTab(1, "a")
	s.OpenTab(2, "b")
	s.FocusTab(2)
	if n := paintRail(buf, w, h, strip, &s, -1, -1); n == 0 {
		t.Fatal("paintRail wrote nothing")
	}
	pix := func(x, y int) uint32 {
		i := (y*w + x) * 4
		return uint32(buf[i]) | uint32(buf[i+1])<<8 | uint32(buf[i+2])<<16 | uint32(buf[i+3])<<24
	}
	// Below the strip must stay zero.
	for y := strip; y < h; y++ {
		for x := 0; x < w; x++ {
			if pix(x, y) != 0 {
				t.Fatalf("pixel %d,%d = %#x; paintRail wrote below the strip", x, y, pix(x, y))
			}
		}
	}
	// The two cells differ: left is idle, right is the §2.3.6 focused
	// cell — a 3px Accent marker at its left edge, Selection fill after.
	left := pix(2, 1)
	right := pix(w/2+8, 1)
	if left == 0 || right == 0 {
		t.Fatalf("strip cells were not painted left=%#x right=%#x", left, right)
	}
	if left == right {
		t.Fatalf("focused and idle cells are the same colour %#x", left)
	}
	// The engine's Opaque fill forces the scanout's X byte 0xff (M71c
	// #1562), so compare the colour with that byte masked off.
	if marker := pix(w/2+1, 1) & 0xffffff; marker != railFocusRGB() {
		t.Fatalf("focused cell's marker %#x want %#x", marker, railFocusRGB())
	}
	if right&0xffffff != theme.Current.Selection {
		t.Fatalf("focused cell %#x want %#x", right, theme.Current.Selection)
	}
	if left&0xffffff != railIdleRGB() {
		t.Fatalf("idle cell %#x want %#x", left, railIdleRGB())
	}
}

func TestPaintRailScanoutGeometry(t *testing.T) {
	resetSeatForTest(t)
	if vi.ScanoutWidth != 1280 || vi.ScanoutHeight != 720 {
		t.Fatalf("scanout %dx%d; rail layout assumes 1280x720", vi.ScanoutWidth, vi.ScanoutHeight)
	}
	if RailHeight >= vi.ScanoutHeight {
		t.Fatal("rail covers the whole scanout")
	}
}

func TestSplitHVUnsplitMachine(t *testing.T) {
	resetSeatForTest(t)
	var s TabStrip
	if s.SplitH() || s.SplitV() {
		t.Fatal("split with no tabs")
	}
	s.OpenTab(4, "Calc")
	if s.SplitV() {
		t.Fatal("split with one tab")
	}
	s.OpenTab(5, "Notepad")
	if !s.SplitV() {
		t.Fatal("SplitV")
	}
	if s.Split() != SplitVert {
		t.Fatalf("kind = %s want v", s.Split())
	}
	if s.SplitH() {
		// SplitH from already-two is allowed (retarget the kind).
	}
	if !s.SplitH() {
		t.Fatal("SplitH retarget")
	}
	if s.Split() != SplitHoriz {
		t.Fatalf("kind = %s want h", s.Split())
	}
	if !s.Unsplit() {
		t.Fatal("Unsplit")
	}
	if s.Split() != SplitNone {
		t.Fatal("Unsplit left a kind")
	}
	if s.Unsplit() {
		t.Fatal("Unsplit twice")
	}
	s.SplitV()
	s.CloseTab(4)
	if s.Split() != SplitNone {
		t.Fatal("CloseTab of a pane must unsplit")
	}
}

func TestSplitRectsIntegerAndMin(t *testing.T) {
	resetSeatForTest(t)
	if PaneMinW != 160 || PaneMinH != 120 {
		t.Fatalf("pane min %dx%d want 160x120 (ADR 0033)", PaneMinW, PaneMinH)
	}
	a, b, ok := SplitRects(SplitVert, 1280, 720)
	if !ok {
		t.Fatal("SplitV 1280x720")
	}
	if a != (Rect{0, 0, 640, 720}) || b != (Rect{640, 0, 640, 720}) {
		t.Fatalf("SplitV rects %+v %+v", a, b)
	}
	if a.W+b.W != 1280 || a.H != 720 || b.H != 720 {
		t.Fatal("SplitV does not cover the scanout")
	}
	a, b, ok = SplitRects(SplitHoriz, 1280, 720)
	if !ok {
		t.Fatal("SplitH 1280x720")
	}
	if a != (Rect{0, 0, 1280, 360}) || b != (Rect{0, 360, 1280, 360}) {
		t.Fatalf("SplitH rects %+v %+v", a, b)
	}
	if a.H+b.H != 720 {
		t.Fatal("SplitH does not cover the scanout")
	}
	// Remainder goes to the far pane.
	a, b, ok = SplitRects(SplitVert, 1281, 720)
	if !ok || a.W != 640 || b.W != 641 || a.W+b.W != 1281 {
		t.Fatalf("odd SplitV %+v %+v ok=%v", a, b, ok)
	}
	if _, _, ok = SplitRects(SplitVert, 200, 720); ok {
		t.Fatal("SplitV 200-wide should miss PaneMinW")
	}
	if _, _, ok = SplitRects(SplitHoriz, 1280, 200); ok {
		t.Fatal("SplitH 200-tall should miss PaneMinH")
	}
	fullA, fullB, ok := SplitRects(SplitNone, 1280, 720)
	if !ok || fullA != FullRect(1280, 720) || fullB != fullA {
		t.Fatalf("SplitNone %+v %+v ok=%v", fullA, fullB, ok)
	}
}

func TestLayoutFileBodyTwoPane(t *testing.T) {
	resetSeatForTest(t)
	var s TabStrip
	if !s.OpenTab(3, "Calc") || !s.OpenTab(4, "Notepad") {
		t.Fatal("OpenTab")
	}
	if !s.SplitV() {
		t.Fatal("SplitV")
	}
	_ = s.FocusTab(3)
	body := layoutFileBody(&s, 1280, 720)
	if len(body) == 0 || body[len(body)-1] != '\n' {
		t.Fatalf("must be LF-terminated, got %q", body)
	}
	for _, c := range body {
		if c == '\r' {
			t.Fatal("CR in LAYOUT.txt")
		}
	}
	lines := strings.Split(strings.TrimSuffix(string(body), "\n"), "\n")
	if len(lines) != 2 {
		t.Fatalf("lines = %d want 2: %q", len(lines), body)
	}
	want0 := "tab=3 bin=GOCALC.ELF x=0 y=0 w=640 h=720 focus=1 split=v"
	want1 := "tab=4 bin=NOTE.ELF x=640 y=0 w=640 h=720 focus=0 split=v"
	if lines[0] != want0 || lines[1] != want1 {
		t.Fatalf("got\n %q\n %q\nwant\n %q\n %q", lines[0], lines[1], want0, want1)
	}
	if s.Unsplit() {
		body = layoutFileBody(&s, 1280, 720)
		lines = strings.Split(strings.TrimSuffix(string(body), "\n"), "\n")
		if len(lines) != 2 || lines[0] != "tab=3 bin=GOCALC.ELF x=0 y=0 w=1280 h=720 focus=1 split=none" {
			t.Fatalf("unsplit dump = %q", body)
		}
	}
}

func TestLayoutLineAndDumpMatch(t *testing.T) {
	resetSeatForTest(t)
	line := layoutLine(5, "Calc", Rect{640, 0, 640, 720}, true, SplitVert)
	want := "tab=5 bin=Calc x=640 y=0 w=640 h=720 focus=1 split=v"
	if line != want {
		t.Fatalf("layoutLine = %q want %q", line, want)
	}
	pane := paneLine(5, Rect{640, 0, 640, 720})
	if pane != "id=5 x=640 y=0 w=640 h=720" {
		t.Fatalf("paneLine = %q", pane)
	}
	if line := layoutLine(1, "", FullRect(1280, 720), false, SplitNone); line !=
		"tab=1 bin=- x=0 y=0 w=1280 h=720 focus=0 split=none" {
		t.Fatalf("empty bin / unsplit: %q", line)
	}
	a, b, _ := SplitRects(SplitVert, 1280, 720)
	if !rectsWithin(a, a, 1) || rectsWithin(a, b, 1) {
		t.Fatal("rectsWithin")
	}
	near := Rect{a.X, a.Y, a.W + 1, a.H}
	if !rectsWithin(a, near, 1) {
		t.Fatal("1px w drift must be within tol")
	}
	far := Rect{a.X, a.Y, a.W + 2, a.H}
	if rectsWithin(a, far, 1) {
		t.Fatal("2px w drift must fail tol=1")
	}
}

func TestPaneRectsNeedTwoTabs(t *testing.T) {
	resetSeatForTest(t)
	var s TabStrip
	s.OpenTab(1, "a")
	if _, _, ok := s.PaneRects(1280, 720); ok {
		t.Fatal("one tab must not yield pane rects")
	}
	s.OpenTab(2, "b")
	s.SplitV()
	a, b, ok := s.PaneRects(1280, 720)
	if !ok || a.W != 640 || b.X != 640 {
		t.Fatalf("PaneRects %+v %+v ok=%v", a, b, ok)
	}
}

func TestPinSortsLeftAndSurvivesFocus(t *testing.T) {
	resetSeatForTest(t)
	if FlagPinned != 0x01 {
		t.Fatalf("FlagPinned = %#x want 0x01 (tabcodec.FlagPinned)", FlagPinned)
	}
	var s TabStrip
	s.OpenTab(2, "A")
	s.OpenTab(3, "B")
	s.OpenTab(4, "C")
	_ = s.FocusTab(4) // C
	if !s.Pin(4) {
		t.Fatal("Pin C")
	}
	if s.At(0).ID != 4 || !s.At(0).Pinned {
		t.Fatalf("pinned C must sit at left, got id=%d pin=%v", s.At(0).ID, s.At(0).Pinned)
	}
	if id, _ := s.Focused(); id != 4 {
		t.Fatalf("focus followed C by id, got %d", id)
	}
	if s.Pin(4) {
		t.Fatal("Pin twice")
	}
	_ = s.FocusTab(2)
	if s.At(0).ID != 4 || !s.At(0).Pinned {
		t.Fatal("pin must stay left across FocusTab")
	}
	if line := orderLine(&s); line != "ids=4,2,3 pin=1,0,0 focus=2" {
		t.Fatalf("orderLine = %q", line)
	}
	if !s.Unpin(4) {
		t.Fatal("Unpin")
	}
	if s.At(0).Pinned {
		t.Fatal("unpin left a pin at front")
	}
}

func TestReorderMatchesMoveTab(t *testing.T) {
	resetSeatForTest(t)
	var s TabStrip
	s.OpenTab(2, "A")
	s.OpenTab(3, "B")
	s.OpenTab(4, "C")
	_ = s.FocusTab(3)
	if !s.Reorder(1, 2) {
		t.Fatal("reorder unpinned B and C")
	}
	if s.At(0).ID != 2 || s.At(1).ID != 4 || s.At(2).ID != 3 {
		t.Fatalf("order after 1->2: %d,%d,%d", s.At(0).ID, s.At(1).ID, s.At(2).ID)
	}
	if id, _ := s.Focused(); id != 3 {
		t.Fatalf("focus followed B, got %d", id)
	}
	if !s.Pin(2) {
		t.Fatal("Pin A")
	}
	// Zig move_tab will move a pinned tab; pin-left is not repaired until
	// the next Pin/Unpin (normalize_pinned).
	if !s.Reorder(0, 1) {
		t.Fatal("Reorder of a pinned tab (Zig move_tab)")
	}
	if s.At(0).ID != 4 || s.At(1).ID != 2 || !s.At(1).Pinned {
		t.Fatalf("pinned A moved off the front: %d,%d pin1=%v", s.At(0).ID, s.At(1).ID, s.At(1).Pinned)
	}
	if !s.Unpin(2) {
		t.Fatal("Unpin A")
	}
	if s.At(0).Pinned || s.At(1).Pinned {
		t.Fatal("Unpin must re-partition")
	}
}

func TestClosePinnedIsAllowed(t *testing.T) {
	resetSeatForTest(t)
	var s TabStrip
	s.OpenTab(2, "A")
	s.OpenTab(3, "B")
	s.Pin(2)
	if !s.CloseTab(2) {
		t.Fatal("close pinned")
	}
	if s.Count() != 1 || s.At(0).ID != 3 {
		t.Fatalf("after close pinned: count=%d id=%d", s.Count(), s.At(0).ID)
	}
	if s.At(0).Pinned {
		t.Fatal("remaining tab inherited pin")
	}
}

// M79c (#1706): an unset sash is the legacy tiling — SplitRectsSash with
// sash <= 0 must be byte-identical to SplitRects, gutterless, so every
// pre-M79c gate assertion still holds.
func TestSplitRectsSashUnsetIsLegacy(t *testing.T) {
	resetSeatForTest(t)
	for _, kind := range []SplitKind{SplitVert, SplitHoriz, SplitNone} {
		for _, sash := range []int{0, -5} {
			a, b, oka := SplitRectsSash(kind, 1280, 720, sash)
			c, d, okb := SplitRects(kind, 1280, 720)
			if oka != okb || a != c || b != d {
				t.Fatalf("kind=%s sash=%d: sash %+v %+v ok=%v != legacy %+v %+v ok=%v",
					kind, sash, a, b, oka, c, d, okb)
			}
		}
	}
	// Odd-width remainder still goes to the far pane when unset.
	a, b, ok := SplitRectsSash(SplitVert, 1281, 720, 0)
	if !ok || a.W != 640 || b.W != 641 {
		t.Fatalf("odd unset SplitV %+v %+v ok=%v", a, b, ok)
	}
}

// M79c (#1706): a set sash centres the SashWidth gutter on the clamped
// divider — the visible divider zone, as geometry the gate can grep.
func TestSplitRectsSashGutter(t *testing.T) {
	resetSeatForTest(t)
	if SashWidth != 6 {
		t.Fatalf("SashWidth = %d want 6", SashWidth)
	}
	a, b, ok := SplitRectsSash(SplitVert, 1280, 720, 800)
	if !ok {
		t.Fatal("SplitV sash=800")
	}
	if a != (Rect{0, 0, 797, 720}) || b != (Rect{803, 0, 477, 720}) {
		t.Fatalf("SplitV sash=800: %+v %+v", a, b)
	}
	if a.W+SashWidth+b.W != 1280 {
		t.Fatal("gutter must account every pixel")
	}
	a, b, ok = SplitRectsSash(SplitHoriz, 1280, 720, 500)
	if !ok {
		t.Fatal("SplitH sash=500")
	}
	if a != (Rect{0, 0, 1280, 497}) || b != (Rect{0, 503, 1280, 217}) {
		t.Fatalf("SplitH sash=500: %+v %+v ok=%v", a, b, ok)
	}
	if a.H+SashWidth+b.H != 720 {
		t.Fatal("gutter must account every pixel")
	}
}

// M79c (#1706): the divider clamps into the pane minima — a drag past the
// floor parks at it, and a scanout too small for two minima plus the
// gutter refuses.
func TestSplitRectsSashClamp(t *testing.T) {
	resetSeatForTest(t)
	a, b, ok := SplitRectsSash(SplitVert, 1280, 720, 10)
	if !ok || a.W != 160 || b.X != 166 || b.W != 1280-166 {
		t.Fatalf("clamp low SplitV: %+v %+v ok=%v", a, b, ok)
	}
	a, b, ok = SplitRectsSash(SplitVert, 1280, 720, 2000)
	if !ok || b.W != 160 || a.W != 1117-3 {
		t.Fatalf("clamp high SplitV: %+v %+v ok=%v", a, b, ok)
	}
	a, b, ok = SplitRectsSash(SplitHoriz, 1280, 720, 5)
	if !ok || a.H != 120 || b.Y != 126 {
		t.Fatalf("clamp low SplitH: %+v %+v ok=%v", a, b, ok)
	}
	a, b, ok = SplitRectsSash(SplitHoriz, 1280, 720, 999)
	if !ok || b.H != 120 || a.H != 597-3 {
		t.Fatalf("clamp high SplitH: %+v %+v ok=%v", a, b, ok)
	}
	if _, _, ok = SplitRectsSash(SplitVert, 200, 720, 100); ok {
		t.Fatal("200-wide scanout must refuse even a centred sash")
	}
	if _, _, ok = SplitRectsSash(SplitKind(9), 1280, 720, 640); ok {
		t.Fatal("bogus kind must refuse")
	}
}

// M79c (#1706): SetSash guards and lifecycle. Refused off a split, a
// no-op on the position already held, and reset by Unsplit and by any
// close that drops the strip below two tabs.
func TestSetSashGuards(t *testing.T) {
	resetSeatForTest(t)
	var s TabStrip
	s.OpenTab(3, "Calc")
	if s.SetSash(800, 1280, 720) {
		t.Fatal("one tab must not take a sash")
	}
	s.OpenTab(4, "Notepad")
	if s.SetSash(800, 1280, 720) {
		t.Fatal("unsplit strip must not take a sash")
	}
	if !s.SplitV() {
		t.Fatal("SplitV")
	}
	// A fresh split centres on the midpoint: setting exactly that is the
	// press-on-the-divider release, an honest no-op.
	if s.SetSash(640, 1280, 720) {
		t.Fatal("midpoint set on an unset sash must be a no-op")
	}
	if !s.SetSash(800, 1280, 720) {
		t.Fatal("SetSash 800")
	}
	if s.SetSash(800, 1280, 720) {
		t.Fatal("same position twice must be a no-op")
	}
	if s.sashCenter(1280, 720) != 800 {
		t.Fatalf("centre = %d want 800", s.sashCenter(1280, 720))
	}
	a, b, ok := s.PaneRects(1280, 720)
	if !ok || a.W != 797 || b.X != 803 {
		t.Fatalf("PaneRects follow the sash: %+v %+v ok=%v", a, b, ok)
	}
	if !s.Unsplit() {
		t.Fatal("Unsplit")
	}
	if s.sashCenter(1280, 720) != -1 {
		t.Fatal("Unsplit must clear the sash (no centre off a split)")
	}
	if !s.SplitH() || !s.SetSash(500, 1280, 720) {
		t.Fatal("re-split H + sash")
	}
	if !s.CloseTab(4) {
		t.Fatal("CloseTab")
	}
	if s.Split() != SplitNone || s.sashCenter(1280, 720) != -1 {
		t.Fatal("close below two tabs must clear split and sash")
	}
}

// M79c (#1706): the divider hit test. Half the SashWidth around the
// centre, below the rail, clear of the bottom chrome; SplitNone never hits.
func TestSashZoneAt(t *testing.T) {
	resetSeatForTest(t)
	v := SplitVert
	if !sashZoneAt(640, 100, v, 640, 1280, 720) {
		t.Fatal("dead centre must hit")
	}
	if !sashZoneAt(637, 100, v, 640, 1280, 720) || !sashZoneAt(643, 100, v, 640, 1280, 720) {
		t.Fatal("gutter edges must hit")
	}
	if sashZoneAt(636, 100, v, 640, 1280, 720) || sashZoneAt(644, 100, v, 640, 1280, 720) {
		t.Fatal("outside the gutter must miss")
	}
	if sashZoneAt(640, 10, v, 640, 1280, 720) {
		t.Fatal("the rail owns y < RailHeight, even over the divider")
	}
	if !sashZoneAt(640, 698, v, 640, 1280, 720) {
		t.Fatal("last chrome-clear row must hit")
	}
	if sashZoneAt(640, 699, v, 640, 1280, 720) {
		t.Fatal("bottom chrome must miss")
	}
	if sashZoneAt(2000, 100, v, 640, 1280, 720) {
		t.Fatal("off-scanout x must miss")
	}
	h := SplitHoriz
	if !sashZoneAt(100, 360, h, 360, 1280, 720) {
		t.Fatal("horizontal centre must hit")
	}
	if !sashZoneAt(100, 357, h, 360, 1280, 720) || sashZoneAt(100, 356, h, 360, 1280, 720) {
		t.Fatal("horizontal gutter edges")
	}
	if sashZoneAt(100, 10, h, 360, 1280, 720) {
		t.Fatal("horizontal rail exclusion")
	}
	if sashZoneAt(100, 100, SplitNone, -1, 1280, 720) {
		t.Fatal("SplitNone must never hit")
	}
}

// M79c (#1706): the sash rides into LAYOUT.txt's x=/w= fields — no new
// field (ADR 0033 pins the line format); the pane x/w ARE the sash.
func TestLayoutFileBodySash(t *testing.T) {
	resetSeatForTest(t)
	var s TabStrip
	if !s.OpenTab(3, "Calc") || !s.OpenTab(4, "Notepad") {
		t.Fatal("OpenTab")
	}
	if !s.SplitV() || !s.SetSash(800, 1280, 720) {
		t.Fatal("split + sash")
	}
	_ = s.FocusTab(3)
	body := layoutFileBody(&s, 1280, 720)
	lines := strings.Split(strings.TrimSuffix(string(body), "\n"), "\n")
	if len(lines) != 2 {
		t.Fatalf("lines = %d want 2: %q", len(lines), body)
	}
	want0 := "tab=3 bin=GOCALC.ELF x=0 y=0 w=797 h=720 focus=1 split=v"
	want1 := "tab=4 bin=NOTE.ELF x=803 y=0 w=477 h=720 focus=0 split=v"
	if lines[0] != want0 || lines[1] != want1 {
		t.Fatalf("got\n %q\n %q\nwant\n %q\n %q", lines[0], lines[1], want0, want1)
	}
}

// #3348 (#1255): a restored row still pending its re-exec'd pid's
// declare spins the staged arc sheet in the rail's icon slot — the
// rail's "coming back" mark. The declare returns the static icon, and an
// absent sheet keeps the icon for the whole wait: the pre-card cell.
func TestPaintRailRestoreSpinner(t *testing.T) {
	resetSeatForTest(t)
	savedPend, savedN := reexecPending, reexecN
	savedSpin, savedIcons := railSpinMarked, railIconsMarked
	savedMap, savedRead := iconByBin, readAppsFile
	savedClock := compositeClock
	defer func() {
		reexecPending, reexecN = savedPend, savedN
		railSpinMarked, railIconsMarked = savedSpin, savedIcons
		iconByBin, readAppsFile = savedMap, savedRead
		compositeClock = savedClock
	}()
	art.Reset()
	defer art.Reset()

	readAppsFile = func(string, int) ([]byte, int64) {
		row := "RESTORE.ELF | Restored | r | dock=false | v=2 | icon=ICONTST\n"
		return []byte(row), int64(len(row))
	}
	iconByBin = nil
	iconPix := make([]uint32, 16)
	for i := range iconPix {
		iconPix[i] = 0xff112233
	}
	art.Stage("ICONTST", &draw.BufferCanvas{Pix: iconPix, W: 4, H: 4})
	// A two-frame sheet — uniform 4x4 cells side by side in the single
	// row: the frame read proves which cell the clock picked.
	sheet := make([]uint32, 32)
	for y := 0; y < 4; y++ {
		for x := 0; x < 4; x++ {
			sheet[y*8+x] = 0xff102030
			sheet[y*8+4+x] = 0xff405060
		}
	}
	art.Stage(art.SpinnerArc, &draw.BufferCanvas{Pix: sheet, W: 8, H: 4})

	var s TabStrip
	if !s.OpenTab(sessionIDBase, "restored") || !s.OpenTab(4, "live") {
		t.Fatal("OpenTab")
	}
	s.tabs[0].Bin = "RESTORE.ELF"
	reexecPending[0] = reexecWait{pid: 9, space: 0, id: sessionIDBase}
	reexecN = 1
	railSpinMarked, railIconsMarked = false, false

	const w, h = 200, 40
	scan := make([]byte, w*h*4)
	compositeClock = 0
	paintRail(scan, w, h, RailHeight, &s, -1, -1)
	pix := asUint32(scan)
	// Cell 0's icon slot opens at x=7,y=3 on the 22px rail.
	slot := func(col, row int) uint32 { return pix[(3+row)*w+7+col] }
	if got := slot(8, 8); got != 0xff102030 {
		t.Fatalf("pending cell painted %#08x, want spinner frame 0", got)
	}
	// The live cell's slot keeps its cell fill — no sheet pixels there.
	if got := pix[(3+8)*w+107+8]; got == 0xff102030 || got == 0xff405060 {
		t.Fatal("a live cell painted spinner pixels")
	}
	if !railSpinMarked {
		t.Fatal("the spinner decode marker never latched")
	}

	// The clock drives the frame: the next tick paints the second cell.
	compositeClock = 1
	paintRail(scan, w, h, RailHeight, &s, -1, -1)
	if got := slot(8, 8); got != 0xff405060 {
		t.Fatalf("pending cell painted %#08x, want spinner frame 1", got)
	}

	// The declare's adoption ends the wait — the pre-card icon returns.
	reexecN = 0
	reexecPending[0] = reexecWait{}
	paintRail(scan, w, h, RailHeight, &s, -1, -1)
	if got := slot(8, 8); got != 0xff112233 {
		t.Fatalf("adopted cell painted %#08x, want the staged icon", got)
	}

	// An absent sheet keeps the icon for the whole wait — the degrade.
	art.Reset()
	art.Stage("ICONTST", &draw.BufferCanvas{Pix: iconPix, W: 4, H: 4})
	reexecPending[0] = reexecWait{pid: 9, space: 0, id: sessionIDBase}
	reexecN = 1
	paintRail(scan, w, h, RailHeight, &s, -1, -1)
	if got := slot(8, 8); got != 0xff112233 {
		t.Fatalf("degraded pending cell painted %#08x, want the icon", got)
	}
}

// --- M216b (#3941): railLayout, titles, the pinned mark -----------------

// railLayout is the only cell-width rule: avail/n floored at 48, the
// last cell clamped to avail, cells a prefix of the strip. Single-space
// geometry is the old full-width rule byte for byte.
func TestRailLayoutSingleSpace(t *testing.T) {
	resetSeatForTest(t)
	defer saveSeatState()()
	resetSpaces()
	for _, width := range []int{1280, 640, 300, 200, 97, 64} {
		for n := 1; n <= 8; n++ {
			l := railLayout(width, n, RailHeight)
			if l.avail != width {
				t.Fatalf("w=%d n=%d avail=%d want %d", width, n, l.avail, width)
			}
			cellW := width / n
			if cellW < 48 {
				cellW = 48
			}
			want := 0
			for i := 0; i < n; i++ {
				x := i * cellW
				if x >= width {
					break
				}
				w := cellW
				if x+w > width {
					w = width - x
				}
				if w <= 1 {
					break
				}
				want = i + 1
				if r := l.rect(i); r.X != x || r.W != w || r.Y != 0 || r.H != RailHeight {
					t.Fatalf("w=%d n=%d cell %d = %+v want x=%d w=%d", width, n, i, r, x, w)
				}
			}
			if l.n != want {
				t.Fatalf("w=%d n=%d cells=%d want %d", width, n, l.n, want)
			}
		}
	}
}

// With spaces up, the cell lane ends at the leftmost pill's edge — no
// cell rect may reach into the pill strip, and avail shrinks exactly by
// the strip's width.
func TestRailLayoutPillsReserve(t *testing.T) {
	resetSeatForTest(t)
	defer saveSeatState()()
	stubSeatRegistry(t, nil)
	resetSpaces()
	for _, n := range []int{1, 2, 4, 8} {
		for _, spacesN := range []int{2, 3, 4} {
			spaceN, spaceCur = spacesN, 0
			width := 1280
			l := railLayout(width, n, RailHeight)
			pills, pn := spacePillRects(width, RailHeight)
			if pn != spacesN {
				t.Fatalf("spacesN=%d pills=%d", spacesN, pn)
			}
			minX := width
			for i := 0; i < pn; i++ {
				if pills[i].W > 0 && pills[i].X < minX {
					minX = pills[i].X
				}
			}
			if l.avail != minX {
				t.Fatalf("n=%d spaces=%d avail=%d want pill edge %d", n, spacesN, l.avail, minX)
			}
			for i := 0; i < l.n; i++ {
				r := l.rect(i)
				if r.X+r.W > l.avail {
					t.Fatalf("n=%d spaces=%d cell %d reaches %d past avail %d", n, spacesN, i, r.X+r.W, l.avail)
				}
			}
			// Every pill rect sits right of the lane.
			for i := 0; i < pn; i++ {
				if pills[i].W > 0 && pills[i].X < l.avail {
					t.Fatalf("pill %d at x=%d overlaps the cell lane ending %d", i, pills[i].X, l.avail)
				}
			}
		}
	}
	// One space: byte-identical to the pre-card rule.
	spaceN, spaceCur = 1, 0
	l := railLayout(1280, 4, RailHeight)
	if l.avail != 1280 || l.n != 4 || l.rect(3).X != 960 || l.rect(3).W != 320 {
		t.Fatalf("single-space layout drifted: %+v", l)
	}
}

// Paint and hit-test agree per pixel column: inside the lane every
// column resolves to the cell whose rect covers it; the remainder and
// pill-strip tail resolve to the last painted cell; the close zone is
// exactly each rect's rightmost railCloseW px.
func TestRailCellHitTestAgreesWithLayout(t *testing.T) {
	resetSeatForTest(t)
	defer saveSeatState()()
	stubSeatRegistry(t, nil)
	resetSpaces()
	for _, spacesN := range []int{1, 3} {
		spaceN, spaceCur = spacesN, 0
		for _, width := range []int{1280, 640, 300, 200} {
			for n := 1; n <= 8; n++ {
				l := railLayout(width, n, RailHeight)
				if l.n == 0 {
					continue
				}
				for px := 0; px < width; px++ {
					got, ok := railCellAt(uint32(px), 10, width, n, RailHeight)
					if !ok {
						t.Fatalf("spaces=%d w=%d n=%d px=%d: rail cell missed", spacesN, width, n, px)
					}
					want := l.n - 1
					for i := 0; i < l.n; i++ {
						r := l.rect(i)
						if px >= r.X && px < r.X+r.W {
							want = i
						}
					}
					if got != want {
						t.Fatalf("spaces=%d w=%d n=%d px=%d hit %d want %d", spacesN, width, n, px, got, want)
					}
				}
				// The close zone is the painted rect's rightmost
				// railCloseW px — probe both edges of every cell.
				for i := 0; i < l.n; i++ {
					r := l.rect(i)
					if r.W < railCloseW {
						continue
					}
					z0, z1 := r.X+r.W-railCloseW, r.X+r.W
					if got, ok := railCloseZoneAt(uint32(z0), 10, width, n, RailHeight); !ok || got != i {
						t.Fatalf("spaces=%d w=%d n=%d cell %d zone start hit %d ok=%v", spacesN, width, n, i, got, ok)
					}
					if got, ok := railCloseZoneAt(uint32(z1-1), 10, width, n, RailHeight); !ok || got != i {
						t.Fatalf("spaces=%d w=%d n=%d cell %d zone end hit %d ok=%v", spacesN, width, n, i, got, ok)
					}
					if z0 > 0 {
						if _, ok := railCloseZoneAt(uint32(z0-1), 10, width, n, RailHeight); ok {
							t.Fatalf("spaces=%d w=%d n=%d cell %d: px %d left of zone hit", spacesN, width, n, i, z0-1)
						}
					}
				}
			}
		}
	}
}

// The title run: first glyph after the icon slot, InkMuted idle / Ink
// focused, clipped before the close zone, skipped when the cell cannot
// hold three glyphs, seatFitText-truncated when it cannot hold the run.
func TestPaintRailTitles(t *testing.T) {
	resetSeatForTest(t)
	tok := theme.Current
	theme.Current = theme.Dark
	t.Cleanup(func() { theme.Current = tok })
	stubSeatRegistry(t, nil)

	const w, h = 1280, 32
	var s TabStrip
	if !s.OpenTab(3, "Edit") || !s.OpenTab(4, "Term") {
		t.Fatal("OpenTab")
	}
	// Focus moves to cell 1: its title inks Ink, cell 0's InkMuted.
	if !s.FocusTab(4) {
		t.Fatal("FocusTab")
	}
	buf := make([]byte, w*h*4)
	paintRail(buf, w, h, RailHeight, &s, -1, -1)
	pix := asUint32(buf)
	at := func(x, y int) uint32 { return pix[y*w+x] & 0xffffff }
	// Cell 0 "Edit": contentX=7, icon slot reserved (w=640 >= 40), so
	// the run starts at x=27, the 8px bitmap band at y=7. 'E' row 0 is
	// 0x7f — pixel (27,7) is lit InkMuted.
	if got := at(27, 7); got != theme.Current.InkMuted {
		t.Fatalf("idle title first px = %#06x want InkMuted %#06x", got, theme.Current.InkMuted)
	}
	// Cell 1 "Term" at x=640: 'T' at (667,7), focused Ink.
	if got := at(667, 7); got != theme.Current.Ink {
		t.Fatalf("focused title first px = %#06x want Ink %#06x", got, theme.Current.Ink)
	}
	// The run stops before the close zone: cell 0's zone starts at
	// 640-16=624 — "Edit" ends at x=59, far clear; a LONG title proves
	// the clip instead.
	var s2 TabStrip
	if !s2.OpenTab(5, "A-Very-Long-Tab-Title-IndeedaLongOne") {
		t.Fatal("OpenTab")
	}
	buf2 := make([]byte, w*h*4)
	paintRail(buf2, w, h, RailHeight, &s2, -1, -1)
	pix2 := asUint32(buf2)
	lit := 0
	for y := 7; y < 15; y++ {
		for x := 624; x < 640; x++ {
			if pix2[y*w+x]&0xffffff == theme.Current.Ink {
				lit++
			}
		}
	}
	if lit != 0 {
		t.Fatalf("title glyphs leaked %d px into the close zone", lit)
	}
	// A cell too narrow for three glyphs draws no title: w=64 cell,
	// titleX=27, clipRight=64-16=48 -> maxW=21 < probe 24.
	const nw = 64
	var s3 TabStrip
	if !s3.OpenTab(6, "Edit") {
		t.Fatal("OpenTab")
	}
	buf3 := make([]byte, nw*h*4)
	paintRail(buf3, nw, h, RailHeight, &s3, -1, -1)
	pix3 := asUint32(buf3)
	for y := 7; y < 15; y++ {
		for x := 27; x < 48; x++ {
			if pix3[y*nw+x]&0xffffff == theme.Current.Ink {
				t.Fatalf("narrow cell painted a title px at %d,%d", x, y)
			}
		}
	}
}

// seatFitText truncates the run; railTitleProbe is the three-glyph
// floor the skip rule measures against.
func TestRailTitleFitAndProbe(t *testing.T) {
	resetSeatForTest(t)
	stubSeatRegistry(t, nil)
	if got := railTitleProbe("Edit"); got != 24 {
		t.Fatalf("probe(Edit) = %d want 24 (3 glyphs x 8px)", got)
	}
	if got := railTitleProbe("AB"); got != 16 {
		t.Fatalf("probe(AB) = %d want 16 (short title probes itself)", got)
	}
	if got := seatFitText("AVeryLongTabTitle", 40, seatTextPx); got != "AV…" {
		t.Fatalf("seatFitText(40) = %q want %q", got, "AV…")
	}
	if got := seatFitText("Edit", 200, seatTextPx); got != "Edit" {
		t.Fatalf("seatFitText(200) = %q want untruncated", got)
	}
}

// The pinned mark is the design master's rail_pin_0 diamond — Muted
// ink at the leading slot — and it displaces the icon/title lane right.
func TestPaintRailPinnedMark(t *testing.T) {
	resetSeatForTest(t)
	tok := theme.Current
	theme.Current = theme.Dark
	t.Cleanup(func() { theme.Current = tok })
	stubSeatRegistry(t, nil)

	const w, h = 1280, 32
	var s TabStrip
	if !s.OpenTab(3, "Edit") || !s.OpenTab(4, "Term") {
		t.Fatal("OpenTab")
	}
	if !s.Pin(3) {
		t.Fatal("Pin")
	}
	buf := make([]byte, w*h*4)
	paintRail(buf, w, h, RailHeight, &s, -1, -1)
	pix := asUint32(buf)
	at := func(x, y int) uint32 { return pix[y*w+x] & 0xffffff }
	// Pinned tabs sort left: cell 0 is id 3. Diamond rect is
	// [7,17) x [7,15), centre (12,11).
	if got := at(12, 11); got != theme.Current.Muted {
		t.Fatalf("pin centre = %#06x want Muted %#06x", got, theme.Current.Muted)
	}
	// The diamond fills its inscribed box, not just the centre line.
	mark := 0
	for y := 8; y < 15; y++ {
		for x := 8; x < 17; x++ {
			if at(x, y) == theme.Current.Muted {
				mark++
			}
		}
	}
	if mark < 12 {
		t.Fatalf("pin diamond painted only %d Muted px, want a filled rhombus", mark)
	}
	// The diamond's bounding box is only 10x8 — (6,11) and (18,11) stay
	// cell face, not mark.
	if got := at(6, 11); got == theme.Current.Muted {
		t.Fatal("pin mark painted outside its 10px slot")
	}
	// Unpinned cell 1 at x=640: same probe coords hold no diamond.
	if got := at(652, 11); got == theme.Current.Muted {
		t.Fatal("unpinned cell painted a pin mark")
	}
	// The mark displaced the lane: pinned cell's 'E' sits at titleX
	// 7+10+4+16+4 = 41, not the unpinned 27.
	if got := at(41, 7); got != theme.Current.Ink {
		t.Fatalf("pinned title first px = %#06x want Ink %#06x", got, theme.Current.Ink)
	}
	if got := at(27, 7); got == theme.Current.Ink && got == theme.Current.InkMuted {
		t.Fatal("pinned title painted at the unpinned origin")
	}
}

// M216e (#3944): the kind-20 unsaved bit rides the tab row — SetUnsaved
// records transitions only (a replayed mirror is a no-op), Peek reads the
// row the close path is about to lose, and the bit never enters the
// session file's shape (tabFlags has no arm for it).
func TestSetUnsavedTransitions(t *testing.T) {
	resetSeatForTest(t)
	var s TabStrip
	if !s.OpenTab(3, "Edit") {
		t.Fatal("OpenTab")
	}
	if s.At(0).Unsaved {
		t.Fatal("a fresh tab opened dirty")
	}
	if !s.SetUnsaved(3, true) {
		t.Fatal("SetUnsaved on")
	}
	if s.SetUnsaved(3, true) {
		t.Fatal("a replayed on-mirror reported a transition")
	}
	if s.SetUnsaved(9, true) || s.SetUnsaved(9, false) {
		t.Fatal("SetUnsaved on a missing id reported a transition")
	}
	if !s.SetUnsaved(3, false) {
		t.Fatal("SetUnsaved off")
	}
	if s.SetUnsaved(3, false) {
		t.Fatal("a replayed off-mirror reported a transition")
	}
	// Peek hands the close path the row before CloseTab shifts it out.
	if !s.SetUnsaved(3, true) {
		t.Fatal("SetUnsaved on again")
	}
	p, ok := s.Peek(3)
	if !ok || !p.Unsaved || p.Title != "Edit" {
		t.Fatalf("Peek = %+v ok=%v", p, ok)
	}
	if _, ok := s.Peek(9); ok {
		t.Fatal("Peek on a missing id")
	}
	// The session flags byte has no unsaved arm — tabFlags stays 0/1/3.
	if f := tabFlags(s.At(0)); f != 0 {
		t.Fatalf("tabFlags = %#x want 0 — Unsaved must not persist", f)
	}
}

// M216e (#3944): the rail's dirty dot — Zig's 4x4 accent square just
// left of the close-x (tabwm.zig:3326), painted on the unsaved cell only
// and clipped out of the title run's way.
func TestPaintRailUnsavedDot(t *testing.T) {
	resetSeatForTest(t)
	tok := theme.Current
	theme.Current = theme.Dark
	t.Cleanup(func() { theme.Current = tok })
	stubSeatRegistry(t, nil)

	const w, h = 200, 40
	var s TabStrip
	if !s.OpenTab(3, "Edit") {
		t.Fatal("OpenTab")
	}
	clean := make([]byte, w*h*4)
	paintRail(clean, w, h, RailHeight, &s, -1, -1)
	cleanPix := asUint32(clean)

	slot, ok := railUnsavedSlot(0, w, RailHeight)
	if !ok {
		t.Fatal("railUnsavedSlot refused a 200px cell")
	}
	if slot.W != railUnsavedW || slot.H != railUnsavedW {
		t.Fatalf("dot slot = %+v want %dx%d", slot, railUnsavedW, railUnsavedW)
	}
	// The slot sits left of the close zone's left edge.
	if right := railContentRight(0, w, RailHeight); slot.X+slot.W > right {
		t.Fatalf("dot right edge %d crosses the close bound %d", slot.X+slot.W, right)
	}
	for y := slot.Y; y < slot.Y+slot.H; y++ {
		for x := slot.X; x < slot.X+slot.W; x++ {
			if cleanPix[y*w+x]&0xffffff == theme.Current.Accent {
				t.Fatalf("clean cell painted accent at (%d,%d)", x, y)
			}
		}
	}

	if !s.SetUnsaved(3, true) {
		t.Fatal("SetUnsaved")
	}
	dirty := make([]byte, w*h*4)
	paintRail(dirty, w, h, RailHeight, &s, -1, -1)
	dirtyPix := asUint32(dirty)
	lit := 0
	for y := slot.Y; y < slot.Y+slot.H; y++ {
		for x := slot.X; x < slot.X+slot.W; x++ {
			if dirtyPix[y*w+x]&0xffffff == theme.Current.Accent {
				lit++
			}
		}
	}
	if lit != railUnsavedW*railUnsavedW {
		t.Fatalf("unsaved dot painted %d accent px, want %d", lit, railUnsavedW*railUnsavedW)
	}
}
