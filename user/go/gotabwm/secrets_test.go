package main

import (
	"testing"

	"virelai/appkit"
	"virelai/chords"
	"virelai/vi"
)

// stubSecrets wires the slot-96 seams for one test and restores them.
func stubSecrets(t *testing.T, list func(uint32, []byte) (int, int64), commit, drop func(uint32) int64) {
	t.Helper()
	l, c, d := secretAskList, secretAskCommit, secretAskDrop
	secretAskList, secretAskCommit, secretAskDrop = list, commit, drop
	t.Cleanup(func() {
		secretAskList, secretAskCommit, secretAskDrop = l, c, d
		secretAskModal = secretAsk{}
	})
}

func TestSecretAskLifecycleGrantAndDecline(t *testing.T) {
	resetSeatForTest(t)
	var commitID, dropID uint32
	var log []string
	oldLog := secretLog
	secretLog = func(s string) { log = append(log, s) }
	t.Cleanup(func() { secretLog = oldLog })
	oldOpen, oldQuery, oldClose, oldRaise := openInputSink, queryWindow, closeWin, focusRaise
	openInputSink = func(x, y, w, h uint32) (int, int64) { return 91, 0 }
	queryWindow = func(id int) ([8]uint32, int64) { var st [8]uint32; return st, 0 }
	closeWin = func(id uint32) int64 { return 0 }
	focusRaise = func(id uint32) int64 { return 0 }
	t.Cleanup(func() { openInputSink, queryWindow, closeWin, focusRaise = oldOpen, oldQuery, oldClose, oldRaise })

	pending := []string{"7\t41\t1000\tGOMAIL.ELF\tmail-x\tnew\tGOMAIL.ELF"}
	stubSecrets(t,
		func(i uint32, buf []byte) (int, int64) {
			if int(i) >= len(pending) {
				return 0, -vi.ErrENOENT
			}
			copy(buf, pending[i])
			return len(pending[i]), int64(len(pending[i]))
		},
		func(id uint32) int64 { commitID = id; return 0 },
		func(id uint32) int64 { dropID = id; return 0 })

	pollSecretAsks()
	if !secretAskModal.open || secretAskModal.id != 7 {
		t.Fatalf("modal open=%v id=%d", secretAskModal.open, secretAskModal.id)
	}
	if log[len(log)-1] != MarkerSecretAsk+"GOMAIL.ELF key=mail-x apps=GOMAIL.ELF" {
		t.Fatalf("ask marker: %v", log)
	}
	// An unrelated key is swallowed, not answered.
	if !handleSecretAskKey(vi.Event{Kind: vi.EvWmKey, Arg0: 0x1b}) || commitID != 0 {
		t.Fatalf("stray key committed %d", commitID)
	}
	// 'g' commits the staged ask and closes the prompt — Arg0 carries the
	// raw HID usage on the kind-21 wire (arg1 is always 0). M216l: the
	// arm delay is a precondition, driven in the dedicated test below.
	secretAskModal.armed = true
	handleSecretAskKey(vi.Event{Kind: vi.EvWmKey, Arg0: 0x0a})
	if commitID != 7 || secretAskModal.open {
		t.Fatalf("commit id=%d open=%v", commitID, secretAskModal.open)
	}
	if log[len(log)-1] != MarkerSecretGrant+"GOMAIL.ELF key=mail-x" {
		t.Fatalf("grant marker: %v", log)
	}

	// A second ask stands after the first answered; Esc declines it.
	pending = []string{"9\t42\t1000\tGOIRC.ELF\tirc\tnew"}
	pollSecretAsks()
	if !secretAskModal.open || secretAskModal.id != 9 {
		t.Fatalf("second ask open=%v", secretAskModal.open)
	}
	handleSecretAskKey(vi.Event{Kind: vi.EvWmKey, Arg0: 0x29})
	if dropID != 9 || secretAskModal.open {
		t.Fatalf("drop id=%d open=%v", dropID, secretAskModal.open)
	}
	if log[len(log)-1] != MarkerSecretDecline+"GOIRC.ELF key=irc" {
		t.Fatalf("decline marker: %v", log)
	}
}

func TestSecretAskParseAndRefusalMarker(t *testing.T) {
	resetSeatForTest(t)
	a, ok := parseSecretAskLine("1\t41\t1000\tA.ELF\tk\tnew\tAPPS")
	if !ok || a.replaces || a.apps != "APPS" {
		t.Fatalf("valid line: ok=%v ask=%+v", ok, a)
	}
	a, ok = parseSecretAskLine("2\t41\t1000\tA.ELF\tk\treplace")
	if !ok || !a.replaces || a.apps != "" {
		t.Fatalf("replace line: ok=%v ask=%+v", ok, a)
	}
	for _, bad := range []string{"", "1\t2\t3", "x\t41\t1000\tA\tk", "1\t41\tx\tA\tk", "1\t41\t1000\t\tk",
		"1\t41\t1000\tA.ELF\tk\tbogus", "1\t41\t1000\tA.ELF\tk"} {
		if _, ok := parseSecretAskLine(bad); ok {
			t.Fatalf("malformed line accepted: %q", bad)
		}
	}
	var log []string
	oldLog := secretLog
	secretLog = func(s string) { log = append(log, s) }
	t.Cleanup(func() { secretLog = oldLog })
	oldOpen, oldQuery := openInputSink, queryWindow
	openInputSink = func(x, y, w, h uint32) (int, int64) { return 91, 0 }
	queryWindow = func(id int) ([8]uint32, int64) { var st [8]uint32; st[5] = 1; return st, 0 }
	t.Cleanup(func() { openInputSink, queryWindow = oldOpen, oldQuery })
	stubSecrets(t,
		func(i uint32, buf []byte) (int, int64) {
			if i != 0 {
				return 0, -vi.ErrENOENT
			}
			l := "5\t41\t1000\tAPP.ELF\tk\tnew"
			copy(buf, l)
			return len(l), int64(len(l))
		},
		func(id uint32) int64 { return -vi.ErrENOSPC },
		func(id uint32) int64 { return 0 })
	pollSecretAsks()
	secretAskModal.armed = true // M216l: grant is dead until armed
	handleSecretAskKey(vi.Event{Kind: vi.EvWmKey, Arg0: 0x0a})
	if log[len(log)-1] != MarkerSecretRefuse+"APP.ELF key=k ENOSPC" {
		t.Fatalf("refuse marker: %v", log)
	}
}

// #3407: a `replace`-mode ask names the overwrite — the standing marker
// carries `replace` so the consent gesture is asked with the decisive
// fact instead of presenting a clobber as an ordinary add.
func TestSecretAskReplaceMarker(t *testing.T) {
	resetSeatForTest(t)
	var log []string
	oldLog := secretLog
	secretLog = func(s string) { log = append(log, s) }
	t.Cleanup(func() { secretLog = oldLog })
	oldOpen, oldQuery := openInputSink, queryWindow
	openInputSink = func(x, y, w, h uint32) (int, int64) { return 91, 0 }
	queryWindow = func(id int) ([8]uint32, int64) { var st [8]uint32; st[5] = 1; return st, 0 }
	t.Cleanup(func() { openInputSink, queryWindow = oldOpen, oldQuery })
	stubSecrets(t,
		func(i uint32, buf []byte) (int, int64) {
			if i != 0 {
				return 0, -vi.ErrENOENT
			}
			l := "5\t41\t1000\tAPP.ELF\tnet-hmac\treplace"
			copy(buf, l)
			return len(l), int64(len(l))
		},
		func(id uint32) int64 { return -vi.ErrEACCES },
		func(id uint32) int64 { return 0 })
	pollSecretAsks()
	if !secretAskModal.open || !secretAskModal.replaces {
		t.Fatalf("modal open=%v replaces=%v", secretAskModal.open, secretAskModal.replaces)
	}
	if log[len(log)-1] != MarkerSecretAsk+"APP.ELF key=net-hmac replace" {
		t.Fatalf("replace marker: %v", log)
	}
}

// stubSecretWindow seams the prompt's sink + focus syscalls for one test:
// sequential sink ids (ask=91, lock=92) so the hand-back is assertable,
// and a focusRaise recorder.
func stubSecretWindow(t *testing.T) (raised *[]uint32) {
	t.Helper()
	oldOpen, oldQuery, oldClose, oldRaise := openInputSink, queryWindow, closeWin, focusRaise
	next := 91
	r := new([]uint32)
	openInputSink = func(x, y, w, h uint32) (int, int64) { id := next; next++; return id, 0 }
	queryWindow = func(id int) ([8]uint32, int64) { var st [8]uint32; st[5] = 1; return st, 0 }
	closeWin = func(id uint32) int64 { return 0 }
	focusRaise = func(id uint32) int64 { *r = append(*r, id); return 0 }
	t.Cleanup(func() { openInputSink, queryWindow, closeWin, focusRaise = oldOpen, oldQuery, oldClose, oldRaise })
	return r
}

// stubOneAsk wires a single pending line through the poll.
func stubOneAsk(t *testing.T, line string, commit, drop *uint32) {
	t.Helper()
	stubSecrets(t,
		func(i uint32, buf []byte) (int, int64) {
			if i != 0 {
				return 0, -vi.ErrENOENT
			}
			copy(buf, line)
			return len(line), int64(len(line))
		},
		func(id uint32) int64 { *commit = id; return 0 },
		func(id uint32) int64 { *drop = id; return 0 })
}

// M216l (#3951): the arm delay — a 'g' before the first heartbeat after
// the presented frame is swallowed whole; once armed it grants. Decline
// never waits for the arm.
func TestSecretAskArmDelay(t *testing.T) {
	resetSeatForTest(t)
	var commitID, dropID uint32
	var log []string
	oldLog := secretLog
	secretLog = func(s string) { log = append(log, s) }
	t.Cleanup(func() { secretLog = oldLog })
	stubSecretWindow(t)
	stubOneAsk(t, "7\t41\t1000\tGOMAIL.ELF\tmail-x\tnew\tGOMAIL.ELF", &commitID, &dropID)
	pollSecretAsks()
	if !secretAskModal.open {
		t.Fatal("ask did not open")
	}

	// A 'g' typed before the prompt could have been seen is swallowed:
	// no commit, and the prompt still stands.
	if !handleSecretAskKey(vi.Event{Kind: vi.EvWmKey, Arg0: 0x0a}) || commitID != 0 {
		t.Fatalf("unarmed g committed %d", commitID)
	}
	if !secretAskModal.open {
		t.Fatal("an unarmed g answered the prompt")
	}
	// The heartbeat alone cannot arm what no presented frame carried.
	armSecretAsk()
	if secretAskModal.armed {
		t.Fatal("armed before any presented frame")
	}
	// A frame that carried the prompt covered does not count either.
	lockActive = true
	noteSecretFrame(1, true)
	lockActive = false
	if secretAskModal.seen {
		t.Fatal("a curtain-covered presented frame counted as seen")
	}
	noteSecretFrame(1, false)
	if secretAskModal.seen {
		t.Fatal("an unpresented frame counted as seen")
	}
	noteSecretFrame(1, true)
	armSecretAsk()
	if !secretAskModal.armed {
		t.Fatal("presented frame + heartbeat did not arm")
	}
	if log[len(log)-1] != MarkerSecretArm+"GOMAIL.ELF key=mail-x" {
		t.Fatalf("arm marker: %v", log)
	}
	handleSecretAskKey(vi.Event{Kind: vi.EvWmKey, Arg0: 0x0a})
	if commitID != 7 || secretAskModal.open {
		t.Fatalf("armed g: commit=%d open=%v", commitID, secretAskModal.open)
	}

	// Decline never waited for the arm: a fresh ask drops on 'd' before
	// any frame could have presented.
	stubOneAsk(t, "9\t42\t1000\tGOIRC.ELF\tirc\tnew", &commitID, &dropID)
	pollSecretAsks()
	if !secretAskModal.open || secretAskModal.armed {
		t.Fatalf("second ask open=%v armed=%v", secretAskModal.open, secretAskModal.armed)
	}
	handleSecretAskKey(vi.Event{Kind: vi.EvWmKey, Arg0: uint32(hidUsageD)})
	if dropID != 9 || secretAskModal.open {
		t.Fatalf("unarmed decline: drop=%d open=%v", dropID, secretAskModal.open)
	}
}

// M216l: Tab cycles the two buttons and Enter runs the focused one —
// the pointer affordance's keyboard twin. Deny is the default focus, so
// an unaimed Enter declines; Grant's Enter obeys the same arm delay 'g'
// does.
func TestSecretAskTabEnterButtons(t *testing.T) {
	resetSeatForTest(t)
	var commitID, dropID uint32
	stubSecretWindow(t)
	stubOneAsk(t, "7\t41\t1000\tGOMAIL.ELF\tmail\tnew", &commitID, &dropID)
	pollSecretAsks()

	// Default focus is Deny: Enter declines even before arming.
	handleSecretAskKey(vi.Event{Kind: vi.EvWmKey, Arg0: 0x28})
	if dropID != 7 || commitID != 0 || secretAskModal.open {
		t.Fatalf("default Enter: drop=%d commit=%d open=%v", dropID, commitID, secretAskModal.open)
	}

	// Tab to Grant: Enter still cannot commit before the arm.
	pollSecretAsks()
	handleSecretAskKey(vi.Event{Kind: vi.EvWmKey, Arg0: 0x2b})
	if secretAskModal.sel != secretBtnGrant {
		t.Fatalf("Tab sel=%d want grant", secretAskModal.sel)
	}
	handleSecretAskKey(vi.Event{Kind: vi.EvWmKey, Arg0: 0x28})
	if commitID != 0 || !secretAskModal.open {
		t.Fatalf("unarmed Enter on Grant: commit=%d open=%v", commitID, secretAskModal.open)
	}
	secretAskModal.armed = true
	handleSecretAskKey(vi.Event{Kind: vi.EvWmKey, Arg0: 0x28})
	if commitID != 7 || secretAskModal.open {
		t.Fatalf("armed Enter on Grant: commit=%d open=%v", commitID, secretAskModal.open)
	}
}

// M216l: the prompt is modal to the pointer — a press on a button
// answers it, a press anywhere else is swallowed and dismisses nothing.
func TestSecretAskPointerModal(t *testing.T) {
	resetSeatForTest(t)
	defer saveSeatState()()
	var commitID, dropID uint32
	stubSecretWindow(t)
	stubOneAsk(t, "7\t41\t1000\tGOMAIL.ELF\tmail\tnew", &commitID, &dropID)
	forwards := 0
	oldPtr := forwardContentPointer
	forwardContentPointer = func(uint32, uint32, uint8, uint8) int64 { forwards++; return 0 }
	t.Cleanup(func() { forwardContentPointer = oldPtr })
	// Two hosted tabs: cell 0 is not focused, so a rail press under the
	// modal would have something to reach.
	tabs = TabStrip{}
	if !tabs.OpenTab(4, "Edit") || !tabs.OpenTab(5, "Term") {
		t.Fatal("seed tabs")
	}
	prevPtrButtons = 0
	pollSecretAsks()
	grant, deny := secretAskButtonRects(vi.ScanoutWidth, vi.ScanoutHeight)
	gx, gy := uint32(grant.X+grant.W/2), uint32(grant.Y+grant.H/2)
	dx, dy := uint32(deny.X+deny.W/2), uint32(deny.Y+deny.H/2)

	// A Grant press before the arm is swallowed like a 'g'.
	handleWmPointer(ptrEvent(gx, gy, hidBtnLeft))
	handleWmPointer(ptrEvent(gx, gy, 0))
	if commitID != 0 || !secretAskModal.open {
		t.Fatalf("unarmed Grant press: commit=%d open=%v", commitID, secretAskModal.open)
	}

	// Presses on the plate off the buttons, on the rail, and on content
	// reach nothing: no forward, no rail hover, no focus change. Cell 1
	// is the unfocused tab — a leaked rail press would move focus to it.
	cell := railLayout(vi.ScanoutWidth, 2, RailHeight).rect(1)
	x, y, _, _ := secretAskRect(vi.ScanoutWidth, vi.ScanoutHeight)
	for _, p := range [][2]uint32{
		{uint32(x + 10), uint32(y + 15)},                       // plate, off the buttons
		{uint32(cell.X + cell.W/2), uint32(cell.Y + cell.H/2)}, // rail cell 1
		{640, 400}, // hosted content
		{338, 702}, // the PWR chrome button
	} {
		handleWmPointer(ptrEvent(p[0], p[1], hidBtnLeft))
		handleWmPointer(ptrEvent(p[0], p[1], 0))
	}
	if forwards != 0 || commitID != 0 || dropID != 0 {
		t.Fatalf("outside presses escaped: fwd=%d commit=%d drop=%d", forwards, commitID, dropID)
	}
	if !secretAskModal.open || powerMenuOpen || railHover != -1 {
		t.Fatalf("outside presses acted: open=%v power=%v hover=%d",
			secretAskModal.open, powerMenuOpen, railHover)
	}
	if id, ok := tabs.Focused(); !ok || id != 4 {
		t.Fatalf("outside press moved focus: %v", id)
	}

	// Armed: a Grant press commits, a Deny press drops.
	secretAskModal.armed = true
	handleWmPointer(ptrEvent(gx, gy, hidBtnLeft))
	handleWmPointer(ptrEvent(gx, gy, 0))
	if commitID != 7 || secretAskModal.open {
		t.Fatalf("Grant press: commit=%d open=%v", commitID, secretAskModal.open)
	}
	pollSecretAsks()
	handleWmPointer(ptrEvent(dx, dy, hidBtnLeft))
	handleWmPointer(ptrEvent(dx, dy, 0))
	if dropID != 7 || secretAskModal.open {
		t.Fatalf("Deny press: drop=%d open=%v", dropID, secretAskModal.open)
	}
}

// M216l: the lock chord is the one seat chord that stays live while the
// ask stands — and the unlock hands the keys back to the prompt's sink,
// not to a hosted window.
func TestSecretAskLockChordAndHandback(t *testing.T) {
	resetSeatForTest(t)
	lockReset(t)
	raised := stubSecretWindow(t)
	old := lockReadFile
	lockReadFile = func(string, int) ([]byte, int64) { return lockTestVault(t), 0 }
	t.Cleanup(func() { lockReadFile = old })
	var commitID, dropID uint32
	stubOneAsk(t, "7\t41\t1000\tGOMAIL.ELF\tmail\tnew", &commitID, &dropID)
	pollSecretAsks()
	askSink := secretAskModal.sink
	if askSink == 0 {
		t.Fatal("ask opened without a sink")
	}

	// A tab-switch chord is swallowed like every other key.
	if !handleSecretAskKey(vi.Event{Kind: vi.EvWmKey, Flags: vi.ModAlt, Arg0: 0x2b}) {
		t.Fatal("alt-tab was not the prompt's to swallow")
	}
	if !secretAskModal.open || commitID != 0 || dropID != 0 {
		t.Fatalf("alt-tab acted: open=%v commit=%d drop=%d",
			secretAskModal.open, commitID, dropID)
	}
	// ctrl+shift+l drops the curtain — the ask keeps standing beneath it.
	lockEv := vi.Event{Kind: vi.EvWmKey, Flags: vi.ModCtrl | vi.ModShift, Arg0: uint32(chords.UsageL)}
	handleSecretAskKey(lockEv)
	if !lockActive {
		t.Fatal("the lock chord did not drop the curtain")
	}
	if !secretAskModal.open {
		t.Fatal("locking answered the prompt")
	}
	// Unlock: the sink, not a hosted window, owns the keys again.
	typeLock(t, string(lockTestPass))
	lockKeyDown(vi.Event{Kind: vi.EvKeyDown, Arg0: 0x28})
	if lockActive {
		t.Fatal("right pass left the curtain up")
	}
	if len(*raised) == 0 || (*raised)[len(*raised)-1] != askSink {
		t.Fatalf("unlock hand-back: raised=%v want last=%d", *raised, askSink)
	}
	if !secretAskModal.open {
		t.Fatal("the unlock answered the prompt")
	}
}

// M216l: the seat outline names the consent like the launcher's strip —
// the prompt carries 'f', both buttons appear, and Grant reads disabled
// until the arm delay passes so the tree never offers a dead control.
func TestSecretAskA11yNodes(t *testing.T) {
	resetSeatForTest(t)
	defer saveA11yState()()
	var commitID, dropID uint32
	stubSecretWindow(t)
	stubOneAsk(t, "7\t41\t1000\tGOMAIL.ELF\tmail\tnew", &commitID, &dropID)
	tabs.OpenTab(3, "Calc")
	pollSecretAsks()

	out := seatA11yOutline()
	// seat, rail, tab, menu, 2 buttons = 6; the rail cell reads 's'.
	if len(out) != 6 {
		t.Fatalf("outline = %+v", out)
	}
	if out[2].Role != appkit.RoleTab || out[2].State != appkit.StateSelected {
		t.Fatalf("rail cell under modal = %+v", out[2])
	}
	if out[3].Role != appkit.RoleMenu ||
		out[3].Name != "GOMAIL.ELF wants to store mail" ||
		out[3].State != appkit.StateFocused|appkit.StateExpanded {
		t.Fatalf("ask node = %+v", out[3])
	}
	if out[4].Role != appkit.RoleButton || out[4].Name != "Grant" ||
		out[4].State != appkit.StateDisabled {
		t.Fatalf("unarmed grant = %+v", out[4])
	}
	if out[5].Role != appkit.RoleButton || out[5].Name != "Deny" ||
		out[5].State != appkit.StateSelected {
		t.Fatalf("default deny = %+v", out[5])
	}
	// Armed + Tab: the grant node loses 'd', gains 's'.
	secretAskModal.armed = true
	secretAskModal.sel = secretBtnGrant
	out = seatA11yOutline()
	if out[4].State != appkit.StateSelected || out[5].State != 0 {
		t.Fatalf("armed outline = %+v %+v", out[4], out[5])
	}
}
