package main

import (
	"slices"
	"strings"
	"testing"

	"virelai/vi"
)

type fakeTimerHarness struct {
	now     int64
	armedNs []uint64
	cancels int
	logs    []string
	armRC   int64
	q       seatTimerQueue
}

func newFakeTimerHarness(startNs int64) *fakeTimerHarness {
	h := &fakeTimerHarness{now: startNs}
	h.q = seatTimerQueue{
		now: func() int64 { return h.now },
		setNs: func(ns uint64) int64 {
			h.armedNs = append(h.armedNs, ns)
			return h.armRC
		},
		cancel: func() int64 {
			h.cancels++
			return 1
		},
		log: func(line string) {
			h.logs = append(h.logs, line)
		},
	}
	return h
}

func TestTimerQueueOrderOfFireAndRearm(t *testing.T) {
	const ms = int64(1_000_000)
	const (
		kindA seatTimerKind = 1
		kindB seatTimerKind = 2
		kindC seatTimerKind = 3
	)
	h := newFakeTimerHarness(1000 * ms)

	// Insert out of deadline order: C(800ms), A(200ms), B(500ms).
	if !h.q.Set(kindC, 800*ms) {
		t.Fatal("Set C failed")
	}
	if !h.q.Set(kindA, 200*ms) {
		t.Fatal("Set A failed")
	}
	if !h.q.Set(kindB, 500*ms) {
		t.Fatal("Set B failed")
	}
	// C armed 800ms, A pulled the comparator forward to 200ms, B is later
	// than A so it must not issue a redundant re-arm.
	wantArms := []uint64{uint64(800 * ms), uint64(200 * ms)}
	if !slices.Equal(h.armedNs, wantArms) {
		t.Fatalf("arms after insert = %v want %v", h.armedNs, wantArms)
	}

	var fired []seatTimerKind
	dispatch := func(k seatTimerKind) { fired = append(fired, k) }

	// Advance to 200ms: A fires, kernel re-arms for remaining 300ms (to B).
	h.now += 200 * ms
	if n := h.q.Fire(dispatch); n != 1 {
		t.Fatalf("Fire at +200ms fired %d want 1", n)
	}
	if !slices.Equal(fired, []seatTimerKind{kindA}) {
		t.Fatalf("fired = %v want [A]", fired)
	}
	if last := h.armedNs[len(h.armedNs)-1]; last != uint64(300*ms) {
		t.Fatalf("rearm after A = %d want %d", last, 300*ms)
	}

	// Advance to 500ms: B fires, kernel re-arms for remaining 300ms (to C).
	h.now += 300 * ms
	if n := h.q.Fire(dispatch); n != 1 {
		t.Fatalf("Fire at +500ms fired %d want 1", n)
	}
	if !slices.Equal(fired, []seatTimerKind{kindA, kindB}) {
		t.Fatalf("fired = %v want [A B]", fired)
	}
	if last := h.armedNs[len(h.armedNs)-1]; last != uint64(300*ms) {
		t.Fatalf("rearm after B = %d want %d", last, 300*ms)
	}

	// Advance to 800ms: C fires, queue empty (no new arm, no cancel).
	h.now += 300 * ms
	armsBefore := len(h.armedNs)
	if n := h.q.Fire(dispatch); n != 1 {
		t.Fatalf("Fire at +800ms fired %d want 1", n)
	}
	if !slices.Equal(fired, []seatTimerKind{kindA, kindB, kindC}) {
		t.Fatalf("fired = %v want [A B C]", fired)
	}
	if len(h.armedNs) != armsBefore || h.q.armed {
		t.Fatalf("empty queue after final fire still armed: arms=%d->%d armed=%v",
			armsBefore, len(h.armedNs), h.q.armed)
	}
}

func TestTimerQueueCancelRearmsAndDisarms(t *testing.T) {
	const ms = int64(1_000_000)
	const (
		kindA seatTimerKind = 1
		kindB seatTimerKind = 2
	)
	h := newFakeTimerHarness(500 * ms)

	h.q.Set(kindA, 200*ms)
	h.q.Set(kindB, 600*ms)

	// Cancelling the earliest entry re-arms for the remaining one.
	h.now += 50 * ms
	if !h.q.Cancel(kindA) {
		t.Fatal("Cancel(A) returned false")
	}
	if h.q.Pending(kindA) || !h.q.Pending(kindB) {
		t.Fatalf("pending after Cancel(A): A=%v B=%v", h.q.Pending(kindA), h.q.Pending(kindB))
	}
	if last := h.armedNs[len(h.armedNs)-1]; last != uint64(550*ms) {
		t.Fatalf("rearm after Cancel(A) = %d want %d", last, 550*ms)
	}

	// Cancelling an absent entry is a no-op.
	if h.q.Cancel(kindA) {
		t.Fatal("second Cancel(A) returned true")
	}

	// Cancelling the final entry calls TimerCancel once.
	if !h.q.Cancel(kindB) {
		t.Fatal("Cancel(B) returned false")
	}
	if h.cancels != 1 || h.q.armed {
		t.Fatalf("cancels=%d armed=%v want 1/false", h.cancels, h.q.armed)
	}
}

func TestTimerQueueRearmExistingKindAndStaleFire(t *testing.T) {
	const ms = int64(1_000_000)
	const kindA seatTimerKind = 1
	h := newFakeTimerHarness(100 * ms)

	h.q.Set(kindA, 500*ms) // deadline 600ms
	h.now += 200 * ms      // now 300ms
	h.q.Set(kindA, 500*ms) // pushed out to 800ms
	if len(h.armedNs) != 2 || h.armedNs[1] != uint64(500*ms) {
		t.Fatalf("arms after push-out = %v want [500ms 500ms]", h.armedNs)
	}

	// A stale EvTimer arriving at 600ms (before the 800ms deadline) must
	// not fire A, and must re-arm for the remaining 200ms.
	h.now = 600 * ms
	fired := 0
	if n := h.q.Fire(func(seatTimerKind) { fired++ }); n != 0 || fired != 0 {
		t.Fatalf("stale fire ran handler: n=%d fired=%d", n, fired)
	}
	if last := h.armedNs[len(h.armedNs)-1]; last != uint64(200*ms) {
		t.Fatalf("rearm after stale EvTimer = %d want %d", last, 200*ms)
	}

	// A handler that re-arms itself during Fire must not loop in the same
	// Fire call; it schedules for the next deadline.
	h.now = 800 * ms
	rearmed := false
	if n := h.q.Fire(func(k seatTimerKind) {
		fired++
		if !rearmed {
			rearmed = true
			h.q.Set(k, 0) // even delay 0 must defer to the next Fire
		}
	}); n != 1 || fired != 1 {
		t.Fatalf("self-rearming handler looped: n=%d fired=%d", n, fired)
	}
	if !h.q.Pending(kindA) || h.armedNs[len(h.armedNs)-1] != seatTimerMinNs {
		t.Fatalf("self-rearm did not arm min quantum: pending=%v last=%d",
			h.q.Pending(kindA), h.armedNs[len(h.armedNs)-1])
	}
}

func TestTimerQueueTwoEntriesDueTogether(t *testing.T) {
	const ms = int64(1_000_000)
	const (
		kindA seatTimerKind = 1
		kindB seatTimerKind = 2
		kindC seatTimerKind = 3
	)
	h := newFakeTimerHarness(0)

	h.q.Set(kindA, 500*ms)
	h.q.Set(kindB, 500*ms)
	h.q.Set(kindC, 900*ms)

	h.now = 500 * ms
	var order []seatTimerKind
	if n := h.q.Fire(func(k seatTimerKind) { order = append(order, k) }); n != 2 {
		t.Fatalf("simultaneous Fire = %d want 2", n)
	}
	if !slices.Equal(order, []seatTimerKind{kindA, kindB}) {
		t.Fatalf("simultaneous fire order = %v want [A B]", order)
	}
	if !h.q.Pending(kindC) || h.armedNs[len(h.armedNs)-1] != uint64(400*ms) {
		t.Fatalf("remaining entry C not re-armed for 400ms: pending=%v arms=%v",
			h.q.Pending(kindC), h.armedNs)
	}

	// If the first due handler cancels the second due entry, the second
	// does not fire.
	h = newFakeTimerHarness(0)
	h.q.Set(kindA, 500*ms)
	h.q.Set(kindB, 500*ms)
	h.now = 500 * ms
	order = nil
	if n := h.q.Fire(func(k seatTimerKind) {
		order = append(order, k)
		if k == kindA {
			h.q.Cancel(kindB)
		}
	}); n != 1 || !slices.Equal(order, []seatTimerKind{kindA}) {
		t.Fatalf("cancelled sibling still fired: n=%d order=%v", n, order)
	}
}

func TestTimerQueueRefusalLatchesAndLogsOnce(t *testing.T) {
	const ms = int64(1_000_000)
	h := newFakeTimerHarness(0)
	h.armRC = -vi.ErrEACCES

	if h.q.Set(timerKindHoverDwell, 500*ms) {
		t.Fatal("Set succeeded despite -EACCES refusal")
	}
	if !h.q.Refused() {
		t.Fatal("Refused() is false after slot-95 refusal")
	}
	if h.q.Pending(timerKindHoverDwell) {
		t.Fatal("refused entry stayed pending")
	}
	if len(h.logs) != 1 || !strings.HasPrefix(h.logs[0], MarkerTimerRefused) {
		t.Fatalf("refusal logs = %v want one %q line", h.logs, MarkerTimerRefused)
	}

	// Subsequent Set attempts return false immediately without repeating
	// the refused syscall or spamming the marker.
	armsBefore := len(h.armedNs)
	if h.q.Set(timerKindHoverDwell, 500*ms) {
		t.Fatal("second Set succeeded after latched refusal")
	}
	if len(h.armedNs) != armsBefore || len(h.logs) != 1 {
		t.Fatalf("latched refusal still called syscall/logged: arms=%d->%d logs=%v",
			armsBefore, len(h.armedNs), h.logs)
	}
}

func TestTimerEventPaintsWithoutMovingHeartbeats(t *testing.T) {
	resetSeatForTest(t)
	now := int64(1_000_000_000)
	seatTimerNow = func() int64 { return now }
	seatTimerSetNs = func(uint64) int64 { return 0 }
	seatTimerCancel = func() int64 { return 1 }

	tabs = TabStrip{}
	tabs.OpenTab(4, "Term")
	railHover = 0
	seatTick = 7
	compositeClock = 7
	pendingComposite = false
	pendingHeartbeat = false

	hoverPreviewReconcile()
	if !seatTimers.Pending(timerKindHoverDwell) {
		t.Fatal("hover dwell did not schedule on timer queue")
	}

	// Drive EvTimer through drainSeatEvents exactly as the main loop does.
	now += hoverPreviewDwellNs
	onTickCalls := 0
	flushCalls := 0
	flushedHeartbeat := false
	drained := drainSeatEvents(
		vi.Event{Kind: vi.EvTimer},
		func() (vi.Event, bool) { return vi.Event{}, false },
		consumeSeatEvent,
		16,
		nil,
		func() bool {
			onTickCalls++
			return true
		},
		func() {
			flushCalls++
			flushedHeartbeat = pendingHeartbeat
			pendingComposite = false
			pendingHeartbeat = false
		},
	)
	if drained != 1 {
		t.Fatalf("drained = %d want 1", drained)
	}
	if !hoverPreview.visible {
		t.Fatal("EvTimer did not reveal the hover preview card")
	}
	if flushCalls != 1 {
		t.Fatalf("flushCalls = %d want 1 (same paint/present path as pointer)", flushCalls)
	}
	if flushedHeartbeat || onTickCalls != 0 || seatTick != 7 || compositeClock != 7 {
		t.Fatalf("EvTimer moved heartbeat state: hb=%v onTick=%d seatTick=%d clock=%d",
			flushedHeartbeat, onTickCalls, seatTick, compositeClock)
	}
}
