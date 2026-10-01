// GOTABWM.ELF — the pointer-first Apps menu and Ctrl+Space share one
// APPS.TXT catalog, layout, exec path and seat-owned input sink.
//
// M82a (#1768): the row the manifest gives is the row that launches. The
// `argv=` field's fixed arguments ride the exec, and the decode receipt says
// what the manifest said — how many rows, the highest row version, and how
// many rows spoke each trailing key (a zero is reported, not hidden).
package main

import (
	"unsafe"

	"virelai/theme"
	"virelai/vi"
)

const (
	MarkerLaunchOpen    = "gotabwm: launcher open n="
	MarkerLaunchFilter  = "gotabwm: launcher filter q="
	MarkerLaunchExec    = "gotabwm: launcher exec "
	MarkerLaunchDismiss = "gotabwm: launcher dismiss"
	MarkerLaunchMissing = "gotabwm: launcher missing "
	MarkerAppsDecode    = "gotabwm: apps decode n="

	hidUsageSpace  uint8 = 0x2C
	hidUsageEnter  uint8 = 0x28
	hidUsageEscape uint8 = 0x29
	hidUsageBksp   uint8 = 0x2A
	hidUsageDown   uint8 = 0x51
	hidUsageUp     uint8 = 0x52

	launchX    = 240
	launchY    = 80
	launchW    = 800
	launchHdr  = 36
	launchMaxH = 520
	filterMax  = 24
)

type launcherState struct {
	open      bool
	catalog   []AppEntry
	filter    string
	filtered  []int
	sel       int
	first     int // first visible filtered row
	reasons   []string
	err       string
	sink      uint32
	prior     uint32
	painted   bool
	width     int
	height    int
	launching bool
}

var launch launcherState
var readAppsFile = vi.ReadFileAll

func appUnavailable(e AppEntry) string {
	bin := e.Bin
	if bin == "TABWM.BIN" || bin == "GOTABWM.ELF" || bin == "WND.BIN" {
		return "Seat: use settings wm"
	}
	h, r := openFile("/host/"+bin, vi.ModeRead)
	if r < 0 {
		return "Binary not staged"
	}
	vi.FileClose(uint32(h))
	return ""
}

func (l *launcherState) rowH() int {
	h := theme.Current.PadLG + theme.Current.PadMD
	if h < 20 {
		h = 20
	}
	return h
}

func (l *launcherState) panelH() int {
	n := len(l.filtered)
	if n < 1 {
		n = 1
	}
	h := launchHdr + n*l.rowH() + theme.Current.PadMD
	if h > launchMaxH {
		h = launchMaxH
	}
	return h
}

func (l *launcherState) refresh() {
	l.painted = false
	l.filtered = filterApps(l.catalog, l.filter)
	if l.sel >= len(l.filtered) {
		l.sel = len(l.filtered) - 1
	}
	if l.sel < 0 {
		l.sel = 0
	}
	l.first = 0
	l.keepSelectionVisible()
}

func (l *launcherState) visibleRows() int {
	return (l.panelH() - launchHdr - theme.Current.PadMD) / l.rowH()
}

func (l *launcherState) keepSelectionVisible() {
	first := l.first
	n := l.visibleRows()
	if n < 1 {
		return
	}
	if l.sel < l.first {
		l.first = l.sel
	}
	if l.sel >= l.first+n {
		l.first = l.sel - n + 1
	}
	if l.first != first {
		l.painted = false
	}
}

// A row is only a target where its complete painted rectangle exists.
// Inter-row gaps, side padding and partial scanout rows are not targets.
func (l *launcherState) rowRect(i, width, height int) (x, y, w, h int, ok bool) {
	x, y = launchX+theme.Current.PadMD, launchY+launchHdr+(i-l.first)*l.rowH()
	w, h = launchW-2*theme.Current.PadMD, l.rowH()-theme.Current.PadXS
	ok = i >= l.first && i < len(l.filtered) && i < l.first+l.visibleRows() &&
		w > 0 && h >= 8 && x+w <= width && y+l.rowH() <= height
	return
}

func (l *launcherState) selected() (AppEntry, bool) {
	if l.sel < 0 || l.sel >= len(l.filtered) {
		return AppEntry{}, false
	}
	i := l.filtered[l.sel]
	if i < 0 || i >= len(l.catalog) {
		return AppEntry{}, false
	}
	return l.catalog[i], true
}

func openLauncher() {
	if launch.open {
		dismissLauncher()
		return
	}
	launch.err = ""
	launch.catalog, launch.reasons, launch.filtered = nil, nil, nil
	launch.filter, launch.sel, launch.first = "", 0, 0
	launch.painted, launch.launching = false, false
	launch.prior, _ = tabs.Focused()
	if !acquireLauncherFocus() {
		vi.ConsoleLine("gotabwm: launcher unavailable " + launch.err)
		// Never claim a modal menu while an app still owns keyboard focus.
		// The always-visible button paints the failed-open error instead.
		launch.open = launch.sink != 0
		return
	}
	body, r := readAppsFile(appsPath, appsMaxBytes)
	if r < 0 || body == nil {
		launch.catalog = nil
		launch.err = "APPS.TXT unavailable"
	} else {
		launch.catalog = parseAppsTXT(string(body))
	}
	launch.filter = ""
	launch.sel = 0
	launch.first = 0
	launch.painted = false
	launch.reasons = make([]string, len(launch.catalog))
	for i, e := range launch.catalog {
		launch.reasons[i] = appUnavailable(e)
	}
	launch.open = true
	launch.refresh()
	// The manifest's own report, before the launcher's: a reader that
	// silently ignored every v2 field would still print `launcher open n=14`,
	// so the decode numbers are what a gate reads to know the v2 fields were
	// decoded rather than skipped.
	s := SummarizeApps(launch.catalog)
	vi.ConsoleLine(MarkerAppsDecode + vi.Itoa64(int64(s.Rows)) +
		" v=" + vi.Itoa64(int64(s.Schema)) +
		" schema=" + vi.Itoa64(int64(appsSchemaVersion)) +
		" argv=" + vi.Itoa64(int64(s.Argv)) +
		" caps=" + vi.Itoa64(int64(s.Caps)) +
		" opens=" + vi.Itoa64(int64(s.Opens)))
	vi.ConsoleLine(MarkerLaunchOpen + vi.Itoa64(int64(len(launch.catalog))))
	vi.ConsoleLine(MarkerLaunchFilter + launch.filter + " n=" + vi.Itoa64(int64(len(launch.filtered))))
}

func dismissLauncher() {
	if !launch.open {
		return
	}
	if !releaseLauncherFocus(true) {
		return
	}
	finishLauncher()
}

func finishLauncher() {
	launch.open = false
	launch.filter = ""
	launch.painted = false
	vi.ConsoleLine(MarkerLaunchDismiss)
}

func emitLaunchFilter() {
	vi.ConsoleLine(MarkerLaunchFilter + launch.filter + " n=" + vi.Itoa64(int64(len(launch.filtered))))
}

func execSelected() {
	if launch.launching {
		if releaseLauncherFocus(false) {
			finishLauncher()
		}
		return
	}
	e, ok := launch.selected()
	if !ok || e.Bin == "" {
		return
	}
	if !ensureLauncherFocus() {
		return
	}
	ci := launch.filtered[launch.sel]
	if ci < len(launch.reasons) && launch.reasons[ci] != "" {
		launch.err = launch.reasons[ci]
		return
	}
	bin := e.Bin
	// Non-app seat entries are refused even if a caller skipped the probe.
	if bin == "TABWM.BIN" || bin == "GOTABWM.ELF" || bin == "WND.BIN" {
		launch.err = "Seat: use settings wm"
		return
	}
	// Preserve the manifest's fixed argv; v1 rows still launch with argc=0.
	_, err := execApp(bin, e.Args...)
	if err != nil {
		vi.ConsoleLine(MarkerLaunchMissing + e.Bin)
		launch.err = "Launch failed: " + bin
		return
	}
	if len(e.Args) > 0 {
		vi.ConsoleLine(MarkerLaunchExec + e.Bin + " argv=" + joinSpace(e.Args))
	} else {
		vi.ConsoleLine(MarkerLaunchExec + e.Bin)
	}
	launch.launching = true
	if releaseLauncherFocus(false) {
		finishLauncher()
	}
}

// joinSpace renders a decoded argv for the marker. It is the inverse of the
// `argv=` split, so the marker and the manifest read the same.
func joinSpace(args []string) string {
	out := ""
	for i, a := range args {
		if i > 0 {
			out += " "
		}
		out += a
	}
	return out
}

// handleLauncherKey owns the launcher's MODAL keys. M82c (#1770): the
// summon chord itself (ctrl+space) is a registry row — hid.go's table
// dispatch opens the launcher with it when the launcher is closed; this
// handler owns only what happens while it is open, including the
// re-summon below.
func handleLauncherKey(e vi.Event) bool {
	usage := uint8(e.Arg0)
	ctrl := e.Flags&vi.ModCtrl != 0
	shift := e.Flags&vi.ModShift != 0
	alt := e.Flags&vi.ModAlt != 0
	if !launch.open {
		return false
	}
	if usage == hidUsageEscape {
		dismissLauncher()
		return true
	}
	if !ensureLauncherFocus() {
		return true
	}
	if alt {
		return true // swallow while modal
	}
	switch usage {
	case hidUsageEnter:
		execSelected()
		return true
	case hidUsageUp, hidUsageDown:
		if usage == hidUsageUp && launch.sel > 0 {
			launch.sel--
		}
		if usage == hidUsageDown && launch.sel+1 < len(launch.filtered) {
			launch.sel++
		}
		launch.keepSelectionVisible()
		return true
	case hidUsageBksp:
		if len(launch.filter) > 0 {
			launch.filter = launch.filter[:len(launch.filter)-1]
			launch.refresh()
			emitLaunchFilter()
		}
		return true
	}
	// Type-to-filter even if Ctrl is still down from the summon chord
	// (virtio HID keeps the modifier on the next report).
	if c, ok := hidUsageChar(usage); ok {
		if usage == hidUsageSpace && ctrl {
			// The summon chord while the launcher is open re-opens it —
			// the same behaviour the old top-of-handler summon had for
			// both open and closed (the closed case now dispatches
			// through the registry row). ctrl+shift+space stays
			// swallowed, exactly as before.
			if shift {
				return true
			}
			openLauncher()
			return true
		}
		if len(launch.filter) < filterMax {
			launch.filter += string(c)
			launch.refresh()
			emitLaunchFilter()
		}
		return true
	}
	return true
}

func hidUsageChar(usage uint8) (byte, bool) {
	if usage >= 0x04 && usage <= 0x1d {
		return 'a' + (usage - 0x04), true
	}
	if usage >= 0x1e && usage <= 0x26 {
		return '1' + (usage - 0x1e), true
	}
	if usage == 0x27 {
		return '0', true
	}
	if usage == hidUsageSpace {
		return ' ', true
	}
	if usage == 0x2d {
		return '-', true
	}
	if usage == 0x37 {
		return '.', true
	}
	return 0, false
}

func launchRowAt(px, py uint32) (int, bool) {
	if !launch.open || !launch.painted {
		return 0, false
	}
	x, y := int(px), int(py)
	if y < launchY+launchHdr {
		return 0, false
	}
	row := launch.first + (y-launchY-launchHdr)/launch.rowH()
	// The scanout is fixed in the guest. Tests can also pin a clipped
	// paint; hit-testing then uses precisely that paint's dimensions.
	width, height := launch.width, launch.height
	rx, ry, rw, rh, ok := launch.rowRect(row, width, height)
	return row, ok && x >= rx && x < rx+rw && y >= ry && y < ry+rh
}

func paintLauncher(scan []byte, width, height int) int {
	launch.painted = false
	if !launch.open || width <= 0 || height <= 0 || len(scan) < 4 {
		return 0
	}
	pixN := len(scan) / 4
	pix := unsafe.Slice((*uint32)(unsafe.Pointer(&scan[0])), pixN)
	maxH := pixN / width
	if maxH > height {
		maxH = height
	}
	if maxH <= 0 {
		return 0
	}
	tok := theme.Current
	launch.painted, launch.width, launch.height = true, width, maxH
	h := launch.panelH()
	written := fillRect(pix, width, maxH, launchX, launchY, launchW, h, tok.Surface)
	written += fillRect(pix, width, maxH, launchX, launchY, tok.BorderW, h, tok.Accent)
	written += fillRect(pix, width, maxH, launchX+launchW-tok.BorderW, launchY, tok.BorderW, h, tok.Accent)
	written += drawText8(pix, width, maxH, launchX+tok.PadMD, launchY+6, "Apps", tok.Ink)
	written += drawText8(pix, width, maxH, launchX+tok.PadMD, launchY+20, "Filter: "+launch.filter, tok.InkMuted)
	if launch.err != "" {
		written += drawText8(pix, width, maxH, launchX+160, launchY+6, launchText(launch.err, 76), tok.Danger)
	}
	if len(launch.filtered) == 0 {
		written += drawText8(pix, width, maxH, launchX+tok.PadMD, launchY+launchHdr+6, "No matching apps", tok.InkMuted)
	}
	for i := launch.first; i < len(launch.filtered); i++ {
		x, y, w, rh, ok := launch.rowRect(i, width, maxH)
		if !ok {
			break
		}
		ci := launch.filtered[i]
		if ci < 0 || ci >= len(launch.catalog) {
			continue
		}
		bg := tok.Surface
		if i == launch.sel {
			bg = tok.Selection
		}
		written += fillRect(pix, width, maxH, x, y, w, rh, bg)
		ink := tok.Ink
		if ci < len(launch.reasons) && launch.reasons[ci] != "" {
			ink = tok.InkMuted
			written += drawText8(pix, width, maxH, x+400, y+6, launchText(launch.reasons[ci], 44), ink)
		}
		written += drawText8(pix, width, maxH, x+6, y+6, launchText(launch.catalog[ci].Label, 48), ink)
	}
	return written
}

func launchText(s string, max int) string {
	if len(s) > max {
		return s[:max-3] + "..."
	}
	return s
}
