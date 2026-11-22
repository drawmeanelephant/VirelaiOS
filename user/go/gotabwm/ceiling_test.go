package main

// ceiling_test.go — M216m (#3952): the user-window ceiling's class-A legs.
// The kernel is a map of live window ids behind the seams the ceiling
// logic already rides (readWindowName, queryWindow, the sink open/close,
// the WM rect/visible/focus calls and the process table).

import (
	"errors"
	"testing"

	"virelai/vi"
)

// ceilKernel is the fake: which user-window ids answer, which of them the
// seat owns, and what the seat did to the windows it owns.
type ceilKernel struct {
	live    map[uint32]bool
	own     map[uint32]bool
	rects   map[uint32][4]uint32
	visible map[uint32]bool
	focused uint32
	closed  []uint32
	procs   []vi.ProcRow
}

func (k *ceilKernel) fill(ids ...uint32) {
	for _, id := range ids {
		k.live[id] = true
	}
}

// ceilSetup swaps every seam and piece of state the ceiling logic touches
// for a fresh fake, and restores the lot afterwards.
func ceilSetup(t *testing.T) *ceilKernel {
	t.Helper()
	resetNotify(t)
	k := &ceilKernel{live: map[uint32]bool{}, own: map[uint32]bool{},
		rects: map[uint32][4]uint32{}, visible: map[uint32]bool{}}
	oName, oQuery, oAlive, oProcs := readWindowName, queryWindow, procsAlive, listProcs
	oOpen, oRect, oVis, oFocus := openSinkWindow, setWindowRect, wmctlSetVisible, focusRaise
	oApps := readAppsFile
	sBinds, sSinks, sPending, sPendingN := rpcBindings, seatSinks, launchPending, launchPendingN
	sNoticed, sNoticeTick := limitNoticed, limitNoticeTick
	sSeen, sBound, sArmed, sRan, sCeilTick := ceilSeen, ceilBound, ceilArmed, ceilRan, ceilTick
	sReserve, sTabs, sTick := reserveWin, tabs, seatTick
	sMenu, sPower, sButton := menuPixels, powerPixels, buttonPixels
	t.Cleanup(func() {
		readWindowName, queryWindow, procsAlive, listProcs = oName, oQuery, oAlive, oProcs
		openSinkWindow, setWindowRect, wmctlSetVisible, focusRaise = oOpen, oRect, oVis, oFocus
		readAppsFile = oApps
		rpcBindings, seatSinks, launchPending, launchPendingN = sBinds, sSinks, sPending, sPendingN
		limitNoticed, limitNoticeTick = sNoticed, sNoticeTick
		ceilSeen, ceilBound, ceilArmed, ceilRan, ceilTick = sSeen, sBound, sArmed, sRan, sCeilTick
		reserveWin, tabs, seatTick = sReserve, sTabs, sTick
		menuPixels, powerPixels, buttonPixels = sMenu, sPower, sButton
	})
	rpcBindings = [256]rpcBinding{}
	seatSinks = [userWinMax]uint32{}
	launchPendingN = 0
	limitNoticed, ceilArmed, ceilRan = false, false, false
	ceilSeen, ceilBound = [256]bool{}, [256]bool{}
	reserveWin.id, reserveWin.lent = 0, false
	tabs = TabStrip{}
	menuPixels, powerPixels, buttonPixels = windowPixels{}, windowPixels{}, windowPixels{}
	seatTick = 100

	readWindowName = func(id uint8) (string, bool) { return "w", k.live[uint32(id)] }
	queryWindow = func(id int) ([8]uint32, int64) {
		if !k.live[uint32(id)] || !k.own[uint32(id)] {
			return [8]uint32{}, -vi.ErrEINVAL
		}
		var st [8]uint32
		if k.focused == uint32(id) {
			st[5] = 1
		}
		return st, 0
	}
	procsAlive = func(pid uint8) bool {
		for _, r := range k.procs {
			if uint8(r.PID) == pid {
				return r.State != vi.ProcExited
			}
		}
		return false
	}
	listProcs = func(rows []vi.ProcRow) (int, int64) {
		return copy(rows, k.procs), 0
	}
	openSinkWindow = func(x, y, w, h uint32) (int, int64) {
		for id := uint32(userWinBase); id < userWinBase+userWinMax; id++ {
			if !k.live[id] {
				k.live[id], k.own[id] = true, true
				k.rects[id] = [4]uint32{x, y, w, h}
				k.focused = id
				return int(id), int64(id)
			}
		}
		return -1, -vi.ErrENOSPC
	}
	setWindowRect = func(id, x, y, w, h uint32) int64 {
		if !k.live[id] {
			return -vi.ErrEINVAL
		}
		k.rects[id] = [4]uint32{x, y, w, h}
		return 0
	}
	wmctlSetVisible = func(id uint32, v bool) int64 {
		k.visible[id] = v
		return 0
	}
	focusRaise = func(id uint32) int64 {
		if !k.live[id] {
			return -vi.ErrEINVAL
		}
		k.focused = id
		return 0
	}
	readAppsFile = func(string, int) ([]byte, int64) {
		return []byte("GOCALC.ELF | Calculator | c\nNOTE.ELF | Notepad | n\n"), 0
	}
	return k
}

// seatAtCeiling is the observed live state: the seat's reserve (2) and
// chrome window (3), and six apps on 4..9.
func seatAtCeiling(k *ceilKernel) {
	k.fill(2, 3, 4, 5, 6, 7, 8, 9)
	k.own[2], k.own[3] = true, true
	reserveWin.id = 2
	buttonPixels.id = 3
	tabs.OpenTab(9, "Notepad")
}

// ceilClose is the close seam as init builds it, over a recording inner.
func ceilClose(k *ceilKernel) func(uint32) int64 {
	return reserveClose(func(id uint32) int64 {
		k.closed = append(k.closed, id)
		delete(k.live, id)
		return 0
	})
}

func procRow(pid uint64, name string, state, status uint64) vi.ProcRow {
	r := vi.ProcRow{PID: pid, State: state, ExitStatus: status}
	copy(r.NameBuf[:], name)
	return r
}

func TestWindowLimitTextNamesTheKernelCap(t *testing.T) {
	if want := "windows: limit " + vi.Itoa64(userWinMax); windowLimitText != want {
		t.Fatalf("notice %q does not name the cap, want %q", windowLimitText, want)
	}
}

func TestAppSlotsCountWindowsPendingAndNotSinks(t *testing.T) {
	k := ceilSetup(t)
	k.fill(2, 3, 4, 5)
	if got := appSlotsUsed(); got != 4 {
		t.Fatalf("four live windows count %d", got)
	}
	// A modal sink is a kernel window but not an app slot.
	id, r := openSeatSink(0, 0, 10, 10)
	if r < 0 || userWindowsLive() != 5 {
		t.Fatalf("sink open id=%d r=%d live=%d", id, r, userWindowsLive())
	}
	if got := appSlotsUsed(); got != 4 {
		t.Fatalf("an open sink took an app slot: %d", got)
	}
	// A sink the kernel reaped stops being discounted.
	delete(k.own, uint32(id))
	if got := appSlotsUsed(); got != 5 {
		t.Fatalf("a window the seat no longer owns still discounted: %d", got)
	}
}

func TestLaunchGuardRefusesAtTheCeilingBeforeExec(t *testing.T) {
	k := ceilSetup(t)
	seatAtCeiling(k)
	execs := 0
	launchApp := ceilingExec(func(string, ...string) (int64, error) { execs++; return 9, nil })

	pid, err := launchApp("GOCALC.ELF")
	if err != errWindowLimit || pid != 0 || execs != 0 {
		t.Fatalf("launch at the ceiling: pid=%d err=%v execs=%d", pid, err, execs)
	}
	if n := notifyCount(); n != 1 || notifyQueue[0].text != windowLimitText ||
		notifyQueue[0].action != notifyActionRetry || notifyQueue[0].snap != "GOCALC.ELF" {
		t.Fatalf("refusal notice = %+v", notifyQueue)
	}
	// A second refusal inside the notice's lifetime is the same report.
	if _, err := launchApp("NOTE.ELF"); err != errWindowLimit || notifyCount() != 1 {
		t.Fatalf("storm: err=%v toasts=%d", err, notifyCount())
	}
	// A window closes: the retry goes through.
	delete(k.live, 9)
	if pid, err := launchApp("GOCALC.ELF"); err != nil || pid != 9 || execs != 1 {
		t.Fatalf("launch below the ceiling: pid=%d err=%v execs=%d", pid, err, execs)
	}
}

func TestLaunchGuardCountsLaunchesThatHaveNoWindowYet(t *testing.T) {
	k := ceilSetup(t)
	k.fill(2, 3, 4, 5, 6, 7) // two slots free
	next := int64(20)
	launchApp := ceilingExec(func(string, ...string) (int64, error) { next++; return next, nil })
	alive := func(pids ...uint64) {
		k.procs = nil
		for _, p := range pids {
			k.procs = append(k.procs, procRow(p, "GOCALC.ELF", vi.ProcRunning, 0))
		}
	}

	// A session re-exec issues its execs before any window exists.
	alive(21, 22)
	if _, err := launchApp("A.ELF"); err != nil {
		t.Fatal(err)
	}
	if _, err := launchApp("B.ELF"); err != nil {
		t.Fatal(err)
	}
	if _, err := launchApp("C.ELF"); err != errWindowLimit {
		t.Fatalf("third launch into two slots: %v", err)
	}
	// One of them binds its window: it is a window now, not a pending
	// launch, and the count is unchanged.
	k.fill(8)
	rpcBindings[8].pid = 21
	if _, err := launchApp("C.ELF"); err != errWindowLimit {
		t.Fatalf("bound launch counted twice or not at all: %v", err)
	}
	// The other dies without a window: its slot is free again.
	alive(21)
	if _, err := launchApp("C.ELF"); err != nil {
		t.Fatalf("a dead launch kept its slot: %v", err)
	}
	// A launch that neither binds nor dies gives the slot back in time.
	alive(21, 23)
	if _, err := launchApp("D.ELF"); err != errWindowLimit {
		t.Fatalf("pending launch not counted: %v", err)
	}
	seatTick += launchPendingTicks
	if _, err := launchApp("D.ELF"); err != nil {
		t.Fatalf("a launch pending past its bound still held a slot: %v", err)
	}
}

func TestLaunchGuardPassesAnExecFailureThrough(t *testing.T) {
	ceilSetup(t)
	boom := errors.New("ENOENT")
	launchApp := ceilingExec(func(string, ...string) (int64, error) { return 0, boom })
	if _, err := launchApp("MISSING.ELF"); err != boom || launchPendingN != 0 {
		t.Fatalf("err=%v pending=%d", err, launchPendingN)
	}
}

func TestLaunchFromAModalWithItsOwnSinkIsNotRefused(t *testing.T) {
	k := ceilSetup(t)
	k.fill(2, 3, 4, 5, 6, 7, 8) // five apps: one slot left
	if _, r := openSeatSink(0, 0, 10, 10); r < 0 || userWindowsLive() != userWinMax {
		t.Fatal("the launcher's sink should take the last window")
	}
	launchApp := ceilingExec(func(string, ...string) (int64, error) { return 30, nil })
	if _, err := launchApp("GOCALC.ELF"); err != nil {
		t.Fatalf("the launcher's own sink refused its launch: %v", err)
	}
}

func TestSeatRefusalSkipsTheWindowLimit(t *testing.T) {
	ceilSetup(t)
	resetRefusal(t)
	seatRefusalErr(errWindowLimit, "exec GOCALC.ELF", "GOCALC.ELF")
	if n := notifyCount(); n != 0 {
		t.Fatalf("the guard's refusal was toasted twice: %d", n)
	}
}

func TestWindowlessLaunchAtTheCeilingIsNamed(t *testing.T) {
	k := ceilSetup(t)
	seatAtCeiling(k)
	rpcBindings[4].pid = 2
	k.procs = []vi.ProcRow{
		procRow(1, "GOTABWM.ELF", vi.ProcRunning, 0),
		procRow(2, "GOCALC.ELF", vi.ProcRunning, 0),
		procRow(5, "NOTE.ELF", vi.ProcExited, 1), // died before the kernel filled
	}
	windowCeilingTick() // the baseline pass
	if notifyCount() != 0 {
		t.Fatal("an exit from before the ceiling was reported")
	}

	// A shell exec of a catalog app: ENOSPC from WinOpen, exit status 1.
	k.procs = append(k.procs, procRow(8, "GOCALC.ELF", vi.ProcExited, 1))
	windowCeilingTick()
	if notifyCount() != 0 {
		t.Fatal("the watch ran twice in one seat tick")
	}
	seatTick++
	windowCeilingTick()
	if n := notifyCount(); n != 1 || notifyQueue[0].text != windowLimitText {
		t.Fatalf("windowless launch notice = %+v", notifyQueue)
	}
	// It is reported once.
	seatTick += NotifyTicks
	windowCeilingTick()
	if n := notifyCount(); n != 1 {
		t.Fatalf("the same exit was reported again: %d", n)
	}
}

func TestExitWatchIgnoresWhatIsNotAWindowlessLaunch(t *testing.T) {
	k := ceilSetup(t)
	seatAtCeiling(k)
	rpcBindings[4].pid = 2
	k.procs = []vi.ProcRow{procRow(2, "GOCALC.ELF", vi.ProcRunning, 0)}
	windowCeilingTick()

	k.procs = []vi.ProcRow{
		procRow(2, "GOCALC.ELF", vi.ProcExited, 139), // held a bound window: a crash
		procRow(6, "LS.ELF", vi.ProcExited, 1),       // not a catalog app
		procRow(7, "NOTE.ELF", vi.ProcExited, 0),     // a clean exit
	}
	seatTick++
	windowCeilingTick()
	if n := notifyCount(); n != 0 {
		t.Fatalf("reported %d exits that are not windowless launches", n)
	}

	// Below the ceiling the watch is off, and the next stretch at the
	// ceiling starts from a fresh baseline.
	delete(k.live, 9)
	k.procs = append(k.procs, procRow(8, "GOCALC.ELF", vi.ProcExited, 1))
	seatTick++
	windowCeilingTick()
	k.fill(9)
	seatTick++
	windowCeilingTick()
	if n := notifyCount(); n != 0 {
		t.Fatalf("an exit from below the ceiling was reported: %d", n)
	}
}

func TestSinkSeamLendsTheReserveOnlyAtTheCeiling(t *testing.T) {
	k := ceilSetup(t)
	k.fill(2, 3, 4)
	k.own[2] = true
	reserveWin.id = 2
	closeSink := ceilClose(k)

	// A free window: a real sink, opened and really closed.
	id, r := openSeatSink(10, 20, 30, 40)
	if r < 0 || id == 2 || reserveWin.lent {
		t.Fatalf("below the ceiling: id=%d r=%d lent=%v", id, r, reserveWin.lent)
	}
	if closeSink(uint32(id)) != 0 || len(k.closed) != 1 || k.closed[0] != uint32(id) {
		t.Fatalf("a real sink was not really closed: %v", k.closed)
	}

	// No free window: the reserve, at the modal's rect, shown and focused.
	seatAtCeiling(k)
	menuPixels = windowPixels{id: 2, w: 30, h: 40, pixels: make([]uint32, 1200)}
	id, r = openSeatSink(10, 20, 30, 40)
	if r < 0 || id != 2 || !reserveWin.lent {
		t.Fatalf("at the ceiling: id=%d r=%d lent=%v", id, r, reserveWin.lent)
	}
	if k.rects[2] != [4]uint32{10, 20, 30, 40} || !k.visible[2] || k.focused != 2 {
		t.Fatalf("lent reserve rect=%v visible=%v focused=%d", k.rects[2], k.visible[2], k.focused)
	}
	if menuPixels.id != 0 {
		t.Fatal("a pixel cache keyed on the reserve survived the lend")
	}
	// One borrower at a time.
	if _, r := openSeatSink(0, 0, 5, 5); r != -vi.ErrENOSPC {
		t.Fatalf("second borrower r=%d", r)
	}

	// Close parks it: hidden, one pixel, still open, focus on the tab.
	if closeSink(2) != 0 || len(k.closed) != 1 || !k.live[2] {
		t.Fatalf("the reserve was really closed: %v", k.closed)
	}
	if reserveWin.lent || k.visible[2] || k.rects[2][2] != 1 || k.rects[2][3] != 1 || k.focused != 9 {
		t.Fatalf("parked reserve lent=%v visible=%v rect=%v focused=%d",
			reserveWin.lent, k.visible[2], k.rects[2], k.focused)
	}
	// And it lends again.
	if id, r := openSeatSink(1, 2, 3, 4); r < 0 || id != 2 {
		t.Fatalf("second lend id=%d r=%d", id, r)
	}
}

func TestSinkSeamKeepsOtherRefusalsAndADeadReserve(t *testing.T) {
	k := ceilSetup(t)
	seatAtCeiling(k)
	rc := -vi.ErrEINVAL
	openSinkWindow = func(uint32, uint32, uint32, uint32) (int, int64) { return -1, rc }
	if _, r := openSeatSink(0, 0, 1, 1); r != -vi.ErrEINVAL || reserveWin.lent {
		t.Fatalf("EINVAL became a lend: r=%d", r)
	}
	rc = -vi.ErrENOSPC
	delete(k.own, 2) // the reserve no longer answers the seat
	if _, r := openSeatSink(0, 0, 1, 1); r != -vi.ErrENOSPC || reserveWin.id != 0 {
		t.Fatalf("dead reserve: r=%d id=%d", r, reserveWin.id)
	}
}
