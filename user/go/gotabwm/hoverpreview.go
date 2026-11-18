// GOTABWM.ELF — #3914: the rail's hover-preview card.
//
// The Zig TABWM seat has BT6 hover-preview (M48 #1126): after the pointer
// rests on a rail row long enough, a bounded card names that tab. The
// boot-default GOTABWM seat never got it. This file is the same affordance
// for the truthful data the seat itself holds — the tab's title, its
// declared id/executable, and its seat status. It never captures app
// pixels and adds no WM wire kind: the card is painted from the strip and
// presented with the ordinary composite pass.
//
// Timing rides the composite HEARTBEAT, not the paint frequency: dwell is
// a difference of seatTick counts, so a coalesced drain that pays one
// paint for several consumed heartbeats still ages the dwell by the real
// heartbeat count. Any pointer sample (motion, press, release) clears the
// pending dwell and dismisses a visible card; leaving the rail or a tab
// identity change (close, reorder, replacement) does the same. A modal
// overlay, an armed drag, or the lock curtain suppresses both the pending
// dwell and a visible card, and clearing that state cannot resurrect the
// stale card — the dwell must start over.

package main

import (
	"strings"

	"virelai/draw"
	"virelai/theme"
	"virelai/vi"
	"virelai/widgets"
)

const (
	// hoverPreviewDelay is the dwell threshold in heartbeats. Zig TABWM's
	// hover_preview_delay is 2 composite ticks (user/src/tabwm.zig:738);
	// this seat uses the same visible-to-the-user interval.
	hoverPreviewDelay = 2

	// The card is a fixed, bounded plate: 240x60 at most, one at a time.
	hoverPreviewW      = 240
	hoverPreviewH      = 60
	hoverPreviewInsetY = 4

	// Post-present receipts. A marker is printed only after the frame
	// carrying (or no longer carrying) the card actually presented, so a
	// pixel probe anchored on it reads the scanout it names.
	MarkerHoverPreview        = "gotabwm: preview id="
	MarkerHoverPreviewDismiss = "gotabwm: preview dismiss id="
)

// hoverPreviewLog is the console seam, var so host tests can capture the
// post-present receipt without a guest serial.
var hoverPreviewLog = vi.ConsoleLine

// hoverPreviewState tracks the dwell state for the rail cell under the
// pointer. The target is the tab ID, never the cell alone: a reorder or a
// close can put a different tab under the same index, and that must never
// inherit the old tab's dwell.
type hoverPreviewState struct {
	cell    int    // rail cell the dwell belongs to, or -1
	id      uint32 // tab identity the dwell belongs to
	armTick uint64 // seatTick at which the current dwell started
	armed   bool
	visible bool   // the card is up (painted on the next composite pass)
	shown   uint32 // id of the card whose presentation was last announced
	sig     string // title/identity/status the dwell was armed against
	block   int    // cell whose card was dismissed by a truth change, or -1
}

var hoverPreview = hoverPreviewState{cell: -1, block: -1}

// hoverPreviewSignature is everything the card would say. A change while
// the card is up means the shown text is already stale — a focus move, a
// pin, a title update — so the card is dismissed instead of reworded
// under the user's eye. Only fresh pointer motion re-arms it.
func hoverPreviewSignature(cell int, id uint32) string {
	return hoverPreviewTitle() + "\x00" + hoverPreviewIdentity(id) + "\x00" + hoverPreviewStatus(id)
}

// hoverPreviewPointer is called for EVERY pointer sample, before any
// chrome early-return. Motion resets the dwell even inside one cell; a
// press/release is a gesture, not rest. Dismissal is immediate — the next
// composite pass paints without the card and the post-present dismiss
// receipt follows that frame.
func hoverPreviewPointer() {
	hoverPreview.armed = false
	hoverPreview.visible = false
	hoverPreview.block = -1
}

// hoverPreviewSuppressed reports the desktop states that own the frame
// instead of a hover card: modal chrome, an armed drag, or the lock.
func hoverPreviewSuppressed() bool {
	if lockActive || launch.open || powerMenuOpen || clipMenuOpen ||
		railMenuOpen || secretAskModal.open || notifyCenterOpen {
		return true
	}
	return drag.armed || railDragFrom >= 0 || sashDragging
}

// hoverPreviewTick advances the dwell once per HEARTBEAT. It is called
// from compositeTick (never from compositePaint), so damage-only repaints
// cannot count as dwell. Visible is derived from the seatTick difference,
// so paint coalescing cannot slow the threshold down.
func hoverPreviewTick() {
	n := tabs.Count()
	if hoverPreviewSuppressed() || railHover < 0 || railHover >= n {
		hoverPreview.armed = false
		hoverPreview.visible = false
		hoverPreview.block = -1
		return
	}
	id := tabs.At(railHover).ID
	if id == 0 {
		hoverPreview.armed = false
		hoverPreview.visible = false
		hoverPreview.block = -1
		return
	}
	if hoverPreview.block >= 0 && hoverPreview.block != railHover {
		hoverPreview.block = -1
	}
	if hoverPreview.block == railHover {
		hoverPreview.armed = false
		hoverPreview.visible = false
		return
	}
	if !hoverPreview.armed || hoverPreview.id != id || hoverPreview.cell != railHover {
		hoverPreview.cell = railHover
		hoverPreview.id = id
		hoverPreview.armTick = seatTick
		hoverPreview.armed = true
		hoverPreview.visible = false
		hoverPreview.sig = hoverPreviewSignature(railHover, id)
		return
	}
	if hoverPreview.sig != hoverPreviewSignature(railHover, id) {
		hoverPreview.armed = false
		hoverPreview.visible = false
		hoverPreview.block = railHover
		return
	}
	if seatTick >= hoverPreview.armTick+hoverPreviewDelay {
		hoverPreview.visible = true
	}
}

// hoverPreviewRect resolves the painted plate: under the hovered cell,
// clamped to the scanout. False when there is no visible, truthful card.
func hoverPreviewRect(width, height int) (widgets.Rect, bool) {
	if !hoverPreview.visible || hoverPreviewSuppressed() {
		return widgets.Rect{}, false
	}
	n := tabs.Count()
	if n <= 0 || hoverPreview.cell < 0 || hoverPreview.cell >= n {
		return widgets.Rect{}, false
	}
	if int(tabs.At(hoverPreview.cell).ID) != int(hoverPreview.id) {
		return widgets.Rect{}, false
	}
	cellW := width / n
	if cellW < 48 {
		cellW = 48
	}
	x := hoverPreview.cell * cellW
	w := hoverPreviewW
	if x+w > width {
		w = width - x
	}
	if w < 96 {
		return widgets.Rect{}, false
	}
	y := RailHeight + hoverPreviewInsetY
	h := hoverPreviewH
	if y+h > height {
		y = height - h
	}
	if y < RailHeight || h <= 0 {
		return widgets.Rect{}, false
	}
	return widgets.Rect{X: x, Y: y, W: w, H: h}, true
}

// hoverPreviewStatus is the seat-held status line. It names only what the
// strip actually knows; nothing here is app-captured.
func hoverPreviewStatus(id uint32) string {
	t := tabs.At(hoverPreview.cell)
	parts := make([]string, 0, 3)
	if fid, ok := tabs.Focused(); ok && fid == id {
		parts = append(parts, "focused")
	}
	if t.Pinned {
		parts = append(parts, "pinned")
	}
	if t.Frozen {
		parts = append(parts, "frozen")
	}
	if len(parts) == 0 {
		return "active"
	}
	return strings.Join(parts, "+")
}

// hoverPreviewTitle is the card's title line: the declared title, else the
// executable, else a plain id spelling. Fit at paint time.
func hoverPreviewTitle() string {
	t := tabs.At(hoverPreview.cell)
	if title := strings.TrimSpace(t.Title); title != "" {
		return title
	}
	if bin := strings.TrimSpace(t.Bin); bin != "" {
		return bin
	}
	return "tab " + vi.Itoa64(int64(t.ID))
}

// hoverPreviewIdentity is the card's identity line: the kernel id the tab
// declared plus its executable.
func hoverPreviewIdentity(id uint32) string {
	t := tabs.At(hoverPreview.cell)
	line := "id=" + vi.Itoa64(int64(id))
	if bin := strings.TrimSpace(t.Bin); bin != "" {
		line += "  " + bin
	}
	return line
}

// paintHoverPreview paints the bounded card over the scanout. Pure seat
// chrome: a theme plate, the title, the id/executable and the status. The
// painted pixel count is the receipt the post-present marker keys on.
func paintHoverPreview(scan []byte, width, height int) int {
	r, ok := hoverPreviewRect(width, height)
	if !ok {
		return 0
	}
	c := seatCanvas(scan, width, height)
	if c == nil {
		return 0
	}
	tok := theme.Current
	n := seatPlate(c, r, radiusControl, tok.Accent, tok.Surface)
	draw.StrokeRoundedRect(c, r, radiusControl, tok.BorderW, draw.Opaque(tok.Accent))
	clip := r.Inset(tok.BorderW + 2)
	title := seatFitText(hoverPreviewTitle(), clip.W-16, seatTextPx)
	seatText(c, clip, r.X+10, r.Y+7, seatTextPx, title, tok.Ink)
	identity := seatFitText(hoverPreviewIdentity(hoverPreview.id), clip.W-16, seatSmallPx)
	seatText(c, clip, r.X+10, r.Y+24, seatSmallPx, identity, tok.InkMuted)
	status := seatFitText(hoverPreviewStatus(hoverPreview.id), clip.W-16, seatSmallPx)
	seatText(c, clip, r.X+10, r.Y+40, seatSmallPx, status, tok.Accent)
	return n
}

// hoverPreviewPresented runs AFTER the ordinary present. It announces a
// card only once the frame carrying it flushed, and announces dismissal
// only after the first presented frame without it. A new identity cannot
// reuse the old announcement: `shown` names the exact id last presented.
func hoverPreviewPresented(painted, presented bool) {
	if painted && presented {
		if hoverPreview.shown != hoverPreview.id {
			hoverPreview.shown = hoverPreview.id
			hoverPreviewLog(MarkerHoverPreview + vi.Itoa64(int64(hoverPreview.id)) +
				" title=" + hoverPreviewMarkerText(hoverPreviewTitle()) +
				" bin=" + hoverPreviewMarkerText(tabs.At(hoverPreview.cell).Bin) +
				" status=" + hoverPreviewStatus(hoverPreview.id))
		}
		return
	}
	if hoverPreview.shown != 0 {
		hoverPreviewLog(MarkerHoverPreviewDismiss + vi.Itoa64(int64(hoverPreview.shown)))
		hoverPreview.shown = 0
	}
}

// hoverPreviewMarkerText keeps a title/bin on one marker line: control
// bytes and newlines become spaces. The payload stays literal for greps.
func hoverPreviewMarkerText(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return "-"
	}
	var b strings.Builder
	b.Grow(len(s))
	for _, r := range s {
		if r < 0x20 || r == 0x7f {
			b.WriteByte(' ')
			continue
		}
		b.WriteRune(r)
	}
	return b.String()
}
