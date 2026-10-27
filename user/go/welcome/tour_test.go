package main

import (
	"testing"

	"virelai/art"
	"virelai/chords"
	"virelai/draw"
	"virelai/tabapp"
	"virelai/theme"
	"virelai/vi"
)

// The card's bar: exactly five gestures and the five named launchers.
func TestTourTables(t *testing.T) {
	gs := gestures()
	if len(gs) != 5 {
		t.Fatalf("gestures = %d, want 5", len(gs))
	}
	for i, g := range gs {
		if g.keys == "" || g.text == "" {
			t.Fatalf("gesture %d has an empty side: %+v", i, g)
		}
	}
	// The chord chips are spelled from the seat's own registry — pin the
	// spellings the tour teaches so a re-homed chord is a deliberate edit,
	// not drift.
	wantKeys := []string{"CLICK", "CTRL+SPACE", "CTRL+TAB", "DRAG", "CTRL+SHIFT+P"}
	for i, want := range wantKeys {
		if gs[i].keys != want {
			t.Fatalf("gesture %d chip = %q, want %q", i, gs[i].keys, want)
		}
	}
	// And the spellings still resolve against the live registry: a seat row
	// losing its binding would empty the chip rather than lie.
	for _, want := range []struct {
		action string
		chord  chords.Chord
	}{
		{"launcher", chords.Chord{Mods: chords.ModCtrl, Usage: chords.UsageSpace}},
		{"cycle-focus", chords.Chord{Mods: chords.ModCtrl, Usage: chords.UsageTab}},
		{"palette", chords.Chord{Mods: chords.ModCtrl | chords.ModShift, Usage: chords.UsageP}},
	} {
		if seatChord(want.action, want.chord) == "" {
			t.Fatalf("seatChord(%q) resolved empty — the registry lost the row", want.action)
		}
	}

	if len(launchers) != 5 {
		t.Fatalf("launchers = %d, want 5", len(launchers))
	}
	wantBins := []string{"GOSET.ELF", "GOFILES.ELF", "GOSTORE.ELF", "GOTERM.ELF", "GOHELP.ELF"}
	wantLabels := []string{"Settings", "Files", "Store", "Terminal", "Help"}
	for i := range launchers {
		if launchers[i].bin != wantBins[i] || launchers[i].label != wantLabels[i] {
			t.Fatalf("launcher %d = %+v, want %s %s", i, launchers[i], wantLabels[i], wantBins[i])
		}
	}
}

func TestDigitLaunch(t *testing.T) {
	for ch := uint32('1'); ch <= '5'; ch++ {
		if got := digitLaunch(ch); got != int(ch-'1') {
			t.Fatalf("digitLaunch(%c) = %d", ch, got)
		}
	}
	for _, ch := range []uint32{'0', '6', 'a', ' '} {
		if digitLaunch(ch) != -1 {
			t.Fatalf("digitLaunch(%c) did not refuse", ch)
		}
	}
}

func TestFootText(t *testing.T) {
	m := newTour(false)
	if got := m.footText(); got != "step 1 of 5 · arrows walk · 1-5 launch · esc closes" {
		t.Fatalf("idle foot = %q", got)
	}
	m.step = 2
	if got := m.footText(); got != "step 3 of 5 · arrows walk · 1-5 launch · esc closes" {
		t.Fatalf("stepped foot = %q", got)
	}
	m.launched = "GOTERM.ELF"
	if got := m.footText(); got != "started GOTERM.ELF — find its tab on the rail" {
		t.Fatalf("launched foot = %q", got)
	}
	m.launched, m.failed = "", "GOSET.ELF"
	if got := m.footText(); got != "could not start GOSET.ELF" {
		t.Fatalf("failed foot = %q", got)
	}
	m.failed = ""
	m.done = true
	if got := m.footText(); got != "tour complete — esc closes · reopen from the launcher" {
		t.Fatalf("done foot = %q", got)
	}
}

// Click routes: a point inside button i fires launcher i; outside fires
// nothing. The rects come from the same layout the paint used — no
// duplicated geometry.
func TestLauncherHitTest(t *testing.T) {
	a := &app{ta: &tabapp.TabApp{W: natW, H: natH}, m: newTour(false)}
	a.layout()
	rects := a.launchRects()
	if len(rects) != 5 {
		t.Fatalf("launchRects = %d", len(rects))
	}
	for i, r := range rects {
		if got := a.m.launcherAt(rects, r.X+r.W/2, r.Y+r.H/2); got != i {
			t.Fatalf("centre of button %d hit %d", i, got)
		}
	}
	if got := a.m.launcherAt(rects, 4, 4); got != -1 {
		t.Fatalf("outside point hit launcher %d", got)
	}
	// The gap between two adjacent buttons is dead space.
	mid := rects[0]
	if got := a.m.launcherAt(rects, mid.X+mid.W+1, mid.Y+mid.H/2); got != -1 {
		t.Fatalf("gap point hit launcher %d", got)
	}
}

// The click path end to end: a left-down inside a button execs its bin via
// the seam; hover alone fires nothing.
func TestPointerLaunchesThroughExecSeam(t *testing.T) {
	var execs []string
	old := execBin
	execBin = func(name string, args ...string) (int64, error) {
		execs = append(execs, name)
		return 42, nil
	}
	defer func() { execBin = old }()

	a := &app{ta: &tabapp.TabApp{W: natW, H: natH}, m: newTour(false)}
	a.layout()
	r := a.launchRects()[3] // Terminal
	// Hover first: no exec, but the hover state repaints.
	if !a.pointer(vi.Event{Kind: vi.EvMouseMove, Arg0: uint32(r.X + r.W/2), Arg1: uint32(r.Y + r.H/2)}) {
		t.Fatal("hover over a launcher did not repaint")
	}
	if len(execs) != 0 {
		t.Fatalf("hover exec'd %v", execs)
	}
	a.pointer(vi.Event{Kind: vi.EvMouseDown, Flags: vi.BtnLeft, Arg0: uint32(r.X + r.W/2), Arg1: uint32(r.Y + r.H/2)})
	if len(execs) != 1 || execs[0] != "GOTERM.ELF" {
		t.Fatalf("click exec'd %v, want GOTERM.ELF", execs)
	}
	if a.m.launched != "GOTERM.ELF" {
		t.Fatalf("launched = %q", a.m.launched)
	}
	// A click in the dead zone launches nothing.
	a.pointer(vi.Event{Kind: vi.EvMouseDown, Flags: vi.BtnLeft, Arg0: 4, Arg1: 4})
	if len(execs) != 1 {
		t.Fatalf("dead-zone click exec'd %v", execs)
	}
	// The designed Help button (apps-V1R00 `btn/Help`, #3790) execs
	// GOHELP.ELF — the same bare launch the palette's help row takes.
	r = a.launchRects()[4]
	a.pointer(vi.Event{Kind: vi.EvMouseDown, Flags: vi.BtnLeft, Arg0: uint32(r.X + r.W/2), Arg1: uint32(r.Y + r.H/2)})
	if len(execs) != 2 || execs[1] != "GOHELP.ELF" {
		t.Fatalf("Help click exec'd %v, want GOHELP.ELF", execs)
	}
}

func TestKeyLaunchAndQuit(t *testing.T) {
	var execs []string
	old := execBin
	execBin = func(name string, args ...string) (int64, error) {
		execs = append(execs, name)
		return 7, nil
	}
	defer func() { execBin = old }()

	a := &app{ta: &tabapp.TabApp{W: natW, H: natH}, m: newTour(false)}
	a.key(vi.Event{Kind: vi.EvKeyDown, Arg1: '2'})
	if len(execs) != 1 || execs[0] != "GOFILES.ELF" {
		t.Fatalf("'2' exec'd %v, want GOFILES.ELF", execs)
	}
	// A ctrl-modified digit is the seat's chord space, not the tour's.
	a.key(vi.Event{Kind: vi.EvKeyDown, Flags: vi.ModCtrl, Arg1: '3'})
	if len(execs) != 1 {
		t.Fatalf("ctrl+digit leaked a launch: %v", execs)
	}
	a.key(vi.Event{Kind: vi.EvKeyDown, Arg0: keyEscape})
	if !a.m.quit {
		t.Fatal("escape did not latch quit")
	}
}

// The render leg: the frame's plates land where layout put them, in the
// theme's own colours, and glyph runs paint ink inside them.
func TestWelcomeFramePaints(t *testing.T) {
	a := &app{ta: &tabapp.TabApp{W: natW, H: natH}, m: newTour(false)}
	c := &draw.BufferCanvas{Pix: make([]uint32, natW*natH), W: natW, H: natH}
	a.paintCanvas(c)

	px := func(x, y int) uint32 { return c.Pix[y*c.W+x] }

	if got := px(1, natH-2); got != op(theme.Current.Bg) {
		t.Fatalf("backdrop = %#x, want opaque Bg %#x", got, op(theme.Current.Bg))
	}
	// A gesture chip carries the accent plate.
	chip := a.chips[0].R
	if got := px(chip.X+chip.W/2, chip.Y+chip.H/2); got != op(theme.Current.Accent) {
		t.Fatalf("gesture chip = %#x, want accent %#x", got, op(theme.Current.Accent))
	}
	// A description row carries the surface plate.
	desc := a.descs[0].R
	if got := px(desc.X+desc.W-4, desc.Y+4); got != op(theme.Current.Surface) {
		t.Fatalf("desc plate = %#x, want surface %#x", got, op(theme.Current.Surface))
	}
	// A launcher button: border around an idle face.
	btn := a.buttons[0].R
	if got := px(btn.X+4, btn.Y+4); got != op(theme.Current.BtnIdle) {
		t.Fatalf("button face = %#x, want idle %#x", got, op(theme.Current.BtnIdle))
	}
	// Text ran: something inside the canvas is the label ink.
	ink := 0
	for _, p := range c.Pix {
		if p == op(theme.Current.Text) || p == op(theme.Current.OnAccent) || p == op(theme.Current.Muted) {
			ink++
		}
	}
	if ink < 200 {
		t.Fatalf("only %d label-ink pixels — the tour's text did not paint", ink)
	}
}

// Hover repaints the button face — the pointer feedback a one-click
// launcher owes the user.
func TestHoverRepaintsButton(t *testing.T) {
	a := &app{ta: &tabapp.TabApp{W: natW, H: natH}, m: newTour(false)}
	c := &draw.BufferCanvas{Pix: make([]uint32, natW*natH), W: natW, H: natH}
	a.paintCanvas(c)

	r := a.launchRects()[0]
	a.pointer(vi.Event{Kind: vi.EvMouseMove, Arg0: uint32(r.X + r.W/2), Arg1: uint32(r.Y + r.H/2)})
	a.paintCanvas(c)
	if got := c.Pix[(r.Y+4)*c.W+r.X+4]; got != op(theme.Current.BtnHover) {
		t.Fatalf("hovered face = %#x, want hover %#x", got, op(theme.Current.BtnHover))
	}
}

func op(rgb uint32) uint32 { return rgb | 0xff000000 }

// --- M195b (#3307): the tour step + the appver-1 envelope ------------------

// The step walk: Right advances to the last row then latches done, Left
// steps back without clearing the latch, both ends are bounded.
func TestStepWalk(t *testing.T) {
	m := newTour(false)
	if m.step != 0 || m.done {
		t.Fatalf("fresh tour = step %d done %v", m.step, m.done)
	}
	// Already at the first row: back is a no-op, not a wrap.
	if m.prev() {
		t.Fatal("prev at step 0 moved")
	}
	for want := 1; want <= 4; want++ {
		if !m.next() || m.step != want {
			t.Fatalf("next %d = step %d", want, m.step)
		}
	}
	if !m.next() || !m.done {
		t.Fatal("advancing past the last row did not latch done")
	}
	if m.next() {
		t.Fatal("a finished tour advanced again")
	}
	// The latch is sticky: reviewing an earlier row keeps the tour done.
	if !m.prev() || m.step != 3 || !m.done {
		t.Fatalf("prev after done = step %d done %v", m.step, m.done)
	}
}

// Click-to-step: a point inside gesture row i jumps the tour to step i —
// the pointer spelling of the arrow keys, over the same laid-out strips
// the paint produced.
func TestGestureHitTest(t *testing.T) {
	a := &app{ta: &tabapp.TabApp{W: natW, H: natH}, m: newTour(false)}
	a.layout()
	rects := a.gestureRects()
	if len(rects) != 5 {
		t.Fatalf("gestureRects = %d", len(rects))
	}
	for i, r := range rects {
		if got := a.m.gestureAt(rects, r.X+r.W/2, r.Y+r.H/2); got != i {
			t.Fatalf("centre of row %d hit %d", i, got)
		}
	}
	if got := a.m.gestureAt(rects, 4, 4); got != -1 {
		t.Fatalf("outside point hit row %d", got)
	}
	// A real click on row 2 moves the tour there.
	a.pointer(vi.Event{Kind: vi.EvMouseDown, Flags: vi.BtnLeft,
		Arg0: uint32(rects[2].X + rects[2].W/2), Arg1: uint32(rects[2].Y + rects[2].H/2)})
	if a.m.step != 2 {
		t.Fatalf("row click left step = %d", a.m.step)
	}
}

// Arrow keys walk the tour; a ctrl-modified arrow stays the seat's chord
// space. Esc and q dismiss — the done latch sets on dismissal so a saved
// envelope restores finished, never re-run.
func TestKeyStepsAndDismiss(t *testing.T) {
	a := &app{ta: &tabapp.TabApp{W: natW, H: natH}, m: newTour(false)}
	if !a.key(vi.Event{Kind: vi.EvKeyDown, Arg0: hidRight}) || a.m.step != 1 {
		t.Fatalf("right = step %d", a.m.step)
	}
	if !a.key(vi.Event{Kind: vi.EvKeyDown, Arg0: hidLeft}) || a.m.step != 0 {
		t.Fatalf("left = step %d", a.m.step)
	}
	if a.key(vi.Event{Kind: vi.EvKeyDown, Flags: vi.ModCtrl, Arg0: hidRight}) {
		t.Fatal("ctrl+right leaked a step")
	}
	if a.m.step != 0 {
		t.Fatalf("ctrl+right moved step = %d", a.m.step)
	}
	a.key(vi.Event{Kind: vi.EvKeyDown, Arg0: keyEscape})
	if !a.m.quit || !a.m.done {
		t.Fatalf("escape = quit %v done %v", a.m.quit, a.m.done)
	}
}

// The envelope round-trips the two durable fields and refuses the foreign
// cases by name: wrong length, wrong fmtver, unknown flag bits, nonzero
// reserved. A step byte past the live table clamps, never refuses.
func TestTourEnvelope(t *testing.T) {
	m := newTour(false)
	m.step = 3
	m.done = true
	st, ok := decodeTourState(m.encode())
	if !ok || st.step != 3 || !st.done {
		t.Fatalf("round-trip = %+v ok=%v", st, ok)
	}
	m2 := newTour(false)
	st, ok = decodeTourState(m2.encode())
	if !ok || st.step != 0 || st.done {
		t.Fatalf("fresh round-trip = %+v ok=%v", st, ok)
	}
	for i, b := range [][]byte{
		{},                         // short
		{1, 0, 0},                  // truncated
		{1, 0, 0, 0, 0},            // trailing byte
		{2, 0, 0, 0},               // foreign fmtver
		{1, flagDone | 0x80, 0, 0}, // unknown flag bit
		{1, 0, 0, 1},               // nonzero reserved
	} {
		if _, ok := decodeTourState(b); ok {
			t.Fatalf("blob %d decoded: %x", i, b)
		}
	}
	// A step byte beyond the table clamps to the last row — the blob is
	// well-formed, so it degrades rather than refusing.
	st, ok = decodeTourState([]byte{stateFmtVer, 0, 250, 0})
	if !ok || st.step != len(gestures())-1 {
		t.Fatalf("clamp = %+v ok=%v", st, ok)
	}
}

// The apply side: a restored blob lands step+done on the live tour, a
// done-flag restore keeps the tour finished, and refusals name
// themselves. No blob waiting is a silent fresh start.
func TestApplyRestored(t *testing.T) {
	// No stored envelope: nothing to report, nothing applied.
	a := &app{ta: &tabapp.TabApp{W: natW, H: natH}, m: newTour(false)}
	if line := a.applyRestored(tourState{}, false); line != "" {
		t.Fatalf("no-restore marker = %q", line)
	}
	// A mid-tour blob restores the step the seat saved.
	a.ta.Restored, a.ta.RestoredVersion = true, stateAppVer
	st, _ := decodeTourState([]byte{stateFmtVer, 0, 2, 0})
	line := a.applyRestored(st, true)
	if a.m.step != 2 || a.m.done {
		t.Fatalf("applied = step %d done %v", a.m.step, a.m.done)
	}
	if line != "gowelcome: state restored step=2" {
		t.Fatalf("restored marker = %q", line)
	}
	// A completed tour restores complete — the latch does not re-run it.
	a2 := &app{ta: &tabapp.TabApp{W: natW, H: natH}, m: newTour(false)}
	a2.ta.Restored, a2.ta.RestoredVersion = true, stateAppVer
	st, _ = decodeTourState([]byte{stateFmtVer, flagDone, 4, 0})
	line = a2.applyRestored(st, true)
	if !a2.m.done || a2.m.step != 4 {
		t.Fatalf("done restore = step %d done %v", a2.m.step, a2.m.done)
	}
	if line != "gowelcome: state restored step=4 done" {
		t.Fatalf("done marker = %q", line)
	}
	// The refusals, each named: foreign appver, torn blob.
	a3 := &app{ta: &tabapp.TabApp{W: natW, H: natH}, m: newTour(false)}
	a3.ta.Restored, a3.ta.RestoredVersion = true, 9
	if line := a3.applyRestored(tourState{}, true); line != "gowelcome: state refused ver=9" {
		t.Fatalf("ver refusal = %q", line)
	}
	a3.ta.RestoredVersion = stateAppVer
	if line := a3.applyRestored(tourState{}, false); line != "gowelcome: state refused bad" {
		t.Fatalf("bad refusal = %q", line)
	}
	if a3.m.step != 0 || a3.m.done {
		t.Fatalf("a refused blob still applied: step %d done %v", a3.m.step, a3.m.done)
	}
}

// --- #3769: the try-it tour ---------------------------------------------

// The try table: six steps, the DRAG row honestly unobservable, every
// prompt naming its action.
func TestTryStepsTable(t *testing.T) {
	steps := trySteps()
	if len(steps) != 6 {
		t.Fatalf("trySteps = %d, want 6", len(steps))
	}
	for i, s := range steps {
		if s.prompt == "" {
			t.Fatalf("try step %d has an empty prompt", i)
		}
	}
	if steps[3].want != evNone {
		t.Fatalf("the rail-drag row pretends to be observable: %v", steps[3].want)
	}
	if steps[0].want != evPress || steps[0].row != 5 {
		t.Fatalf("the press step = %+v, want evPress on the launcher line", steps[0])
	}
	for r := 0; r < 5; r++ {
		if rowStep(r) < 0 {
			t.Fatalf("gesture row %d owns no try step", r)
		}
	}
}

// Evidence resolves only the armed row and only on the class it asked
// for; the cursor advances to the next pending row each time.
func TestTryObserve(t *testing.T) {
	m := newTour(true)
	if m.armed() != 0 {
		t.Fatalf("fresh try tour armed = %d", m.armed())
	}
	// A blur while the press row is armed is not its evidence.
	if _, ok := m.observe(evBlur); ok {
		t.Fatal("blur resolved the press row")
	}
	i, ok := m.observe(evPress)
	if !ok || i != 0 || m.states[0] != stepDone || m.tried != 1 {
		t.Fatalf("press = %d %v state %v tried %d", i, ok, m.states[0], m.tried)
	}
	if m.armed() != 1 {
		t.Fatalf("after resolve armed = %d", m.armed())
	}
	// A read-mode tour observes nothing.
	r := newTour(false)
	if _, ok := r.observe(evBlur); ok {
		t.Fatal("read mode resolved a try step")
	}
}

// The honest paths: Right-skip one row at a time, the evNone row can
// only ever be skipped, and the last resolve latches the tour done.
func TestTrySkipWalk(t *testing.T) {
	m := newTour(true)
	// Two real blurs, then the skip chain over cycle/drag/palette.
	m.observe(evPress)
	m.observe(evBlur)
	i, ok := m.skip()
	if !ok || i != 2 || m.states[2] != stepSkipped || m.skipped != 1 {
		t.Fatalf("skip = %d %v state %v skipped %d", i, ok, m.states[2], m.skipped)
	}
	// The drag row is skip-only by declaration: no evidence resolves it.
	if _, ok := m.observe(evBlur); ok {
		t.Fatal("the unobservable drag row took blur evidence")
	}
	if _, ok := m.observe(evPress); ok {
		t.Fatal("the unobservable drag row took press evidence")
	}
	if i, ok := m.skip(); !ok || i != 3 {
		t.Fatalf("drag-row skip = %d %v", i, ok)
	}
	// Skip the palette row, then the cycle row takes its blur.
	m.skip()
	i, ok = m.observe(evBlur)
	if !ok || i != 5 || !m.done {
		t.Fatalf("press = %d %v done %v", i, ok, m.done)
	}
	if m.tried != 3 || m.skipped != 3 {
		t.Fatalf("counts tried=%d skipped=%d, want 3/3", m.tried, m.skipped)
	}
	if !m.atDone() {
		t.Fatal("a finished try tour does not show the done screen")
	}
}

// The marker vocabulary the spec pins: a blur resolve, the skips, the
// done line's counts — each line in the order it leaves the console.
func TestTryMarkers(t *testing.T) {
	var lines []string
	old := consoleLine
	consoleLine = func(s string) { lines = append(lines, s) }
	defer func() { consoleLine = old }()

	a := &app{ta: &tabapp.TabApp{W: natW, H: natH}, m: newTour(true)}
	a.observeTry(evPress)
	a.skipTry(false)
	a.skipTry(true)
	want := []string{
		"gowelcome: try 0 done press",
		"gowelcome: try 1 armed",
		"gowelcome: try 1 skip",
		"gowelcome: try 2 armed",
		"gowelcome: try 2 skip",
		"gowelcome: try 3 skip",
		"gowelcome: try 4 skip",
		"gowelcome: try 5 skip",
		"gowelcome: done tried=1 skipped=5",
	}
	if len(lines) != len(want) {
		t.Fatalf("lines = %v", lines)
	}
	for i := range want {
		if lines[i] != want[i] {
			t.Fatalf("line %d = %q, want %q (all: %v)", i, lines[i], want[i], lines)
		}
	}
}

// argv: the bare exec is the try tour, --read is the walk, --try names
// the default, anything else is refused by name.
func TestParseMode(t *testing.T) {
	for _, tc := range []struct {
		args []string
		try  bool
		bad  string
	}{
		{nil, true, ""},
		{[]string{appName}, true, ""},
		{[]string{appName, "--try"}, true, ""},
		{[]string{appName, "--read"}, false, ""},
		{[]string{appName, "--bogus"}, true, "--bogus"},
		{[]string{appName, "--read", "extra"}, false, "extra"},
	} {
		try, bad := parseMode(tc.args)
		if try != tc.try || bad != tc.bad {
			t.Fatalf("parseMode(%v) = %v %q, want %v %q", tc.args, try, bad, tc.try, tc.bad)
		}
	}
}

// The flagTry bit round-trips the mode through the envelope; an old
// read blob stays read, foreign flag bits still refuse whole.
func TestTryEnvelope(t *testing.T) {
	m := newTour(true)
	m.step = 5
	m.done = true
	st, ok := decodeTourState(m.encode())
	if !ok || st.step != 5 || !st.done || !st.try {
		t.Fatalf("try round-trip = %+v ok=%v", st, ok)
	}
	st, ok = decodeTourState([]byte{stateFmtVer, 0, 3, 0})
	if !ok || st.try {
		t.Fatalf("read blob decoded try: %+v", st)
	}
	if _, ok = decodeTourState([]byte{stateFmtVer, flagDone | flagTry | 0x80, 0, 0}); ok {
		t.Fatal("an unknown flag bit decoded")
	}
	// The try clamp bound is the six-row try table, not the five rows.
	st, ok = decodeTourState([]byte{stateFmtVer, flagTry, 250, 0})
	if !ok || st.step != len(trySteps())-1 {
		t.Fatalf("try clamp = %+v ok=%v", st, ok)
	}
}

// A restored try blob resumes interactive: the mode lands, rows before
// the cursor settle without inventing evidence, and a done blob stays
// finished on the done screen.
func TestApplyRestoredTry(t *testing.T) {
	a := &app{ta: &tabapp.TabApp{W: natW, H: natH}, m: newTour(true)}
	a.ta.Restored, a.ta.RestoredVersion = true, stateAppVer
	st, _ := decodeTourState([]byte{stateFmtVer, flagTry, 4, 0})
	a.applyRestored(st, true)
	if !a.m.try || a.m.step != 4 || a.m.armed() != 4 {
		t.Fatalf("restored try = try %v step %d armed %d", a.m.try, a.m.step, a.m.armed())
	}
	for i := 0; i < 4; i++ {
		if a.m.states[i] != stepDone {
			t.Fatalf("row %d not settled: %v", i, a.m.states[i])
		}
	}
	// A read blob over a try exec downgrades the live tour too.
	a2 := &app{ta: &tabapp.TabApp{W: natW, H: natH}, m: newTour(true)}
	a2.ta.Restored, a2.ta.RestoredVersion = true, stateAppVer
	st2, _ := decodeTourState([]byte{stateFmtVer, 0, 2, 0})
	a2.applyRestored(st2, true)
	if a2.m.try || a2.m.states != nil || a2.m.step != 2 {
		t.Fatalf("read restore = try %v states %v step %d", a2.m.try, a2.m.states, a2.m.step)
	}
	// A finished blob keeps the done screen up on the reopen.
	a3 := &app{ta: &tabapp.TabApp{W: natW, H: natH}, m: newTour(true)}
	a3.ta.Restored, a3.ta.RestoredVersion = true, stateAppVer
	st3, _ := decodeTourState([]byte{stateFmtVer, flagDone | flagTry, 5, 0})
	a3.applyRestored(st3, true)
	if !a3.m.atDone() {
		t.Fatalf("restored done = done %v step %d", a3.m.done, a3.m.step)
	}
}

// The completion surface: the celebrate pose blits through the same
// degrade contract as the wave, the receipt prints once, and the
// counts line names this run's observed record.
func TestDoneScreenPaints(t *testing.T) {
	art.Reset()
	defer art.Reset()
	art.Stage(art.MascotCelebrate, &draw.BufferCanvas{
		Pix: []uint32{0xffff0000}, W: 1, H: 1,
	})
	var lines []string
	old := consoleLine
	consoleLine = func(s string) { lines = append(lines, s) }
	defer func() { consoleLine = old }()
	oldShown := doneMascotShown
	doneMascotShown = false
	defer func() { doneMascotShown = oldShown }()

	a := &app{ta: &tabapp.TabApp{W: natW, H: natH}, m: newTour(true)}
	a.m.done, a.m.step = true, 5
	a.m.tried, a.m.skipped = 4, 2
	c := &draw.BufferCanvas{Pix: make([]uint32, natW*natH), W: natW, H: natH}
	a.paintCanvas(c)
	// The staged pose painted inside its rect.
	m := a.dMascot
	if got := c.Pix[(m.Y+m.H/2)*c.W+m.X+m.W/2]; got != 0xffff0000 {
		t.Fatalf("done mascot pixel = %#x", got)
	}
	if len(lines) != 1 || lines[0] != "gowelcome: done mascot qoi 1x1" {
		t.Fatalf("receipt lines = %v", lines)
	}
	// The receipt is one-shot across repaints.
	a.paintCanvas(c)
	if len(lines) != 1 {
		t.Fatalf("receipt repeated: %v", lines)
	}
	if a.dSub.Label != "4 tried · 2 skipped of 6" {
		t.Fatalf("counts line = %q", a.dSub.Label)
	}
}
