// GOTABWM.ELF — M69c2 (#1535): Ctrl+Space launcher. One list parsed from
// /host/APPS.TXT, type-to-filter, Enter/click execs the named ELF as a
// hosted tab. Not a Sexiburger port (D1). Fail closed on a missing ELF (D3).
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

	launchX    = 240
	launchY    = 80
	launchW    = 800
	launchHdr  = 36
	launchMaxH = 520
	filterMax  = 24
)

type launcherState struct {
	open     bool
	catalog  []AppEntry
	filter   string
	filtered []int
	sel      int
}

var launch launcherState

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
	l.filtered = filterApps(l.catalog, l.filter)
	if l.sel >= len(l.filtered) {
		l.sel = len(l.filtered) - 1
	}
	if l.sel < 0 {
		l.sel = 0
	}
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
	body, r := vi.ReadFileAll(appsPath, appsMaxBytes)
	if r < 0 || body == nil {
		launch.catalog = nil
	} else {
		launch.catalog = parseAppsTXT(string(body))
	}
	launch.filter = ""
	launch.sel = 0
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
	launch.open = false
	launch.filter = ""
	vi.ConsoleLine(MarkerLaunchDismiss)
}

func emitLaunchFilter() {
	vi.ConsoleLine(MarkerLaunchFilter + launch.filter + " n=" + vi.Itoa64(int64(len(launch.filtered))))
}

func execSelected() {
	e, ok := launch.selected()
	if !ok || e.Bin == "" {
		return
	}
	// M82a: `argv=` rides the exec. A row with no `argv=` launches exactly as
	// it did before the field existed — the binary alone — which is why the
	// marker below is unchanged for every shipping row today.
	_, err := execApp(e.Bin, e.Args...)
	if err != nil {
		vi.ConsoleLine(MarkerLaunchMissing + e.Bin)
		return
	}
	if len(e.Args) > 0 {
		vi.ConsoleLine(MarkerLaunchExec + e.Bin + " argv=" + joinSpace(e.Args))
	} else {
		vi.ConsoleLine(MarkerLaunchExec + e.Bin)
	}
	dismissLauncher()
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

func handleLauncherKey(e vi.Event) bool {
	usage := uint8(e.Arg0)
	ctrl := e.Flags&vi.ModCtrl != 0
	shift := e.Flags&vi.ModShift != 0
	alt := e.Flags&vi.ModAlt != 0
	if ctrl && !shift && !alt && usage == hidUsageSpace {
		openLauncher()
		return true
	}
	if !launch.open {
		return false
	}
	if alt {
		return true // swallow while modal
	}
	switch usage {
	case hidUsageEscape:
		dismissLauncher()
		return true
	case hidUsageEnter:
		execSelected()
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
	if !launch.open {
		return 0, false
	}
	x, y := int(px), int(py)
	if x < launchX || x >= launchX+launchW || y < launchY || y >= launchY+launch.panelH() {
		return 0, false
	}
	row := (y - launchY - launchHdr) / launch.rowH()
	if row < 0 || row >= len(launch.filtered) {
		return 0, false
	}
	return row, true
}

func paintLauncher(scan []byte, width, height int) int {
	if !launch.open || width <= 0 || height <= 0 || len(scan) < 4 {
		return 0
	}
	pixN := len(scan) / 4
	pix := unsafe.Slice((*uint32)(unsafe.Pointer(&scan[0])), pixN)
	maxH := pixN / width
	if maxH <= 0 {
		return 0
	}
	tok := theme.Current
	h := launch.panelH()
	written := fillRect(pix, width, maxH, launchX, launchY, launchW, h, tok.Surface)
	written += fillRect(pix, width, maxH, launchX, launchY, tok.BorderW, h, tok.Accent)
	written += fillRect(pix, width, maxH, launchX+launchW-tok.BorderW, launchY, tok.BorderW, h, tok.Accent)
	rowH := launch.rowH()
	for i, ci := range launch.filtered {
		y := launchY + launchHdr + i*rowH
		if y+rowH > launchY+h {
			break
		}
		bg := tok.Surface
		if i == launch.sel {
			bg = tok.Selection
		}
		written += fillRect(pix, width, maxH, launchX+tok.PadMD, y, launchW-2*tok.PadMD, rowH-tok.PadXS, bg)
		_ = ci
	}
	return written
}
