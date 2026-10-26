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

// --- the #3769 "try it" tour -------------------------------------------
//
// Try mode (the bare exec's default) turns each gesture row into a step
// the user performs: the tour arms a row and waits for its evidence —
// the event the app can honestly observe — before the cursor moves on.
// `GOWELCOME.ELF --read` keeps the read-only walk the scripted path
// types, and the envelope's flagTry bit carries the mode through a seat
// close/reopen so a restored tour resumes in the same flow.
//
// What a step can observe is the honest limit: a seat modal (launcher,
// palette, a rail menu) takes kernel focus exactly the way a tab switch
// does, so every "drive the seat" row waits on WIN_BLUR and reports
// just that — the marker names the OBSERVED event, never the action it
// stood in for. The rail's drag is invisible to the app outright (the
// strip is seat chrome; no event follows a reorder), so its row is
// skip-only by declaration, not by a guess. The launcher row's own
// press is the app's most certain evidence of all.
type tryEvidence uint8

const (
	evNone  tryEvidence = iota // no honest observable — skip is the only advance
	evBlur                     // WIN_BLUR: focus left the window
	evPress                    // a launcher button's own press edge
)

// stepState is one try-step's outcome.
type stepState uint8

const (
	stepPending stepState = iota
	stepDone               // its evidence arrived
	stepSkipped            // the user passed on it — named, never faked
)

// tryStep is one "do it yourself" row: the ask painted while it is
// armed, the evidence class that resolves it, and the painted row it
// owns — 0..4 name the gesture rows, 5 the launcher line.
type tryStep struct {
	prompt string
	want   tryEvidence
	row    int
}

// trySteps is the try tour's execution order — the step cursor runs
// over this table, opening on the launcher press (the one piece of
// evidence the app sees with complete certainty) and closing on the
// cycle. The bound rows' spellings ride gestures()' registry lookups
// so a re-homed chord updates the ask for free.
func trySteps() []tryStep {
	g := gestures()
	return []tryStep{
		{"click a button below — the apps live in their own tabs", evPress, 5},
		{"press " + g[1].keys + " — the launcher takes the keys", evBlur, 1},
		{"click another tab — its rail cell up top takes the focus", evBlur, 0},
		{"drag a rail tab to reorder — the tour cannot watch the rail", evNone, 3},
		{"press " + g[4].keys + " — the palette takes the keys", evBlur, 4},
		{"press " + g[2].keys + " — the focus moves to the next tab", evBlur, 2},
	}
}

// rowStep is row's inverse: the try-step index owning painted row r,
// or -1 — the launcher line's step has no gesture row to light.
func rowStep(r int) int {
	for i, s := range trySteps() {
		if s.row == r {
			return i
		}
	}
	return -1
}

// tour is the frame's state: the gesture step the tour is teaching, the
// done latch (walked to the end or dismissed — a restore never restarts a
// finished tour), the launcher hover/press for the pointer, the quit latch
// for Esc/q, and the last launch outcome the status line echoes.
type tour struct {
	step     int         // cursor: gesture row in read mode, armed try-step in try mode
	done     bool        // completed or dismissed — the envelope's terminal flag
	try      bool        // #3769: the interactive tour vs the read-only walk
	states   []stepState // per try-step outcome; nil in read mode
	tried    int         // try-steps this run saw real evidence for
	skipped  int         // try-steps this run skipped past
	hover    int         // launcher index under the pointer; -1 when none
	pressed  int         // launcher index held on a left press; -1 when none
	launched string      // last bin the tour successfully started
	failed   string      // last bin the exec refused
	quit     bool
}

func newTour(try bool) tour {
	t := tour{hover: -1, pressed: -1, try: try}
	if try {
		t.states = make([]stepState, len(trySteps()))
	}
	return t
}

// stepsLen is the cursor's bound — the try table in try mode, the
// gesture rows on the read-only walk.
func (t *tour) stepsLen() int {
	if t.try {
		return len(trySteps())
	}
	return len(gestures())
}

// atDone reports that the painted surface flips to the completion
// screen: the tour is finished AND the cursor sits at its end — the
// read walk's look-back keeps the rows up for review while the latch
// itself stays set.
func (t *tour) atDone() bool {
	return t.done && t.step >= t.stepsLen()-1
}

// armed is the try-step awaiting its evidence: the cursor row while it
// is pending. -1 in read mode, on a finished tour, or off-table.
func (t *tour) armed() int {
	if !t.try || t.done || t.step < 0 || t.step >= len(t.states) {
		return -1
	}
	if t.states[t.step] != stepPending {
		return -1
	}
	return t.step
}

// advance moves the cursor to the next pending row after i; none left
// latches the tour done. It only looks forward — rows resolve in order,
// so a pending row is never behind the cursor.
func (t *tour) advance() int {
	for j := t.step + 1; j < len(t.states); j++ {
		if t.states[j] == stepPending {
			t.step = j
			return j
		}
	}
	t.done = true
	return -1
}

// observe lands evidence on the armed row when that row asked for this
// event class. ok=true carries the resolved index so the caller can
// print its marker — the model decides, main reports.
func (t *tour) observe(ev tryEvidence) (i int, ok bool) {
	i = t.armed()
	if i < 0 || trySteps()[i].want != ev {
		return -1, false
	}
	t.states[i] = stepDone
	t.tried++
	t.advance()
	return i, true
}

// skip resolves the armed row WITHOUT evidence — the honest path past a
// step the user could not perform (or the tour cannot watch, the
// evNone case). ok=true carries the skipped index for the marker.
func (t *tour) skip() (i int, ok bool) {
	i = t.armed()
	if i < 0 {
		return -1, false
	}
	t.states[i] = stepSkipped
	t.skipped++
	t.advance()
	return i, true
}

// next steps the tour one gesture forward; stepping past the last row
// latches done — the tour ran to its end. The latch is sticky: stepping
// back to review an earlier row does not un-complete a finished tour.
// The try tour never walks: its cursor moves only on evidence or a skip.
func (t *tour) next() bool {
	if t.try {
		return false
	}
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
// the tour reached its end it stays complete. Try mode keeps the whole
// table painted with per-row outcomes instead of a walk-back cursor.
func (t *tour) prev() bool {
	if t.try || t.step == 0 {
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
	if n >= t.stepsLen() {
		n = t.stepsLen() - 1
	}
	if t.try {
		return "try " + string(rune('1'+n)) + " of 6 · → skips · a skips all · 1-4 launch · esc closes"
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
//	   dismissed; a restored done flag never re-runs a finished tour.
//	   bit 1: try (#3769) — the session was the interactive tour; a
//	   reopen resumes it rather than degrading to the read walk
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
	flagTry  = 1 << 1
)

// tourState is one decoded session blob.
type tourState struct {
	step int
	done bool
	try  bool
}

// encode serializes the tour's durable state. Bounded by construction:
// four bytes, forever under the seat's 64 KiB cap.
func (t *tour) encode() []byte {
	var flags byte
	if t.done {
		flags |= flagDone
	}
	if t.try {
		flags |= flagTry
	}
	return []byte{stateFmtVer, flags, byte(t.step), 0}
}

// decodeTourState is encode's strict inverse. ok=false means stale or
// foreign — refused whole, never half-applied; the caller names the
// refusal. The step byte clamps into the live table's range — six try
// rows when the blob says try, five gesture rows on the read walk — so
// a stale-but-well-formed blob degrades to the nearest live row instead
// of refusing the session.
func decodeTourState(b []byte) (tourState, bool) {
	var st tourState
	if len(b) != 4 || b[0] != stateFmtVer {
		return st, false
	}
	if b[1]&^(flagDone|flagTry) != 0 || b[3] != 0 {
		return st, false
	}
	st.done = b[1]&flagDone != 0
	st.try = b[1]&flagTry != 0
	st.step = int(b[2])
	limit := len(gestures())
	if st.try {
		limit = len(trySteps())
	}
	if st.step >= limit {
		st.step = limit - 1
	}
	return st, true
}
