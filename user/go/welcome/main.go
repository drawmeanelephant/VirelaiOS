// Command welcome is GOWELCOME.ELF: the M122b (issue #2571) first-boot
// welcome tour — five core gestures and one-click launchers on a tabapp
// canvas. main.go owns the shell: tabapp lifecycle, the exec seam the
// launcher buttons ride, the event loop and the serial markers. tour.go
// holds the pure model the host tests pin — including the M195b (#3307)
// appver-1 state envelope, so a seat save/restore mid-tour resumes on the
// same gesture step instead of restarting the tour.
//
// #3769: the bare exec runs the interactive "try it" tour — each row
// waits for the event the app can honestly observe (a blur for the
// seat-surface moves, the button's own press for the launcher row) and
// the done flag paints a completion surface standing the staged
// MSC_CELB pose. `GOWELCOME.ELF --read` keeps the read-only walk the
// scripted path types.
//
// Every marker below is printed only AFTER its syscall returned, so the
// seat legs' asserts (go-welcome.spec, go-wm-default) can only pass if
// the app actually ran.
package main

import (
	"virelai/appkit"
	"virelai/art"
	"virelai/draw"
	"virelai/tabapp"
	"virelai/theme"
	"virelai/vi"
	"virelai/widgets"
)

const (
	appName  = "GOWELCOME.ELF"
	appTitle = "Welcome"
	natW     = 512
	natH     = 384

	markerOpen       = "gowelcome: open id="
	markerDeclare    = "gowelcome: declare accepted"
	markerRefused    = "gowelcome: declare refused"
	markerPresent    = "gowelcome: present"
	markerReady      = "gowelcome: ready"
	markerStep       = "gowelcome: step "
	markerLaunch     = "gowelcome: launch "
	markerLaunchFail = "gowelcome: launch fail "
	markerResized    = "gowelcome: resized "
	markerClose      = "gowelcome: close"
	markerOK         = "gowelcome OK"
	markerOpenErr    = "gowelcome: error open "
	markerMascot     = "gowelcome: mascot qoi "
	// The #3769 try-tour vocabulary: the mode this run is in, the armed /
	// done-evidence / skip line per row, the completion line with this
	// run's observed counts, and the celebrate pose's decode receipt.
	markerMode       = "gowelcome: mode "
	markerTry        = "gowelcome: try "
	markerDone       = "gowelcome: done"
	markerDoneMascot = "gowelcome: done mascot qoi "
	// markerState prefixes the M195b envelope report: "saved step=" /
	// "restored step=" / "refused <why>" — same vocabulary NOTE and
	// GOCALC print for theirs.
	markerState = "gowelcome: state "

	keyEscape = 0x29

	// ADR 0009 row 1: arg0 carries the HID usage, arg1 the decoded
	// symbol — the tour's arrow keys are read from arg0 (as NOTE reads
	// its arrows).
	hidRight = 0x4f
	hidLeft  = 0x50
)

// execBin is the launch seam — vi.Exec in the guest, a stub in host tests
// (the execApp pattern in gotabwm's hid.go: bypassing vi's syscall hook
// keeps the click -> pid route observable). consoleLine is the marker
// seam the same tests read back.
var (
	execBin     = vi.Exec
	consoleLine = vi.ConsoleLine
)

type app struct {
	ta      *tabapp.TabApp
	m       tour
	title   widgets.Text
	sub     widgets.Text
	cur     widgets.Text
	chips   []widgets.Text
	descs   []widgets.Text
	head    widgets.Text
	buttons []widgets.Button
	foot    widgets.Text
	mascot  widgets.Rect
	dTitle  widgets.Text
	dSub    widgets.Text
	dMascot widgets.Rect
}

// doneMascotShown latches the one-shot decode receipt for the staged
// celebrate pose (MSC_CELB) standing on the done screen — the same
// latch shape gosnap/gostore keep for theirs.
var doneMascotShown bool

// parseMode reads the tour-mode argv: the bare exec is the interactive
// tour, --read keeps the read-only walk. ok=false names the arg refused
// — argv[0] is the image name, not a flag (the agentd loop's rule).
func parseMode(args []string) (try bool, bad string) {
	try = true
	for i, a := range args {
		if i == 0 && a == appName {
			continue
		}
		switch a {
		case "--try":
			try = true
		case "--read":
			try = false
		default:
			return try, a
		}
	}
	return try, ""
}

func loadGuestTheme() {
	b, r := vi.ReadFileAll("/host/SETTINGS.TXT", 2048)
	if r < 0 || b == nil {
		return
	}
	_ = theme.ApplySettings(b)
}

func main() {
	loadGuestTheme()

	tryMode, bad := parseMode(vi.Args())
	if bad != "" {
		vi.ConsoleLine("gowelcome: refuse argv " + bad)
		vi.Exit(1)
	}

	// The M195b state hooks, the GOTOP shape: SaveState reads the live
	// app (the closure captures the variable, so an ask always serializes
	// the current step); RestoreState fires inside tabapp.Init before the
	// app exists, so it only decodes — the apply lands on first present,
	// after the fresh frame has already painted once.
	var a *app
	var restored tourState
	var restoredOK bool
	ta := tabapp.Init(tabapp.Config{
		Name: appName, Title: appTitle, X: 32, Y: 32, W: natW, H: natH,
		StateVersion: stateAppVer,
		SaveState: func() []byte {
			if a == nil {
				return nil
			}
			return a.m.encode()
		},
		RestoreState: func(b []byte) { restored, restoredOK = decodeTourState(b) },
	})
	if ta == nil {
		consoleLine(markerOpenErr + "-1")
		vi.Exit(1)
	}
	consoleLine(markerOpen + vi.Itoa64(int64(ta.Win)))
	if ta.TabAware {
		consoleLine(markerDeclare)
	} else {
		consoleLine(markerRefused)
	}

	a = &app{ta: ta, m: newTour(tryMode)}
	loop := appkit.NewLoop(a.ta, a.draw, a.handle)
	loop.SubscribeSetting("theme", func(value string) bool {
		return theme.Set(value)
	})
	loop.ShouldQuit = func() (int, bool) { return 0, a.m.quit }
	loop.OnInitialPresent = func() {
		consoleLine(markerPresent)
		// M203 (#3338): the f-artcraft #1250 wave pose, decoded by the
		// first paint — its marker reports what the share carried.
		if m := art.Load(art.MascotWave); m != nil {
			consoleLine(markerMascot +
				vi.Itoa64(int64(m.W)) + "x" + vi.Itoa64(int64(m.H)))
		}
		// The envelope applies AFTER the first paint: a reopened tour
		// shows its fresh frame once, then the restored step repaints
		// and the marker reports what the blob decided.
		if line := a.applyRestored(restored, restoredOK); line != "" {
			a.draw()
			a.ta.Present()
			consoleLine(line)
		}
		// The mode line reports the flow the blob's say produced — a
		// try envelope on a reopen resumes interactive whatever argv
		// asked for — then the armed marker names the step waiting on
		// evidence (none on a restored done tour: its screen is up).
		if a.m.try {
			consoleLine(markerMode + "try")
		} else {
			consoleLine(markerMode + "read")
		}
		if i := a.m.armed(); i >= 0 {
			consoleLine(markerTry + vi.Itoa64(int64(i)) + " armed")
		}
		consoleLine(markerReady)
	}
	loop.OnExit = func(status int) {
		consoleLine(markerClose)
		consoleLine(markerOK)
		a.ta.CloseAndExit(status)
	}
	// The seat's save ask is mailbox traffic, not an input event: answer
	// it once per pass before polling, the same discipline the other
	// hooked apps keep. The saved marker prints only on an acked
	// handoff — never on silence or a refused post.
	loop.RunWith(func() (vi.Event, int64, bool) {
		if a.ta.PollStateAsk() {
			consoleLine(markerState + "saved step=" + vi.Itoa64(int64(a.m.step)))
		}
		return vi.PollEventRaw()
	}, vi.Sleep)
}

func (a *app) handle(ev vi.Event) bool {
	switch ev.Kind {
	case vi.EvKeyDown:
		return a.key(ev)
	case vi.EvMouseMove, vi.EvMouseUp, vi.EvMouseDown:
		return a.pointer(ev)
	case vi.EvWinBlur:
		// Focus leaving the window is the observable half of every
		// "drive the seat" step — the launcher, the palette and a tab
		// switch all take the keys the same way.
		return a.observeTry(evBlur)
	case vi.EvWinFocus:
		// Focus returned — repaint so the frame the modal covered
		// re-presents fresh (and an armed row shows it is still waiting).
		return true
	}
	return false
}

// evidenceName is the marker's word for what was OBSERVED — never the
// action the row hoped for.
func evidenceName(ev tryEvidence) string {
	switch ev {
	case evBlur:
		return "blur"
	case evPress:
		return "press"
	}
	return "none"
}

// observeTry resolves the armed row when it waited on this evidence
// class and prints the transition pair — the row's own line first,
// then the armed/done line it landed on.
func (a *app) observeTry(ev tryEvidence) bool {
	i, ok := a.m.observe(ev)
	if !ok {
		return false
	}
	consoleLine(markerTry + vi.Itoa64(int64(i)) + " done " + evidenceName(ev))
	a.armMarker()
	return true
}

// armMarker reports the landing after a resolve: the next row's armed
// line, or the done line with this run's observed counts when nothing
// waits anymore.
func (a *app) armMarker() {
	if i := a.m.armed(); i >= 0 {
		consoleLine(markerTry + vi.Itoa64(int64(i)) + " armed")
		return
	}
	if a.m.done {
		consoleLine(markerDone +
			" tried=" + vi.Itoa64(int64(a.m.tried)) +
			" skipped=" + vi.Itoa64(int64(a.m.skipped)))
	}
}

// skipTry is the honest advance past a step whose evidence never came
// (or never could — the evNone row): every skipped row says so on the
// wire. all=true is the 'a' verb — finish the tour now, each remaining
// row named.
func (a *app) skipTry(all bool) bool {
	moved := false
	for {
		i, ok := a.m.skip()
		if !ok {
			break
		}
		moved = true
		consoleLine(markerTry + vi.Itoa64(int64(i)) + " skip")
		if !all {
			break
		}
	}
	if moved {
		a.armMarker()
	}
	return moved
}

// pointer runs the launcher row and the gesture strips: hover tracking on
// every motion, a left press lights the button and fires the one-click
// launch — there is no separate confirm step. A press on a gesture row
// jumps the tour to that step, the click spelling of the arrow keys.
func (a *app) pointer(ev vi.Event) bool {
	x, y := int(ev.Arg0), int(ev.Arg1)
	down := ev.Kind == vi.EvMouseDown && ev.Flags&vi.BtnLeft != 0
	changed := false
	hit := a.m.launcherAt(a.launchRects(), x, y)
	for i := range a.buttons {
		if a.buttons[i].Hovered != (i == hit) {
			a.buttons[i].Hovered = i == hit
			changed = true
		}
		p := down && i == hit
		if a.buttons[i].Pressed != p {
			a.buttons[i].Pressed = p
			changed = true
		}
	}
	if down && hit >= 0 {
		return a.launch(hit) || true
	}
	// The click-to-step jump is the read walk's spelling of the arrows —
	// the try tour's cursor belongs to evidence and skips, so its rows
	// are display-only.
	if down && !a.m.try {
		if g := a.m.gestureAt(a.gestureRects(), x, y); g >= 0 && g != a.m.step {
			a.m.step = g
			consoleLine(a.stepMarker())
			return true
		}
	}
	return changed
}

// key: on the read walk Right/Left step the tour; on the try tour Right
// skips the armed row and 'a' skips all that remain — both honest
// advances, each row's marker named. Digits 1-4 fire the matching
// launcher, and Escape or q dismisses — the done latch sets on
// dismissal too, so a saved envelope restores a finished tour instead
// of re-running it. A chord the seat owns (ctrl-*) never reaches here —
// the mod guard keeps the routing honest anyway.
func (a *app) key(ev vi.Event) bool {
	if ev.Kind != vi.EvKeyDown {
		return false
	}
	if ev.Flags&vi.ModCtrl != 0 {
		return false
	}
	switch ev.Arg0 {
	case hidRight:
		if a.m.try {
			return a.skipTry(false)
		}
		if a.m.next() {
			consoleLine(a.stepMarker())
			return true
		}
		return false
	case hidLeft:
		if a.m.prev() {
			consoleLine(a.stepMarker())
			return true
		}
		return false
	case keyEscape:
		a.m.done = true
		a.m.quit = true
		return false
	}
	if i := digitLaunch(ev.Arg1); i >= 0 {
		return a.launch(i)
	}
	if ev.Arg1 == 'a' || ev.Arg1 == 'A' {
		return a.skipTry(true)
	}
	if ev.Arg1 == 'q' || ev.Arg1 == 'Q' {
		a.m.done = true
		a.m.quit = true
	}
	return false
}

// stepMarker reports the step the tour is on — or the done latch when the
// last advance ran the tour to its end.
func (a *app) stepMarker() string {
	if a.m.done {
		return markerStep + "done"
	}
	return markerStep + vi.Itoa64(int64(a.m.step))
}

// launch execs one launcher's bin and records the outcome for the status
// line. The marker prints only after the syscall answered — a refused exec
// is its own honest line, never a silent no-op. The press itself is the
// launcher try-step's evidence either way: the step asked for the click,
// and the exec's own marker already reports what became of it.
func (a *app) launch(i int) bool {
	if i < 0 || i >= len(launchers) {
		return false
	}
	l := launchers[i]
	pid, err := execBin(l.bin)
	if err != nil || pid <= 0 {
		a.m.failed = l.bin
		a.m.launched = ""
		consoleLine(markerLaunchFail + l.bin)
		a.observeTry(evPress)
		return true
	}
	a.m.launched = l.bin
	a.m.failed = ""
	consoleLine(markerLaunch + l.bin)
	a.observeTry(evPress)
	return true
}

// launchRects is the launchers' live-canvas geometry — the click path's
// copy of what layout() last painted.
func (a *app) launchRects() []rect {
	out := make([]rect, len(a.buttons))
	for i := range a.buttons {
		r := a.buttons[i].R
		out[i] = rect{X: r.X, Y: r.Y, W: r.W, H: r.H}
	}
	return out
}

// gestureRects is the gesture rows' live-canvas geometry — chip through
// description as one strip, the click-to-step hit test's copy of what
// layout() last painted.
func (a *app) gestureRects() []rect {
	out := make([]rect, len(a.chips))
	for i := range a.chips {
		c, d := a.chips[i].R, a.descs[i].R
		out[i] = rect{X: c.X, Y: c.Y, W: d.X + d.W - c.X, H: c.H}
	}
	return out
}

func (a *app) layout() {
	ta := a.ta
	tok := theme.Current
	sc := func(r widgets.Rect) widgets.Rect {
		s := ta.Layout(tabapp.Rect{X: r.X, Y: r.Y, W: r.W, H: r.H}, natW, natH)
		return widgets.Rect{X: s.X, Y: s.Y, W: s.W, H: s.H}
	}

	pad := 12
	full := natW - 2*pad
	sub := "Five gestures for the first hour"
	if a.m.try {
		if i := a.m.armed(); i >= 0 {
			sub = "try it: " + trySteps()[i].prompt
		}
	}
	a.title = widgets.Text{
		R:     sc(widgets.Rect{X: pad, Y: 10, W: full, H: 24}),
		Label: "Welcome to VirelaiOS",
		Fg:    tok.Text, Bg: tok.Surface,
	}
	a.sub = widgets.Text{
		R:     sc(widgets.Rect{X: pad, Y: 38, W: full, H: 16}),
		Label: sub,
		Fg:    tok.Muted, Bg: tok.Surface,
	}

	rows := gestures()
	if len(a.chips) != len(rows) {
		a.chips = make([]widgets.Text, len(rows))
		a.descs = make([]widgets.Text, len(rows))
	}
	const chipW = 118
	for i, g := range rows {
		y := 66 + i*44
		chipFg, chipBg, descFg := tok.OnAccent, tok.Accent, tok.Text
		if a.m.try && i < len(a.m.states) {
			// The row's state is its owning step's — execution order
			// and paint order differ (the press runs first but lights
			// the launcher line, not gesture row zero).
			st, j := stepPending, rowStep(i)
			if j >= 0 {
				st = a.m.states[j]
			}
			switch {
			case st == stepDone:
				chipBg = tok.Success
			case st == stepSkipped:
				chipFg, chipBg, descFg = tok.Muted, tok.BtnIdle, tok.Muted
			case j != a.m.step:
				// pending, not armed yet — dimmed until its turn
				chipFg, chipBg, descFg = tok.Muted, tok.Surface, tok.Muted
			}
		}
		a.chips[i] = widgets.Text{
			R:     sc(widgets.Rect{X: pad, Y: y, W: chipW, H: 38}),
			Label: g.keys,
			Fg:    chipFg, Bg: chipBg,
		}
		a.descs[i] = widgets.Text{
			R:     sc(widgets.Rect{X: pad + chipW + 8, Y: y, W: full - chipW - 8, H: 38}),
			Label: g.text,
			Fg:    descFg, Bg: tok.Surface,
		}
	}
	// The tour's position marker rides the left margin against the row it
	// is on; its plate is the backdrop colour, so only the glyph shows.
	// The launcher step (the try table's sixth) has no gesture row — its
	// marker points at the buttons' own line.
	cur := a.m.step
	if cur < 0 {
		cur = 0
	}
	curRow := cur
	if a.m.try && cur < len(trySteps()) {
		curRow = trySteps()[cur].row
	}
	curY := 66 + curRow*44 + 15
	if curRow >= len(rows) {
		curY = 291
	}
	a.cur = widgets.Text{
		R:     sc(widgets.Rect{X: 0, Y: curY, W: pad, H: 10}),
		Label: ">",
		Fg:    tok.Accent, Bg: tok.Bg,
	}

	// The wave pose (f-artcraft #1250) greets from the header's right
	// edge — the title run never reaches that corner.
	a.mascot = sc(widgets.Rect{X: natW - pad - 48, Y: 4, W: 48, H: 48})

	a.head = widgets.Text{
		R:     sc(widgets.Rect{X: pad, Y: 288, W: full, H: 16}),
		Label: "Get started — one click opens it:",
		Fg:    tok.Muted, Bg: tok.Surface,
	}

	if len(a.buttons) != len(launchers) {
		a.buttons = make([]widgets.Button, len(launchers))
	}
	const gap = 8
	btnW := (full - (len(launchers)-1)*gap) / len(launchers)
	for i, l := range launchers {
		a.buttons[i] = widgets.Button{
			R:       sc(widgets.Rect{X: pad + i*(btnW+gap), Y: 308, W: btnW, H: 48}),
			Label:   l.label,
			Hovered: a.buttons[i].Hovered,
			Pressed: a.buttons[i].Pressed,
		}
	}

	a.foot = widgets.Text{
		R:     sc(widgets.Rect{X: pad, Y: 362, W: full, H: 16}),
		Label: a.m.footText(),
		Fg:    tok.Muted, Bg: tok.Surface,
	}

	// The done screen's plates — only painted when atDone flips the
	// surface. The celebrate pose stands centre-top with the verdict
	// under it; the counts are this run's observed record.
	a.dTitle = widgets.Text{
		R:     sc(widgets.Rect{X: pad, Y: 196, W: full, H: 24}),
		Label: "Tour complete",
		Fg:    tok.Text, Bg: tok.Surface,
	}
	dSub := "every row seen, end to end"
	if a.m.try {
		dSub = vi.Itoa64(int64(a.m.tried)) + " tried · " +
			vi.Itoa64(int64(a.m.skipped)) + " skipped of " +
			vi.Itoa64(int64(len(trySteps())))
	}
	a.dSub = widgets.Text{
		R:     sc(widgets.Rect{X: pad, Y: 224, W: full, H: 16}),
		Label: dSub,
		Fg:    tok.Muted, Bg: tok.Surface,
	}
	a.dMascot = sc(widgets.Rect{X: natW/2 - 48, Y: 84, W: 96, H: 96})
}

func (a *app) draw() {
	a.paintCanvas(a.ta.Canvas())
}

// paintCanvas renders the laid-out tour onto cv — split from draw so the
// host tests can pin the painted surface on a BufferCanvas without a bound
// window (the calc golden's pattern: Canvas() on the host is the
// FillerCanvas degrade, not the chrome this app paints in the guest).
func (a *app) paintCanvas(cv widgets.Canvas) {
	if cv == nil {
		return
	}
	a.layout()
	cv.FillRect(widgets.Rect{W: int(a.ta.W), H: int(a.ta.H)}, draw.Opaque(tabapp.FillRGB()))
	if a.m.atDone() {
		a.paintDone(cv)
		return
	}
	a.title.Draw(cv)
	a.sub.Draw(cv)
	// M203 (#3338): the delivered mascot wave — an upgrade on the header,
	// never a dependency: a share without MSC_WAVE keeps the text-only
	// layout (Load's nil is the degrade signal).
	if bc, ok := cv.(*draw.BufferCanvas); ok {
		if m := art.Load(art.MascotWave); m != nil {
			draw.BlitScaled(bc, m, a.mascot)
		}
	}
	for i := range a.chips {
		a.chips[i].Draw(cv)
		a.descs[i].Draw(cv)
	}
	a.cur.Draw(cv)
	a.head.Draw(cv)
	for i := range a.buttons {
		a.buttons[i].Draw(cv)
	}
	a.foot.Draw(cv)
}

// paintDone renders the #3769 completion surface — the celebrate pose
// (MSC_CELB) standing over the verdict line — and prints its one-shot
// decode receipt, the mascot convention from #3687: QOI decode ->
// paint -> `app: <pose> qoi` receipt -> class-B pin. The degrade
// contract is the fleet's: an absent or refused asset, or a canvas
// that is not a pixel buffer, keeps the text-only plate.
func (a *app) paintDone(cv widgets.Canvas) {
	a.dTitle.Draw(cv)
	a.dSub.Draw(cv)
	if bc, ok := cv.(*draw.BufferCanvas); ok {
		if m := art.Load(art.MascotCelebrate); m != nil {
			draw.BlitScaled(bc, m, a.dMascot)
			if !doneMascotShown {
				doneMascotShown = true
				consoleLine(markerDoneMascot +
					vi.Itoa64(int64(m.W)) + "x" + vi.Itoa64(int64(m.H)))
			}
		}
	}
	a.foot.Draw(cv)
}

// applyRestored lands a decoded envelope on the live tour and returns the
// marker to print — "" when the seat handed nothing over. It runs from
// OnInitialPresent so the blob applies AFTER the first paint, the card's
// ordering. The envelope's appver is the app's own stale check: a foreign
// version or a torn blob is refused by name, never silently misread, and
// a done-flag restore keeps the tour finished rather than re-running it.
func (a *app) applyRestored(st tourState, ok bool) string {
	if a.ta == nil || !a.ta.Restored {
		return ""
	}
	if a.ta.RestoredVersion != stateAppVer {
		return markerState + "refused ver=" + vi.Itoa64(int64(a.ta.RestoredVersion))
	}
	if !ok {
		return markerState + "refused bad"
	}
	a.m.step = st.step
	a.m.done = st.done
	a.m.try = st.try
	if a.m.try && a.m.states == nil {
		a.m.states = make([]stepState, len(trySteps()))
	}
	if !a.m.try {
		a.m.states = nil
	}
	// The blob does not carry per-row outcomes — rows before the
	// restored cursor are simply past, so the frame settles them
	// stepDone without claiming this run saw their evidence. The done
	// line's counts only ever name what THIS run observed.
	for i := 0; i < len(a.m.states); i++ {
		if i < a.m.step || a.m.done {
			a.m.states[i] = stepDone
		}
	}
	line := markerState + "restored step=" + vi.Itoa64(int64(a.m.step))
	if a.m.done {
		line += " done"
	}
	return line
}
