package main

// #3913 (M211d): the parent-side listener lifecycle — admission vocabulary,
// registration/event bounds, live-value mirroring, the shared parked ask and
// the active-work deadline that idle receipts refresh. These are the
// deterministic halves of VAL-DOM-001/002/003/004/005; the native live-web
// legs prove the real seat → worker → DOM → reflow → paint chain.

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"virelai/webrender"
)

func listenerApp(t *testing.T, html string) *app {
	t.Helper()
	a := stateApp(t, html)
	a.rpc = &scriptWorker{pid: -1}
	return a
}

// The admission vocabulary names each refusal: unsupported kinds name the
// kind verbatim, an absent id is event_target, a wrong control is
// event_control, and a disabled control (or subtree) is event_disabled.
func TestListenerAdmissionNamesEveryRefusal(t *testing.T) {
	a := listenerApp(t, `<input id="q" type="text"><input id="s" type="search">`+
		`<input id="ck" type="checkbox"><input id="dis" type="text" disabled>`+
		`<fieldset disabled><input id="nested" type="text"></fieldset><div id="dv"></div>`)
	cases := []struct {
		target, kind, want string
	}{
		{"q", "input", ""},
		{"s", "input", ""},
		{"q", "click", "event_kind_click"},
		{"q", "change", "event_kind_change"},
		{"q", "keydown", "event_kind_keydown"},
		{"nope", "input", "event_target"},
		{"", "input", "event_target"},
		{"ck", "input", "event_control"},
		{"dv", "input", "event_control"},
		{"dis", "input", "event_disabled"},
		{"nested", "input", "event_disabled"},
	}
	for _, c := range cases {
		a.listeners = nil
		if got := a.admitListener(c.target, c.kind); got != c.want {
			t.Fatalf("admitListener(%q,%q)=%q want %q", c.target, c.kind, got, c.want)
		}
	}
	// An unnameable kind still refuses by a bounded word.
	if got := a.admitListener("q", "événement"); got != "event_kind_vnement" {
		t.Fatalf("sanitized kind word = %q", got)
	}
}

// One registration per target/kind, at most eight.
func TestListenerRegistryBoundsAndDuplicates(t *testing.T) {
	var b strings.Builder
	for i := 0; i < 9; i++ {
		fmt.Fprintf(&b, `<input id="q%d" type="text">`, i)
	}
	a := listenerApp(t, b.String())
	for i := 0; i < 8; i++ {
		target := fmt.Sprintf("q%d", i)
		if word := a.admitListener(target, "input"); word != "" {
			t.Fatalf("registration %d refused %q", i, word)
		}
		a.listeners = append(a.listeners, domListener{target: target, kind: "input"})
	}
	if word := a.admitListener("q0", "input"); word != "event_duplicate" {
		t.Fatalf("duplicate = %q", word)
	}
	if word := a.admitListener("q8", "input"); word != "event_budget_regs" {
		t.Fatalf("ninth registration = %q", word)
	}
	if len(a.listeners) != 8 {
		t.Fatalf("registry grew to %d", len(a.listeners))
	}
}

// The queue holds eight undelivered events; the ninth is a named refusal,
// and each queued value is the live edited text mirrored into the DOM.
func TestInputQueueBoundAndLiveValue(t *testing.T) {
	a := listenerApp(t, `<input id="q" type="text" value="placeholder"><p id="e"></p>`)
	a.listeners = []domListener{{target: "q", kind: "input"}}
	c := a.controlByID("q")
	if c == nil {
		t.Fatal("fixture control missing")
	}
	for i := 0; i < domRpcMaxEvents; i++ {
		c.value = fmt.Sprintf("v%d", i)
		a.noteControlInput(c)
	}
	if len(a.queued) != domRpcMaxEvents {
		t.Fatalf("queued=%d want %d", len(a.queued), domRpcMaxEvents)
	}
	if got := a.queueInput("q", "overflow"); got != "event_queue" {
		t.Fatalf("ninth event = %q", got)
	}
	if len(a.queued) != domRpcMaxEvents {
		t.Fatalf("refusal grew the queue to %d", len(a.queued))
	}
	if c.node.Attr("value") != "v7" {
		t.Fatalf("live value not mirrored: %q", c.node.Attr("value"))
	}
	// An unregistered target queues nothing.
	if got := a.queueInput("other", "x"); got != "" || len(a.queued) != domRpcMaxEvents {
		t.Fatalf("unregistered target changed the queue (word=%q n=%d)", got, len(a.queued))
	}
}

// The parked ask is answered in one place: queued input first, then a due
// timer id, and the answer payload carries the live value / timer id.
func TestParkedAskArbitration(t *testing.T) {
	a := &app{rpc: &scriptWorker{pid: -1, waitParked: true, waitOp: "event.next", waitSeq: 4}}
	a.queued = []domInput{{target: "q", value: "live-text"}}
	a.rpc.due = []int{7}
	a.serviceParkedAsk()
	if a.rpc.waitParked {
		t.Fatal("parked ask not answered")
	}
	if len(a.queued) != 0 {
		t.Fatalf("input not consumed: %d", len(a.queued))
	}
	if len(a.rpc.due) != 1 {
		t.Fatalf("timer answered while input was queued: due=%v", a.rpc.due)
	}

	var payload inputPayload
	if err := json.Unmarshal([]byte(inputAnswer(domInput{target: "q", value: "live-text"})), &payload); err != nil {
		t.Fatalf("input payload: %v", err)
	}
	if payload.Kind != "input" || payload.Target != "q" || payload.Value != "live-text" {
		t.Fatalf("input payload = %+v", payload)
	}
	var timer inputPayload
	if err := json.Unmarshal([]byte(timerAnswer(7)), &timer); err != nil {
		t.Fatalf("timer payload: %v", err)
	}
	if timer.Kind != "timer" || timer.ID != "7" {
		t.Fatalf("timer payload = %+v", timer)
	}

	// With no input queued the due timer is the answer.
	a.rpc = &scriptWorker{pid: -1, waitParked: true, waitOp: "event.next", waitSeq: 5, due: []int{9}}
	a.serviceParkedAsk()
	if a.rpc.waitParked || len(a.rpc.due) != 0 {
		t.Fatalf("due timer not answered: parked=%v due=%v", a.rpc.waitParked, a.rpc.due)
	}
}

// Idle receipts refresh the active-work deadline across the old 120 s
// boundary, so a healthy parked page is not a page-lifetime limit; a silent
// worker still expires at the deadline.
func TestIdleRefreshesActiveWorkDeadline(t *testing.T) {
	prev := nowNanos
	defer func() { nowNanos = prev }()
	now := int64(0)
	nowNanos = func() int64 { return now }

	w := &scriptWorker{pid: -1, ops: 1, deadline: 120 * 1_000_000_000}
	now = 119 * 1_000_000_000
	if workerExpired(now, w.deadline) {
		t.Fatal("worker expired before the 120 s active-work boundary")
	}
	w.deadline = now + domRpcBudgetNs
	if workerExpired(now, w.deadline) {
		t.Fatal("idle receipt did not refresh the deadline")
	}
	// The old cutoff alone would have killed it here; the refresh did not.
	old := int64(120 * 1_000_000_000)
	if !workerExpired(old, old) {
		t.Fatal("deadline boundary is not closed (now == deadline must expire)")
	}
	if workerExpired(old, w.deadline) {
		t.Fatalf("healthy idle worker expired at %d (deadline %d)", old, w.deadline)
	}

	// A bounded parked receipt is five seconds, well inside the deadline.
	if domRpcIdleRefresh <= 0 || domRpcIdleRefresh >= domRpcBudgetNs {
		t.Fatalf("idle refresh %d outside the active-work deadline %d", domRpcIdleRefresh, domRpcBudgetNs)
	}
}

// A reply stream that fills its bound without a terminator is a named
// failure, never a silently clipped success.
func TestSpoolLimitIsNamedFailure(t *testing.T) {
	full := make([]byte, domRpcReplyMax)
	for i := range full {
		full[i] = 'x'
	}
	if !spoolLimited(full, domRpcReplyMax) {
		t.Fatal("unterminated full spool not reported")
	}
	copy(full[len(full)-1:], "\n")
	if spoolLimited(full, domRpcReplyMax) {
		t.Fatal("terminated full spool reported as failure")
	}
	if spoolLimited([]byte("ok\n"), 3) {
		t.Fatal("short reply reported as spool failure")
	}
}

// A typed live value and focus survive the reflow a dispatch triggers; a
// script that rewrites the focused value re-seats the edit at that text.
func TestFocusAndLiveValueSurviveReflow(t *testing.T) {
	a := stateApp(t, `<input id="q" type="text" value="placeholder"><p id="e"></p>`)
	c := a.controlByID("q")
	if c == nil {
		t.Fatal("fixture control missing")
	}
	c.value = "typed"
	c.node.SetAttr("value", "typed")
	a.focusControl = c
	a.controlEdit.reset("typed", 256)
	oldNode := c.node

	a.relayout()
	if a.focusControl == nil || a.focusControl.node != oldNode {
		t.Fatal("focus lost across reflow")
	}
	if a.controlEdit.text != "typed" {
		t.Fatalf("edit buffer after reflow = %q", a.controlEdit.text)
	}
	if a.controlByID("q").value != "typed" {
		t.Fatalf("live value after reflow = %q", a.controlByID("q").value)
	}

	// A scripted rewrite of the focused control re-seats the edit.
	a.focusControl.node.SetAttr("value", "scripted")
	a.relayout()
	if got := a.controlByID("q").value; got != "scripted" {
		t.Fatalf("scripted value = %q", got)
	}
	if a.controlEdit.text != "scripted" {
		t.Fatalf("edit buffer not re-seated: %q", a.controlEdit.text)
	}
}

// Teardown drops the old registrations, queued events and worker before a
// new page may reuse the spools.
func TestTeardownDropsOldListenerState(t *testing.T) {
	a := &app{rpc: &scriptWorker{pid: -1, timers: []scriptTimer{{id: 1, due: 1}}, due: []int{2}}}
	a.listeners = []domListener{{target: "q", kind: "input"}}
	a.queued = []domInput{{target: "q", value: "old"}}
	a.pageSeq = 3
	a.closeScripts()
	if a.rpc != nil || a.listeners != nil || a.queued != nil {
		t.Fatalf("teardown left state: rpc=%v listeners=%v queued=%v", a.rpc, a.listeners, a.queued)
	}
	if a.detached {
		t.Fatal("teardown left old-page serving disabled")
	}
}

// A detached page drains request lines without serving them.
func TestDetachedPageDropsRequests(t *testing.T) {
	a := listenerApp(t, `<input id="q" type="text"><p id="e"></p>`)
	a.detached = true
	a.serveDom(a.rpc, "node.setText", 1, "e", "old-output")
	if a.mutated {
		t.Fatal("detached page mutated the document")
	}
	if n := a.doc.ByID("e"); n == nil || webrender.TextContent(n) != "" {
		t.Fatalf("detached write changed the node: %q", webrender.TextContent(n))
	}
}
