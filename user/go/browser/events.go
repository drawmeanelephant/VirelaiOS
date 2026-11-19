// M211d (#3913): WEB's bounded input-listener side. ADR 0039's v4 wire
// admits exactly one user-driven surface — `input` on an enabled,
// ID-addressed text/search control — and this file is the parent half:
// registration admission, the bounded event queue, the single parked
// event/timer ask, and the live-value/focus rules a dispatch rides.
//
// The worker never sees the control tree; it asks `event.next` and this
// side answers with either the next queued input or a due timer id. One
// ask, one answer file, one worker — the same one-outstanding-request
// rule the timer drain already had.

package main

import (
	"encoding/json"
	"strings"

	"virelai/vi"
	"virelai/webrender"
)

const (
	// domRpcMaxListeners / domRpcMaxEvents mirror domrpc.listener_max and
	// domrpc.event_queue_max: at most eight registrations (one per
	// target/kind) and eight queued-but-undelivered input events.
	domRpcMaxListeners = 8
	domRpcMaxEvents    = 8
	// domRpcIdleRefresh is the parent's bounded parked-idle marker
	// cadence. The worker writes nothing while it waits on `event.next`;
	// this side knows the acknowledged waitParked state, schedules the
	// marker itself, and exempts that interval from the active-work
	// deadline. The marker is a parent receipt, not worker progress, so
	// it never refreshes the deadline.
	domRpcIdleRefresh = int64(5) * 1_000_000_000
)

// domListener is one admitted registration. The target is the id the
// script addressed; the worker's own callback table is keyed by it too.
type domListener struct {
	target, kind string
}

// domInput is one queued input dispatch — the live value is captured at
// enqueue time, after the control state has been mirrored into the DOM's
// value attribute.
type domInput struct {
	target, value string
}

// listenerKindWord names an unsupported listener kind verbatim inside the
// wire's own word charset, so the refusal itself says "click" or "change".
func listenerKindWord(kind string) string {
	var b strings.Builder
	for _, r := range kind {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '_', r == '-':
			b.WriteRune(r)
		}
		if b.Len() >= 16 {
			break
		}
	}
	if b.Len() == 0 {
		return "event_kind"
	}
	return "event_kind_" + b.String()
}

// controlByID resolves an id to the retained form control initForms built
// — the same object whose disabled bit already includes disabled ancestors.
func (a *app) controlByID(id string) *formControl {
	if id == "" {
		return nil
	}
	for _, c := range a.controls {
		if c != nil && c.node.Attr("id") == id {
			return c
		}
	}
	return nil
}

// admitListener checks one `listener.add` request and returns the named
// refusal when it is not admissible. The checks are ordered so the most
// specific truth wins: an unsupported kind is named even for a bad target,
// an absent/non-addressable id is `event_target`, a node that is not an
// enabled text/search control is `event_control`/`event_disabled`, and the
// bounded registry refuses `event_duplicate`/`event_budget_regs`.
func (a *app) admitListener(target, kind string) string {
	if !qjsListenerKind(kind) {
		return listenerKindWord(kind)
	}
	if a.doc == nil || a.doc.ByID(target) == nil {
		return "event_target"
	}
	c := a.controlByID(target)
	if c == nil || (c.kind != "text" && c.kind != "search") {
		return "event_control"
	}
	if c.disabled {
		return "event_disabled"
	}
	for _, l := range a.listeners {
		if l.target == target && l.kind == kind {
			return "event_duplicate"
		}
	}
	if len(a.listeners) >= domRpcMaxListeners {
		return "event_budget_regs"
	}
	return ""
}

// qjsListenerKind mirrors domrpc.listenerKindOk — the one admitted kind.
func qjsListenerKind(kind string) bool { return kind == "input" }

// noteControlInput mirrors the edited control state into its value
// attribute (the live value `e.target.value` and any later reflow read),
// then queues one input dispatch when this target/kind is registered.
// The queue bound is a named refusal, never a silent drop.
func (a *app) noteControlInput(c *formControl) {
	if c == nil {
		return
	}
	id := c.node.Attr("id")
	if id == "" {
		return
	}
	c.node.SetAttr("value", c.value)
	if word := a.queueInput(id, c.value); word != "" {
		vi.ConsoleLine(markerDom + "event.next " + word)
	}
}

// queueInput appends one dispatch when the target/kind is registered and
// the queue has room. It answers a parked worker immediately; otherwise
// the event waits for the next `event.next` ask.
func (a *app) queueInput(target, value string) string {
	if a.rpc == nil || target == "" {
		return ""
	}
	registered := false
	for _, l := range a.listeners {
		if l.target == target && l.kind == "input" {
			registered = true
			break
		}
	}
	if !registered {
		return ""
	}
	if len(a.queued) >= domRpcMaxEvents {
		return "event_queue"
	}
	a.queued = append(a.queued, domInput{target: target, value: value})
	a.serviceParkedAsk()
	return ""
}

// inputPayload is one `event.next` answer body: the worker parses the
// answer's `value` string as JSON. `kind` is "input" or "timer".
type inputPayload struct {
	Kind   string `json:"kind"`
	Target string `json:"target,omitempty"`
	Value  string `json:"value,omitempty"`
	ID     string `json:"id,omitempty"`
}

// eventAnswer builds the answer value for one input dispatch or due timer.
func inputAnswer(ev domInput) string {
	line, _ := json.Marshal(inputPayload{Kind: "input", Target: ev.target, Value: ev.value})
	return string(line)
}

func timerAnswer(id int) string {
	line, _ := json.Marshal(inputPayload{Kind: "timer", ID: itoa(id)})
	return string(line)
}

// publishAnswer writes the parked ask's answer atomically on the existing
// rendezvous, then clears the parked state. One ask, one answer. The active
// deadline is re-armed FIRST: clearing the parked bit drops the idle
// exemption, so the unparked interval must already have its full budget.
func (a *app) publishAnswer(w *scriptWorker, value string) {
	line, err := json.Marshal(domAnswer{Op: w.waitOp, Seq: w.waitSeq, OK: true, Value: &value})
	if err != nil {
		return
	}
	a.touch(w)
	w.waitParked = false
	vi.WriteFileSafe(domRpcAnsPath, append(line, '\n'))
}

// serviceParkedAsk answers a parked `event.next` from the queued input or,
// when no input is waiting, a due timer id — the shared arbitration that
// keeps events and timers on one worker and one outstanding request.
func (a *app) serviceParkedAsk() {
	w := a.rpc
	if w == nil || !w.waitParked || w.waitOp != "event.next" {
		return
	}
	if len(a.queued) > 0 {
		ev := a.queued[0]
		a.queued = a.queued[1:]
		if len(a.queued) == 0 {
			a.queued = nil
		}
		a.publishAnswer(w, inputAnswer(ev))
		vi.ConsoleLine(markerDom + "event.next ok target=" + clipTarget(ev.target))
		return
	}
	if len(w.due) > 0 {
		id := w.due[0]
		w.due = w.due[1:]
		a.publishAnswer(w, timerAnswer(id))
		vi.ConsoleLine(markerDom + "event.next ok timer=" + itoa(id))
	}
}

// clipTarget bounds a serial receipt label.
func clipTarget(id string) string {
	if len(id) > 64 {
		return id[:64]
	}
	return id
}

// touch refreshes the worker's active-work deadline. Only an actual wire
// line or a served op is activity; a parked interval is exempt structurally
// (waitParked), not by pretending the parent's idle marker is progress.
func (a *app) touch(w *scriptWorker) {
	w.deadline = nowNanos() + domRpcBudgetNs
}

// nowNanos is the deadline clock, swappable by host tests.
var nowNanos = vi.Nanos

// probeWorker is the process-exit observation, swappable by host tests.
// It is consulted on every poll pass, parked or not: a vanished worker is
// retired immediately and its acknowledgments prove nothing.
var probeWorker = vi.Probe

// workerExpired is the active-work deadline boundary: a worker is alive
// while now < deadline and killed at the deadline itself.
func workerExpired(now, deadline int64) bool { return now >= deadline }

// parkedEventIdle reports the one acknowledged idle interval: the worker
// asked `event.next` and the parent has not answered it yet. Nothing is
// pending on the worker, so the active-work deadline does not apply.
func parkedEventIdle(w *scriptWorker) bool {
	return w != nil && w.waitParked && w.waitOp == "event.next"
}

// idleMarkDue schedules the parent's bounded parked-idle marker. It fires
// at most once per domRpcIdleRefresh and never touches the deadline: the
// serial line says the parent is servicing a parked page, not that the
// worker ran or made progress.
func (a *app) idleMarkDue(w *scriptWorker, now int64) bool {
	if !parkedEventIdle(w) {
		return false
	}
	if w.idleAt == 0 {
		w.idleAt = now + domRpcIdleRefresh
		return false
	}
	if now < w.idleAt {
		return false
	}
	w.idleAt = now + domRpcIdleRefresh
	return true
}

// workerVerdict names whether a RUNNING worker must retire on this pass.
// Spool exhaustion wins over every liveness rule: it is an immediate named
// failure, never a truncation and never a wait for the later deadline. A
// parked event drain is exempt from the active deadline; a silent worker
// (a stuck callback, an unanswered active op) still expires at it.
func workerVerdict(w *scriptWorker, now int64) string {
	if w.spoolLimit {
		return "spool-limit"
	}
	if parkedEventIdle(w) {
		return ""
	}
	if workerExpired(now, w.deadline) {
		return "deadline"
	}
	return ""
}

// spoolLimited reports whether the reply stream outgrew its bound. Actual
// exhaustion is named whatever the alignment: a byte past the bound (the
// prefix may end exactly on a newline) and a partial line still open at
// the bound are both failures, never a silently truncated prefix.
func spoolLimited(body []byte, overflow bool) bool {
	if overflow {
		return true
	}
	if len(body) < domRpcReplyMax {
		return false
	}
	return len(body) == 0 || body[len(body)-1] != '\n'
}

// controlNode is the retained node for a control's id, if any (used by
// tests and the live-value mirror).
func (a *app) controlNode(id string) *webrender.Node {
	if a.doc == nil {
		return nil
	}
	return a.doc.ByID(id)
}
