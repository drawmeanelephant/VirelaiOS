// Command calc is the Go calculator (Zig CALC.BIN successor): a usable 64-bit
// integer calc (digits, + - * /, equals, clear), full-viewport inside Zig
// TABWM via user/go/tabapp. Zig CALC.BIN is gone (M62h / #1406). Not
// CALC's programmer-mode feature list. Kernel untouched. No new Zig app.
//
// Every marker below is printed only AFTER its syscall returned, so the
// go-calc VZ gate's asserts can only pass if the app actually ran.
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
	appName  = "GOCALC.ELF"
	appTitle = "Calc"
	natW     = 512
	natH     = 384

	markerOpen              = "gocalc: open id="
	markerDeclare           = "gocalc: declare accepted"
	markerDogfood           = "dogfood: calc" // M69a (#1528): go-dogfood.spec's marker
	markerPresent           = "gocalc: present"
	markerSettingSubscribed = "gocalc: settings subscribed key=theme"
	markerSettingPresent    = "gocalc: settings repaint key="
	markerResult            = "gocalc: result "
	markerSaveErr           = "gocalc: save error "
	markerClose             = "gocalc: close"
	markerOK                = "gocalc OK"
	markerOpenErr           = "gocalc: error open "

	defaultPath = "/host/CALC/RESULT.TXT"

	keyEnter  = 0x28
	keyEscape = 0x29
	keyBacksp = 0x2a

	padRows = 4
	padCols = 4
)

var padLabels = [padRows][padCols]string{
	{"7", "8", "9", "/"},
	{"4", "5", "6", "*"},
	{"1", "2", "3", "-"},
	{"0", "C", "=", "+"},
}

type app struct {
	ta         *tabapp.TabApp
	eng        Engine
	path       string
	expr       string
	lastLine   string
	justEvaled bool
	disp       widgets.Text
	keys       [padRows * padCols]widgets.Button
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
	path := defaultPath
	if args := vi.Args(); len(args) > 1 && len(args[1]) > 0 {
		path = args[1]
	}

	ta := tabapp.Init(tabapp.Config{Name: appName, Title: appTitle, X: 32, Y: 32, W: natW, H: natH})
	if ta == nil {
		vi.ConsoleLine(markerOpenErr + "-1")
		vi.Exit(1)
	}
	vi.ConsoleLine(markerOpen + vi.Itoa64(int64(ta.Win)))
	if ta.TabAware {
		vi.ConsoleLine(markerDeclare)
		// M69a (#1528): printed only on the accepted-declare path -- the
		// calculator is HOSTED by a WM seat, not merely running.
		vi.ConsoleLine(markerDogfood)
	} else {
		vi.ConsoleLine("gocalc: declare refused")
	}

	a := &app{ta: ta, path: path}
	loop := appkit.NewLoop(a.ta, a.draw, a.handle)
	if loop.SubscribeSetting("theme", func(value string) bool {
		return theme.Set(value)
	}) {
		vi.ConsoleLine(markerSettingSubscribed)
	}
	loop.OnSettingPresent = func(key, value string) {
		vi.ConsoleLine(markerSettingPresent + key + " value=" + value)
	}
	loop.OnInitialPresent = func() { vi.ConsoleLine(markerPresent) }
	loop.OnExit = func(status int) {
		vi.ConsoleLine(markerClose)
		vi.ConsoleLine(markerOK)
		a.ta.CloseAndExit(status)
	}
	loop.Run()
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

func (a *app) pointer(ev vi.Event) bool {
	x, y := int(ev.Arg0), int(ev.Arg1)
	down := ev.Kind == vi.EvMouseDown && ev.Flags&vi.BtnLeft != 0
	changed := false
	hit := -1
	for i := range a.keys {
		h := a.keys[i].HitTest(x, y)
		if a.keys[i].Hovered != h {
			a.keys[i].Hovered = h
			changed = true
		}
		p := down && h
		if a.keys[i].Pressed != p {
			a.keys[i].Pressed = p
			changed = true
		}
		if h {
			hit = i
		}
	}
	if down && hit >= 0 {
		return a.press(padLabels[hit/padCols][hit%padCols]) || changed
	}
	return changed
}

func (a *app) key(ev vi.Event) bool {
	if ev.Kind != vi.EvKeyDown {
		return false
	}
	if ev.Flags&vi.ModCtrl != 0 {
		return false
	}
	switch ev.Arg0 {
	case keyEnter:
		return a.doEval()
	case keyEscape:
		return a.doClear()
	case keyBacksp:
		return a.doBackspace()
	}
	ch := ev.Arg1
	if ch >= '0' && ch <= '9' {
		return a.doDigit(uint8(ch - '0'))
	}
	switch ch {
	case '+', '-', '*', '/':
		return a.doOp(byte(ch))
	case '=', '\r', '\n':
		return a.doEval()
	case 'c', 'C':
		return a.doClear()
	case 0x08:
		return a.doBackspace()
	}
	return false
}

func (a *app) press(label string) bool {
	if len(label) != 1 {
		return false
	}
	ch := label[0]
	if ch >= '0' && ch <= '9' {
		return a.doDigit(uint8(ch - '0'))
	}
	switch ch {
	case '+', '-', '*', '/':
		return a.doOp(ch)
	case '=':
		return a.doEval()
	case 'C':
		return a.doClear()
	}
	return false
}

func (a *app) doDigit(d uint8) bool {
	if a.justEvaled {
		a.expr = ""
		a.justEvaled = false
	}
	a.eng.InputDigit(d)
	a.expr += string('0' + d)
	return true
}

func (a *app) doOp(op byte) bool {
	a.justEvaled = false
	a.eng.SetOp(op)
	if a.eng.hasErr {
		a.expr = ""
		return true
	}
	a.expr += string(op)
	return true
}

func (a *app) doEval() bool {
	a.eng.Evaluate()
	disp := a.eng.Display()
	a.lastLine = a.expr + "=" + disp
	a.justEvaled = true
	if !a.writeResult(a.lastLine) {
		vi.ConsoleLine(markerSaveErr + a.path)
	}
	vi.ConsoleLine(markerResult + a.lastLine)
	return true
}

func (a *app) doClear() bool {
	a.eng.Clear()
	a.expr = ""
	a.lastLine = ""
	a.justEvaled = false
	return true
}

func (a *app) doBackspace() bool {
	if !a.eng.entering {
		return false
	}
	a.eng.Backspace()
	if len(a.expr) > 0 {
		last := a.expr[len(a.expr)-1]
		if last >= '0' && last <= '9' {
			a.expr = a.expr[:len(a.expr)-1]
		}
	}
	return true
}

// writeResult publishes the result line crash-safe (M81e #1765): one
// temp+fsync+delete-then-rename publish, so the result file is either the old
// line or the new one — never the tail of a torn write.
func (a *app) writeResult(line string) bool {
	return vi.WriteFileSafe(a.path, []byte(line+"\n")) >= 0
}

func (a *app) layout() {
	ta := a.ta
	tok := theme.Current
	a.disp = widgets.Text{
		R:     scaleR(ta, widgets.Rect{X: tok.PadMD, Y: tok.PadMD, W: int(natW) - 2*tok.PadMD, H: 48}),
		Label: a.eng.Display(),
		Fg:    tok.Ink,
		Bg:    tok.Surface,
	}
	const (
		btnW = 118
		btnH = 68
	)
	gap := tok.PadMD
	x0 := tok.PadMD
	y0 := 64
	for r := 0; r < padRows; r++ {
		for c := 0; c < padCols; c++ {
			a.keys[r*padCols+c] = widgets.Button{
				R:       scaleR(ta, widgets.Rect{X: x0 + c*(btnW+gap), Y: y0 + r*(btnH+gap), W: btnW, H: btnH}),
				Label:   padLabels[r][c],
				Hovered: a.keys[r*padCols+c].Hovered,
				Pressed: a.keys[r*padCols+c].Pressed,
			}
		}
	}
}

func (a *app) draw() {
	a.layout()
	var f vi.Filler
	f.Rect(a.ta.Win, 0, 0, a.ta.W, a.ta.H, tabapp.FillRGB())
	cv := &draw.FillerCanvas{Filler: &f, WindowID: a.ta.Win}
	a.disp.Draw(cv)
	for i := range a.keys {
		a.keys[i].Draw(cv)
	}
	f.Flush()
}

func scaleR(ta *tabapp.TabApp, r widgets.Rect) widgets.Rect {
	s := ta.Layout(tabapp.Rect{X: r.X, Y: r.Y, W: r.W, H: r.H}, natW, natH)
	return widgets.Rect{X: s.X, Y: s.Y, W: s.W, H: s.H}
}
