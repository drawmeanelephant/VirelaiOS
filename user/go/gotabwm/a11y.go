// a11y.go — M197a (#3381): the seat describes itself.
//
// The #a11y v1 outline seam (user/go/appkit/a11y.go, M177b) was an
// app-side protocol: an appkit window stages its control tree at
// /host/A11Y.<win> and a reader finds it through the kind-16 focused
// row. Seat chrome — rail cells, the launcher/palette strip, toasts,
// the lock curtain — paints inside the seat and is never a kind-16
// row, so the per-window path cannot carry it: this file is the
// seat-side producer the card calls for, staging one outline of the
// whole chrome at the fixed appkit.A11ySeatPath rendezvous.
//
// The tree is the presented frame read in stacking order: the rail
// (with one cell per live tab), the open launcher/palette strip, then
// the toast stack. A full-frame veil — the lock curtain, else the
// idle curtain — replaces the ambient description entirely: the
// outline names what the veil shows and leaks nothing it covers.
//
// Focus is the kernel truth, not the strip's last focus: while a
// modal seat surface owns the keys (the launcher/palette sink, the
// power or rail menu, the secret ask, the clipboard menu) its own
// node carries 'f' — the launcher field, the curtain's field — and
// the rail's focused cell reads 's' instead, because no app window
// holds the keyboard while the sink does.
package main

import (
	"virelai/appkit"
	"virelai/vi"
)

// MarkerA11ySeat is the per-publish line — printed only when the staged
// bytes actually changed (the markRail discipline: transitions, never a
// per-tick flood).
const MarkerA11ySeat = "gotabwm: a11y seat nodes="

// The file seams — vi.WriteFileSafe/FileDelete on the guest, stubs a
// host test captures through (the stateFileWrite pattern).
var (
	seatA11yWrite  = vi.WriteFileSafe
	seatA11yDelete = vi.FileDelete
)

// seatA11yLast is the last published body — the demo's lastOutline dedup:
// compositePaint calls in every presented frame, and an unchanged chrome
// description must not restage the file each tick.
var seatA11yLast string

// seatA11yOutline assembles the chrome's node list. The order is the
// paint's stacking order — rail, then the modal strip, then toasts —
// which is also the top-to-bottom read.
func seatA11yOutline() []appkit.Node {
	nodes := []appkit.Node{{Role: appkit.RoleSeat, Name: "Desktop"}}
	if lockActive {
		// The curtain owns the whole frame; the field's name is its
		// label, never the passphrase contents (D2 — lockPass is
		// bytes, not a label, and stays out of the outline entirely).
		return append(nodes,
			appkit.Node{Depth: 1, Role: appkit.RoleCurtain, Name: "Locked"},
			appkit.Node{Depth: 2, Role: appkit.RoleTextField,
				State: appkit.StateFocused, Name: "passphrase"})
	}
	if idleState == idleCurtain {
		return append(nodes, appkit.Node{Depth: 1, Role: appkit.RoleCurtain,
			Name: "Visual curtain"})
	}
	nodes = append(nodes, tabs.a11yRailNodes(1, !seatChromeModal())...)
	if launch.open {
		nodes = append(nodes, launch.a11yNodes(1)...)
	}
	nodes = append(nodes, toastA11yNodes(1)...)
	return nodes
}

// seatChromeModal reports whether a seat-owned surface holds kernel
// focus through its input sink — while one does, the rail's focused
// cell is the strip's selection ('s'), not the keys' owner ('f').
func seatChromeModal() bool {
	return launch.open || powerMenuOpen || railMenuOpen ||
		secretAskModal.open || clipMenuOpen
}

// seatA11yPublish stages the outline on a presented frame — like the
// seat's own paint markers, the file may only claim what actually
// flushed to the scanout. Identical bytes are not restaged.
func seatA11yPublish(presented bool) {
	if !presented {
		return
	}
	body := appkit.EncodeOutline(seatA11yOutline())
	if string(body) == seatA11yLast {
		return
	}
	if seatA11yWrite(appkit.A11ySeatPath, body) != 0 {
		return
	}
	seatA11yLast = string(body)
	vi.ConsoleLine(MarkerA11ySeat + vi.Itoa64(int64(seatA11yNodes(body))))
}

// seatA11yNodes counts the published body's node lines — every line but
// the header carries one node — so the marker reports what the file
// holds, encode truncation included, never the pre-encode count.
func seatA11yNodes(body []byte) int {
	n := 0
	for _, c := range body {
		if c == '\n' {
			n++
		}
	}
	if n > 0 {
		n--
	}
	return n
}

// seatA11yUnpublish drops the staged outline on a clean exit. A seat
// that is killed leaves the file behind — dead data either way, since
// no seat is up to read it against — but a graceful exit can still
// leave the share honest.
func seatA11yUnpublish() {
	_ = seatA11yDelete(appkit.A11ySeatPath)
	seatA11yLast = ""
}
