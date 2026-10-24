// Command welcome is GOWELCOME.ELF: the M122b (issue #2571) first-boot
// welcome tour — five core gestures and one-click launchers on a tabapp
// canvas. The seat execs it beside GOSH.ELF on a LIVE first boot (the demo
// choreography owns the strip and would eat it as a second tab; a restored
// session replays the strip the user left, never adds to it). After first
// boot the launcher catalogue keeps it reachable.
//
// tour.go is the pure model: the gesture and launcher tables, the click /
// key -> launcher mapping and the status text. It touches no vi calls, so
// the host tests pin the frame's content and every launch route; main.go
// owns the tabapp shell, the exec seam and the serial markers.
package main

import (
	"strings"

	"virelai/chords"
)

// gesture is one taught row: the spelling painted on the key chip and a
// one-line description of what the move does.
type gesture struct {
	keys string
	text string
}

// seatChord pulls the seat's OWN spelling of a bound action out of the
// global registry (virelai/chords is the normative table hid.go dispatches
// from), matched on the full chord — an action with several spellings
// (cycle-focus rides alt+tab AND ctrl+tab) names the one the tour teaches.
// A re-homed chord then updates the tour for free instead of stranding it
// on a dead spelling — and a missing row yields "", which the tests
// refuse, so a registry edit can never silently empty a chip.
func seatChord(action string, want chords.Chord) string {
	for _, r := range chords.Global {
		if r.Scope == chords.ScopeSeat && r.Action == action && r.Chord == want {
			return strings.ToUpper(r.Chord.String())
		}
	}
	return ""
}

// gestures is the first hour's five moves. The two pointer moves have no
// chord to borrow; the three bound rows spell theirs from the registry.
func gestures() []gesture {
	return []gesture{
		{"CLICK", "focus a tab — the rail up top lists open windows"},
		{seatChord("launcher", chords.Chord{Mods: chords.ModCtrl, Usage: chords.UsageSpace}), "summon the launcher — the whole app catalogue"},
		{seatChord("cycle-focus", chords.Chord{Mods: chords.ModCtrl, Usage: chords.UsageTab}), "cycle the open tabs · +shift steps back"},
		{"DRAG", "drag a rail tab to reorder it"},
		{seatChord("palette", chords.Chord{Mods: chords.ModCtrl | chords.ModShift, Usage: chords.UsageP}), "command palette — find anything"},
	}
}

// launcher is a one-click app start: the button label and the ELF it
// execs. These are the four daily apps the tour hands a newcomer —
// Settings, Files, Store and Terminal are all dock-side catalogue rows.
type launcher struct {
	label string
	bin   string
}

var launchers = []launcher{
	{"Settings", "GOSET.ELF"},
	{"Files", "GOFILES.ELF"},
	{"Store", "GOSTORE.ELF"},
	{"Terminal", "GOTERM.ELF"},
}

// tour is the frame's state: the gesture step the tour is teaching, the
// done latch (walked to the end or dismissed — a restore never restarts a
// finished tour), the launcher hover/press for the pointer, the quit latch
// for Esc/q, and the last launch outcome the status line echoes.
type tour struct {
	step     int    // gesture row the tour is on, 0..len(gestures())-1
	done     bool   // completed or dismissed — the envelope's terminal flag
	hover    int    // launcher index under the pointer; -1 when none
	pressed  int    // launcher index held on a left press; -1 when none
	launched string // last bin the tour successfully started
	failed   string // last bin the exec refused
	quit     bool
}

func newTour() tour {
	return tour{hover: -1, pressed: -1}
}

// next steps the tour one gesture forward; stepping past the last row
// latches done — the tour ran to its end. The latch is sticky: stepping
// back to review an earlier row does not un-complete a finished tour.
func (t *tour) next() bool {
	if t.step < len(gestures())-1 {
		t.step++
		return true
	}
	if !t.done {
		t.done = true
		return true
	}
	return false
}

// prev steps the tour one gesture back. The done latch survives — once
// the tour reached its end it stays complete.
func (t *tour) prev() bool {
	if t.step == 0 {
		return false
	}
	t.step--
	return true
}

// launcherAt maps a canvas point to a launcher index over the laid-out
// button rects, or -1. Kept on the model so the click path and the tests
// share one mapping — a rect drift cannot split the two.
func (t *tour) launcherAt(rects []rect, x, y int) int {
	for i := range launchers {
		if i < len(rects) && rects[i].contains(x, y) {
			return i
		}
	}
	return -1
}

// gestureAt maps a canvas point to a gesture row over the laid-out row
// strips, or -1 — a click on a row jumps the tour to that step. Kept on
// the model so the click path and the tests share one mapping, the same
// rule launcherAt keeps.
func (t *tour) gestureAt(rects []rect, x, y int) int {
	for i := range rects {
		if rects[i].contains(x, y) {
			return i
		}
	}
	return -1
}

// digitLaunch maps the keyboard spelling of a launch: '1'..'4' fire the
// same exec as a click on that button. Out-of-range is no launch.
func digitLaunch(ch uint32) int {
	if ch >= '1' && ch < '1'+uint32(len(launchers)) {
		return int(ch - '1')
	}
	return -1
}

// footText is the status line: the launch outcome when one ran, else the
// tour's progress — done names the finished state, the running tour names
// its step.
func (t *tour) footText() string {
	if t.failed != "" {
		return "could not start " + t.failed
	}
	if t.launched != "" {
		return "started " + t.launched + " — find its tab on the rail"
	}
	if t.done {
		return "tour complete — esc closes · reopen from the launcher"
	}
	n := t.step
	if n < 0 {
		n = 0
	}
	if n >= len(gestures()) {
		n = len(gestures()) - 1
	}
	return "step " + string(rune('1'+n)) + " of 5 · arrows walk · 1-4 launch · esc closes"
}

// rect is a canvas-space box; contains is the hit test the pointer path
// and the tests share.
type rect struct{ X, Y, W, H int }

func (r rect) contains(x, y int) bool {
	return x >= r.X && x < r.X+r.W && y >= r.Y && y < r.Y+r.H
}

// --- the M195b (#3307) appver-1 state envelope -----------------------------
//
// The tour's half of the M125a (#2586) seat-mediated save/restore
// protocol — the same shape NOTE's state.go and GOCALC's envelope keep:
// the seat stores the blob verbatim on the tab's session row (kind-20
// save, kind-21 restore) and hands it back on a reopen, so a seat
// save/restore mid-tour resumes on the same step instead of restarting
// the tour from gesture one. The seat owns the lifecycle; the tour owns
// the format:
//
//	u8 fmtVer (=stateFmtVer) — the blob's own version byte; also the
//	   Config's StateVersion, so the envelope's appver and the payload
//	   agree (a stale serialization is refused twice over)
//	u8 flags — bit 0: done — the tour walked to its last gesture or was
//	   dismissed; a restored done flag never re-runs a finished tour
//	u8 step  — the gesture row the tour was teaching
//	u8 reserved (=0)
//
// Decode is total: a wrong length, a foreign fmtVer, unknown flag bits or
// a nonzero reserved byte refuses the WHOLE blob — a torn handoff can
// never land half-applied. The step value itself clamps into range, the
// GOTOP rule: a stale-but-well-formed blob degrades to the nearest live
// row rather than refusing the session.

const (
	stateFmtVer = 1
	stateAppVer = 1

	flagDone = 1 << 0
)

// tourState is one decoded session blob.
type tourState struct {
	step int
	done bool
}

// encode serializes the tour's durable state. Bounded by construction:
// four bytes, forever under the seat's 64 KiB cap.
func (t *tour) encode() []byte {
	var flags byte
	if t.done {
		flags |= flagDone
	}
	return []byte{stateFmtVer, flags, byte(t.step), 0}
}

// decodeTourState is encode's strict inverse. ok=false means stale or
// foreign — refused whole, never half-applied; the caller names the
// refusal. The step byte clamps into the live gesture table's range: a
// shorter future tour degrades to its last row instead of a refusal.
func decodeTourState(b []byte) (tourState, bool) {
	var st tourState
	if len(b) != 4 || b[0] != stateFmtVer {
		return st, false
	}
	if b[1]&^flagDone != 0 || b[3] != 0 {
		return st, false
	}
	st.done = b[1]&flagDone != 0
	st.step = int(b[2])
	if st.step >= len(gestures()) {
		st.step = len(gestures()) - 1
	}
	return st, true
}
