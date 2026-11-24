// overlays.go — M216p (#3955): one ordered registry for every seat overlay.
//
// Every seat overlay (full-frame veils, modal menus, notification surfaces,
// drag ghost, watchdog banners, hover preview) is declared once in
// seatOverlays, ordered top -> bottom (highest input/visual priority first).
// Paint iterates bottom -> top (reverse); pointer, key, reap, focus hand-back
// and the derived modal/hover/region/wheel predicates all iterate top -> bottom
// over this same table, so no hand-kept modal list can drift out of sync.
//
// Two-modal rule:
//   - Lock outranks everything: engaging lock closes dismissible modals and
//     blocks new dismissible modals while locked, but never dismisses a
//     standing secret-ask prompt (unlock hands focus back to its sink).
//   - Secret-ask is never dismissed by another overlay: opening a consent
//     closes dismissible modals before acquiring its sink, and while it stands
//     no dismissible modal may open (only lock may engage over it).
//   - Dismissible modals (notification center, power menu, clipboard menu,
//     rail menu, launcher/palette) are mutually exclusive: opening one closes
//     any other before acquiring a sink. Crossing between launcher and palette
//     or retargeting the rail menu stays inside the same overlay entry.
package main

import "virelai/vi"

type overlayID uint8

const (
	overlayLock overlayID = iota
	overlayIdle
	overlaySecretAsk
	overlayDragGhost
	overlayNotifyCenter
	overlayNotify
	overlayPowerMenu
	overlayClipMenu
	overlayRailMenu
	overlayLauncher
	overlayWatchdog
	overlayHoverPreview
	overlayCount
)

// ptrSample is the parsed pointer event handed through overlay dispatch,
// rail/sash/pane handling, and client-area forwarding.
type ptrSample struct {
	px, py    uint32
	btn, mods uint8
	down      bool
	up        bool
	midDown   bool
	rightDown bool
}

// seatOverlay describes one overlay layer in seatOverlays.
type seatOverlay struct {
	id            overlayID
	name          string
	modal         bool // holds exclusive seat input (a11y rail focus, cursor lock)
	dismissible   bool // closed when another modal opens
	blocksModals  bool // refuses opening dismissible modals while open
	swallowAllPtr bool // swallows pointer/wheel anywhere on screen while open
	suppressHover bool // suppresses hover-preview dwell and card
	suppressReg   bool // suppresses app cursor regions across the whole frame
	open          func() bool
	sink          func() uint32
	ensureSink    func() bool
	clearSink     func()
	close         func()
	hit           func(px, py uint32) bool
	paint         func(scan []byte, width, height int) int
	pointer       func(p ptrSample) bool
	key           func(e vi.Event) bool
}

// seatOverlays is the single authoritative overlay stack, ordered top -> bottom.
// Paint walks it in reverse (bottom -> top); every input, reap, and predicate
// pass walks it forward (top -> bottom). Populated in init() because the
// handlers transitively reference the predicates derived from this table.
var seatOverlays [overlayCount]seatOverlay

func init() {
	seatOverlays = [overlayCount]seatOverlay{
		{
			id:            overlayLock,
			name:          "lock",
			modal:         true,
			blocksModals:  true,
			swallowAllPtr: true,
			suppressHover: true,
			suppressReg:   true,
			open:          func() bool { return lockActive },
			sink:          func() uint32 { return lockSink },
			paint:         paintLockCurtain,
			pointer:       handleLockOverlayPointer,
			key:           handleLockOverlayKey,
		},
		{
			id:            overlayIdle,
			name:          "idle",
			swallowAllPtr: true,
			suppressHover: true,
			suppressReg:   true,
			open:          func() bool { return idleState != idleActive },
			paint:         paintIdleOverlay,
			pointer:       handleIdleOverlayPointer,
			key:           handleIdleOverlayKey,
		},
		{
			id:            overlaySecretAsk,
			name:          "secret-ask",
			modal:         true,
			blocksModals:  true,
			swallowAllPtr: true,
			suppressHover: true,
			suppressReg:   true,
			open:          func() bool { return secretAskModal.open },
			sink:          func() uint32 { return secretAskModal.sink },
			ensureSink:    ensureSecretFocus,
			clearSink:     func() { secretAskModal.sink = 0 },
			close:         closeSecretAsk,
			paint:         paintSecretAsk,
			pointer:       handleSecretAskOverlayPointer,
			key:           handleSecretAskKey,
		},
		{
			id:            overlayDragGhost,
			name:          "drag-ghost",
			swallowAllPtr: true,
			suppressHover: true,
			open:          func() bool { return drag.armed },
			close:         func() { dragAbort("chrome") },
			paint:         paintDragGhostOverlay,
			pointer:       handleDragOverlayPointer,
		},
		{
			id:            overlayNotifyCenter,
			name:          "notify-center",
			dismissible:   true,
			suppressHover: true,
			suppressReg:   true,
			open:          func() bool { return notifyCenterOpen },
			close:         closeNotifyCenter,
			hit: func(px, py uint32) bool {
				return notifyCenterHit(px, py, vi.ScanoutWidth, vi.ScanoutHeight)
			},
			paint:   paintNotifyCenter,
			pointer: handleNotifyCenterOverlayPointer,
			key:     handleNotifyCenterOverlayKey,
		},
		{
			id:   overlayNotify,
			name: "notify",
			open: func() bool { return len(notifyQueue) > 0 },
			hit: func(px, py uint32) bool {
				_, ok := notifyHit(px, py, vi.ScanoutWidth, vi.ScanoutHeight)
				return ok
			},
			paint: func(scan []byte, width, height int) int {
				return paintNotify(scan, width, height, compositeClock)
			},
			pointer: handleNotifyOverlayPointer,
		},
		{
			id:            overlayPowerMenu,
			name:          "power-menu",
			modal:         true,
			dismissible:   true,
			swallowAllPtr: true,
			suppressHover: true,
			suppressReg:   true,
			open:          func() bool { return powerMenuOpen },
			sink:          func() uint32 { return powerSink },
			ensureSink:    ensurePowerFocus,
			clearSink:     func() { powerSink = 0 },
			close:         dismissPowerMenu,
			paint:         paintPowerMenu,
			pointer:       handlePowerMenuOverlayPointer,
			key:           handlePowerKey,
		},
		{
			id:            overlayClipMenu,
			name:          "clip-menu",
			modal:         true,
			dismissible:   true,
			swallowAllPtr: true,
			suppressHover: true,
			suppressReg:   true,
			open:          func() bool { return clipMenuOpen },
			sink:          func() uint32 { return clipMenuSink },
			ensureSink:    ensureClipMenuFocus,
			clearSink:     func() { clipMenuSink = 0 },
			close:         dismissClipMenu,
			paint:         paintClipMenu,
			pointer:       handleClipMenuOverlayPointer,
			key:           handleClipMenuKey,
		},
		{
			id:            overlayRailMenu,
			name:          "rail-menu",
			modal:         true,
			dismissible:   true,
			swallowAllPtr: true,
			suppressHover: true,
			suppressReg:   true,
			open:          func() bool { return railMenuOpen },
			sink:          func() uint32 { return railMenuSink },
			ensureSink:    ensureRailMenuFocus,
			clearSink:     func() { railMenuSink = 0 },
			close:         dismissRailMenu,
			paint:         paintRailMenu,
			pointer:       handleRailMenuOverlayPointer,
			key:           handleRailMenuKey,
		},
		{
			id:            overlayLauncher,
			name:          "launcher",
			modal:         true,
			dismissible:   true,
			swallowAllPtr: true,
			suppressHover: true,
			suppressReg:   true,
			open:          func() bool { return launch.open },
			sink:          func() uint32 { return launch.sink },
			ensureSink:    ensureLauncherFocus,
			clearSink:     func() { launch.sink = 0 },
			close:         dismissLauncher,
			paint:         paintLauncher,
			pointer:       handleLauncherOverlayPointer,
			key:           handleLauncherKey,
		},
		{
			id:   overlayWatchdog,
			name: "watchdog",
			open: watchdogAnyBannerUp,
			hit: func(px, py uint32) bool {
				wid, _ := watchdogHit(px, py, vi.ScanoutWidth, vi.ScanoutHeight)
				return wid != 0
			},
			paint:   paintWatchdog,
			pointer: handleWatchdogOverlayPointer,
		},
		{
			id:    overlayHoverPreview,
			name:  "hover-preview",
			open:  func() bool { return hoverPreview.visible },
			close: hoverPreviewCancel,
			paint: paintHoverPreview,
		},
	}
}

// paintOverlays renders every open overlay bottom -> top and returns the
// painted pixel count per overlay for compositePaint's post-present receipts.
func paintOverlays(scan []byte, width, height int) [overlayCount]int {
	var px [overlayCount]int
	for i := len(seatOverlays) - 1; i >= 0; i-- {
		o := &seatOverlays[i]
		if o.paint != nil && o.open() {
			px[o.id] = o.paint(scan, width, height)
		}
	}
	return px
}

// dispatchOverlayPointer routes one pointer sample top -> bottom through
// open overlays. The menuGesture latch sits directly below drag-ghost so
// an active secret-ask or drag always wins while a button release from a
// dismissed menu cannot leak to lower chrome or content.
func dispatchOverlayPointer(p ptrSample) bool {
	for i := range seatOverlays {
		o := &seatOverlays[i]
		if o.id == overlayNotifyCenter && menuGesture {
			if p.btn == 0 {
				menuGesture = false
			}
			return true
		}
		if o.pointer == nil || !o.open() {
			continue
		}
		if o.pointer(p) {
			return true
		}
	}
	return false
}

// dispatchOverlayKey routes one WM_KEY event top -> bottom through open
// overlays, returning true if an overlay consumed it.
func dispatchOverlayKey(e vi.Event) bool {
	for i := range seatOverlays {
		o := &seatOverlays[i]
		if o.key == nil || !o.open() {
			continue
		}
		if o.key(e) {
			return true
		}
	}
	return false
}

// reapOverlays dismisses any open sink-holding overlay whose kernel sink
// window has vanished or lost mandatory focus. Lock has ensureSink == nil
// and is never auto-dismissed.
func reapOverlays() {
	for i := range seatOverlays {
		o := &seatOverlays[i]
		if o.ensureSink == nil || o.sink == nil || !o.open() {
			continue
		}
		if o.sink() != 0 && !o.ensureSink() {
			o.close()
		}
	}
}

// reapOverlaySink handles a released window mirror naming id: if an open
// reapable overlay owns that dead sink, clear the sink id first (avoiding a
// close on a reissued id) and dismiss the overlay.
func reapOverlaySink(id uint32) {
	if id == 0 {
		return
	}
	for i := range seatOverlays {
		o := &seatOverlays[i]
		if o.ensureSink == nil || o.sink == nil || !o.open() || o.sink() != id {
			continue
		}
		if _, r := queryWindow(int(id)); r < 0 {
			if o.clearSink != nil {
				o.clearSink()
			}
			o.close()
		}
	}
}

// overlayBlocksModal reports whether a higher-authority overlay (lock or
// secret-ask) is open and forbids opening a dismissible modal.
func overlayBlocksModal(opening overlayID) bool {
	for i := range seatOverlays {
		o := &seatOverlays[i]
		if o.id != opening && o.blocksModals && o.open() {
			return true
		}
	}
	return false
}

// closeDismissibleModals closes every open dismissible overlay other than
// exclude (two-modal rule). Never closes lock or secret-ask.
func closeDismissibleModals(exclude overlayID) {
	for i := range seatOverlays {
		o := &seatOverlays[i]
		if o.id == exclude || !o.dismissible || o.close == nil || !o.open() {
			continue
		}
		o.close()
	}
}

// --- derived predicates -------------------------------------------------

func overlayHasModal() bool {
	for i := range seatOverlays {
		o := &seatOverlays[i]
		if o.modal && o.open() {
			return true
		}
	}
	return false
}

func overlayHasModalSink() bool {
	for i := range seatOverlays {
		o := &seatOverlays[i]
		if o.open() && o.sink != nil && o.sink() != 0 {
			return true
		}
	}
	return false
}

// overlayFocusSink returns the input sink window id of the highest-priority
// open overlay other than exclude, or 0 if none holds a sink.
func overlayFocusSink(exclude overlayID) uint32 {
	for i := range seatOverlays {
		o := &seatOverlays[i]
		if o.id == exclude || o.sink == nil || !o.open() {
			continue
		}
		if id := o.sink(); id != 0 {
			return id
		}
	}
	return 0
}

func overlaySuppressesHover() bool {
	for i := range seatOverlays {
		o := &seatOverlays[i]
		if o.suppressHover && o.open() {
			return true
		}
	}
	return false
}

func overlaySuppressesRegion(px, py uint32) bool {
	for i := range seatOverlays {
		o := &seatOverlays[i]
		if !o.open() {
			continue
		}
		if o.suppressReg {
			return true
		}
		if o.hit != nil && o.hit(px, py) {
			return true
		}
	}
	return false
}

func overlaySwallowsWheel(px, py uint32) bool {
	for i := range seatOverlays {
		o := &seatOverlays[i]
		if !o.open() {
			continue
		}
		if o.swallowAllPtr {
			return true
		}
		if o.hit != nil && o.hit(px, py) {
			return true
		}
	}
	return false
}

// overlayBlocksDragCursor reports whether a modal overlay outranking
// drag-ghost (lock or secret-ask) is open and forces the arrow cursor even
// over an armed drag.
func overlayBlocksDragCursor() bool {
	for i := range seatOverlays {
		o := &seatOverlays[i]
		if o.id == overlayDragGhost {
			return false
		}
		if o.modal && o.open() {
			return true
		}
	}
	return false
}

// --- per-overlay paint/pointer/key adapters -----------------------------

func paintIdleOverlay(scan []byte, width, height int) int {
	if lockActive {
		return 0
	}
	switch idleState {
	case idleDim:
		return paintIdleDim(scan, width, height)
	case idleCurtain:
		return paintIdleCurtain(scan, width, height)
	default:
		return 0
	}
}

func paintDragGhostOverlay(scan []byte, _, _ int) int {
	if secretAskModal.open {
		return 0
	}
	return paintDragGhost(scan)
}

func handleLockOverlayPointer(_ ptrSample) bool {
	lastInputTick = seatTick
	hoverPreviewCancel()
	return true
}

func handleLockOverlayKey(_ vi.Event) bool {
	lastInputTick = seatTick
	hoverPreviewCancel()
	return true
}

func handleIdleOverlayPointer(_ ptrSample) bool {
	if idleInput(seatTick) {
		hoverPreviewCancel()
		return true
	}
	return false
}

func handleIdleOverlayKey(_ vi.Event) bool {
	if idleInput(seatTick) {
		hoverPreviewCancel()
		return true
	}
	return false
}

func handleSecretAskOverlayPointer(p ptrSample) bool {
	if p.down {
		handleSecretAskPointer(p.px, p.py)
	}
	return true
}

func handleDragOverlayPointer(p ptrSample) bool {
	if p.up {
		contentDown, railDragFrom, sashDragging = false, -1, false
	}
	handleDragPointer(p.px, p.py, p.up)
	return true
}

func handleNotifyCenterOverlayPointer(p ptrSample) bool {
	if p.down {
		return notifyCenterClick(p.px, p.py)
	}
	return notifyCenterHit(p.px, p.py, vi.ScanoutWidth, vi.ScanoutHeight)
}

func handleNotifyCenterOverlayKey(e vi.Event) bool {
	if e.Arg0 == 0x29 && e.Flags&(vi.ModCtrl|vi.ModShift|vi.ModAlt) == 0 {
		closeNotifyCenter()
		return true
	}
	return false
}

func handleNotifyOverlayPointer(p ptrSample) bool {
	if _, onToast := notifyHit(p.px, p.py, vi.ScanoutWidth, vi.ScanoutHeight); !onToast {
		return false
	}
	if p.down {
		notifyClicked(p.px, p.py)
		if launch.open {
			menuGesture = true
			_ = ensureLauncherFocus()
		}
		return true
	}
	if p.up || sashDragging || seatChromeModal() {
		return false
	}
	updateRailHover(p.px, p.py, true)
	return true
}

func handlePowerMenuOverlayPointer(p ptrSample) bool {
	if p.down {
		dismissPowerMenu()
		if r := powerMenuRowAt(p.px, p.py, vi.ScanoutWidth, vi.ScanoutHeight); r >= 0 {
			powerSel = r
			runPowerRow(r)
		}
	}
	return true
}

func handleClipMenuOverlayPointer(p ptrSample) bool {
	if p.down {
		if r := clipMenuRowAt(p.px, p.py, vi.ScanoutWidth, vi.ScanoutHeight); r >= 0 {
			clipMenuSel = r
			applyClipMenuPick(r)
		} else {
			dismissClipMenu()
		}
	}
	return true
}

func handleRailMenuOverlayPointer(p ptrSample) bool {
	if p.down || p.rightDown {
		r := railMenuRowAt(p.px, p.py, vi.ScanoutWidth, vi.ScanoutHeight)
		if r < 0 && p.rightDown {
			if i, ok := railCellAt(p.px, p.py, vi.ScanoutWidth, tabs.Count(), RailHeight); ok {
				openRailMenu(i)
				return true
			}
		}
		if r >= 0 {
			railMenuSel = r
			applyRailMenuPick(r)
		} else {
			dismissRailMenu()
		}
	}
	return true
}

func handleLauncherOverlayPointer(p ptrSample) bool {
	if p.btn != 0 {
		menuGesture = true
		railDragFrom, contentDown, sashDragging = -1, false, false
	}
	if p.down {
		i, ok := launchRowAt(p.px, p.py)
		if !ok {
			x, y := int(p.px), int(p.py)
			if x < launchX || x >= launchX+launchW || y < launchY || y >= launchY+launch.panelH() {
				dismissLauncher()
			}
			return true
		}
		launch.sel = i
		execSelected()
	}
	return true
}

func handleWatchdogOverlayPointer(p ptrSample) bool {
	if !p.down {
		return false
	}
	if wid, act := watchdogHit(p.px, p.py, vi.ScanoutWidth, vi.ScanoutHeight); wid != 0 {
		watchdogPress(wid, act)
		return true
	}
	return false
}
