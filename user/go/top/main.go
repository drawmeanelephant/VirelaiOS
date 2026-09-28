// Command top is GOTOP.ELF — the M71g (issue #1566) Go successor to Zig
// TOP.BIN, with SYSTEM MONITOR's one-job remit folded into it: a live process
// list with click-to-kill, and the network counters view. Zig's TOP.BIN and
// SYSMON.BIN are deleted by the same card; after it the launcher's dock has
// one row for "see what is running".
//
// The marker vocabulary is TOP's own (`top: ...`) ON PURPOSE: twelve class-B
// specs assert it and the vocabulary is the CONTRACT, not the binary — the
// same discipline M66c used when `note:` outlived the Zig notepad, and the same
// reason `gofiles:` kept FILE.BIN's shape. Every marker below is printed only
// AFTER its syscall returned, so no assert can pass on a program that never ran.
//
// What GOTOP deliberately does NOT port, and why (measured, not assumed):
//   - TOP's text FILTER and its per-column header sorting both rode on Zig's
//     ui.TextInput widget. The Go widget kit is exactly Text/Button/List
//     (widgets.go), so there is no text-entry control to port the filter onto.
//     Sorting stays (click the header / press 1-4) because it needs no input
//     widget; the filter is retired with the widget, and C8's live coverage was
//     never a gate assertion (it was top.zig's host tests).
//   - The CPU/memory bar. `sys_procs` carries no CPU or memory figure and no
//     cheap Go seam exposes one, so the summary line reports what the snapshot
//     actually knows: row, running and exited counts.
//
// Kill IS the live proof (the card's D2): `k`, or the Kill button, arms the
// selected pid through slot 29, and the kernel converts it into the reserved
// status 137. A refusal (cross-principal EACCES) is printed, never hidden.
package main

import (
	"virelai/draw"
	"virelai/tabapp"
	"virelai/theme"
	"virelai/vi"
	"virelai/vsys"
	"virelai/widgets"
)

const (
	appName    = "GOTOP.ELF"
	appTitle   = "Top"
	winX       = 40 // TOP.BIN's presentation rect, kept: the WM specs move
	winY       = 40 // and sample GOTOP where they moved and sampled TOP
	natW       = 512
	natH       = 384
	exitStatus = 43 // TOP's exit status, kept (its markers say 43)

	autoRefreshTicks = 12 // ≈1 s on VZ (top.zig auto_refresh_ticks)

	markerOpen       = "top: open id="
	markerTabAware   = "top: tab-aware (full-viewport)"
	markerTabShy     = "top: not-tab-aware (shim or WND desktop)"
	markerReady      = "top: ready"
	markerProcs      = "top: tab=procs"
	markerNetwork    = "top: tab=network"
	markerRefreshed  = "top: refreshed ok"
	markerKill       = "top: kill pid="
	markerCount      = "top: procs n="
	markerRow        = "top: row pid="
	markerResize     = "top: resize relayout"
	markerClose      = "top: win_close"
	markerExit       = "top: exiting 43"
	markerRefreshErr = "top: procs err="

	// rowMarkerMax bounds the per-row serial lines one refresh prints. The
	// snapshot itself is capped at maxProcs (16), so this is belt-and-braces
	// against a refresh storm flooding the serial log.
	rowMarkerMax = 16
)

// Key codes (ADR 0009): arrows carry their code in Arg0, characters in Arg1.
const (
	keyDownArrow = 0x51
	keyUpArrow   = 0x52
	keyOne       = '1'
	keyTwo       = '2'
	keyThree     = '3'
	keyFour      = '4'
)

// page is the active tab.
type page int

const (
	pageProcs page = iota
	pageNetwork
)

// Native layout (top.zig's native_* rects, so GOTOP opens where TOP opened).
var (
	natRefresh = widgets.Rect{X: 6, Y: 6, W: 54, H: 20}
	natKill    = widgets.Rect{X: 64, Y: 6, W: 38, H: 20}
	natAuto    = widgets.Rect{X: 106, Y: 6, W: 42, H: 20}
	natProcs   = widgets.Rect{X: 152, Y: 6, W: 44, H: 20}
	natNetwork = widgets.Rect{X: 200, Y: 6, W: 38, H: 20}
	natSummary = widgets.Rect{X: 296, Y: 6, W: 210, H: 20}
	natHeader  = widgets.Rect{X: 6, Y: 52, W: 500, H: 16}
	natList    = widgets.Rect{X: 6, Y: 70, W: 500, H: 256}
	natFoot    = widgets.Rect{X: 6, Y: 330, W: 500, H: 18}
	natRowH    = 16
)

type app struct {
	ta   *tabapp.TabApp
	tab  *Table
	net  *NetView
	pg   page
	auto bool

	items  []string
	status string

	refreshBtn widgets.Button
	killBtn    widgets.Button
	autoBtn    widgets.Button
	procsBtn   widgets.Button
	netBtn     widgets.Button
	summary    widgets.Text
	header     widgets.Text
	foot       widgets.Text
	list       widgets.List
}

func loadGuestTheme() {
	b, r := vi.ReadFileAll("/host/SETTINGS.TXT", 2048)
	if r < 0 || b == nil {
		return
	}
	_ = theme.ApplySettings(b)
}

func main() {
	loadGuestTheme()

	ta := tabapp.Init(tabapp.Config{Name: appName, Title: appTitle, X: winX, Y: winY, W: natW, H: natH})
	if ta == nil {
		vi.ConsoleLine("top: failed to open window")
		vi.Exit(1)
	}
	// The REAL window id, not TOP.BIN's hardcoded "id=3": the id is whatever
	// the host WM assigned, and a literal would make the marker a lie.
	vi.ConsoleLine(markerOpen + vi.Itoa64(int64(ta.Win)))
	if ta.TabAware {
		vi.ConsoleLine(markerTabAware)
	} else {
		vi.ConsoleLine(markerTabShy)
	}

	a := &app{
		ta:  ta,
		tab: NewTable(),
		net: NewNetView(),
		pg:  pageProcs,
	}
	a.refreshAll(true, false)

	a.layout()
	a.draw()
	a.ta.Present()
	vi.ConsoleLine(markerReady)

	for {
		ev, r, ok := vi.PollEventRaw()
		if !ok {
			if r < 0 {
				break
			}
			vi.Sleep(1)
			continue
		}
		switch a.ta.Dispatch(ev) {
		case tabapp.ActionClosed:
			vi.ConsoleLine(markerClose)
			if a.auto {
				vsys.TimerCancel()
			}
			vi.ConsoleLine(markerExit)
			a.ta.CloseAndExit(exitStatus)
		case tabapp.ActionResized:
			vi.ConsoleLine(markerResize)
			a.layout()
			a.draw()
			a.ta.Present()
		case tabapp.ActionNone:
			if a.handle(ev) {
				a.layout()
				a.draw()
				a.ta.Present()
			}
		}
	}
}

// refreshAll takes one process + network sample and rebuilds the view.
//
// rows prints the per-row serial lines (the startup listing and an explicit
// refresh do; the 1 Hz timer does not, so an idle session leaves a bounded
// log). announce prints `top: refreshed ok`, and ONLY an explicit refresh
// does: a boot's startup listing must not satisfy a spec that asserts the
// marker as proof the refresh REQUEST was served (live-n4-top-net gates its
// `r` chord on it — TOP's startup was silent for exactly this reason).
func (a *app) refreshAll(rows, announce bool) {
	n, rc := a.tab.Refresh()
	if rc < 0 {
		a.status = "procs err"
		vi.ConsoleLine(markerRefreshErr + vi.Itoa64(rc))
		return
	}
	a.net.Refresh()
	running, exited, _ := a.tab.Counts()
	vi.ConsoleLine(markerCount + vi.Itoa64(int64(n)) +
		" running=" + vi.Itoa64(int64(running)) +
		" exited=" + vi.Itoa64(int64(exited)))
	if rows {
		a.printRows()
	}
	a.status = "n=" + vi.Itoa64(int64(n)) + " run=" + vi.Itoa64(int64(running))
	if announce {
		vi.ConsoleLine(markerRefreshed)
	}
	a.rebuildItems()
}

// printRows emits one bounded marker per visible row — the serial evidence
// that the list really holds the kernel's table (and, after a kill, that the
// target was armed).
func (a *app) printRows() {
	n := a.tab.Len()
	if n > rowMarkerMax {
		n = rowMarkerMax
	}
	for i := 0; i < n; i++ {
		p, ok := a.tab.Row(i)
		if !ok {
			break
		}
		line := markerRow + vi.Itoa64(int64(p.PID)) + " name=" + p.Name + " state=" + StateName(p.State)
		if p.State == vi.ProcExited {
			line += " exit=" + vi.Itoa64(int64(p.Exit))
		}
		vi.ConsoleLine(line)
	}
}

// rebuildItems refreshes the list widget's labels from the table.
func (a *app) rebuildItems() {
	a.items = a.tab.Labels()
}

// layout re-derives every widget rect for the current canvas.
func (a *app) layout() {
	ta := a.ta
	t := theme.Current
	scaleR := func(r widgets.Rect) widgets.Rect {
		s := ta.Layout(tabapp.Rect{X: r.X, Y: r.Y, W: r.W, H: r.H}, natW, natH)
		return widgets.Rect{X: s.X, Y: s.Y, W: s.W, H: s.H}
	}
	scaleH := func(h int) int {
		s := ta.Layout(tabapp.Rect{X: 0, Y: 0, W: 1, H: h}, natW, natH)
		if s.H < 1 {
			return 1
		}
		return s.H
	}

	a.refreshBtn = widgets.Button{R: scaleR(natRefresh), Label: "Refresh"}
	a.killBtn = widgets.Button{R: scaleR(natKill), Label: "Kill", Face: t.Danger, LabelRGB: t.OnAccent}
	a.autoBtn = widgets.Button{R: scaleR(natAuto), Label: "Auto", Pressed: a.auto}
	a.procsBtn = widgets.Button{R: scaleR(natProcs), Label: "Procs"}
	a.netBtn = widgets.Button{R: scaleR(natNetwork), Label: "Net"}
	if a.pg == pageProcs {
		a.procsBtn.Face = t.Accent
		a.procsBtn.LabelRGB = t.OnAccent
	} else {
		a.netBtn.Face = t.Accent
		a.netBtn.LabelRGB = t.OnAccent
	}

	a.summary = widgets.Text{R: scaleR(natSummary), Label: a.status, Fg: t.Muted, Bg: t.Bg}
	a.header = widgets.Text{R: scaleR(natHeader), Label: a.tab.Header(), Fg: t.Accent, Bg: t.Surface}
	a.foot = widgets.Text{R: scaleR(natFoot), Label: a.footLine(), Fg: t.Muted, Bg: t.Bg}

	items := a.items
	if a.pg == pageNetwork {
		items = a.net.Lines()
	}
	sel := a.tab.SelIndex()
	if a.pg == pageNetwork {
		sel = -1
	}
	a.list = widgets.List{
		R:     scaleR(natList),
		Items: items,
		RowH:  scaleH(natRowH),
		Sel:   sel,
		Fg:    t.Text,
		Bg:    t.Bg,
		SelBg: t.Selection,
	}
}

// footLine is the keyboard hint / status line.
func (a *app) footLine() string {
	if a.pg == pageNetwork {
		return "n procs   r refresh   a auto   rx/s " + vi.Itoa64(int64(a.net.RXRate)) +
			" B   tx/s " + vi.Itoa64(int64(a.net.TXRate)) + " B"
	}
	return "k kill   r refresh   a auto   n net   1-4 sort"
}

func (a *app) draw() {
	var f vi.Filler
	f.Rect(a.ta.Win, 0, 0, a.ta.W, a.ta.H, theme.Current.Bg)
	cv := &draw.FillerCanvas{Filler: &f, WindowID: a.ta.Win}

	a.summary.Draw(cv)
	a.procsBtn.Draw(cv)
	a.netBtn.Draw(cv)
	a.refreshBtn.Draw(cv)
	a.killBtn.Draw(cv)
	a.autoBtn.Draw(cv)
	if a.pg == pageProcs {
		a.header.Draw(cv)
	}
	a.list.Draw(cv)
	a.foot.Draw(cv)
	f.Flush()
}

// setPage switches tabs and reports it.
func (a *app) setPage(p page) bool {
	if a.pg == p {
		return false
	}
	a.pg = p
	if p == pageNetwork {
		a.net.Refresh()
		vi.ConsoleLine(markerNetwork)
	} else {
		vi.ConsoleLine(markerProcs)
	}
	return true
}

// toggleAuto arms or disarms the 1 Hz app timer (slot 40/41).
func (a *app) toggleAuto() {
	a.auto = !a.auto
	if a.auto {
		vsys.TimerSet(autoRefreshTicks)
	} else {
		vsys.TimerCancel()
	}
}

// killSelected arms the highlighted process through slot 29. The marker is
// printed after the syscall returned and carries the kernel's own error code,
// so a refusal is evidence rather than silence (live-trust-caps pins -7).
func (a *app) killSelected() bool {
	p, ok := a.tab.Selected()
	if !ok {
		return false
	}
	rc := vi.Kill(p.PID)
	if rc == 0 {
		vi.ConsoleLine(markerKill + vi.Itoa64(int64(p.PID)))
		a.refreshAll(true, true)
	} else {
		vi.ConsoleLine(markerKill + vi.Itoa64(int64(p.PID)) + " err=" + vi.Itoa64(rc))
	}
	return true
}

// handle routes one non-lifecycle event. It reports whether the frame changed.
func (a *app) handle(ev vi.Event) bool {
	switch ev.Kind {
	case vi.EvTimer:
		if !a.auto {
			return false
		}
		a.refreshAll(false, false)
		vsys.TimerSet(autoRefreshTicks) // one timer in flight, never a queue
		return true
	case vi.EvKeyDown:
		return a.handleKey(ev)
	case vi.EvMouseDown:
		if ev.Flags&vi.BtnLeft == 0 {
			return false
		}
		return a.handleClick(int(ev.Arg0), int(ev.Arg1))
	}
	return false
}

func (a *app) handleKey(ev vi.Event) bool {
	switch ev.Arg0 {
	case keyUpArrow:
		return a.tab.MoveBy(-1)
	case keyDownArrow:
		return a.tab.MoveBy(1)
	}
	switch ev.Arg1 {
	case 'k', 'K':
		return a.killSelected()
	case 'r', 'R':
		a.refreshAll(true, true)
		return true
	case 'a', 'A':
		a.toggleAuto()
		return true
	case 'n', 'N':
		return a.setPage(pageNetwork)
	case 'p', 'P':
		return a.setPage(pageProcs)
	case keyOne:
		a.tab.SortBy(ColPID)
		return true
	case keyTwo:
		a.tab.SortBy(ColName)
		return true
	case keyThree:
		a.tab.SortBy(ColState)
		return true
	case keyFour:
		a.tab.SortBy(ColExit)
		return true
	}
	return false
}

func (a *app) handleClick(x, y int) bool {
	if a.procsBtn.HitTest(x, y) {
		return a.setPage(pageProcs)
	}
	if a.netBtn.HitTest(x, y) {
		return a.setPage(pageNetwork)
	}
	if a.refreshBtn.HitTest(x, y) {
		a.refreshAll(true, true)
		return true
	}
	if a.killBtn.HitTest(x, y) {
		return a.killSelected()
	}
	if a.autoBtn.HitTest(x, y) {
		a.toggleAuto()
		return true
	}
	if a.pg == pageProcs {
		// Header click sorts on the column under the pointer (TOP's C8
		// behaviour), scaled with the same rect the header is drawn in.
		if a.header.HitTest(x, y) {
			a.tab.SortBy(a.columnAt(x, a.header.R))
			return true
		}
		if i := a.list.ItemAt(x, y); i >= 0 {
			return a.tab.SelectRow(i)
		}
	}
	return false
}

// columnAt maps an x inside the header to a column: PID | Name | State | Exit,
// using the same widths the row label pads to, so the header and the rows
// cannot disagree about where a column starts.
func (a *app) columnAt(x int, header widgets.Rect) Column {
	rel := x - header.X
	// cell = 6 px per glyph; "PID"(3) + 2 + Name(14) + 1 + State(7) + 1
	const cell = 6
	pidEnd := 3 * cell
	nameEnd := (3 + 2 + 14) * cell
	stateEnd := (3 + 2 + 14 + 1 + 7) * cell
	switch {
	case rel < pidEnd:
		return ColPID
	case rel < nameEnd:
		return ColName
	case rel < stateEnd:
		return ColState
	}
	return ColExit
}
