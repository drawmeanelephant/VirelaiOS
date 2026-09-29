// GOTABWM.ELF - M57c (issue #1318): the Go seat hosts UNMODIFIED Zig apps.
//
// This is the card that makes the seat real. The client under test is the Go
// editor NOTE.ELF (it replaced the Zig notepad, which #1485 deleted): it rides
// user/go/tabapp, opens its own window, then declares itself to the running WM
// over the WM_RPC mailbox and waits for the ack. It discovers the WM
// purely by process name (abi.zig wm_proc_names), which M57c extended with
// GOTABWM.ELF - the same additive step M42 SX3 took for TABWM.BIN. No app
// source change, no port: the same binaries that run under TABWM run here.
//
// The seat is the SERVER half. It drains its own mailbox, applies each request
// with the kernel's own primitives, and acks to the requester. The markers are:
//
//	declare_fullscreen (8)      gotabwm: tab open id=<n>
//	                            gotabwm: host focus / tab focus id=<n>
//	                            gotabwm: host view id=<n>
//	                            gotabwm: rpc declare id=<n>
//	raise (1)                   gotabwm: rpc raise / tab focus id=<n>
//	attach (5)                  gotabwm: tab open (if new); rpc attach id=<n>
//	detach (6)                  gotabwm: rpc detach id=<n>
//	cycle (7)                   gotabwm: tab focus id=<n>; rpc cycle
//	set title (11)              gotabwm: title id=<n> <text>
//	notify (12)                 gotabwm: notify id=<n> <text>
//	                            gotabwm: notify dismiss id=<n> (on expiry or click)
//	anything else               gotabwm: rpc other kind=<n>
//	rail after paint            gotabwm: rail n=<n> focus=<id>
//	close focused / last        gotabwm: host close / tab close id=<n>
//	                            then tab focus remaining, or tabs empty
//	loop ends                   gotabwm: host done
//
// The ack is the M56b wire (wmclient.go / wnd_core.zig): kind | 0x80, id, seq,
// applied. The hosted Go calc's "gocalc: declare accepted" proves the ack
// carried applied=1; "gocalc: present" / "notepad: resize relayout"
// proves the full-viewport proposal reached it; "gocalc: close" proves the
// close did.
package main

import (
	"virelai/vi"
)

// The interop markers the class-B gate greps. Exported so interop_test.go can
// pin the exact shapes (a drift is a host-test failure, not a silent live miss).
const (
	MarkerRpcDeclare = "gotabwm: rpc declare id="
	MarkerRpcRaise   = "gotabwm: rpc raise id="
	MarkerRpcAttach  = "gotabwm: rpc attach id="
	MarkerRpcDetach  = "gotabwm: rpc detach id="
	MarkerRpcCycle   = "gotabwm: rpc cycle"
	MarkerTitle      = "gotabwm: title id="
	MarkerRpcOther   = "gotabwm: rpc other kind="
	MarkerHostFocus  = "gotabwm: host focus id="
	MarkerHostView   = "gotabwm: host view id="
	MarkerHostClose  = "gotabwm: host close id="
	MarkerHostDone   = "gotabwm: host done"
)

// M79e (#1708): the pending navigation target. ONE slot, keyed by the window
// id it belongs to and drained poll-once — Zig tabwm's pending_nav_id /
// pending_nav_path, same bound. A second step before the first is polled
// overwrites the first: the seat queues what the user most recently asked
// for, not a backlog.
var (
	pendingNavID   uint32
	pendingNavPath string
)

// rpcReplyPayload is the nav-poll channel's answer: the ack's `title` field
// carries the target path the app must navigate to, which is exactly how
// TABWM does it (Zig's rpc_reply_payload, consumed by the reply). Kept
// separate from pendingNav* because they have different lifetimes — this
// one lives for exactly one reply, that one until the app polls.
var rpcReplyPayload string

// setPendingNav queues a step target for one app.
func setPendingNav(id uint32, path string) {
	pendingNavID = id
	pendingNavPath = path
}

// navPoll drains the queued target for id, clearing the slot. A slot queued
// for a different window is NOT stolen — the poll is refused and the target
// stays for its own app.
func navPoll(id uint32) (string, bool) {
	if id == 0 || pendingNavID != id || pendingNavPath == "" {
		return "", false
	}
	p := pendingNavPath
	pendingNavID = 0
	pendingNavPath = ""
	return p, true
}

// clearPendingNav drops any target queued for id. Called from the one place
// a tab leaves the strip: Zig lets the slot outlive its tab, which means a
// LATER window that reuses the same id can poll and receive a dead tab's
// navigation. One line at the choke point is cheaper than that bug.
func clearPendingNav(id uint32) {
	if pendingNavID == id {
		pendingNavID = 0
		pendingNavPath = ""
	}
}

// takeReplyPayload consumes the pending ack payload. Consume-on-use (not a
// read) so a payload can never leak into the NEXT request's reply.
func takeReplyPayload() string {
	p := rpcReplyPayload
	rpcReplyPayload = ""
	return p
}

// tabs is the in-process strip (M62b). hostedApp is the focused client's
// window id (0 = none) kept in sync so the M57c paint-suppression check
// and go-wm-seat close path keep working with one hosted app.
var (
	tabs      TabStrip
	hostedApp uint32
	// dogfoodHosted (M69a, issue #1528) latches at the first tab this boot
	// put on the strip -- the declare and attach paths both set it. It is the
	// honesty gate for `dogfood: ok`: the seat closes its tabs before
	// host-done, so the strip is empty again by then and the latch is the only
	// record that this boot hosted anything at all.
	dogfoodHosted bool
)

// hostTicks is how long the seat hosts a SINGLE app before closing it —
// enough composite ticks for the app to declare, take the viewport and relayout,
// plus one `--pointer-virtio` click (pointerClickHold). Two tabs skip this
// countdown and use the strip choreography in seat.go.
// M79a (#1704): both the countdown and the choreography are DEMO mode only
// (the seeded /host/GOTABWM.DEMO trigger); live mode never auto-closes.
// M62g: GOEDIT starts slower than the other clients (the Zig CALC/NOTEPAD pair
// when this was written; both are Go apps now — GOCALC.ELF/NOTE.ELF), so the
// second declare needs more than three ticks or the first tab is closed.
const hostTicks = 16

// hostTicksLeft counts down while exactly one tab is open; the composite
// loop closes that tab when it reaches zero (go-wm-seat / go-wm-default).
var hostTicksLeft int

// Two-tab close choreography (M62b): wait until the rail has been painted
// with n>=2, close the focused tab, then close the last. The seat stays
// registered after the strip is empty.
var (
	stripSawTwo    bool
	stripClosedOne bool
	stripDone      bool
	stripStep      int // two-tab: rail → reorder → pin → V → unsplit → H → unsplit → close pinned → close last
	stripHoldLeft  int // M63b: ticks left before stripStep advances
)

// serviceRPC drains the seat's mailbox and services every queued WM_RPC
// request. It is called from the composite loop, so the seat answers while it
// holds the scanout cadence. Bounded per call so a flood cannot starve the tick
// loop. Returns how many requests were served.
func serviceRPC() int {
	var buf [vi.WmRpcMax]byte
	served := 0
	for i := 0; i < 8; i++ {
		n, r := vi.IpcRecv(buf[:])
		if r < 0 || n < 38 {
			return served
		}
		req, ok := vi.DecodeWmRpc(buf[:n])
		if !ok {
			continue
		}
		// An ack is never a request (the M56b reply flag).
		if req.Kind&vi.WmRpcReplyFlag != 0 {
			continue
		}
		applied := applyRPC(req)
		replyRPC(req, applied)
		served++
	}
	return served
}

// applyRPC performs one WM_RPC request with the kernel's own primitives and
// returns whether it was applied. Only the paths a tab-aware Zig app exercises
// are implemented; everything else is refused (applied=0) rather than faked.
func applyRPC(req vi.WmRpc) bool {
	id := uint32(req.ID)
	switch req.Kind & 0x7f {
	case vi.WmRpcKindDeclareFullscreen: // 8, the path lib/tabapp.zig uses
		if tabs.OpenTab(id, req.TitleString()) {
			noteStripOpen()
			dogfoodHosted = true
			vi.ConsoleLine(MarkerTabOpen + vi.Itoa64(int64(id)))
		}
		hostedApp = id
		hostTicksLeft = hostTicks
		// Focus and raise through the kernel's taskbar-click primitive, so the
		// app receives the real WIN_FOCUS. New declares take focus so two
		// clients leave exactly one focused tab (M62b).
		if vi.WmctlTaskbarClick(id) == 0 {
			_ = tabs.FocusTab(id)
			vi.ConsoleLine(MarkerHostFocus + vi.Itoa64(int64(id)))
			vi.ConsoleLine(MarkerTabFocus + vi.Itoa64(int64(id)))
		}
		// Propose the full viewport. The kernel clamps (WM proposes, kernel
		// clamps) and pushes WIN_RESIZE to the app, which relayouts.
		if vi.WmctlSetWindowRect(id, 0, 0, uint32(vi.ScanoutWidth), uint32(vi.ScanoutHeight)) == 0 {
			vi.ConsoleLine(MarkerHostView + vi.Itoa64(int64(id)))
		}
		vi.ConsoleLine(MarkerRpcDeclare + vi.Itoa64(int64(id)))
		// M79g (#1718): an app that joined the strip is part of the
		// session. This is the arm a launcher exec reaches (Ctrl+Space ->
		// execSelected has no marker-gated write site of its own), so the
		// RPC — not the launcher — owns the write.
		noteSessionMutation()
		return true
	case vi.WmRpcKindRaise: // 1
		if vi.WmctlTaskbarClick(id) == 0 {
			_ = tabs.FocusTab(id)
			vi.ConsoleLine(MarkerRpcRaise + vi.Itoa64(int64(id)))
			vi.ConsoleLine(MarkerTabFocus + vi.Itoa64(int64(id)))
			return true
		}
		return false
	case vi.WmRpcKindAttachTab: // 5
		if tabs.OpenTab(id, req.TitleString()) {
			noteStripOpen()
			dogfoodHosted = true
			vi.ConsoleLine(MarkerTabOpen + vi.Itoa64(int64(id)))
			noteSessionMutation() // M79g (#1718): a new tab persists
		}
		// Same host-state sync as declare: an attach-without-declare client
		// still lands on the strip with focus and the single-tab close budget.
		_ = tabs.FocusTab(id)
		hostedApp = id
		hostTicksLeft = hostTicks
		vi.ConsoleLine(MarkerRpcAttach + vi.Itoa64(int64(id)))
		return true
	case vi.WmRpcKindDetachTab: // 6
		closed := tabs.CloseTab(id)
		syncHostedFromStrip()
		vi.ConsoleLine(MarkerRpcDetach + vi.Itoa64(int64(id)))
		if closed {
			// M79g (#1718): the tab really left the strip, so the file
			// must say so. An unknown id changed nothing and is not a
			// write.
			noteSessionMutation()
		}
		return true
	case vi.WmRpcKindCycleTab: // 7
		if nid, ok := tabs.NextID(); ok {
			if vi.WmctlTaskbarClick(nid) == 0 {
				_ = tabs.FocusTab(nid)
				vi.ConsoleLine(MarkerTabFocus + vi.Itoa64(int64(nid)))
			}
		}
		vi.ConsoleLine(MarkerRpcCycle)
		return true
	case vi.WmRpcKindNavDeclare: // 9, client -> seat: the app navigated
		// The path rides the request title, exactly as TABWM reads it.
		// A re-declaration of the current entry is a no-op that still
		// ACKS applied=1 (the tab is known and the request was honoured)
		// but prints no marker — the state did not move, and a marker
		// claiming a record that was deduped would be a lie.
		known, changed := tabs.NavDeclare(id, req.TitleString())
		if !known {
			return false
		}
		if !changed {
			return true
		}
		vi.ConsoleLine(MarkerNavDeclare + vi.Itoa64(int64(id)) + " path=" + req.TitleString())
		return true
	case vi.WmRpcKindNavPoll: // 10, client -> seat: give me my back/forward
		path, ok := navPoll(id)
		if !ok {
			// Nothing queued: applied=0, and NO marker. The client reads
			// the empty ack as "no target", which is the honest answer.
			return false
		}
		rpcReplyPayload = path
		vi.ConsoleLine(MarkerNavPoll + vi.Itoa64(int64(id)) + " path=" + path)
		return true
	case vi.WmRpcKindSetTitle: // 11, client -> seat
		title := req.TitleString()
		if !tabs.SetTitle(id, title) {
			return false
		}
		// The strip is the rail's render model; the next composite tick paints
		// the new label. The marker follows the successful state mutation.
		vi.ConsoleLine(MarkerTitle + vi.Itoa64(int64(id)) + " " + title)
		// M79g (#1718): a title the app set is the title the session
		// restores. Bin is untouched — SetTitle never re-guesses it, so
		// reopen identity survives a rename.
		noteSessionMutation()
		return true
	case vi.WmRpcKindNotify: // 12, client -> seat: say something to the user
		// M79k (#1720). The request id is the SENDER, so the toast's
		// click-through has a tab to focus without a second lookup, and
		// the title is the whole message budget (24 NUL-trimmed bytes).
		//
		// An unknown sender or an empty message is REFUSED (applied=0,
		// no marker): a toast that cannot be clicked back to anything is
		// noise, and the client learns the truth from its own ack instead
		// of the user learning it from a toast that does nothing.
		text := req.TitleString()
		if text == "" || tabs.index(id) < 0 {
			return false
		}
		text, _, dropped := notifyPush(id, text, seatTick)
		// M82d2 (#1785): under do-not-disturb the notice is in the center's
		// history and nothing else — no toast was queued, so there is no
		// drop to report and no `notify id=` to claim. The ack is applied
		// all the same: an app must not learn the user is away.
		if notifyDND {
			vi.ConsoleLine(MarkerNotifyHeld + vi.Itoa64(int64(id)) + " " + text)
			return true
		}
		// The drop marker comes FIRST: the older toast has already left
		// the queue, and a stream that silently lost one must say so
		// before it announces the one it kept.
		if dropped {
			vi.ConsoleLine(MarkerNotifyDrop + vi.Itoa64(int64(notifyDropped)))
		}
		// Printed after the push, so the marker can never claim a toast
		// the queue does not hold. The next composite tick paints it.
		vi.ConsoleLine(MarkerNotify + vi.Itoa64(int64(id)) + " " + text)
		return true
	case vi.WmRpcKindSettingsSubscribe: // 13, client -> seat: subscribe to one key
		return applySettingsSubscribe(req)
	case vi.WmRpcKindSettingsPublish: // 14, writer -> seat: a persisted key changed
		return applySettingsPublish(req)
	default:
		vi.ConsoleLine(MarkerRpcOther + vi.Itoa64(int64(req.Kind&0x7f)))
		return false
	}
}

// replyRPC acks one request to its requester. The frame mirrors the request's
// id/seq and carries the applied flag plus the reply bit, exactly like TABWM's
// wnd_mail_reply, so an unmodified app accepts it.
func replyRPC(req vi.WmRpc, applied bool) {
	// M79e (#1708): the title carries the nav-poll payload, the same
	// channel TABWM uses (Zig's rpc_reply_payload). takeReplyPayload is
	// consume-on-use, so only the nav-poll reply carries a path and the
	// next ack is zeroed again.
	if req.ReplyTo != 0 {
		_ = vi.IpcSend(uint32(req.ReplyTo), buildReply(req, applied, takeReplyPayload()).Encode())
	}
}

// buildReply is the pure half of replyRPC: the ack frame for one request. Split
// out so the host test pins the wire (reply bit, mirrored id/seq, applied flag,
// and the nav-poll title payload) without a guest.
func buildReply(req vi.WmRpc, applied bool, payload string) vi.WmRpc {
	var rep vi.WmRpc
	rep.Kind = req.Kind | vi.WmRpcReplyFlag
	rep.ID = req.ID
	rep.Seq = req.Seq
	rep.ReplyTo = req.ReplyTo
	if applied {
		rep.Applied = 1
		// Only an applied ack carries a payload: a refused poll must not
		// hand the client a path it was not given.
		if payload != "" {
			n := len(payload)
			if n > len(rep.Title) {
				n = len(rep.Title)
			}
			copy(rep.Title[:], payload[:n])
		}
	}
	return rep
}

// closeWin / focusRaise are the close path's WM-primitive seams (the execApp
// pattern in hid.go): vi.Wmctl* calls the raw syscall2 gateway and so bypasses
// vi's host-test syscall hook, which would leave the close decision
// unobservable off the guest. They are vi.WmctlWinClose / WmctlTaskbarClick
// in the guest.
var (
	closeWin   = vi.WmctlWinClose
	focusRaise = vi.WmctlTaskbarClick
)

// closeTabByID closes id's window through the WM seam; the app receives the
// real WIN_CLOSE and exits. Every close path lands here (the choreography's
// closeHosted, M79b's close-x), so `host close id=` / `tab close id=` carry
// exactly one shape. A close of the FOCUSED tab moves focus to the remaining
// tab (M62b) and says so; closing an unfocused tab (the close-x) changes no
// focus and prints no focus markers. Returns whether a close was issued.
func closeTabByID(id uint32) bool {
	if id == 0 {
		return false
	}
	fid, focused := tabs.Focused()
	wasFocused := focused && fid == id
	if closeWin(id) != 0 {
		return false
	}
	vi.ConsoleLine(MarkerHostClose + vi.Itoa64(int64(id)))
	vi.ConsoleLine(MarkerTabClose + vi.Itoa64(int64(id)))
	closed := tabs.CloseTab(id)
	syncHostedFromStrip()
	if closed {
		// M79g (#1718): every close lands on this seam (choreography,
		// close-x, exit sweep), so one hook here covers them all — a
		// closed tab must not be on the next boot's strip. Closing the
		// LAST tab leaves an empty strip, which writeSession refuses:
		// the file keeps the previous contents rather than publishing an
		// empty session.
		noteSessionMutation()
	}
	if nid, ok := tabs.Focused(); ok {
		// Closing one side of a split leaves a single tab: restore
		// full-viewport (Unsplit already ran in the two-tab choreography;
		// this covers a close while still split).
		if tabs.Count() == 1 {
			_ = applyRect(nid, FullRect(uint32(vi.ScanoutWidth), uint32(vi.ScanoutHeight)))
		}
		// The focus handoff is the focused close's story (M62b): an
		// unfocused close-x leaves the keyboard where it was, so it
		// neither raises nor reports a focus change.
		if wasFocused {
			if focusRaise(nid) == 0 {
				vi.ConsoleLine(MarkerTabFocus + vi.Itoa64(int64(nid)))
				vi.ConsoleLine(MarkerHostFocus + vi.Itoa64(int64(nid)))
			}
		}
	} else {
		vi.ConsoleLine(MarkerTabsEmpty)
	}
	return true
}

// closeHosted closes the focused tab's window (the M62b choreography close
// seam). Focus moves to the remaining tab or the strip goes empty.
func closeHosted() bool {
	id, ok := tabs.Focused()
	if !ok {
		if hostedApp == 0 {
			return false
		}
		id = hostedApp
	}
	return closeTabByID(id)
}

func syncHostedFromStrip() {
	if id, ok := tabs.Focused(); ok {
		hostedApp = id
		return
	}
	hostedApp = 0
}

// noteStripOpen restarts close-handling when a tab lands on an empty strip.
// Without this, a late OpenTab after the single-tab path set stripDone would
// stay open until process exit (the gate-unreachable edge: a second declare
// more than hostTicks after the first). A second tab on an already-populated
// strip must not clear stripSawTwo — that latch is what keeps the two-tab
// close path from falling through to the single-tab countdown.
func noteStripOpen() {
	if tabs.Count() != 1 {
		return
	}
	stripDone = false
	stripSawTwo = false
	stripClosedOne = false
	stripStep = 0
	stripHoldLeft = 0
}
