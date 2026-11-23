// timerq.go — M216n (#3953): the seat's single-timer deadline queue.
//
// A process has one kernel app timer (ADR 0007 slot 95 sys_timer_set_ns /
// slot 41 sys_timer_cancel, ADR 0060 D1–D2, firing as kind-9 vi.EvTimer).
// This queue multiplexes a small fixed table of (deadline, kind) entries
// onto that single timer: the kernel timer is always armed for the
// earliest pending deadline and re-armed on insert, cancel, and fire.
//
// Deadlines are computed from the monotonic nanosecond clock (vi.Nanos —
// CNTPCT_EL0 on guest, time.Now().UnixNano() on host) behind a swappable
// seam for unit tests. A fired entry runs its handler and marks the frame
// dirty through the same non-heartbeat composite path pointer events use;
// it never prints MarkerTick and never moves a heartbeat counter.
//
// When slot 95 is refused (for example on an image whose fleet grants omit
// `timer`), the queue latches refused, logs MarkerTimerRefused once, and
// lets callers fall back to heartbeat timing.

package main

import "virelai/vi"

const (
	// seatTimerCap bounds the fixed queue table (zero allocation).
	seatTimerCap = 8

	// Kernel bounds for slot 95 sys_timer_set_ns (kernel/src/syscall.zig):
	// ns == 0 and ns > 3600 s return -EINVAL; ADR 0060 D2 quantizes to 1 ms.
	seatTimerMinNs uint64 = 1_000_000
	seatTimerMaxNs uint64 = 3600 * 1_000_000_000

	// MarkerTimerRefused names a slot-95 arm refusal once per seat boot
	// before falling back to heartbeat-paced hover dwell.
	MarkerTimerRefused = "gotabwm: timer refused rc="
)

// seatTimerKind names a scheduled seat deadline. At most one pending entry
// exists per kind: re-scheduling a kind replaces its deadline in place.
type seatTimerKind uint8

const (
	timerKindNone seatTimerKind = iota
	timerKindHoverDwell
)

type seatTimerEntry struct {
	active   bool
	kind     seatTimerKind
	deadline int64
	seq      uint64
}

// Package-level syscall/clock seams for the seat's default timer queue.
// Swapped in unit tests; resetSeatForTest restores them.
var (
	seatTimerNow    = vi.Nanos
	seatTimerSetNs  = vi.TimerSetNs
	seatTimerCancel = vi.TimerCancel
	seatTimerLog    = vi.ConsoleLine
)

// seatTimerQueue multiplexes up to seatTimerCap deadlines onto the
// process's one app timer.
type seatTimerQueue struct {
	entries       [seatTimerCap]seatTimerEntry
	nextSeq       uint64
	armed         bool
	armedDeadline int64
	refused       bool

	// Optional per-instance overrides for isolated unit tests; when nil,
	// the package seams above are used.
	now    func() int64
	setNs  func(uint64) int64
	cancel func() int64
	log    func(string)
}

var seatTimers seatTimerQueue

func (q *seatTimerQueue) nowFn() int64 {
	if q.now != nil {
		return q.now()
	}
	return seatTimerNow()
}

func (q *seatTimerQueue) setNsFn(ns uint64) int64 {
	if q.setNs != nil {
		return q.setNs(ns)
	}
	return seatTimerSetNs(ns)
}

func (q *seatTimerQueue) cancelFn() int64 {
	if q.cancel != nil {
		return q.cancel()
	}
	return seatTimerCancel()
}

func (q *seatTimerQueue) logFn(line string) {
	if q.log != nil {
		q.log(line)
		return
	}
	seatTimerLog(line)
}

// Refused reports whether slot 95 was refused by the kernel.
func (q *seatTimerQueue) Refused() bool {
	return q.refused
}

// Pending reports whether kind currently has a scheduled deadline.
func (q *seatTimerQueue) Pending(kind seatTimerKind) bool {
	for i := range q.entries {
		if q.entries[i].active && q.entries[i].kind == kind {
			return true
		}
	}
	return false
}

// Set schedules kind to fire after delayNs nanoseconds on the monotonic
// clock, replacing any existing entry of the same kind. Returns true when
// scheduled, or false if kind is invalid, the table is full, or slot 95 is
// refused (in which case refused is latched and MarkerTimerRefused logged
// once).
func (q *seatTimerQueue) Set(kind seatTimerKind, delayNs int64) bool {
	if kind == timerKindNone || q.refused {
		return false
	}
	if delayNs < 0 {
		delayNs = 0
	}
	now := q.nowFn()
	deadline := now + delayNs

	slot := -1
	for i := range q.entries {
		if q.entries[i].active && q.entries[i].kind == kind {
			slot = i
			break
		}
		if !q.entries[i].active && slot < 0 {
			slot = i
		}
	}
	if slot < 0 {
		return false
	}
	q.nextSeq++
	q.entries[slot] = seatTimerEntry{
		active:   true,
		kind:     kind,
		deadline: deadline,
		seq:      q.nextSeq,
	}
	return q.rearm(now)
}

// Cancel removes kind from the queue and re-arms the kernel timer for the
// next earliest entry (or cancels the kernel timer when the queue empties).
func (q *seatTimerQueue) Cancel(kind seatTimerKind) bool {
	found := false
	for i := range q.entries {
		if q.entries[i].active && q.entries[i].kind == kind {
			q.entries[i] = seatTimerEntry{}
			found = true
			break
		}
	}
	if !found {
		return false
	}
	if !q.refused {
		_ = q.rearm(q.nowFn())
	}
	return true
}

// CancelAll drops every pending entry and disarms the kernel timer if armed.
func (q *seatTimerQueue) CancelAll() {
	q.clearEntries()
	if q.armed && !q.refused {
		_ = q.cancelFn()
	}
	q.armed = false
	q.armedDeadline = 0
}

// Fire handles one kind-9 vi.EvTimer delivery: dispatches every entry whose
// deadline is <= now in (deadline, insertion) order, then re-arms the
// kernel timer for any remaining future entries. Entries inserted by a
// handler during Fire are not dispatched in the same pass (avoiding loops)
// and are armed by the trailing rearm. Returns the number of fired entries.
func (q *seatTimerQueue) Fire(dispatch func(seatTimerKind)) int {
	// The kernel app timer is one-shot; it is now disarmed until rearm runs.
	q.armed = false
	q.armedDeadline = 0
	if q.refused {
		return 0
	}
	now := q.nowFn()
	cutoffSeq := q.nextSeq
	fired := 0
	for {
		idx := q.earliestDueIdx(now, cutoffSeq)
		if idx < 0 {
			break
		}
		kind := q.entries[idx].kind
		q.entries[idx] = seatTimerEntry{}
		fired++
		if dispatch != nil {
			dispatch(kind)
		}
		if q.refused {
			return fired
		}
	}
	_ = q.rearm(q.nowFn())
	return fired
}

func (q *seatTimerQueue) clearEntries() {
	for i := range q.entries {
		q.entries[i] = seatTimerEntry{}
	}
}

func (q *seatTimerQueue) earliestIdx() int {
	best := -1
	for i := range q.entries {
		e := &q.entries[i]
		if !e.active {
			continue
		}
		if best < 0 || e.deadline < q.entries[best].deadline ||
			(e.deadline == q.entries[best].deadline && e.seq < q.entries[best].seq) {
			best = i
		}
	}
	return best
}

func (q *seatTimerQueue) earliestDueIdx(now int64, maxSeq uint64) int {
	best := -1
	for i := range q.entries {
		e := &q.entries[i]
		if !e.active || e.deadline > now || e.seq > maxSeq {
			continue
		}
		if best < 0 || e.deadline < q.entries[best].deadline ||
			(e.deadline == q.entries[best].deadline && e.seq < q.entries[best].seq) {
			best = i
		}
	}
	return best
}

func (q *seatTimerQueue) rearm(now int64) bool {
	if q.refused {
		return false
	}
	idx := q.earliestIdx()
	if idx < 0 {
		if q.armed {
			_ = q.cancelFn()
			q.armed = false
			q.armedDeadline = 0
		}
		return true
	}
	target := q.entries[idx].deadline
	if q.armed && q.armedDeadline == target {
		return true
	}
	var delta uint64
	if target <= now {
		delta = seatTimerMinNs
	} else {
		d := uint64(target - now)
		if d < seatTimerMinNs {
			delta = seatTimerMinNs
		} else if d > seatTimerMaxNs {
			delta = seatTimerMaxNs
		} else {
			delta = d
		}
	}
	rc := q.setNsFn(delta)
	if rc < 0 {
		q.refused = true
		q.armed = false
		q.armedDeadline = 0
		q.clearEntries()
		q.logFn(MarkerTimerRefused + vi.Itoa64(rc))
		return false
	}
	q.armed = true
	q.armedDeadline = target
	return true
}

// dispatchSeatTimer routes a fired queue entry to its seat handler.
func dispatchSeatTimer(kind seatTimerKind) {
	switch kind {
	case timerKindHoverDwell:
		hoverPreviewFire()
	}
}

// seatTimerFire consumes a kind-9 vi.EvTimer event in the seat loop.
// Any fired entry marks the frame dirty for the non-heartbeat paint path;
// stale timer events whose entries were already cancelled cost no paint.
func seatTimerFire() {
	if seatTimers.Fire(dispatchSeatTimer) > 0 {
		pendingComposite = true
	}
}
