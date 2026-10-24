// view.go — the M122c (#2572) windowed half of GOINFER.ELF: what a bare
// launch (the launcher's "LLM Infer" row) opens.
//
// The window runs the same journeys the console form does — 's'/Stub runs
// jStub, 'm'/Model runs jModel on the staged VIW — and mirrors every
// goinfer: console line into an on-window log through the viewHost seam
// wrapper, so the licence receipt, the named refusals and the answer are
// visible where the click happened, not only on serial.
//
// The one-session bound is stated, not hidden: OpenCompute maps a fixed-VA
// window a process gets exactly once, so after a journey that opened the
// session the buttons report spent and say to relaunch — a second press
// is refused on the window rather than answered with a kernel EBUSY.
//
// On a window-less boot the bare launch falls back to naming the journeys
// and exits nonzero — the honest headless answer.
package main

import (
	"io"
	"strings"

	uidraw "virelai/draw"
	"virelai/infer"
	"virelai/tabapp"
	"virelai/theme"
	"virelai/vi"
	"virelai/widgets"
)

const (
	viewTitle = "LLM Infer"
	viewW     = 560
	viewH     = 340

	// viewLogMax bounds the on-window log; older lines scroll off.
	viewLogMax = 12
)

var (
	openViewWindow = tabapp.Init    // seam var, the host-test convention
	viewConsole    = vi.ConsoleLine // the window path's own serial writes
)

// viewHost mirrors the journey's console output into the view's log and
// notes when the process's one compute session has been taken. Every
// other call passes through to the real host unchanged.
type viewHost struct {
	inner  host
	v      *inferView
	opened bool
}

func (w *viewHost) ConsoleLine(s string) {
	w.inner.ConsoleLine(s)
	w.v.say(s)
	w.v.repaint()
}

func (w *viewHost) MkdirShare(p string) error               { return w.inner.MkdirShare(p) }
func (w *viewHost) WriteShareFile(p string, b []byte) error { return w.inner.WriteShareFile(p, b) }
func (w *viewHost) FileExists(p string) bool                { return w.inner.FileExists(p) }
func (w *viewHost) OpenRead(p string) (io.ReadCloser, error) {
	return w.inner.OpenRead(p)
}
func (w *viewHost) OpenComp() (infer.Dev, error) {
	d, err := w.inner.OpenComp()
	if err == nil {
		w.opened = true
	}
	return d, err
}
func (w *viewHost) Nanos() int64         { return w.inner.Nanos() }
func (w *viewHost) SelfMemBytes() uint64 { return w.inner.SelfMemBytes() }

// inferView is the window's state.
type inferView struct {
	ta    *tabapp.TabApp
	h     *viewHost
	lines []string
	busy  bool
	// armed is the last journey the person armed ("", "stub" or
	// "model"); modelPath the weights a model journey runs with (""
	// keeps the card default). The M195e envelope round-trips both.
	armed     string
	modelPath string
}

func newInferView(ta *tabapp.TabApp, inner host) *inferView {
	v := &inferView{ta: ta}
	v.h = &viewHost{inner: inner, v: v}
	v.say("goinfer: window open")
	v.say("goinfer: 's' Stub run (no weights needed) · 'm' Model run (" + modelPath + ")")
	v.say("goinfer: one compute session per process — a run spends it")
	return v
}

// say appends a line to the bounded on-window log.
func (v *inferView) say(s string) {
	v.lines = append(v.lines, s)
	if len(v.lines) > viewLogMax {
		v.lines = v.lines[len(v.lines)-viewLogMax:]
	}
}

// journey runs the named journey through the view's mirrored host. A run
// that consumed the compute session is the last one this process can do —
// the window says so instead of letting the next press die on the kernel's
// refusal.
func (v *inferView) journey(name string) {
	if v.busy {
		return
	}
	if v.h.opened {
		v.say("goinfer: session spent — close and relaunch GOINFER.ELF to run again")
		v.repaint()
		return
	}
	v.busy = true
	v.armed = name
	args := []string{name}
	if name == "model" && v.modelPath != "" {
		args = append(args, v.modelPath)
	}
	_ = run(v.h, args)
	v.busy = false
	if v.h.opened {
		v.say("goinfer: session spent — close and relaunch GOINFER.ELF to run again")
	}
	v.repaint()
}

// key applies one key event; returns whether the window should close.
func (v *inferView) key(ev vi.Event) bool {
	if ev.Arg0 == 0x29 || ev.Arg1 == 0x1b {
		return true
	}
	switch ev.Arg1 {
	case 's', 'S':
		v.journey("stub")
	case 'm', 'M':
		v.journey("model")
	}
	return false
}

// click applies a mouse-down point; a hit on a journey button runs it.
func (v *inferView) click(x, y int) {
	if b := v.stubBtn(); b.HitTest(x, y) {
		v.journey("stub")
	}
	if b := v.modelBtn(); b.HitTest(x, y) {
		v.journey("model")
	}
}

func (v *inferView) stubBtn() widgets.Button {
	return widgets.Button{R: v.scale(widgets.Rect{X: 8, Y: 44, W: 96, H: 24}), Label: "Run stub"}
}

func (v *inferView) modelBtn() widgets.Button {
	return widgets.Button{R: v.scale(widgets.Rect{X: 112, Y: 44, W: 110, H: 24}), Label: "Run model"}
}

// scale maps a native rect into the live canvas, the demo's pattern.
func (v *inferView) scale(r widgets.Rect) widgets.Rect {
	s := v.ta.Layout(tabapp.Rect{X: r.X, Y: r.Y, W: r.W, H: r.H}, viewW, viewH)
	return widgets.Rect{X: s.X, Y: s.Y, W: s.W, H: s.H}
}

func (v *inferView) draw() {
	cv := v.ta.Canvas()
	tok := theme.Current
	cv.FillRect(widgets.Rect{W: int(v.ta.W), H: int(v.ta.H)}, uidraw.Opaque(tabapp.FillRGB()))
	(&widgets.Text{
		R:     v.scale(widgets.Rect{X: 8, Y: 8, W: viewW - 16, H: 18}),
		Label: viewTitle,
		Fg:    tok.Text,
		Bg:    tok.Surface,
	}).Draw(cv)
	(&widgets.Text{
		R:     v.scale(widgets.Rect{X: 8, Y: 26, W: viewW - 16, H: 14}),
		Label: "GPU compute journeys — receipts land in " + receiptTSV,
		Fg:    tok.Muted,
		Bg:    tok.Surface,
	}).Draw(cv)
	sb, mb := v.stubBtn(), v.modelBtn()
	sb.Disabled, mb.Disabled = v.busy || v.h.opened, v.busy || v.h.opened
	sb.Draw(cv)
	mb.Draw(cv)
	(&widgets.List{
		R:     v.scale(widgets.Rect{X: 8, Y: 76, W: viewW - 16, H: viewH - 84}),
		Items: v.lines,
		RowH:  18,
		Sel:   -1,
		Fg:    tok.Ink,
		Bg:    tok.ChromeBg,
		SelBg: tok.Selection,
	}).Draw(cv)
}

// repaint redraws and presents — called from the ConsoleLine mirror so a
// running journey narrates itself live.
func (v *inferView) repaint() {
	v.draw()
	v.ta.Present()
}

// runView is the bare-launch presentation: open, paint, then the event
// loop — keys/buttons run journeys, Esc/WIN_CLOSE exits. Ends on the same
// `goinfer: done` anchor the console journeys print.
func runView() int {
	defer viewConsole("goinfer: done")
	// The M195e state hooks, the GOSNAP shape: SaveState reads the live
	// view (the closure captures the variable, so an ask always
	// serializes the current armed journey and log tail); RestoreState
	// fires inside tabapp.Init before the view exists, so it only
	// decodes — the apply lands once the window's intro lines exist.
	var v *inferView
	var restored inferState
	var restoredOK bool
	ta := openViewWindow(tabapp.Config{
		Name: appName, Title: viewTitle, X: 48, Y: 40, W: viewW, H: viewH,
		StateVersion: stateAppVer,
		SaveState: func() []byte {
			if v == nil {
				return nil
			}
			return v.encodeState()
		},
		RestoreState: func(b []byte) { restored, restoredOK = decodeState(b) },
	})
	if ta == nil {
		viewConsole("goinfer: window refused — journeys: GOINFER.ELF stub | model [path]")
		return 1
	}
	viewConsole("goinfer: view open id=" + vi.Itoa64(int64(ta.Win)))
	v = newInferView(ta, guestHost{})
	if line := v.applyRestored(restored, restoredOK); line != "" {
		viewConsole(line)
	}
	v.draw()
	ta.Present()
	for {
		// M195e: the seat's save ask (snapshot, tab close) is a mailbox
		// notice nothing wakes; answer it once per pass, the same
		// discipline PollNav keeps in the other hooked apps.
		ta.PollStateAsk()
		ev, r, ok := vi.PollEventRaw()
		if !ok {
			if r < 0 {
				return 0
			}
			vi.Sleep(1)
			continue
		}
		switch ta.Dispatch(ev) {
		case tabapp.ActionClosed:
			ta.Close()
			return 0
		case tabapp.ActionResized:
			v.repaint()
		case tabapp.ActionNone:
			switch ev.Kind {
			case vi.EvKeyDown:
				if v.key(ev) {
					ta.Close()
					return 0
				}
			case vi.EvMouseDown:
				if ev.Flags&vi.BtnLeft != 0 {
					v.click(int(ev.Arg0), int(ev.Arg1))
				}
			}
		}
	}
}

// --- the M195e (#3310) appver-1 state envelope -----------------------------
//
// The windowed face's half of the M125a (#2586) seat-mediated
// save/restore protocol — the same shape GOSNAP's M195c envelope keeps:
// the seat stores the blob verbatim on the tab's session row (kind-20
// save, kind-21 restore) and hands it back on a reopen, so a
// close/reopen shows the last session's log tail and remembers which
// journey was armed last. The seat owns the lifecycle; the face owns
// the format:
//
//	u8  fmtVer (=stateFmtVer) — also Config's StateVersion, so the
//	    envelope's appver and the payload agree (a stale serialization
//	    is refused twice over)
//	u8  flags (=0, reserved)
//	u8  journey — 0 none / 1 stub / 2 model, the last journey the
//	    person armed
//	u16 pathLen | path — the weights a model journey runs with;
//	    empty when the card default stands
//	u8  nLines | nLines × (u16 len | bytes) — the on-window log tail
//
// Restore NEVER runs a journey: a reopened window shows the previous
// session's tail and carries the armed journey and weights path
// forward, but a run stays the person's click — a fresh process owns a
// fresh compute session, and replaying a run would spend it without
// being asked.
//
// Decode is total: a bad length, foreign fmtVer, nonzero flags, an
// unknown journey tag, a non-canonical path, truncation or trailing
// bytes refuse the WHOLE blob — a torn handoff never lands half-applied.

const (
	stateFmtVer = 1
	stateAppVer = 1

	// markerState prefixes the M195e envelope report: "restored
	// journey=" / "refused <why>" — the vocabulary NOTE/GOSNAP/GOSH
	// print for their M125a blobs.
	markerState = "goinfer: state "

	// statePathMax is the kernel's path bound (file_table.max_path_len):
	// a stored path longer than anything the kernel could open is a
	// foreign blob, refused before it is trusted. stateLineMax bounds
	// one serialized log line the same way.
	statePathMax = 512
	stateLineMax = 1024
)

// inferState is one decoded session blob: journey is ""/"stub"/"model",
// path the armed weights ("" = the card default), tail the on-window
// log's last lines.
type inferState struct {
	journey string
	path    string
	tail    []string
}

func appendU16(out []byte, v int) []byte { return append(out, byte(v), byte(v>>8)) }

func readU16(b []byte) int { return int(b[0]) | int(b[1])<<8 }

// journeyCode maps the armed name to its wire tag; an unrecognized name
// (none yet) is 0.
func journeyCode(name string) byte {
	switch name {
	case "stub":
		return 1
	case "model":
		return 2
	}
	return 0
}

// journeyName is journeyCode's inverse; ok=false on an unknown tag —
// a corrupt blob, never a journey.
func journeyName(code byte) (string, bool) {
	switch code {
	case 0:
		return "", true
	case 1:
		return "stub", true
	case 2:
		return "model", true
	}
	return "", false
}

// canonPath reports whether p is a canonical /-rooted path — the only
// shape a stored weights path may carry. A blob field holding "..", a
// doubled slash or a relative name is a corrupt blob, refused whole
// rather than steering the next model run at a path encodeState could
// never have written.
func canonPath(p string) bool {
	if p == "/" {
		return true
	}
	if !strings.HasPrefix(p, "/") || strings.HasSuffix(p, "/") {
		return false
	}
	for _, comp := range strings.Split(p[1:], "/") {
		if comp == "" || comp == "." || comp == ".." {
			return false
		}
	}
	return true
}

// encodeState serializes the face's durable state for the seat's save
// ask. Bounded by construction: the log already caps at viewLogMax
// lines and a path is a share name — the blob stays nowhere near the
// seat's 64 KiB envelope ceiling.
func (v *inferView) encodeState() []byte {
	out := []byte{stateFmtVer, 0, journeyCode(v.armed)}
	out = appendU16(out, len(v.modelPath))
	out = append(out, v.modelPath...)
	out = append(out, byte(len(v.lines)))
	for _, l := range v.lines {
		if len(l) > stateLineMax {
			l = l[:stateLineMax]
		}
		out = appendU16(out, len(l))
		out = append(out, l...)
	}
	return out
}

// decodeState is encodeState's strict inverse — ok=false means stale or
// foreign, refused whole by the caller, never half-applied. Every
// declared length is re-verified and the blob must be consumed exactly:
// the seat carries bytes, it does not vouch for them.
func decodeState(b []byte) (inferState, bool) {
	var st inferState
	if len(b) < 5 || b[0] != stateFmtVer || b[1] != 0 {
		return st, false
	}
	journey, ok := journeyName(b[2])
	if !ok {
		return st, false
	}
	o := 3
	pl := readU16(b[o : o+2])
	o += 2
	if pl > statePathMax || len(b)-o < pl+1 {
		return st, false
	}
	path := string(b[o : o+pl])
	o += pl
	if pl != 0 && !canonPath(path) {
		return st, false
	}
	st.path = path
	n := int(b[o])
	o++
	for i := 0; i < n; i++ {
		if len(b)-o < 2 {
			return st, false
		}
		ll := readU16(b[o : o+2])
		o += 2
		if ll > stateLineMax || len(b)-o < ll {
			return st, false
		}
		st.tail = append(st.tail, string(b[o:o+ll]))
		o += ll
	}
	if o != len(b) {
		return st, false
	}
	st.journey = journey
	return st, true
}

// applyRestored lands a decoded envelope on the live view and returns
// the marker to print — "" when the seat handed nothing over. The
// stored tail replays into the on-window log after the intro lines and
// the armed journey and weights path carry forward (a model press
// re-arms the same weights) — but nothing runs: a journey stays the
// person's click. A foreign appver or a torn blob is refused by name.
func (v *inferView) applyRestored(st inferState, ok bool) string {
	if v.ta == nil || !v.ta.Restored {
		return ""
	}
	if v.ta.RestoredVersion != stateAppVer {
		return markerState + "refused ver=" + vi.Itoa64(int64(v.ta.RestoredVersion))
	}
	if !ok {
		return markerState + "refused bad"
	}
	for _, l := range st.tail {
		v.say(l)
	}
	v.armed = st.journey
	if st.path != "" {
		v.modelPath = st.path
	}
	journey := st.journey
	if journey == "" {
		journey = "-"
	}
	line := markerState + "restored journey=" + journey
	if st.path != "" {
		line += " path=" + st.path
	}
	return line + " tail=" + vi.Itoa64(int64(len(st.tail)))
}
