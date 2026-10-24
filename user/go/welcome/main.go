// Command welcome is GOWELCOME.ELF: the M122b (issue #2571) first-boot
// welcome tour — five core gestures and one-click launchers on a tabapp
// canvas. main.go owns the shell: tabapp lifecycle, the exec seam the
// launcher buttons ride, the event loop and the serial markers. tour.go
// holds the pure model the host tests pin — including the M195b (#3307)
// appver-1 state envelope, so a seat save/restore mid-tour resumes on the
// same gesture step instead of restarting the tour.
//
// Every marker below is printed only AFTER its syscall returned, so the
// seat legs' asserts (go-welcome.spec, go-wm-default) can only pass if
// the app actually ran.
package main

import (
	"virelai/appkit"
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
// keeps the click -> pid route observable).
var execBin = vi.Exec

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
		vi.ConsoleLine(markerOpenErr + "-1")
		vi.Exit(1)
	}
	vi.ConsoleLine(markerOpen + vi.Itoa64(int64(ta.Win)))
	if ta.TabAware {
		vi.ConsoleLine(markerDeclare)
	} else {
		vi.ConsoleLine(markerRefused)
	}

	a = &app{ta: ta, m: newTour()}
	loop := appkit.NewLoop(a.ta, a.draw, a.handle)
	loop.SubscribeSetting("theme", func(value string) bool {
		return theme.Set(value)
	})
	loop.ShouldQuit = func() (int, bool) { return 0, a.m.quit }
	loop.OnInitialPresent = func() {
		vi.ConsoleLine(markerPresent)
		// The envelope applies AFTER the first paint: a reopened tour
		// shows its fresh frame once, then the restored step repaints
		// and the marker reports what the blob decided.
		if line := a.applyRestored(restored, restoredOK); line != "" {
			a.draw()
			a.ta.Present()
			vi.ConsoleLine(line)
		}
		vi.ConsoleLine(markerReady)
	}
	loop.OnExit = func(status int) {
		vi.ConsoleLine(markerClose)
		vi.ConsoleLine(markerOK)
		a.ta.CloseAndExit(status)
	}
	// The seat's save ask is mailbox traffic, not an input event: answer
	// it once per pass before polling, the same discipline the other
	// hooked apps keep. The saved marker prints only on an acked
	// handoff — never on silence or a refused post.
	loop.RunWith(func() (vi.Event, int64, bool) {
		if a.ta.PollStateAsk() {
			vi.ConsoleLine(markerState + "saved step=" + vi.Itoa64(int64(a.m.step)))
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
	}
	return false
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
	if down {
		if g := a.m.gestureAt(a.gestureRects(), x, y); g >= 0 && g != a.m.step {
			a.m.step = g
			vi.ConsoleLine(a.stepMarker())
			return true
		}
	}
	return changed
}

// key: Right/Left walk the tour's gesture steps, digits 1-4 fire the
// matching launcher, and Escape or q dismisses — the done latch sets on
// dismissal too, so a saved envelope restores a finished tour instead of
// re-running it. A chord the seat owns (ctrl-*) never reaches here — the
// mod guard keeps the routing honest anyway.
func (a *app) key(ev vi.Event) bool {
	if ev.Kind != vi.EvKeyDown {
		return false
	}
	if ev.Flags&vi.ModCtrl != 0 {
		return false
	}
	switch ev.Arg0 {
	case hidRight:
		if a.m.next() {
			vi.ConsoleLine(a.stepMarker())
			return true
		}
		return false
	case hidLeft:
		if a.m.prev() {
			vi.ConsoleLine(a.stepMarker())
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
// is its own honest line, never a silent no-op.
func (a *app) launch(i int) bool {
	if i < 0 || i >= len(launchers) {
		return false
	}
	l := launchers[i]
	pid, err := execBin(l.bin)
	if err != nil || pid <= 0 {
		a.m.failed = l.bin
		a.m.launched = ""
		vi.ConsoleLine(markerLaunchFail + l.bin)
		return true
	}
	a.m.launched = l.bin
	a.m.failed = ""
	vi.ConsoleLine(markerLaunch + l.bin)
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
	a.title = widgets.Text{
		R:     sc(widgets.Rect{X: pad, Y: 10, W: full, H: 24}),
		Label: "Welcome to VirelaiOS",
		Fg:    tok.Text, Bg: tok.Surface,
	}
	a.sub = widgets.Text{
		R:     sc(widgets.Rect{X: pad, Y: 38, W: full, H: 16}),
		Label: "Five gestures for the first hour",
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
		a.chips[i] = widgets.Text{
			R:     sc(widgets.Rect{X: pad, Y: y, W: chipW, H: 38}),
			Label: g.keys,
			Fg:    tok.OnAccent, Bg: tok.Accent,
		}
		a.descs[i] = widgets.Text{
			R:     sc(widgets.Rect{X: pad + chipW + 8, Y: y, W: full - chipW - 8, H: 38}),
			Label: g.text,
			Fg:    tok.Text, Bg: tok.Surface,
		}
	}
	// The tour's position marker rides the left margin against the row it
	// is on; its plate is the backdrop colour, so only the glyph shows.
	cur := a.m.step
	if cur < 0 {
		cur = 0
	}
	if cur >= len(rows) {
		cur = len(rows) - 1
	}
	a.cur = widgets.Text{
		R:     sc(widgets.Rect{X: 0, Y: 66 + cur*44 + 15, W: pad, H: 10}),
		Label: ">",
		Fg:    tok.Accent, Bg: tok.Bg,
	}

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
	a.title.Draw(cv)
	a.sub.Draw(cv)
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
	line := markerState + "restored step=" + vi.Itoa64(int64(a.m.step))
	if a.m.done {
		line += " done"
	}
	return line
}
