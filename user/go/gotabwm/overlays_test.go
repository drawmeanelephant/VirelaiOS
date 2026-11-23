package main

// overlays_test.go — M216p (#3955): pins the single overlay registry's
// canonical order, every pass's coverage (paint, pointer, key, reap, and
// derived predicates), the repaired defects, the two-modal rule, and the
// three-way handleWmPointer split.

import (
	"testing"

	"virelai/theme"
	"virelai/vi"
)

func stubOverlaySinks(t *testing.T) (opened *int, closed *[]uint32, raised *[]uint32) {
	t.Helper()
	next := 60
	op := new(int)
	cl := new([]uint32)
	ra := new([]uint32)
	live := map[uint32]bool{}

	oldOpen, oldQuery, oldRaise, oldClose := openInputSink, queryWindow, focusRaise, closeWin
	oldApps, oldClipGet := readAppsFile, hostClipGet
	openInputSink = func(x, y, w, h uint32) (int, int64) {
		next++
		*op++
		live[uint32(next)] = true
		return next, int64(next)
	}
	queryWindow = func(id int) ([8]uint32, int64) {
		if !live[uint32(id)] {
			return [8]uint32{}, -vi.ErrEINVAL
		}
		return [8]uint32{0, 0, 10, 10, 0, 1}, 0
	}
	focusRaise = func(id uint32) int64 {
		*ra = append(*ra, id)
		return 0
	}
	closeWin = func(id uint32) int64 {
		*cl = append(*cl, id)
		delete(live, id)
		return 0
	}
	readAppsFile = func(string, int) ([]byte, int64) {
		return []byte("GOCALC.ELF | Calculator | c\n"), 0
	}
	hostClipGet = func(buf []byte) (int, int64) { return 0, -vi.ErrENOENT }
	t.Cleanup(func() {
		openInputSink, queryWindow, focusRaise, closeWin = oldOpen, oldQuery, oldRaise, oldClose
		readAppsFile, hostClipGet = oldApps, oldClipGet
	})
	return op, cl, ra
}

func TestOverlayRegistryOrderAndCompleteness(t *testing.T) {
	resetSeatForTest(t)

	if len(seatOverlays) != int(overlayCount) {
		t.Fatalf("registry length = %d, want %d", len(seatOverlays), overlayCount)
	}
	wantOrder := []struct {
		id   overlayID
		name string
	}{
		{overlayLock, "lock"},
		{overlayIdle, "idle"},
		{overlaySecretAsk, "secret-ask"},
		{overlayDragGhost, "drag-ghost"},
		{overlayNotifyCenter, "notify-center"},
		{overlayNotify, "notify"},
		{overlayPowerMenu, "power-menu"},
		{overlayClipMenu, "clip-menu"},
		{overlayRailMenu, "rail-menu"},
		{overlayLauncher, "launcher"},
		{overlayWatchdog, "watchdog"},
		{overlayHoverPreview, "hover-preview"},
	}
	if len(wantOrder) != int(overlayCount) {
		t.Fatalf("wantOrder covers %d entries, registry has %d", len(wantOrder), overlayCount)
	}
	seen := map[string]bool{}
	for i, w := range wantOrder {
		o := seatOverlays[i]
		if o.id != w.id || o.name != w.name {
			t.Fatalf("slot %d = (%d,%q), want (%d,%q)", i, o.id, o.name, w.id, w.name)
		}
		if seen[o.name] {
			t.Fatalf("duplicate overlay name %q", o.name)
		}
		seen[o.name] = true
		if o.open == nil {
			t.Fatalf("overlay %q missing open predicate", o.name)
		}
		if o.paint == nil {
			t.Fatalf("overlay %q missing paint handler", o.name)
		}
		if o.sink != nil {
			if o.id == overlayLock {
				if o.ensureSink != nil {
					t.Fatal("lock must never have an auto-reap ensureSink hook")
				}
			} else {
				if o.ensureSink == nil || o.close == nil || o.clearSink == nil {
					t.Fatalf("sink overlay %q missing reap/close/clearSink hook", o.name)
				}
			}
		}
		if o.dismissible {
			if o.close == nil {
				t.Fatalf("dismissible overlay %q missing close hook", o.name)
			}
			if o.blocksModals {
				t.Fatalf("overlay %q cannot be both dismissible and modal-blocking", o.name)
			}
		}
	}
	if !seatOverlays[overlayLock].blocksModals || !seatOverlays[overlaySecretAsk].blocksModals {
		t.Fatal("lock and secret-ask must both block dismissible modals")
	}
	if seatOverlays[overlayLock].dismissible || seatOverlays[overlaySecretAsk].dismissible {
		t.Fatal("lock and secret-ask must never be dismissible by other modals")
	}
}

func TestOverlayPaintOrderRepairsClipMenuAndSecretAsk(t *testing.T) {
	resetSeatForTest(t)
	theme.Current = theme.Dark
	tabs.OpenTab(3, "App")

	// 1. clipMenu now paints ABOVE watchdog banners (was buried in chromeTick).
	wdWedged[3] = true
	clipMenuOpen = true
	clipMenuRows = 2
	clipMenuList[0] = clipEntry{mime: "text/plain", blob: []byte("alpha")}
	clipMenuList[1] = clipEntry{mime: "text/plain", blob: []byte("bravo")}

	scan := make([]byte, vi.ScanoutWidth*vi.ScanoutHeight*4)
	pxAt := func(x, y int) uint32 {
		i := (y*vi.ScanoutWidth + x) * 4
		return (uint32(scan[i]) | uint32(scan[i+1])<<8 | uint32(scan[i+2])<<16) & 0xffffff
	}
	paintOverlays(scan, vi.ScanoutWidth, vi.ScanoutHeight)

	// Watchdog strip spans y=RailHeight..RailHeight+24 across the pane;
	// clipMenu starts at y=RailHeight+clipMenuGap (RailHeight+8), centered.
	cx, cy, cw, _ := clipMenuRect(vi.ScanoutWidth, vi.ScanoutHeight)
	strip, _, _, _ := watchdogLayout(3, vi.ScanoutWidth, vi.ScanoutHeight)
	if cy < strip.Y || cy >= strip.Y+strip.H {
		t.Fatalf("expected clipMenu top y=%d to overlap watchdog strip y=%d..%d",
			cy, strip.Y, strip.Y+strip.H)
	}
	// clipMenu's top border stroke (tok.Rule) must survive the overlap —
	// a watchdog painted over clipMenu would overwrite it with Surface.
	if got := pxAt(cx+cw/2, cy); got != theme.Current.Rule {
		t.Fatalf("clipMenu/watchdog overlap pixel = #%06x, want clipMenu Rule #%06x",
			got, theme.Current.Rule)
	}

	// 2. secretAsk now paints ABOVE notifyCenter (was painted below it).
	wdWedged[3] = false
	clipMenuOpen = false
	notifyCenterOpen = true
	secretAskModal = secretAsk{open: true, id: 1, image: "GOMAIL.ELF", key: "k"}
	sx, sy, sw, sh := secretAskRect(vi.ScanoutWidth, vi.ScanoutHeight)
	clear(scan)
	ovPx := paintOverlays(scan, vi.ScanoutWidth, vi.ScanoutHeight)
	if ovPx[overlaySecretAsk] == 0 || ovPx[overlayNotifyCenter] == 0 {
		t.Fatalf("painted px: secret=%d center=%d", ovPx[overlaySecretAsk], ovPx[overlayNotifyCenter])
	}
	// secretAsk's left accent rule (x=sx+1, mid-height) sits at ChromeBg/Accent;
	// verify secretAsk painted on top of notifyCenter's rect (if overlapping)
	// and verify secretAsk's paint index is strictly above notifyCenter and
	// railMenu in the registry.
	if overlaySecretAsk >= overlayNotifyCenter || overlaySecretAsk >= overlayRailMenu ||
		overlaySecretAsk >= overlayNotify {
		t.Fatal("secret-ask must outrank notify-center, notify, and rail-menu")
	}
	if got := pxAt(sx+1, sy+sh/2); got != theme.Current.Accent {
		t.Fatalf("secretAsk accent rule = #%06x, want #%06x", got, theme.Current.Accent)
	}
	_ = sw
}

func TestOverlayPointerAndKeyOrderRepairs(t *testing.T) {
	resetSeatForTest(t)
	stubOverlaySinks(t)
	tabs.OpenTab(3, "Edit")

	// 1. Key repair: secret-ask receives Escape BEFORE notifyCenter.
	notifyCenterOpen = true
	var dropped uint32
	secretAskDrop = func(id uint32) int64 { dropped = id; return 0 }
	secretAskModal = secretAsk{open: true, id: 9, image: "GOVAULT.ELF", key: "k"}

	handleWmKey(vi.Event{Kind: vi.EvWmKey, Arg0: 0x29}) // Esc
	if secretAskModal.open || dropped != 9 {
		t.Fatalf("Esc did not decline secret-ask first: open=%v dropped=%d", secretAskModal.open, dropped)
	}
	if !notifyCenterOpen {
		t.Fatal("Esc dismissed notify-center while secret-ask was standing")
	}
	// Second Esc now dismisses notify-center.
	handleWmKey(vi.Event{Kind: vi.EvWmKey, Arg0: 0x29})
	if notifyCenterOpen {
		t.Fatal("second Esc did not dismiss notify-center")
	}

	// 2. Key order unification: clipMenu outranks launcher (matching pointer).
	launch.open = true
	clipMenuOpen = true
	clipMenuRows = 1
	handleWmKey(vi.Event{Kind: vi.EvWmKey, Arg0: 0x29}) // Esc
	if clipMenuOpen {
		t.Fatal("Esc did not dismiss clip-menu ahead of launcher")
	}
	if !launch.open {
		t.Fatal("launcher should not have consumed clip-menu's Esc")
	}
	launch.open = false

	// 3. Pointer repair: clicking the bottom-right clock panel while a modal
	// is open dismisses the modal instead of opening notifyCenter over it.
	launch.open = true
	cx, cy, cw, ch := chromeRect(vi.ScanoutWidth, vi.ScanoutHeight)
	handleWmPointer(ptrEvent(uint32(cx+cw/2), uint32(cy+ch/2), hidBtnLeft))
	handleWmPointer(ptrEvent(uint32(cx+cw/2), uint32(cy+ch/2), 0))
	if launch.open {
		t.Fatal("clock click outside launcher did not dismiss launcher")
	}
	if notifyCenterOpen {
		t.Fatal("clock click while launcher was open opened notify-center over it")
	}
	// With no modal open, clicking the clock panel opens notify-center.
	handleWmPointer(ptrEvent(uint32(cx+cw/2), uint32(cy+ch/2), hidBtnLeft))
	handleWmPointer(ptrEvent(uint32(cx+cw/2), uint32(cy+ch/2), 0))
	if !notifyCenterOpen {
		t.Fatal("clock click with no modal open did not open notify-center")
	}
}

func TestOverlayReapCoversAllSinkModalsAndNeverReapsLock(t *testing.T) {
	resetSeatForTest(t)

	// Stub a dead sink: queryWindow returns -EINVAL.
	queryWindow = func(int) ([8]uint32, int64) { return [8]uint32{}, -vi.ErrEINVAL }
	closeWin = func(uint32) int64 { return 0 }
	focusRaise = func(uint32) int64 { return 0 }

	cases := []struct {
		name  string
		arm   func()
		still func() bool
	}{
		{"secret-ask", func() { secretAskModal = secretAsk{open: true, sink: 41} }, func() bool { return secretAskModal.open }},
		{"power-menu", func() { powerMenuOpen, powerSink = true, 42 }, func() bool { return powerMenuOpen }},
		{"clip-menu", func() { clipMenuOpen, clipMenuSink = true, 43 }, func() bool { return clipMenuOpen }},
		{"rail-menu", func() { railMenuOpen, railMenuSink = true, 44 }, func() bool { return railMenuOpen }},
		{"launcher", func() { launch.open, launch.sink = true, 45 }, func() bool { return launch.open }},
	}
	for _, tc := range cases {
		t.Run("reap/"+tc.name, func(t *testing.T) {
			resetSeatForTest(t)
			queryWindow = func(int) ([8]uint32, int64) { return [8]uint32{}, -vi.ErrEINVAL }
			closeWin = func(uint32) int64 { return 0 }
			focusRaise = func(uint32) int64 { return 0 }
			tc.arm()
			reapOverlays()
			if tc.still() {
				t.Fatalf("reapOverlays did not dismiss %s with dead sink", tc.name)
			}
		})
		t.Run("mirror/"+tc.name, func(t *testing.T) {
			resetSeatForTest(t)
			queryWindow = func(int) ([8]uint32, int64) { return [8]uint32{}, -vi.ErrEINVAL }
			closed := 0
			closeWin = func(uint32) int64 { closed++; return 0 }
			focusRaise = func(uint32) int64 { return 0 }
			tc.arm()
			// Find the sink id we armed via the registry.
			var id uint32
			for i := range seatOverlays {
				o := &seatOverlays[i]
				if o.open() && o.sink != nil {
					id = o.sink()
				}
			}
			reapOverlaySink(id)
			if tc.still() {
				t.Fatalf("reapOverlaySink(%d) did not dismiss %s", id, tc.name)
			}
			if closed != 0 {
				t.Fatalf("reapOverlaySink called closeWin %d times on dead/reused sink %d", closed, id)
			}
		})
	}

	// Lock must NEVER be reaped even if its sink vanishes.
	resetSeatForTest(t)
	queryWindow = func(int) ([8]uint32, int64) { return [8]uint32{}, -vi.ErrEINVAL }
	lockActive, lockSink = true, 99
	reapOverlays()
	reapOverlaySink(99)
	if !lockActive {
		t.Fatal("reap unlocked the screen on a dead lock sink")
	}
}

func TestOverlayDerivedPredicatesAgreeWithRegistry(t *testing.T) {
	resetSeatForTest(t)
	tabs.OpenTab(5, "Edit")
	tabs.tabs[tabs.index(5)].Bin = "GOEDIT.ELF"
	rpcBindings[5] = rpcBinding{pid: 5, token: 0x5e1}
	surfaceXY[5] = 0
	gpu3dRects[5] = uint32(vi.ScanoutWidth) | uint32(vi.ScanoutHeight)<<16
	if !serveRegionDeclare(regionReq(5, 0, vi.CursorTxt, 0, 24, uint16(vi.ScanoutWidth), uint16(vi.ScanoutHeight-24))) {
		t.Fatal("serveRegionDeclare failed")
	}

	// 1. seatModalFocus covers ALL sink-holding overlays (repaired defect).
	sinkCases := []struct {
		name string
		arm  func(sink uint32)
		off  func()
	}{
		{"lock", func(s uint32) { lockActive, lockSink = true, s }, func() { lockActive, lockSink = false, 0 }},
		{"secret-ask", func(s uint32) { secretAskModal = secretAsk{open: true, sink: s} }, func() { secretAskModal = secretAsk{} }},
		{"power-menu", func(s uint32) { powerMenuOpen, powerSink = true, s }, func() { powerMenuOpen, powerSink = false, 0 }},
		{"clip-menu", func(s uint32) { clipMenuOpen, clipMenuSink = true, s }, func() { clipMenuOpen, clipMenuSink = false, 0 }},
		{"rail-menu", func(s uint32) { railMenuOpen, railMenuSink = true, s }, func() { railMenuOpen, railMenuSink = false, 0 }},
		{"launcher", func(s uint32) { launch.open, launch.sink = true, s }, func() { launch.open, launch.sink = false, 0 }},
	}
	for _, tc := range sinkCases {
		tc.arm(0)
		if seatModalFocus() {
			t.Fatalf("%s with sink=0 reported seatModalFocus=true", tc.name)
		}
		if !seatChromeModal() {
			t.Fatalf("%s reported seatChromeModal=false", tc.name)
		}
		tc.arm(77)
		if !seatModalFocus() {
			t.Fatalf("%s with sink=77 reported seatModalFocus=false", tc.name)
		}
		if v := focusBadgeState(); v.scope != "seat" {
			t.Fatalf("%s with sink=77 focusBadgeState=%+v, want scope=seat", tc.name, v)
		}
		if got := pickCursorShape(400, 300); got != vi.CursorPtr {
			t.Fatalf("%s pickCursorShape=%d, want CursorPtr", tc.name, got)
		}
		tc.off()
	}

	// 2. Watchdog banner suppresses app cursor region under its strip (repaired defect).
	if got := pickCursorShape(400, uint32(RailHeight+4)); got != vi.CursorTxt {
		t.Fatalf("baseline region pick = %d, want CursorTxt", got)
	}
	wdWedged[5] = true
	if got := pickCursorShape(400, uint32(RailHeight+4)); got != vi.CursorPtr {
		t.Fatalf("region pick under watchdog banner = %d, want CursorPtr", got)
	}
	wdWedged[5] = false

	// 3. lockFocusTarget prioritizes secret-ask ahead of launcher and sees all sinks.
	launch.open, launch.sink = true, 51
	if got := lockFocusTarget(); got != 51 {
		t.Fatalf("lockFocusTarget(launcher) = %d, want 51", got)
	}
	secretAskModal = secretAsk{open: true, sink: 52}
	if got := lockFocusTarget(); got != 52 {
		t.Fatalf("lockFocusTarget(secret-ask + launcher) = %d, want 52", got)
	}
	secretAskModal = secretAsk{}
	launch.open, launch.sink = false, 0
	clipMenuOpen, clipMenuSink = true, 53
	if got := lockFocusTarget(); got != 53 {
		t.Fatalf("lockFocusTarget(clip-menu) = %d, want 53", got)
	}
	clipMenuOpen, clipMenuSink = false, 0
	if got := lockFocusTarget(); got != 5 {
		t.Fatalf("lockFocusTarget(tab only) = %d, want 5", got)
	}
}

func TestOverlayTwoModalRule(t *testing.T) {
	resetSeatForTest(t)

	type modalSpec struct {
		name string
		id   overlayID
		open func()
		isUp func() bool
	}
	modals := []modalSpec{
		{"notify-center", overlayNotifyCenter, openNotifyCenter, func() bool { return notifyCenterOpen }},
		{"power-menu", overlayPowerMenu, openPowerMenu, func() bool { return powerMenuOpen }},
		{"clip-menu", overlayClipMenu, applyClipMenu, func() bool { return clipMenuOpen }},
		{"rail-menu", overlayRailMenu, func() { openRailMenu(0) }, func() bool { return railMenuOpen }},
		{"launcher", overlayLauncher, openLauncher, func() bool { return launch.open && !launch.palette }},
		{"palette", overlayLauncher, openPalette, func() bool { return launch.open && launch.palette }},
	}

	setup := func(t *testing.T) {
		resetSeatForTest(t)
		stubOverlaySinks(t)
		tabs.OpenTab(3, "Edit")
		tabs.OpenTab(4, "Calc")
		cliphistPush([]byte("hello"))
	}

	// 1. Opening any dismissible modal closes any other dismissible modal.
	for _, a := range modals {
		for _, b := range modals {
			if a.name == b.name {
				continue
			}
			t.Run(a.name+"_then_"+b.name, func(t *testing.T) {
				setup(t)
				a.open()
				if !a.isUp() {
					t.Fatalf("failed to open %s", a.name)
				}
				b.open()
				if !b.isUp() {
					t.Fatalf("failed to open %s over %s", b.name, a.name)
				}
				if a.isUp() {
					t.Fatalf("opening %s left %s open", b.name, a.name)
				}
			})
		}
	}

	// 2. Launcher <-> palette crossover keeps the same sink window in place.
	t.Run("launcher_palette_crossover_reuses_sink", func(t *testing.T) {
		setup(t)
		openLauncher()
		sink := launch.sink
		if !launch.open || launch.palette || sink == 0 {
			t.Fatalf("openLauncher: open=%v palette=%v sink=%d", launch.open, launch.palette, sink)
		}
		openPalette()
		if !launch.open || !launch.palette || launch.sink != sink {
			t.Fatalf("openPalette crossover: open=%v palette=%v sink=%d want %d",
				launch.open, launch.palette, launch.sink, sink)
		}
		openLauncher()
		if !launch.open || launch.palette || launch.sink != sink {
			t.Fatalf("openLauncher crossover: open=%v palette=%v sink=%d want %d",
				launch.open, launch.palette, launch.sink, sink)
		}
	})

	// 3. Secret-ask closes any open dismissible modal and refuses all new ones.
	t.Run("secret_ask_outranks_and_blocks_dismissible_modals", func(t *testing.T) {
		for _, m := range modals {
			setup(t)
			m.open()
			if !m.isUp() {
				t.Fatalf("failed to open %s", m.name)
			}
			openSecretAsk(secretAsk{id: 7, image: "GOMAIL.ELF", key: "k"})
			if !secretAskModal.open {
				t.Fatal("secret-ask did not open")
			}
			if m.isUp() {
				t.Fatalf("opening secret-ask did not close %s", m.name)
			}
			// Now try opening every dismissible modal while secret-ask stands.
			for _, other := range modals {
				other.open()
				if other.isUp() {
					t.Fatalf("%s opened while secret-ask was standing", other.name)
				}
				if !secretAskModal.open {
					t.Fatalf("attempting to open %s dismissed secret-ask", other.name)
				}
			}
		}
	})

	// 4. Lock closes dismissible modals, never dismisses secret-ask, and blocks
	// dismissible modals while locked.
	t.Run("lock_outranks_all_and_preserves_secret_ask", func(t *testing.T) {
		for _, m := range modals {
			setup(t)
			lockReadFile = func(string, int) ([]byte, int64) { return lockTestVault(t), 0 }
			m.open()
			if !m.isUp() {
				t.Fatalf("failed to open %s", m.name)
			}
			requestLock()
			if !lockActive {
				t.Fatalf("lock failed to engage over %s", m.name)
			}
			if m.isUp() {
				t.Fatalf("engaging lock left %s open", m.name)
			}
			for _, other := range modals {
				other.open()
				if other.isUp() {
					t.Fatalf("%s opened while locked", other.name)
				}
			}
		}

		// Secret-ask survives lock and receives focus back on unlock.
		setup(t)
		_, _, raised := stubOverlaySinks(t)
		lockReadFile = func(string, int) ([]byte, int64) { return lockTestVault(t), 0 }
		openSecretAsk(secretAsk{id: 7, image: "GOMAIL.ELF", key: "k"})
		askSink := secretAskModal.sink
		requestLock()
		if !lockActive || !secretAskModal.open {
			t.Fatalf("lock over secret-ask: lock=%v ask=%v", lockActive, secretAskModal.open)
		}
		typeLock(t, string(lockTestPass))
		lockKeyDown(vi.Event{Kind: vi.EvKeyDown, Arg0: 0x28})
		if lockActive || !secretAskModal.open {
			t.Fatalf("after unlock: lock=%v ask=%v", lockActive, secretAskModal.open)
		}
		if len(*raised) == 0 || (*raised)[len(*raised)-1] != askSink {
			t.Fatalf("unlock focus handback = %v, want last=%d", *raised, askSink)
		}
	})

	// 5. At the 8-window ceiling, switching between two modals parks the
	// reserve window before the second modal borrows it.
	t.Run("ceiling_reserve_handoff_between_modals", func(t *testing.T) {
		resetSeatForTest(t)
		k := ceilSetup(t)
		seatAtCeiling(k)
		closeWin = ceilClose(k)
		openInputSink = openSeatSink
		openPowerMenu()
		if !powerMenuOpen || powerSink != reserveWin.id || !reserveWin.lent {
			t.Fatalf("powerMenu at ceiling: open=%v sink=%d lent=%v",
				powerMenuOpen, powerSink, reserveWin.lent)
		}
		openLauncher()
		if powerMenuOpen {
			t.Fatal("opening launcher did not close powerMenu")
		}
		if !launch.open || launch.sink != reserveWin.id || !reserveWin.lent {
			t.Fatalf("launcher at ceiling did not acquire reserve: open=%v sink=%d lent=%v err=%q",
				launch.open, launch.sink, reserveWin.lent, launch.err)
		}
		openSecretAsk(secretAsk{id: 9, image: "GOVAULT.ELF", key: "k"})
		if launch.open {
			t.Fatal("opening secret-ask did not close launcher")
		}
		if !secretAskModal.open || secretAskModal.sink != reserveWin.id || !reserveWin.lent {
			t.Fatalf("secret-ask at ceiling did not acquire reserve: open=%v sink=%d lent=%v",
				secretAskModal.open, secretAskModal.sink, reserveWin.lent)
		}
	})
}

func TestHandleWmPointerSplitLayers(t *testing.T) {
	resetSeatForTest(t)
	stubOverlaySinks(t)
	tabs.OpenTab(3, "Edit")
	tabs.OpenTab(4, "Calc")

	var fwd []uint32
	forwardContentPointer = func(px, py uint32, btn, mods uint8) int64 {
		fwd = append(fwd, px|py<<16)
		return 0
	}

	// 1. Overlay layer consumes before rail/sash/client.
	openPowerMenu()
	handleWmPointer(ptrEvent(500, 400, hidBtnLeft))
	handleWmPointer(ptrEvent(500, 400, 0))
	if powerMenuOpen || len(fwd) != 0 {
		t.Fatalf("overlay layer: open=%v fwd=%v", powerMenuOpen, fwd)
	}

	// 2. Rail/sash/pane layer consumes rail clicks before client forward.
	handleWmPointer(ptrEvent(960, 10, hidBtnLeft))
	handleWmPointer(ptrEvent(960, 10, 0))
	if len(fwd) != 0 {
		t.Fatalf("rail click forwarded to client: %v", fwd)
	}

	// 3. Client-area forward receives press + release outside overlays/chrome.
	handleWmPointer(ptrEvent(500, 400, hidBtnLeft))
	handleWmPointer(ptrEvent(500, 400, 0))
	if len(fwd) != 2 {
		t.Fatalf("client press/release forwarded %d samples, want 2", len(fwd))
	}
}
