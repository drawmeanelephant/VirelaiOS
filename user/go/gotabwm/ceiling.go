// GOTABWM.ELF — M216m (issue #3952): the kernel's user-window ceiling.
//
// The kernel has eight user windows (ids 2..9) and the seat holds two of
// them for its whole life: the client-death probe runWindowPhase leaves
// open, and the Apps/PWR chrome window. Six are left for apps. Observed on
// the card before this file existed: the seventh app's WinOpen answers
// ENOSPC and the app exits status 1 with nothing on the seat; the launcher
// chord stalls the seat for 240 ticks; the lock refuses `no_sink`; the
// power and clipboard menus open without key focus.
//
// The design, in three parts:
//
//   - Launches are refused BEFORE exec. Every seat launch rides the execApp
//     seam, so the guard wraps the seam once (init, below) instead of
//     nineteen call sites. It counts the kernel's live user windows (wmctl
//     cmd 14 answers for any owner), drops the seat's transient modal
//     sinks (they close with the launch), adds the launches it issued that
//     have not bound a window yet, and refuses at the ceiling with the
//     `launch refused` marker and the `windows: limit 8` notice. The
//     notice's click is Retry: close something, click, and it launches.
//   - A launch the seat did not issue (a shell exec) is invisible until it
//     dies. While the kernel is full the seat watches the process table: a
//     catalog app that exits nonzero without ever holding a bound window
//     gets the `launch windowless` marker and the same notice. The cause
//     is INFERRED from "full + died unbound" — the app's errno never
//     reaches the seat. The process is left alone because there is
//     nothing to end: it has already exited on its own.
//   - Seat modals stay openable through ONE long-lived window. The death
//     probe is that window already, so the reserve costs apps no slot.
//     When the sink seam's WinOpen answers ENOSPC the reserve is lent:
//     moved to the modal's rect, shown and focused. The close seam parks
//     it (1x1, hidden) instead of closing it. One borrower at a time —
//     except the lock, which needs only a seat-owned window holding kernel
//     focus and so shares whatever the seat owns (lockcurtain.go).
package main

import "virelai/vi"

const (
	// The kernel's user-window id range (driving_award.zig
	// user_window_id_base / user_windows_max). Out of scope to raise.
	userWinBase = 2
	userWinMax  = 8

	// windowLimitText is the notice's text and the refusal's error text.
	windowLimitText = "windows: limit 8"

	// MarkerLaunchRefused names a launch the seat refused before exec.
	MarkerLaunchRefused = "gotabwm: launch refused "
	// MarkerLaunchWindowless names a catalog app that died at the ceiling
	// without ever holding a bound window.
	MarkerLaunchWindowless = "gotabwm: launch windowless "
	// MarkerSinkReserve names the reserve window's lend and park.
	MarkerSinkReserve = "gotabwm: sink reserve "

	// launchPendingTicks bounds how long an issued launch holds a slot in
	// the count without binding a window. An app that opens a window but
	// never binds is counted twice for this long — conservative, bounded.
	launchPendingTicks = 30
)

// errWindowLimit is the guard's refusal. seatRefusalErr skips it: the
// guard has already given the refusal its face.
var errWindowLimit error = windowLimitErr{}

type windowLimitErr struct{}

func (windowLimitErr) Error() string { return windowLimitText }

// openSinkWindow and listProcs are the kernel seams under the ceiling
// logic (the execApp pattern): vi.WinOpen / vi.Procs on the guest.
var (
	openSinkWindow = vi.WinOpen
	listProcs      = vi.Procs
)

// The seams this file wraps are declared by the files that own them
// (execApp in hid.go, closeWin in interop.go). Package-level initializers
// run before init, so both hold the raw syscalls here.
func init() {
	execApp = ceilingExec(execApp)
	closeWin = reserveClose(closeWin)
}

// --- the count ----------------------------------------------------------

// userWindowsLive is the kernel's truth: how many of the eight user-window
// ids answer. Off the guest every read fails and the count is 0, so the
// guard is open in host tests that do not stub the seam.
func userWindowsLive() int {
	n := 0
	for id := userWinBase; id < userWinBase+userWinMax; id++ {
		if _, ok := readWindowName(uint8(id)); ok {
			n++
		}
	}
	return n
}

// seatSinks holds the ids of modal sinks the seam opened as real windows.
// They are in the kernel's count but not in the app count: a sink closes
// with the gesture that launches.
var seatSinks [userWinMax]uint32

func noteSeatSink(id uint32) {
	for i := range seatSinks {
		if seatSinks[i] == 0 || seatSinks[i] == id {
			seatSinks[i] = id
			return
		}
	}
}

func dropSeatSink(id uint32) {
	for i := range seatSinks {
		if seatSinks[i] == id {
			seatSinks[i] = 0
		}
	}
}

// seatSinksLive counts the tracked sinks that still exist. queryWindow is
// owner-only, so a sink the kernel reaped — or whose id passed to an app —
// stops answering and is dropped.
func seatSinksLive() int {
	n := 0
	for i, id := range seatSinks {
		if id == 0 {
			continue
		}
		if _, r := queryWindow(int(id)); r < 0 {
			seatSinks[i] = 0
			continue
		}
		n++
	}
	return n
}

// launchPending is the launches the guard let through that have not bound
// a window yet. Session re-exec issues several execs before any window
// exists; without this the kernel count would admit all of them.
var (
	launchPending [userWinMax]struct {
		pid  uint8
		tick uint64
	}
	launchPendingN int
)

func pidBound(pid uint8) bool {
	for id := userWinBase; id < userWinBase+userWinMax; id++ {
		if rpcBindings[id].pid == pid {
			return true
		}
	}
	return false
}

// pendingLaunches prunes and counts: a launch stops pending when its pid
// binds a window, dies, or outlives launchPendingTicks.
func pendingLaunches() int {
	for i := 0; i < launchPendingN; {
		p := launchPending[i]
		if !pidBound(p.pid) && procsAlive(p.pid) && seatTick-p.tick < launchPendingTicks {
			i++
			continue
		}
		launchPendingN--
		launchPending[i] = launchPending[launchPendingN]
	}
	return launchPendingN
}

// appSlotsUsed is the count the guard refuses on.
func appSlotsUsed() int {
	return userWindowsLive() - seatSinksLive() + pendingLaunches()
}

// --- the launch guard ---------------------------------------------------

// ceilingExec wraps the exec seam with the refusal. The refusal is an
// error like any other exec failure, so every call site's own failure
// path (drop the row, keep the launcher open) runs unchanged.
func ceilingExec(inner func(string, ...string) (int64, error)) func(string, ...string) (int64, error) {
	return func(bin string, args ...string) (int64, error) {
		if appSlotsUsed() >= userWinMax {
			vi.ConsoleLine(MarkerLaunchRefused + bin + " " + windowLimitText)
			noteWindowLimit(bin)
			return 0, errWindowLimit
		}
		pid, err := inner(bin, args...)
		if err == nil && pid > 0 && pid <= 255 && launchPendingN < len(launchPending) {
			launchPending[launchPendingN].pid = uint8(pid)
			launchPending[launchPendingN].tick = seatTick
			launchPendingN++
		}
		return pid, err
	}
}

// The notice is one toast per NotifyTicks window, however many launches
// were refused inside it (a session re-exec past the ceiling is a storm).
var (
	limitNoticed    bool
	limitNoticeTick uint64
)

func noteWindowLimit(bin string) {
	if limitNoticed && seatTick-limitNoticeTick < NotifyTicks {
		return
	}
	limitNoticed, limitNoticeTick = true, seatTick
	if bin == "" {
		notifyPush(0, windowLimitText, compositeClock)
		return
	}
	notifyPushAction(0, windowLimitText, compositeClock, notifyActionRetry, bin, 0)
}

// --- the launch the seat did not issue ----------------------------------

// The exit watch. ceilSeen marks exited rows already accounted for;
// ceilBound marks pids that held a bound window when last seen alive (a
// crash, not a windowless launch). ceilArmed is false until the first
// pass of a stretch at the ceiling, which only takes the baseline — every
// row that exited before the kernel filled is history, not a launch.
var (
	ceilSeen  [256]bool
	ceilBound [256]bool
	ceilArmed bool
	ceilRan   bool
	ceilTick  uint64
)

// windowCeilingTick runs on the composite cadence, at most once per seat
// tick, and does nothing unless the kernel is full. A pid reused for a
// launch that dies before the seat sees it alive is missed: the row reads
// as the exit already accounted for.
func windowCeilingTick() {
	if ceilRan && seatTick == ceilTick {
		return
	}
	ceilRan, ceilTick = true, seatTick
	if userWindowsLive() < userWinMax {
		ceilArmed = false
		return
	}
	var rows [64]vi.ProcRow
	n, r := listProcs(rows[:])
	if r < 0 {
		return
	}
	baseline := !ceilArmed
	ceilArmed = true
	for i := 0; i < n; i++ {
		pid := uint8(rows[i].PID)
		if rows[i].State != vi.ProcExited {
			ceilSeen[pid] = false
			ceilBound[pid] = pidBound(pid)
			continue
		}
		if ceilSeen[pid] {
			continue
		}
		ceilSeen[pid] = true
		name := rows[i].Name()
		if baseline || ceilBound[pid] || rows[i].ExitStatus == 0 || !catalogHasBin(name) {
			continue
		}
		vi.ConsoleLine(MarkerLaunchWindowless + name +
			" pid=" + vi.Itoa64(int64(pid)) +
			" status=" + vi.Itoa64(int64(rows[i].ExitStatus)) +
			" " + windowLimitText)
		noteWindowLimit(name)
	}
}

// catalogHasBin reports whether the manifest launches bin — the test that
// keeps a failed shell command from reading as a failed app launch. The
// manifest is read at the moment of a candidate exit, which is rare.
func catalogHasBin(bin string) bool {
	body, r := readAppsFile(appsPath, appsMaxBytes)
	if r < 0 || body == nil {
		return false
	}
	for _, e := range parseAppsTXT(string(body)) {
		if e.Bin == bin {
			return true
		}
	}
	return false
}

// --- the reserve window -------------------------------------------------

// reserveWin is the seat's long-lived window (the client-death probe).
// lent is true while a modal holds it as its sink.
var reserveWin struct {
	id   uint32
	lent bool
}

// openSeatSink is the modal sink seam's guest value: a fresh window while
// the kernel has one, the reserve when it answers ENOSPC.
func openSeatSink(x, y, w, h uint32) (int, int64) {
	id, r := openSinkWindow(x, y, w, h)
	if r >= 0 {
		noteSeatSink(uint32(id))
		return id, r
	}
	if r != -vi.ErrENOSPC {
		return id, r
	}
	return lendReserve(x, y, w, h)
}

// lendReserve hands the reserve to one modal. Every refusal answers
// ENOSPC — the caller's question was "is there a window", and there is not.
func lendReserve(x, y, w, h uint32) (int, int64) {
	id := reserveWin.id
	if id == 0 || reserveWin.lent {
		return -1, -vi.ErrENOSPC
	}
	if _, r := queryWindow(int(id)); r < 0 {
		reserveWin.id = 0
		return -1, -vi.ErrENOSPC
	}
	if setWindowRect(id, x, y, w, h) != 0 {
		return -1, -vi.ErrENOSPC
	}
	_ = wmctlSetVisible(id, true)
	if focusRaise(id) != 0 {
		parkReserve()
		return -1, -vi.ErrENOSPC
	}
	// The window's pixels are new: a cache keyed on this id is stale.
	if menuPixels.id == int(id) {
		menuPixels = windowPixels{}
	}
	if powerPixels.id == int(id) {
		powerPixels = windowPixels{}
	}
	reserveWin.lent = true
	vi.ConsoleLine(MarkerSinkReserve + "lend id=" + vi.Itoa64(int64(id)) +
		" w=" + vi.Itoa64(int64(w)) + " h=" + vi.Itoa64(int64(h)))
	return int(id), int64(id)
}

// setWindowRect is the WM-seam rect call (the wmctlSetVisible pattern).
var setWindowRect = vi.WmctlSetWindowRect

// parkReserve takes the reserve off the desk: one hidden pixel in the
// corner the lock's own sink uses, which also returns the panel-sized
// back-buffer to the pool.
func parkReserve() {
	id := reserveWin.id
	_ = setWindowRect(id, uint32(vi.ScanoutWidth-1), uint32(vi.ScanoutHeight-1), 1, 1)
	_ = wmctlSetVisible(id, false)
}

// reserveClose wraps the close seam: closing the lent reserve parks it.
// A real close moves kernel focus to the next window; the reserve is not
// closed, so focus is handed to the focused tab here and the modal's own
// restore, when it has one, runs after and wins.
func reserveClose(inner func(uint32) int64) func(uint32) int64 {
	return func(id uint32) int64 {
		if id == 0 || id != reserveWin.id || !reserveWin.lent {
			dropSeatSink(id)
			return inner(id)
		}
		reserveWin.lent = false
		if tab, ok := tabs.Focused(); ok && tab < sessionIDBase {
			_ = focusRaise(tab)
		}
		parkReserve()
		vi.ConsoleLine(MarkerSinkReserve + "park id=" + vi.Itoa64(int64(id)))
		return 0
	}
}

// seatOwnedWindow is a live window the seat owns, for a borrower that
// needs kernel focus and nothing else: the reserve whether or not it is
// lent, else the chrome window. 0 when the seat owns none.
func seatOwnedWindow() uint32 {
	for _, id := range [2]uint32{reserveWin.id, uint32(buttonPixels.id)} {
		if id == 0 {
			continue
		}
		if _, r := queryWindow(int(id)); r >= 0 {
			return id
		}
	}
	return 0
}
