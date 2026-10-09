// M79e (#1708): the per-tab navigation seam, host-pinned.
//
// The live half of this card is a single client/host round trip that a
// class-B gate proves; what is pinned HERE is every rule that round trip
// silently depends on, so a regression is a host-test failure at
// `cd user/go && go test ./...` rather than a red VZ boot:
//
//   - the history is BOUNDED (navHistMax) and evicts the oldest, not the newest
//   - a path is truncated to the wire's title field, because that is the only
//     channel a declared path and a polled target can travel in
//   - a consecutive re-declare is a no-op that changes nothing
//   - a declare drops the forward stack
//   - the pending slot is id-matched, poll-once, and one-deep
//   - the ack title carries the target, and only on an applied ack
//   - a closed tab's queued target is dropped, so a later window reusing the
//     id cannot poll a dead tab's navigation
//   - the chord steps the FOCUSED tab and is a silent no-op at either end
package main

import (
	"testing"

	"virelai/vi"
)

// navRPC builds a WM_RPC request whose title carries path.
func navRPC(kind uint8, id uint32, path string) vi.WmRpc {
	var r vi.WmRpc
	r.Kind = kind
	r.ID = uint8(id)
	r.Seq = 9
	r.ReplyTo = 3
	r.SetTitle(path)
	return r
}

// resetNav strips the strip and the pending slots so one test cannot hand
// the next a queued target. The pending slot is package state (Zig's
// pending_nav_id too), so every test that touches it must start clean.
func resetNav() {
	tabs = TabStrip{}
	pendingNavID = 0
	pendingNavPath = ""
	rpcReplyPayload = ""
}

func declare(t *testing.T, id uint32, path string) bool {
	t.Helper()
	known, changed := tabs.NavDeclare(id, path)
	if !known {
		t.Fatalf("NavDeclare(%d, %q): tab not on the strip", id, path)
	}
	return changed
}

// The cap: recording more than navHistMax entries keeps the NEWEST
// navHistMax and drops the oldest. Evicting the newest instead would be the
// classic ring bug — it would silently make back-step useless exactly when
// a user has a long history.
func TestNavHistoryIsBoundedAndEvictsOldest(t *testing.T) {
	resetNav()
	tabs.OpenTab(5, "Files")
	for i := 0; i < navHistMax+6; i++ {
		if !declare(t, 5, navPath(i)) {
			t.Fatalf("declare %d reported no change", i)
		}
	}
	if got := tabs.NavDepth(5); got != navHistMax {
		t.Fatalf("NavDepth = %d want the cap %d", got, navHistMax)
	}
	// Walk all the way back: the entries must be the LAST navHistMax paths,
	// oldest first. Stepping back is the only way to observe the eviction.
	var walked []string
	for {
		p, ok := tabs.NavBack(5)
		if !ok {
			break
		}
		walked = append(walked, p)
	}
	// Back from the newest entry visits navHistMax-1 entries, then stops at
	// the oldest surviving one (cursor 0 has nothing behind it).
	if len(walked) != navHistMax-1 {
		t.Fatalf("walked back %d entries, want %d", len(walked), navHistMax-1)
	}
	// The recorded set was d0..d13 and the ring kept the newest 8 (d6..d13).
	// The cursor sits on d13, so the first step back lands on d12 and the
	// walk continues d11 ... d6. Landing on d6 first would mean the ring
	// evicted the NEWEST entry.
	cursor := navHistMax + 6 - 1
	for i, p := range walked {
		want := navPath(cursor - 1 - i)
		if p != want {
			t.Fatalf("walked[%d] = %q want %q (the oldest entry must be the one evicted)", i, p, want)
		}
	}
}

// navPath is the i-th distinct path used by the cap test.
func navPath(i int) string {
	return "/host/d" + itoa(i)
}

func itoa(i int) string {
	if i == 0 {
		return "0"
	}
	var b [4]byte
	n := 0
	for i > 0 {
		b[n] = byte('0' + i%10)
		i /= 10
		n++
	}
	out := make([]byte, n)
	for k := 0; k < n; k++ {
		out[k] = b[n-1-k]
	}
	return string(out)
}

// A path longer than the 24-byte title field is truncated at the wire. The
// seat must record what the client can actually GET BACK, not the longer
// string it was handed — otherwise poll returns a path the app never
// declared and the round trip is a lie.
func TestNavPathTruncatesToTheWireField(t *testing.T) {
	resetNav()
	tabs.OpenTab(5, "Files")
	long := "/host/this/path/is/far/longer/than/twenty/four"
	if len(long) <= navPathMax {
		t.Fatalf("fixture is not longer than the field: %d", len(long))
	}
	declare(t, 5, long)
	declare(t, 5, "/host/second")
	target, ok := tabs.NavBack(5)
	if !ok {
		t.Fatal("NavBack refused with two entries")
	}
	if len(target) != navPathMax {
		t.Fatalf("stored path is %d bytes, want the %d-byte field", len(target), navPathMax)
	}
	if target != long[:navPathMax] {
		t.Fatalf("stored path = %q want %q", target, long[:navPathMax])
	}
}

// A re-declaration of the CURRENT entry is the dedupe: an app that
// re-announces its path on every repaint must not grow the history, and
// must not be reported as a change (which is what silences the marker).
func TestNavDeclareDedupesConsecutive(t *testing.T) {
	resetNav()
	tabs.OpenTab(5, "Files")
	if !declare(t, 5, "/host/a") {
		t.Fatal("first declare reported no change")
	}
	if declare(t, 5, "/host/a") {
		t.Fatal("consecutive re-declare reported a change; the marker would lie")
	}
	if got := tabs.NavDepth(5); got != 1 {
		t.Fatalf("NavDepth = %d after a deduped declare, want 1", got)
	}
	// A DIFFERENT path is not a duplicate, even though /host/a is still
	// in the history.
	if !declare(t, 5, "/host/b") {
		t.Fatal("a different path reported no change")
	}
	if got := tabs.NavDepth(5); got != 2 {
		t.Fatalf("NavDepth = %d want 2", got)
	}
	// An empty path is never a record.
	if declare(t, 5, "") {
		t.Fatal("empty path reported a change")
	}
	if got := tabs.NavDepth(5); got != 2 {
		t.Fatalf("NavDepth = %d after an empty declare, want 2", got)
	}
}

// After stepping back, a NEW declare must drop the forward stack: the user
// navigated somewhere else, so the entries ahead are unreachable.
func TestNavDeclareDropsForwardStack(t *testing.T) {
	resetNav()
	tabs.OpenTab(5, "Files")
	declare(t, 5, "/host/a")
	declare(t, 5, "/host/b")
	declare(t, 5, "/host/c")
	if _, ok := tabs.NavBack(5); !ok {
		t.Fatal("NavBack refused with three entries")
	}
	declare(t, 5, "/host/d") // navigate from b to a NEW destination
	// The cursor is on the newest entry and nothing is ahead of it.
	if _, ok := tabs.NavForward(5); ok {
		t.Fatal("forward survived a new declare; the forward stack was not dropped")
	}
	if got := tabs.NavDepth(5); got != 3 {
		t.Fatalf("NavDepth = %d want 3 (a, b, d — c evicted by the new declare)", got)
	}
	// Back from d lands on b, not on the dropped c.
	if p, ok := tabs.NavBack(5); !ok || p != "/host/b" {
		t.Fatalf("NavBack = %q ok=%v want /host/b", p, ok)
	}
}

// The pending slot is ONE deep and poll-once: the first poll drains it and
// the second finds nothing.
func TestPendingNavIsPollOnce(t *testing.T) {
	resetNav()
	tabs.OpenTab(5, "Files")
	declare(t, 5, "/host/a")
	declare(t, 5, "/host/b")
	if _, ok := tabs.NavBack(5); !ok {
		t.Fatal("NavBack refused with two entries")
	}
	setPendingNav(5, "/host/a")
	p, ok := navPoll(5)
	if !ok || p != "/host/a" {
		t.Fatalf("navPoll = %q ok=%v want /host/a", p, ok)
	}
	if _, ok := navPoll(5); ok {
		t.Fatal("second poll succeeded; the slot is not poll-once")
	}
}

// A target queued for one window is never handed to another. This is the
// property that keeps two hosted tabs from stealing each other's back/forward.
func TestPendingNavIsIdMatched(t *testing.T) {
	resetNav()
	tabs.OpenTab(5, "Files")
	tabs.OpenTab(6, "Other")
	setPendingNav(6, "/host/other")
	if _, ok := navPoll(5); ok {
		t.Fatal("window 5 polled and took window 6's queued target")
	}
	// The refused poll must not have consumed it either.
	if p, ok := navPoll(6); !ok || p != "/host/other" {
		t.Fatalf("the owner's poll = %q ok=%v, want its own target", p, ok)
	}
	// id 0 is never a valid target (Zig's `id == 0` guard).
	setPendingNav(0, "/host/x")
	if _, ok := navPoll(0); ok {
		t.Fatal("window 0 polled successfully; id 0 must be refused")
	}
}

// A closed tab's queued target must not survive: a later window that reuses
// the id would otherwise receive a dead tab's navigation. Zig leaks this
// slot; GOTABWM clears it at the strip's one close choke point.
func TestClosedTabDropsItsQueuedTarget(t *testing.T) {
	resetNav()
	tabs.OpenTab(5, "Files")
	declare(t, 5, "/host/a")
	declare(t, 5, "/host/b")
	if _, ok := tabs.NavBack(5); !ok {
		t.Fatal("NavBack refused with two entries")
	}
	setPendingNav(5, "/host/a")
	tabs.CloseTab(5)
	// A NEW tab reusing id 5 polls: it must find nothing.
	tabs.OpenTab(5, "Files")
	if _, ok := navPoll(5); ok {
		t.Fatal("a fresh tab reusing the id claimed the closed tab's queued target")
	}
}

// The RPC arms end to end through applyRPC: declare records, the chord
// queues, and the poll ARM hands the target to the ack title — the exact
// bytes vi.PollNav reads on the client.
func TestNavPollArmCarriesTargetInAckTitle(t *testing.T) {
	resetNav()
	tabs.OpenTab(5, "Files")
	tabs.FocusTab(5)

	// Two declares through the real RPC arm.
	if !applyRPC(navRPC(vi.WmRpcKindNavDeclare, 5, "/host/alpha")) {
		t.Fatal("nav-declare arm refused a known tab")
	}
	if !applyRPC(navRPC(vi.WmRpcKindNavDeclare, 5, "/host/beta")) {
		t.Fatal("nav-declare arm refused a known tab")
	}
	if got := tabs.NavDepth(5); got != 2 {
		t.Fatalf("NavDepth = %d after two RPC declares, want 2", got)
	}

	// The chord steps back and queues for the focused tab.
	if !applyNavStep(true) {
		t.Fatal("the back chord refused with two entries recorded")
	}
	// The poll arm drains it into the reply payload.
	if !applyRPC(navRPC(vi.WmRpcKindNavPoll, 5, "")) {
		t.Fatal("nav-poll arm refused a queued target")
	}
	rep := buildReply(navRPC(vi.WmRpcKindNavPoll, 5, ""), true, takeReplyPayload(), vi.WmRpcPadPlain, 0)
	if rep.Applied != 1 {
		t.Fatalf("ack applied = %d want 1", rep.Applied)
	}
	if got := rep.TitleString(); got != "/host/alpha" {
		t.Fatalf("ack title = %q want /host/alpha", got)
	}
	// The payload must survive the wire the client decodes.
	got, ok := vi.DecodeWmRpc(rep.Encode())
	if !ok {
		t.Fatal("ack did not survive the wire round-trip")
	}
	if got.TitleString() != "/host/alpha" {
		t.Fatalf("decoded ack title = %q want /host/alpha", got.TitleString())
	}
	// Consume-on-use: the next ack must be clean.
	if p := takeReplyPayload(); p != "" {
		t.Fatalf("payload leaked into the next reply: %q", p)
	}
	// And a second poll finds nothing: the round trip is exactly once.
	if applyRPC(navRPC(vi.WmRpcKindNavPoll, 5, "")) {
		t.Fatal("a second poll succeeded; the target was not poll-once")
	}
}

// An unknown window id is refused by both arms, and refused HONESTLY: no
// history recorded, and applied=0 so the client sees the refusal.
func TestNavArmsRefuseUnknownTab(t *testing.T) {
	resetNav()
	if applyRPC(navRPC(vi.WmRpcKindNavDeclare, 99, "/host/ghost")) {
		t.Fatal("nav-declare accepted a tab that is not on the strip")
	}
	if applyRPC(navRPC(vi.WmRpcKindNavPoll, 99, "")) {
		t.Fatal("nav-poll answered for a tab that is not on the strip")
	}
	if tabs.NavDepth(99) != 0 {
		t.Fatal("a refused declare created history")
	}
}

// A refused poll (nothing queued) must not leave a payload behind for the
// next reply.
func TestRefusedPollLeavesNoPayload(t *testing.T) {
	resetNav()
	tabs.OpenTab(5, "Files")
	if applyRPC(navRPC(vi.WmRpcKindNavPoll, 5, "")) {
		t.Fatal("poll succeeded with nothing queued")
	}
	if p := takeReplyPayload(); p != "" {
		t.Fatalf("a refused poll queued the payload %q", p)
	}
}

// The chord steps the FOCUSED tab, and is a silent no-op at both ends of the
// history — including leaving an ALREADY QUEUED target alone, so a refused
// step cannot swallow what an accepted step is waiting to deliver.
func TestNavChordIsFocusedTabAndSilentAtTheEnds(t *testing.T) {
	resetNav()
	tabs.OpenTab(5, "Files")
	tabs.OpenTab(6, "Other")
	tabs.FocusTab(6)

	// No history at all: both directions refuse.
	if applyNavStep(true) || applyNavStep(false) {
		t.Fatal("a chord stepped a tab with no history")
	}
	// Record on tab 5, but the FOCUSED tab is 6 — the chord must not move 5.
	declare(t, 5, "/host/a")
	declare(t, 5, "/host/b")
	if applyNavStep(true) {
		t.Fatal("the chord stepped a tab that is not focused")
	}
	if _, ok := navPoll(5); ok {
		t.Fatal("the chord queued a target for the unfocused tab")
	}

	// Focus 5 and step: the queue is the focused tab's.
	tabs.FocusTab(5)
	if !applyNavStep(true) {
		t.Fatal("the back chord refused on the focused tab")
	}
	if p, ok := navPoll(5); !ok || p != "/host/a" {
		t.Fatalf("queued target = %q ok=%v want /host/a", p, ok)
	}

	// Already at the oldest entry: back refuses, and the one entry ahead
	// takes exactly one forward to reach the newest.
	if applyNavStep(true) {
		t.Fatal("back succeeded at the oldest entry")
	}
	if !applyNavStep(false) {
		t.Fatal("forward refused with an entry ahead")
	}
	// At the newest: a second forward refuses, and must NOT clear the queue
	// the accepted step left behind — a refused chord swallowing a pending
	// target would strand the app on the wrong page.
	if applyNavStep(false) {
		t.Fatal("forward succeeded at the newest entry")
	}
	if p, ok := navPoll(5); !ok || p != "/host/b" {
		t.Fatalf("pending after a refused forward = %q ok=%v want /host/b", p, ok)
	}
}

// The bounds themselves are the wire's, not arbitrary numbers.
func TestNavBoundsMatchTheWire(t *testing.T) {
	if navPathMax != 24 {
		t.Fatalf("navPathMax = %d, want the 24-byte WM_RPC title field", navPathMax)
	}
	if navHistMax != 8 {
		t.Fatalf("navHistMax = %d, want Zig's hist_max of 8", navHistMax)
	}
}
