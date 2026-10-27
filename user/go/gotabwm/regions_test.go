package main

// #3348: the WM_RPC region declare (kind 31) — the seat's slot table,
// the refusals, and the pick consulting declared zones only inside
// their window's live rect while seat gestures and chrome keep
// precedence. The fixture mirrors selFixture: real TabStrip rows plus
// the kind-20 mirror pair as the geometry source.

import (
	"testing"

	"virelai/vi"
)

type regionFixture struct{}

func (f *regionFixture) install(t *testing.T) {
	t.Helper()
	oldTabs, oldBind := tabs, rpcBindings
	oldXY, oldRects := surfaceXY, gpu3dRects
	oldDrag, oldGhost := drag, dragGhostPainted
	oldSash, oldSashNode, oldRail := sashDragging, sashDragNode, railDragFrom
	oldLaunch, oldPower, oldRailMenu := launch, powerMenuOpen, railMenuOpen
	oldSecret, oldClip := secretAskModal, clipMenuOpen
	oldCenter, oldQueue := notifyCenterOpen, notifyQueue
	oldSet, oldCur, oldSeen := setCursorShape, curCursorShape, cursorShapeSeen
	t.Cleanup(func() {
		tabs, rpcBindings = oldTabs, oldBind
		surfaceXY, gpu3dRects = oldXY, oldRects
		drag, dragGhostPainted = oldDrag, oldGhost
		sashDragging, sashDragNode, railDragFrom = oldSash, oldSashNode, oldRail
		launch, powerMenuOpen, railMenuOpen = oldLaunch, oldPower, oldRailMenu
		secretAskModal, clipMenuOpen = oldSecret, oldClip
		notifyCenterOpen, notifyQueue = oldCenter, oldQueue
		setCursorShape, curCursorShape, cursorShapeSeen = oldSet, oldCur, oldSeen
	})
	tabs = TabStrip{}
	rpcBindings = [256]rpcBinding{}
	surfaceXY, gpu3dRects = [256]uint32{}, [256]uint32{}
	drag, dragGhostPainted = dragSession{}, false
	sashDragging, sashDragNode, railDragFrom = false, -1, -1
	launch, powerMenuOpen, railMenuOpen = launcherState{}, false, false
	secretAskModal, clipMenuOpen = secretAsk{}, false
	notifyCenterOpen, notifyQueue = false, nil
	curCursorShape, cursorShapeSeen = vi.CursorPtr, [10]bool{}
}

// hostTab puts a bound live tab on the strip at rect (x,y,w,h) — the
// kind-20 mirror pair records the same live geometry the kernel's
// SET_WINDOW mirror would push.
func (f *regionFixture) hostTab(id uint32, bin string, x, y, w, h uint32) {
	if !tabs.OpenTab(id, bin+" window") {
		panic("hostTab: OpenTab refused")
	}
	tabs.tabs[tabs.index(id)].Bin = bin
	rpcBindings[id] = rpcBinding{pid: uint8(id), token: 0x5e1}
	surfaceXY[id] = (x & 0xffff) | (y&0xffff)<<16
	gpu3dRects[id] = (w & 0xffff) | (h&0xffff)<<16
}

func regionReq(id uint32, slot uint8, shape uint64, x, y, w, h uint16) vi.WmRpc {
	var r vi.WmRpc
	r.Kind = vi.WmRpcKindRegionDeclare
	r.ID = uint8(id)
	r.ReplyTo = uint8(id)
	t, ok := vi.EncodeRegionTitle(slot, shape, x, y, w, h)
	if !ok {
		panic("regionReq: encode refused")
	}
	r.Title = t
	return r
}

func TestRegionDeclareStoresAndClears(t *testing.T) {
	f := &regionFixture{}
	f.install(t)
	f.hostTab(5, "GOEDIT.ELF", 0, 0, 1280, 720)
	if !serveRegionDeclare(regionReq(5, 0, vi.CursorTxt, 0, 24, 1280, 696)) {
		t.Fatal("a legal declare refused")
	}
	r := tabs.tabs[tabs.index(5)].regions[0]
	if r.shape != uint8(vi.CursorTxt) || r.x != 0 || r.y != 24 ||
		r.w != 1280 || r.h != 696 {
		t.Fatalf("slot 0 = %+v", r)
	}
	// A second declare on the same slot replaces; w=h=0 clears it.
	if !serveRegionDeclare(regionReq(5, 0, vi.CursorLnk, 8, 40, 100, 12)) {
		t.Fatal("a re-declare refused")
	}
	r = tabs.tabs[tabs.index(5)].regions[0]
	if r.shape != uint8(vi.CursorLnk) || r.w != 100 {
		t.Fatalf("replaced slot 0 = %+v", r)
	}
	if !serveRegionDeclare(regionReq(5, 0, vi.CursorPtr, 0, 0, 0, 0)) {
		t.Fatal("the clear form refused")
	}
	if r = tabs.tabs[tabs.index(5)].regions[0]; r.w != 0 || r.h != 0 {
		t.Fatalf("cleared slot 0 = %+v", r)
	}
}

func TestRegionDeclareRefusals(t *testing.T) {
	f := &regionFixture{}
	f.install(t)
	f.hostTab(5, "GOEDIT.ELF", 0, 0, 1280, 720)
	// A window not on the strip refuses — the seat stores regions on
	// tabs, so there is nowhere honest for a stranger's zone to land.
	if serveRegionDeclare(regionReq(9, 0, vi.CursorTxt, 0, 0, 10, 10)) {
		t.Fatal("a non-tab declare applied")
	}
	// The codec's refuse shapes never reach the seat over the wire; a
	// hand-forged frame with a bad slot still has to refuse here.
	var forged vi.WmRpc
	forged.Kind = vi.WmRpcKindRegionDeclare
	forged.ID, forged.ReplyTo = 5, 5
	forged.Title[0] = regionSlotsMax // slot past the table
	if serveRegionDeclare(forged) {
		t.Fatal("a slot past the table applied")
	}
	forged.Title[0], forged.Title[1] = 0, 10 // shape past the table
	if serveRegionDeclare(forged) {
		t.Fatal("a shape past the table applied")
	}
	forged.Title[1] = 1
	forged.Title[8] = 9 // h=9 with w=0 — the half-empty rect
	if serveRegionDeclare(forged) {
		t.Fatal("a half-empty rect applied")
	}
	if r := tabs.tabs[tabs.index(5)].regions[0]; r.w != 0 || r.h != 0 {
		t.Fatalf("a refused declare still wrote slot 0: %+v", r)
	}
}

func TestRegionPickFollowsTheDeclaredZone(t *testing.T) {
	f := &regionFixture{}
	f.install(t)
	f.hostTab(5, "GOEDIT.ELF", 0, 0, 1280, 720)
	if !serveRegionDeclare(regionReq(5, 0, vi.CursorTxt, 0, 24, 1280, 696)) {
		t.Fatal("declare refused")
	}
	// Inside the document band the pick is the declared i-beam; the
	// app's own chrome row (y < 24) and the seat's bottom strip keep
	// the arrow, and the rail keeps its own picks entirely.
	if got := pickCursorShape(400, 300); got != vi.CursorTxt {
		t.Fatalf("pick inside the region = %d, want txt", got)
	}
	if got := pickCursorShape(400, 23); got != vi.CursorPtr {
		t.Fatalf("pick on the app's chrome row = %d, want ptr", got)
	}
	if got := pickCursorShape(400, 710); got != vi.CursorPtr {
		t.Fatalf("pick on the bottom chrome = %d, want ptr", got)
	}
	// A cleared slot falls back to the arrow on the next sample.
	if !serveRegionDeclare(regionReq(5, 0, vi.CursorPtr, 0, 0, 0, 0)) {
		t.Fatal("clear refused")
	}
	if got := pickCursorShape(400, 300); got != vi.CursorPtr {
		t.Fatalf("pick after clear = %d, want ptr", got)
	}
}

func TestRegionPickTranslatesPaneOrigin(t *testing.T) {
	f := &regionFixture{}
	f.install(t)
	// A split's second pane: the window sits at (660,0), so a sample at
	// (700,100) is window-local (40,100). Narrow zones declare in LOWER
	// slots than the broad zones that cover them — the lowest set slot
	// containing the point wins, so slot 0's link zone beats slot 1's
	// document band inside the overlap.
	f.hostTab(5, "GOEDIT.ELF", 660, 0, 620, 720)
	if !serveRegionDeclare(regionReq(5, 0, vi.CursorLnk, 300, 40, 120, 60)) {
		t.Fatal("link declare refused")
	}
	if !serveRegionDeclare(regionReq(5, 1, vi.CursorTxt, 0, 24, 620, 696)) {
		t.Fatal("declare refused")
	}
	if got := pickCursorShape(700, 100); got != vi.CursorTxt {
		t.Fatalf("pick at pane-local (40,100) = %d, want txt", got)
	}
	if got := pickCursorShape(960, 60); got != vi.CursorLnk {
		t.Fatalf("pick at pane-local (300,60) = %d, want lnk", got)
	}
}

func TestRegionPickYieldsToSeatOwnership(t *testing.T) {
	f := &regionFixture{}
	f.install(t)
	f.hostTab(5, "GOEDIT.ELF", 0, 0, 1280, 720)
	if !serveRegionDeclare(regionReq(5, 0, vi.CursorTxt, 0, 24, 1280, 696)) {
		t.Fatal("declare refused")
	}
	// A modal seat surface owns every sample — the region beneath must
	// not pick.
	launch.open = true
	if got := pickCursorShape(400, 300); got != vi.CursorPtr {
		t.Fatalf("pick under the open launcher = %d, want ptr", got)
	}
	launch.open = false
	// An armed guest drag owns the pointer outright.
	drag.armed = true
	if got := pickCursorShape(400, 300); got != vi.CursorGrg {
		t.Fatalf("pick mid-drag = %d, want grg", got)
	}
	drag.armed = false
	// And the kernel accepts only through the state swap: update applies
	// the pick once, latched, and a same-shape sample is silent.
	var calls []uint64
	setCursorShape = func(s uint64) int64 { calls = append(calls, s); return 0 }
	updateCursorShape(400, 300)
	updateCursorShape(410, 300)
	if len(calls) != 1 || calls[0] != vi.CursorTxt {
		t.Fatalf("setCursorShape calls = %v, want one txt", calls)
	}
	if !cursorShapeSeen[vi.CursorTxt] {
		t.Fatal("the txt receipt did not latch")
	}
}
