// Host tests for #3914 / M216n (#3953) rail hover-preview. The card is
// seat state: dwell rides the 500 ms ADR 0060 deadline on the seat timer
// queue (with heartbeat fallback when slot 95 is refused), the target is
// a tab identity, and every conflicting desktop state (including lock and
// idle) cancels both the pending timer and the card.

package main

import (
	"strings"
	"testing"

	"virelai/vi"
)

// hoverPreviewFixture isolates the preview state, timer queue seams, and
// every desktop-state flag the suppression predicate reads.
type hoverPreviewFixture struct {
	now       int64
	armedNs   []uint64
	cancels   int
	armRC     int64
	logs      []string
	timerLogs []string
}

func newHoverPreviewFixture(t *testing.T, ids ...uint32) *hoverPreviewFixture {
	t.Helper()
	savedTabs, savedHover, savedTick := tabs, railHover, seatTick
	savedState := hoverPreview
	savedLog := hoverPreviewLog
	savedLaunch, savedPower, savedClip := launch, powerMenuOpen, clipMenuOpen
	savedRailMenu, savedSecret, savedCenter := railMenuOpen, secretAskModal, notifyCenterOpen
	savedLock, savedIdle, savedDrag, savedRailDrag, savedSash := lockActive, idleState, drag, railDragFrom, sashDragging
	savedTimers := seatTimers
	savedNow, savedSetNs, savedCancel, savedTLog := seatTimerNow, seatTimerSetNs, seatTimerCancel, seatTimerLog
	t.Cleanup(func() {
		tabs, railHover, seatTick = savedTabs, savedHover, savedTick
		hoverPreview = savedState
		hoverPreviewLog = savedLog
		launch, powerMenuOpen, clipMenuOpen = savedLaunch, savedPower, savedClip
		railMenuOpen, secretAskModal, notifyCenterOpen = savedRailMenu, savedSecret, savedCenter
		lockActive, idleState, drag, railDragFrom, sashDragging = savedLock, savedIdle, savedDrag, savedRailDrag, savedSash
		seatTimers = savedTimers
		seatTimerNow, seatTimerSetNs, seatTimerCancel, seatTimerLog = savedNow, savedSetNs, savedCancel, savedTLog
	})
	tabs = TabStrip{}
	railHover = -1
	seatTick = 0
	hoverPreview = hoverPreviewState{cell: -1, block: -1}
	launch.open = false
	powerMenuOpen = false
	clipMenuOpen = false
	railMenuOpen = false
	secretAskModal = secretAsk{}
	notifyCenterOpen = false
	lockActive = false
	idleState = idleActive
	drag = dragSession{}
	railDragFrom = -1
	sashDragging = false
	seatTimers = seatTimerQueue{}
	f := &hoverPreviewFixture{now: 1_000_000_000}
	hoverPreviewLog = func(line string) { f.logs = append(f.logs, line) }
	seatTimerNow = func() int64 { return f.now }
	seatTimerSetNs = func(ns uint64) int64 {
		f.armedNs = append(f.armedNs, ns)
		return f.armRC
	}
	seatTimerCancel = func() int64 {
		f.cancels++
		return 1
	}
	seatTimerLog = func(line string) { f.timerLogs = append(f.timerLogs, line) }
	for _, id := range ids {
		if !tabs.OpenTab(id, "Tab "+vi.Itoa64(int64(id))) {
			t.Fatalf("OpenTab(%d)", id)
		}
	}
	return f
}

// viPointer builds the WM_POINTER event handleWmPointer/consumeSeatEvent consumes.
func viPointer(x, y uint32) vi.Event {
	return vi.Event{Kind: vi.EvWmPointer, Arg0: x | y<<16, Flags: 0}
}

func (f *hoverPreviewFixture) advanceAndFire(ns int64) {
	f.now += ns
	consumeSeatEvent(vi.Event{Kind: vi.EvTimer})
}

func (f *hoverPreviewFixture) dwellToVisible(t *testing.T) {
	t.Helper()
	hoverPreviewReconcile()
	if hoverPreview.visible {
		t.Fatal("card appeared before the 500ms dwell elapsed")
	}
	f.advanceAndFire(hoverPreviewDwellNs)
	if !hoverPreview.visible {
		t.Fatal("card did not appear when the 500ms dwell fired")
	}
}

func (f *hoverPreviewFixture) contains(t *testing.T, want string) bool {
	t.Helper()
	for _, line := range f.logs {
		if strings.Contains(line, want) {
			return true
		}
	}
	return false
}

// Dwell runs on the 500 ms ADR 0060 deadline queue, independent of the
// 1 Hz heartbeat: heartbeats alone do not reveal the card, and firing
// EvTimer at 500 ms reveals it without advancing seatTick.
func TestHoverPreviewDwellUsesTimerQueue(t *testing.T) {
	resetSeatForTest(t)
	f := newHoverPreviewFixture(t, 3, 4)
	railHover = 1
	seatTick = 10
	hoverPreviewPointer()

	hoverPreviewReconcile()
	if hoverPreview.visible {
		t.Fatal("card appeared immediately on arm")
	}
	if len(f.armedNs) != 1 || f.armedNs[0] != uint64(hoverPreviewDwellNs) {
		t.Fatalf("armedNs = %v want [%d]", f.armedNs, hoverPreviewDwellNs)
	}

	// Passing heartbeats do not reveal the card while slot 95 is healthy.
	seatTick = 12
	hoverPreviewTick()
	if hoverPreview.visible {
		t.Fatal("heartbeat revealed card while timer queue was healthy")
	}

	// Early EvTimer (499ms) does not reveal the card.
	f.advanceAndFire(hoverPreviewDwellNs - 1_000_000)
	if hoverPreview.visible {
		t.Fatal("card appeared 1ms early")
	}

	// Reaching 500ms reveals the card without changing seatTick.
	f.advanceAndFire(1_000_000)
	if !hoverPreview.visible {
		t.Fatal("card did not appear at the 500ms deadline")
	}
	if hoverPreview.id != 4 {
		t.Fatalf("preview target id = %d want 4", hoverPreview.id)
	}
	if seatTick != 12 {
		t.Fatalf("EvTimer moved seatTick to %d want 12", seatTick)
	}
}

// When slot 95 is refused (an image without the `timer` grant), the seat
// logs one named refusal marker and falls back to the 2-heartbeat dwell.
func TestHoverPreviewRefusalFallsBackToHeartbeatDwell(t *testing.T) {
	resetSeatForTest(t)
	f := newHoverPreviewFixture(t, 3, 4)
	f.armRC = -vi.ErrEACCES
	railHover = 1
	seatTick = 10
	hoverPreviewPointer()

	hoverPreviewTick() // tries slot 95, gets refused, arms at heartbeat 10
	if !seatTimers.Refused() {
		t.Fatal("queue did not latch refusal")
	}
	if len(f.timerLogs) != 1 || !strings.HasPrefix(f.timerLogs[0], MarkerTimerRefused) {
		t.Fatalf("timer refusal logs = %v want one %q line", f.timerLogs, MarkerTimerRefused)
	}
	if hoverPreview.visible {
		t.Fatal("card appeared on the arming heartbeat")
	}
	hoverPreviewTick() // same heartbeat repetition must not age dwell
	if hoverPreview.visible {
		t.Fatal("paint-frequency repetition aged the fallback dwell")
	}
	seatTick = 11
	hoverPreviewTick()
	if hoverPreview.visible {
		t.Fatalf("card appeared one heartbeat early (dwell=%d)", seatTick-hoverPreview.armTick)
	}
	seatTick = 12
	hoverPreviewTick()
	if !hoverPreview.visible {
		t.Fatal("card did not appear at the heartbeat fallback threshold")
	}
	if hoverPreview.id != 4 {
		t.Fatalf("preview target id = %d want 4", hoverPreview.id)
	}

	// Coalesced heartbeats in refusal mode still reach the threshold, and
	// the refusal marker is never repeated.
	hoverPreview = hoverPreviewState{cell: -1, block: -1}
	seatTick = 20
	hoverPreviewPointer()
	hoverPreviewTick()
	seatTick = 22
	hoverPreviewTick()
	if !hoverPreview.visible {
		t.Fatal("coalesced two-heartbeat jump did not reach the fallback dwell threshold")
	}
	if len(f.timerLogs) != 1 {
		t.Fatalf("refusal marker repeated: %v", f.timerLogs)
	}
}

// The receipt follows the present: no marker for an unpresented card, one
// marker for the presented frame, and dismissal only after a frame
// without the card actually flushed.
func TestHoverPreviewMarkersFollowPresentation(t *testing.T) {
	resetSeatForTest(t)
	f := newHoverPreviewFixture(t, 3, 4)
	railHover = 1
	hoverPreviewPointer()
	f.dwellToVisible(t)
	hoverPreviewPresented(true, false) // paint failed to present
	if len(f.logs) != 0 {
		t.Fatalf("marker before a present: %v", f.logs)
	}
	hoverPreviewPresented(true, true)
	if !f.contains(t, MarkerHoverPreview+"4 ") {
		t.Fatalf("missing post-present preview receipt: %v", f.logs)
	}
	hoverPreviewPresented(true, true)
	if n := strings.Count(strings.Join(f.logs, "\n"), MarkerHoverPreview); n != 1 {
		t.Fatalf("preview receipt repeated: n=%d logs=%v", n, f.logs)
	}
	if hoverPreview.shown != 4 {
		t.Fatalf("shown id = %d want 4", hoverPreview.shown)
	}

	// A failed present is not evidence the announced card left the
	// scanout: the prior frame still shows it.
	hoverPreviewPresented(true, false)
	hoverPreviewPresented(false, false)
	if hoverPreview.shown != 4 {
		t.Fatalf("failed present cleared shown: %d", hoverPreview.shown)
	}
	if n := strings.Count(strings.Join(f.logs, "\n"), MarkerHoverPreviewDismiss); n != 0 {
		t.Fatalf("failed present emitted a dismissal: n=%d logs=%v", n, f.logs)
	}

	// Successful recovery: the announced card is already on the scanout.
	hoverPreviewPresented(true, true)
	if n := strings.Count(strings.Join(f.logs, "\n"), MarkerHoverPreview); n != 1 {
		t.Fatalf("successful recovery reannounced the card: n=%d logs=%v", n, f.logs)
	}

	// One successfully presented card-free frame dismisses exactly once.
	hoverPreviewPointer()
	hoverPreviewPresented(false, true)
	if !f.contains(t, MarkerHoverPreviewDismiss+"4") {
		t.Fatalf("missing post-present dismiss receipt: %v", f.logs)
	}
	if n := strings.Count(strings.Join(f.logs, "\n"), MarkerHoverPreviewDismiss); n != 1 {
		t.Fatalf("dismiss receipt count = %d want 1: %v", n, f.logs)
	}
	if hoverPreview.shown != 0 {
		t.Fatalf("shown id after dismissal = %d want 0", hoverPreview.shown)
	}
	if strings.Contains(strings.Join(f.logs, "\n"), MarkerHoverPreview+"4 title") == false {
		t.Fatal("preview receipt lost its title payload")
	}
}

// Motion — including same-cell motion — and leaving the rail dismiss a
// visible card and restart/cancel the pending deadline.
func TestHoverPreviewMotionAndLeaveResetDwell(t *testing.T) {
	resetSeatForTest(t)
	f := newHoverPreviewFixture(t, 3, 4)
	railHover = 1
	hoverPreviewPointer()
	f.dwellToVisible(t)

	// Same-cell motion through consumeSeatEvent dismisses the card and
	// starts a fresh 500ms deadline from the new pointer sample.
	consumeSeatEvent(viPointer(1000, 10))
	if hoverPreview.visible {
		t.Fatal("same-cell motion left the card visible")
	}
	if railHover != 1 {
		t.Fatalf("same-cell motion moved the hover: %d", railHover)
	}
	if !seatTimers.Pending(timerKindHoverDwell) {
		t.Fatal("same-cell motion did not re-arm the 500ms dwell")
	}
	f.advanceAndFire(hoverPreviewDwellNs / 2)
	if hoverPreview.visible {
		t.Fatal("card resurrected before the fresh 500ms dwell elapsed")
	}
	f.advanceAndFire(hoverPreviewDwellNs / 2)
	if !hoverPreview.visible {
		t.Fatal("a fresh 500ms dwell after motion did not show the card")
	}

	// Leaving the rail clears the card and cancels the timer entry.
	consumeSeatEvent(viPointer(1000, 360))
	if hoverPreview.visible || hoverPreview.armed || seatTimers.Pending(timerKindHoverDwell) {
		t.Fatal("rail leave left the dwell or card active")
	}
	f.advanceAndFire(hoverPreviewDwellNs)
	if hoverPreview.visible {
		t.Fatal("dwell fired after a rail leave")
	}
}

// Reordering or replacing a tab cannot transfer a dwell to a different
// identity, and closing the target removes its card.
func TestHoverPreviewIdentityChangeAndCloseReset(t *testing.T) {
	resetSeatForTest(t)
	f := newHoverPreviewFixture(t, 3, 4, 5)
	railHover = 1
	hoverPreviewPointer()
	f.dwellToVisible(t)
	if hoverPreview.id != 4 {
		t.Fatalf("fixture card id = %d want 4", hoverPreview.id)
	}

	// Move cell 1's tab elsewhere: a different identity now sits under
	// the same pointer. The old dwell must not travel with the index.
	if !tabs.Reorder(1, 2) {
		t.Fatal("Reorder")
	}
	hoverPreviewTick()
	if hoverPreview.visible {
		t.Fatal("reorder kept the old identity's card")
	}
	if !hoverPreview.armed || hoverPreview.id != tabs.At(1).ID || !seatTimers.Pending(timerKindHoverDwell) {
		t.Fatalf("dwell did not re-arm for the current identity: armed=%v id=%d", hoverPreview.armed, hoverPreview.id)
	}
	f.advanceAndFire(hoverPreviewDwellNs)
	if !hoverPreview.visible || hoverPreview.id != tabs.At(1).ID {
		t.Fatalf("later dwell did not show only the current target: id=%d", hoverPreview.id)
	}

	// Close the target: the card leaves immediately.
	target := hoverPreview.id
	if !tabs.CloseTab(target) {
		t.Fatal("CloseTab")
	}
	hoverPreviewTick()
	if hoverPreview.visible {
		t.Fatal("closed target kept its preview visible")
	}
	if hoverPreview.armed && hoverPreview.id == target {
		t.Fatal("closed target kept its dwell")
	}
	if _, ok := hoverPreviewRect(1280, 720); ok {
		t.Fatal("closed target still resolved a card rect")
	}
}

// A truth change while the pointer is still — focus moving elsewhere,
// a pin, a retitle — dismisses the card instead of leaving stale words
// on screen, and no timer or heartbeat resurrects it. Only fresh motion
// restarts a truthful dwell.
func TestHoverPreviewTruthChangeDismissesUntilMotion(t *testing.T) {
	resetSeatForTest(t)
	f := newHoverPreviewFixture(t, 3, 4)
	if !tabs.FocusTab(4) {
		t.Fatal("FocusTab(4)")
	}
	railHover = 1
	hoverPreviewPointer()
	f.dwellToVisible(t)
	hoverPreviewPresented(true, true)
	if !f.contains(t, "status=focused") {
		t.Fatalf("fixture card did not say focused: %v", f.logs)
	}

	// Focus leaves the hovered tab; the card's status line is now false.
	if !tabs.FocusTab(3) {
		t.Fatal("FocusTab")
	}
	hoverPreviewReconcile()
	if hoverPreview.visible || seatTimers.Pending(timerKindHoverDwell) {
		t.Fatal("stale card survived focus change")
	}
	for tick := uint64(8); tick <= 16; tick++ {
		seatTick = tick
		hoverPreviewTick()
		f.advanceAndFire(hoverPreviewDwellNs)
		if hoverPreview.visible {
			t.Fatalf("stale card resurfaced at heartbeat %d after the focus change", tick)
		}
	}
	hoverPreviewPresented(false, true)
	if !f.contains(t, MarkerHoverPreviewDismiss+"4") {
		t.Fatalf("no dismiss receipt for the stale card: %v", f.logs)
	}

	// Fresh motion is a new gesture: the card is allowed again, with the
	// status it now reads.
	consumeSeatEvent(viPointer(700, 10))
	f.advanceAndFire(hoverPreviewDwellNs)
	if !hoverPreview.visible {
		t.Fatal("motion did not restart an eligible dwell")
	}
	hoverPreviewPresented(true, true)
	if !f.contains(t, "status=active") {
		t.Fatalf("fresh card kept the stale status: %v", f.logs)
	}
}

// Overlay, drag, lock, and idle states suppress both a pending timer and
// a visible card; clearing the state cannot resurrect the stale one.
func TestHoverPreviewConflictingStatesSuppress(t *testing.T) {
	resetSeatForTest(t)
	cases := []struct {
		name string
		set  func(bool)
	}{
		{"launcher", func(on bool) { launch.open = on }},
		{"power-menu", func(on bool) { powerMenuOpen = on }},
		{"clip-menu", func(on bool) { clipMenuOpen = on }},
		{"rail-menu", func(on bool) { railMenuOpen = on }},
		{"secret-ask", func(on bool) { secretAskModal.open = on }},
		{"notify-center", func(on bool) { notifyCenterOpen = on }},
		{"lock", func(on bool) { lockActive = on }},
		{"idle-dim", func(on bool) {
			idleState = idleActive
			if on {
				idleState = idleDim
			}
		}},
		{"idle-curtain", func(on bool) {
			idleState = idleActive
			if on {
				idleState = idleCurtain
			}
		}},
		{"guest-drag", func(on bool) { drag.armed = on }},
		{"rail-drag", func(on bool) {
			railDragFrom = 0
			if !on {
				railDragFrom = -1
			}
		}},
		{"sash-drag", func(on bool) { sashDragging = on }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := newHoverPreviewFixture(t, 3, 4)
			railHover = 1
			hoverPreviewPointer()
			f.dwellToVisible(t)
			hoverPreviewPresented(true, true)
			tc.set(true)
			hoverPreviewTick()
			if hoverPreview.visible || seatTimers.Pending(timerKindHoverDwell) {
				t.Fatal("conflicting state left the card or timer active")
			}
			hoverPreviewPresented(false, true)
			if !f.contains(t, MarkerHoverPreviewDismiss+"4") {
				t.Fatalf("no dismiss receipt after suppression: %v", f.logs)
			}

			// Clearing the state must not resurrect the card: the dwell
			// starts over and waits for the full 500ms deadline again.
			tc.set(false)
			hoverPreviewTick()
			if hoverPreview.visible {
				t.Fatal("clearing the conflict resurrected the stale card")
			}
			f.advanceAndFire(hoverPreviewDwellNs)
			if !hoverPreview.visible {
				t.Fatal("an eligible new dwell did not show the card")
			}
			hoverPreviewPresented(true, true)
			if !f.contains(t, MarkerHoverPreview+"4 ") {
				t.Fatalf("new dwell earned no preview receipt: %v", f.logs)
			}
		})
	}
}

// The painted card is the bounded plate under the hovered cell: one
// 240x60 card, accent border, nothing outside its rect.
func TestHoverPreviewPaintsBoundedCard(t *testing.T) {
	resetSeatForTest(t)
	f := newHoverPreviewFixture(t, 3, 4)
	railHover = 1
	hoverPreviewPointer()
	f.dwellToVisible(t)
	rect, ok := hoverPreviewRect(1280, 720)
	if !ok {
		t.Fatal("no card rect")
	}
	if rect.W != hoverPreviewW || rect.H != hoverPreviewH {
		t.Fatalf("card rect = %dx%d want %dx%d", rect.W, rect.H, hoverPreviewW, hoverPreviewH)
	}
	if rect.X != 640 || rect.Y != RailHeight+hoverPreviewInsetY {
		t.Fatalf("card rect = (%d,%d) want (640,%d)", rect.X, rect.Y, RailHeight+hoverPreviewInsetY)
	}
	scan := make([]byte, 1280*720*4)
	n := paintHoverPreview(scan, 1280, 720)
	if n != rect.W*rect.H {
		t.Fatalf("painted area = %d want %d", n, rect.W*rect.H)
	}
	pixel := func(x, y int) []byte {
		p := (y*1280 + x) * 4
		return scan[p : p+4]
	}
	edge := pixel(rect.X, rect.Y+rect.H/2)
	if edge[0] != 0xf6 || edge[1] != 0x82 || edge[2] != 0x3b || edge[3] != 0xff {
		t.Fatalf("card edge = % x want the accent border", edge)
	}
	if out := pixel(rect.X-1, rect.Y+rect.H/2); out[3] != 0 {
		t.Fatalf("card painted left of its rect: % x", out)
	}
	if out := pixel(rect.X+rect.W, rect.Y+rect.H/2); out[3] != 0 {
		t.Fatalf("card painted right of its rect: % x", out)
	}
	if body := pixel(rect.X+rect.W/2, rect.Y+rect.H/2); body[3] != 0xff ||
		body[0] != 0x35 || body[1] != 0x2d || body[2] != 0x22 {
		t.Fatalf("card body = % x want the theme surface", body)
	}
}

// The card must not paint when there is no visible target, and exactly
// one card exists even with many rail cells.
func TestHoverPreviewOneCardOnly(t *testing.T) {
	resetSeatForTest(t)
	f := newHoverPreviewFixture(t, 3, 4, 5)
	railHover = 2
	hoverPreviewPointer()
	hoverPreviewReconcile()
	if _, ok := hoverPreviewRect(1280, 720); ok {
		t.Fatal("pending dwell painted a card")
	}
	f.advanceAndFire(hoverPreviewDwellNs)
	rect, ok := hoverPreviewRect(1280, 720)
	if !ok {
		t.Fatal("no card rect after dwell")
	}
	cellW := 1280 / 3
	if rect.X != 2*cellW {
		t.Fatalf("card x = %d want under cell 2 (%d)", rect.X, 2*cellW)
	}
	scan := make([]byte, 1280*720*4)
	paintHoverPreview(scan, 1280, 720)
	if again := paintHoverPreview(scan, 1280, 720); again != 0 {
		t.Logf("second paint area = %d (same rect, one card)", again)
	}
	for x := 0; x < 1280; x += 64 {
		if x >= rect.X && x < rect.X+rect.W {
			continue
		}
		p := (rect.Y+rect.H/2)*1280 + x
		if scan[p*4+3] != 0 {
			t.Fatalf("second card painted at x=%d", x)
		}
	}
}

// M216b (#3941): the card anchors under the cell's PAINTED left edge —
// railLayout's rect — so the pill reservation moves the anchor with the
// paint and hit tests, and an unpainted tail cell shows no card.
func TestHoverPreviewAnchorsToRailLayout(t *testing.T) {
	resetSeatForTest(t)
	defer saveSeatState()()
	stubSeatRegistry(t, nil)
	f := newHoverPreviewFixture(t, 3, 4, 5)
	resetSpaces()
	spaceN, spaceCur = 3, 0

	for cell := 0; cell < 3; cell++ {
		railHover = cell
		hoverPreviewPointer()
		f.dwellToVisible(t)
		rect, ok := hoverPreviewRect(1280, 720)
		if !ok {
			t.Fatalf("cell %d: no card rect", cell)
		}
		l := railLayout(1280, 3, RailHeight)
		if want := l.rect(cell).X; rect.X != want {
			t.Fatalf("cell %d: card x=%d want layout x=%d", cell, rect.X, want)
		}
		hoverPreview.visible = false
		hoverPreview.cell = -1
	}

	// Single space: byte-identical anchor — cell 2 at 2*(1280/3).
	spaceN, spaceCur = 1, 0
	railHover = 2
	hoverPreviewPointer()
	f.dwellToVisible(t)
	rect, ok := hoverPreviewRect(1280, 720)
	if !ok {
		t.Fatal("single-space: no card rect")
	}
	if want := 2 * (1280 / 3); rect.X != want {
		t.Fatalf("single-space card x=%d want %d", rect.X, want)
	}
}
