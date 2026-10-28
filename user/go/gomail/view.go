package main

// view.go — M208a (issue #3822): the no-argument face of GOMAIL — a
// tabapp window with three panes (mailboxes, the selected box's headers,
// the selected message's body) plus a status line. Everything the panes
// paint comes out of the MAIL/ cache the verbs already write — a bare
// launch never dials. The 'f' Fetch key runs the existing fetch path
// (runFetch) for the selected box, and the markers/refusals it emits
// land in the status line by name through the sayTap seam.
//
// Panes and model are host-testable (view_test.go drives them over the
// fake share): key/stroke handling mutates the view, drawing only reads
// it. Serial markers keep the CLI's own spellings where the fact is the
// same — 'gomail: box', 'gomail: hdr', 'gomail: body', 'gomail: text'
// mean the same rows a console leg prints.
//
// Keys: Up/Down or k/j move the selection inside the focused pane;
// Left/Right, h/l or Tab cycle the pane focus; 'f' fetches; 'q' quits;
// the seat's rail close stays the WIN_CLOSE path. A click picks a box or
// header row directly. The letter spellings ride alongside the arrows
// because the seat's own chord palette can claim unmodified arrows —
// the letters always reach the app.

import (
	"strings"

	uidraw "virelai/draw"
	"virelai/tabapp"
	"virelai/theme"
	"virelai/vi"
	"virelai/widgets"
)

const (
	viewNatW = 640
	viewNatH = 400

	viewRailW   = 150 // the mailboxes column
	viewHeadH   = 20  // the title strip
	viewStatusH = 18  // the status strip at the foot
	viewRowH    = 14  // one list row (the goirc rail arithmetic)

	paneBoxes = iota
	paneHdrs
	paneBody
	paneCount
)

// USB HID usages (the same table goclock/goirc pin).
const (
	keyEnter = 0x28
	keyTab   = 0x2b
	keyPgUp  = 0x4b
	keyPgDn  = 0x4e
	keyRight = 0x4f
	keyLeft  = 0x50
	keyDown  = 0x51
	keyUp    = 0x52
)

// mailView is the window's model — every field the draw pass reads and
// the key/mouse handlers mutate. The cache is the only data source until
// fetch() runs the wire path.
type mailView struct {
	cfg Config

	boxes  []string    // the BOXES corpus
	boxSel int         // picked mailbox
	hdrs   []cachedHdr // the picked box's envelope rows
	hdrSel int         // picked envelope
	body   []string    // the picked message's rendered lines
	bodyAt int         // scroll offset into body
	pane   int         // focused pane
	status string      // the status strip's line — marker text by name

	// fetchFn is the wire seam: runFetch on the guest, a stub in tests
	// so the view model runs without a socket.
	fetchFn func(cfg Config, box string, seqs []int) error
}

func (v *mailView) curBox() string {
	if v.boxSel < 0 || v.boxSel >= len(v.boxes) {
		return ""
	}
	return v.boxes[v.boxSel]
}

func (v *mailView) curHdr() (cachedHdr, bool) {
	if v.hdrSel < 0 || v.hdrSel >= len(v.hdrs) {
		return cachedHdr{}, false
	}
	return v.hdrs[v.hdrSel], true
}

// load reads the whole view out of the cache: the remembered folder
// list, then the selected box. The remembered `use` (resolveBox's own
// rule) picks the initial box; absent that, INBOX leads the list.
func (v *mailView) load() {
	// A reload keeps the box the user is on — captured BEFORE the list
	// repaints; the first load has no pick yet and falls to the
	// remembered `use` (resolveBox's own rule), INBOX failing that.
	want := v.curBox()
	if want == "" {
		want = resolveBox("")
	}
	v.boxes = cacheLoadBoxes()
	for _, b := range v.boxes {
		say("gomail: box " + b)
	}
	v.boxSel = 0
	for i, b := range v.boxes {
		if b == want {
			v.boxSel = i
			break
		}
	}
	v.loadBox(false)
}

// loadBox repaints the picked box's header list out of the cache and the
// picked header's body. mark reports the selection marker — the initial
// load stays quiet (the box rows just printed carry the same fact).
func (v *mailView) loadBox(mark bool) {
	v.hdrs = cacheHdrsFor(v.curBox())
	v.hdrSel = 0
	if mark {
		say("gomail: sel " + v.curBox())
	}
	for _, h := range v.hdrs {
		say("gomail: hdr " + itoa(int64(h.seq)) + " " + h.from + ": " + h.subject)
	}
	v.loadBody()
}

// loadBody repaints the message pane for the picked envelope. A cached
// text/plain body renders its lines; any other remembered mime gets the
// named not-rendered row (HTML rendering is out of scope); a body the
// cache does not hold is a cache-miss, never invented text.
func (v *mailView) loadBody() {
	v.body = nil
	v.bodyAt = 0
	h, ok := v.curHdr()
	if !ok {
		return
	}
	rec, hit := cacheGetBody(v.curBox(), h.seq)
	if !hit {
		miss := "gomail: cache-miss " + v.curBox() + " " + itoa(int64(h.seq))
		say(miss)
		v.status = miss
		v.body = []string{"(no cached body — f fetches)"}
		return
	}
	if rec.mime != "text/plain" {
		say("gomail: body " + itoa(int64(h.seq)) + " " + rec.mime + " bytes=" + itoa(int64(len(rec.data))))
		nr := "gomail: not-rendered " + rec.mime + " " + itoa(int64(h.seq))
		say(nr)
		v.status = nr
		v.body = []string{rec.mime + " body — not rendered"}
		return
	}
	printBodyRows(h.seq, rec.mime, false, rec.data)
	text := strings.TrimRight(string(rec.data), "\r\n")
	v.body = strings.Split(text, "\n")
	for i := range v.body {
		v.body[i] = strings.TrimRight(v.body[i], "\r")
	}
	if len(v.body) > bodyLineMax {
		v.body = v.body[:bodyLineMax]
	}
}

// move steps the focused pane's selection by d (clamped): the box list,
// the header list, or the body pane's scroll. A moved selection repaints
// what it points at — the pick IS the open.
func (v *mailView) move(d int) {
	switch v.pane {
	case paneBoxes:
		if len(v.boxes) == 0 {
			return
		}
		sel := v.boxSel + d
		if sel < 0 {
			sel = 0
		}
		if sel >= len(v.boxes) {
			sel = len(v.boxes) - 1
		}
		if sel != v.boxSel {
			v.boxSel = sel
			v.loadBox(true)
		}
	case paneHdrs:
		if len(v.hdrs) == 0 {
			return
		}
		sel := v.hdrSel + d
		if sel < 0 {
			sel = 0
		}
		if sel >= len(v.hdrs) {
			sel = len(v.hdrs) - 1
		}
		if sel != v.hdrSel {
			v.hdrSel = sel
			v.loadBody()
		}
	case paneBody:
		v.bodyAt += d
		if v.bodyAt < 0 {
			v.bodyAt = 0
		}
		if max := len(v.body) - 1; v.bodyAt > max {
			v.bodyAt = max
		}
		if v.bodyAt < 0 {
			v.bodyAt = 0
		}
	}
}

// setPane moves the focus ring. Entering the body pane from the header
// list leaves the picked message open — the pane is its reader.
func (v *mailView) setPane(p int) {
	if p < 0 {
		p = 0
	}
	if p >= paneCount {
		p = paneCount - 1
	}
	v.pane = p
}

// fetch runs the existing fetch path for the picked box — the same
// runFetch the 'fetch' verb drives, seqs bound to the picked envelope so
// a body read joins the headers when one is selected. Every marker the
// wire path emits also lands in the status line by name via sayTap; a
// refusal or a dead transport names itself there instead of killing the
// window.
func (v *mailView) fetch() {
	box := v.curBox()
	if box == "" {
		v.status = "gomail: cache-miss boxes"
		say("gomail: cache-miss boxes")
		return
	}
	if v.cfg.Host == "" {
		v.status = "gomail: account missing host"
		say(v.status)
		return
	}
	var seqs []int
	if h, ok := v.curHdr(); ok {
		seqs = []int{h.seq}
	}
	say("gomail: fetch " + box)
	last := ""
	sayTap = func(s string) { last = s }
	err := v.fetchFn(v.cfg, box, seqs)
	sayTap = nil
	if err != nil {
		s := err.Error()
		if !strings.HasPrefix(s, "gomail:") {
			s = "gomail: error " + s
		}
		say(s)
		v.status = s
		return
	}
	if last == "" {
		last = "gomail: fetch " + box + " ok"
	}
	// The wire just proved a newer corpus — repaint it, then mark the
	// fetch's end AFTER the fresh rows printed so the marker orders
	// last. The fetch's own status is assigned after the repaint so a
	// reloaded pane's cache-miss cannot clobber it.
	v.load()
	v.status = last
	say("gomail: fetch done")
}

// fetchRun is the guest's fetchFn — the unmodified fetch path.
func fetchRun(cfg Config, box string, seqs []int) error {
	return runFetch(cfg, box, seqs, true)
}

// --- the app shell ------------------------------------------------------------

type mailUI struct {
	ta    *tabapp.TabApp
	v     mailView
	dirty bool
	quit  bool
}

func (u *mailUI) layout() (head, rail, hdrs, body, status widgets.Rect) {
	w, h := int(u.ta.W), int(u.ta.H)
	head = widgets.Rect{X: 0, Y: 0, W: w, H: viewHeadH}
	rail = widgets.Rect{X: 0, Y: viewHeadH, W: viewRailW, H: h - viewHeadH - viewStatusH}
	rest := h - viewHeadH - viewStatusH
	hh := rest / 2
	hdrs = widgets.Rect{X: viewRailW, Y: viewHeadH, W: w - viewRailW, H: hh}
	body = widgets.Rect{X: viewRailW, Y: viewHeadH + hh, W: w - viewRailW, H: rest - hh}
	status = widgets.Rect{X: 0, Y: h - viewStatusH, W: w, H: viewStatusH}
	return
}

// key routes one EvKeyDown. True means the frame changed.
func (u *mailUI) key(ev vi.Event) bool {
	if ev.Flags&vi.ModCtrl != 0 {
		return false
	}
	v := &u.v
	switch ev.Arg0 {
	case keyLeft:
		v.setPane(v.pane - 1)
		return true
	case keyRight, keyTab:
		v.setPane(v.pane + 1)
		return true
	case keyUp:
		v.move(-1)
		return true
	case keyDown:
		v.move(1)
		return true
	case keyEnter:
		if v.pane < paneCount-1 {
			v.setPane(v.pane + 1)
			return true
		}
		return false
	case keyPgUp:
		v.setPane(paneBody)
		v.move(-8)
		return true
	case keyPgDn:
		v.setPane(paneBody)
		v.move(8)
		return true
	}
	switch ch := byte(ev.Arg1); ch {
	case 'q', 'Q':
		u.quit = true
		return false
	case 'f', 'F':
		v.fetch()
		return true
	case 'j', 'J':
		v.move(1)
		return true
	case 'k', 'K':
		v.move(-1)
		return true
	case 'h', 'H':
		v.setPane(v.pane - 1)
		return true
	case 'l', 'L':
		v.setPane(v.pane + 1)
		return true
	}
	return false
}

func (u *mailUI) click(x, y int) bool {
	v := &u.v
	_, rail, hdrs, _, _ := u.layout()
	if rail.Contains(x, y) {
		l := widgets.List{R: rail, RowH: viewRowH, Items: v.boxes}
		if i := l.ItemAt(x, y); i >= 0 && i != v.boxSel {
			v.boxSel = i
			v.loadBox(true)
		}
		v.setPane(paneBoxes)
		return true
	}
	if hdrs.Contains(x, y) {
		l := widgets.List{R: hdrs, RowH: viewRowH, Items: hdrLabels(v.hdrs)}
		if i := l.ItemAt(x, y); i >= 0 && i != v.hdrSel {
			v.hdrSel = i
			v.loadBody()
		}
		v.setPane(paneHdrs)
		return true
	}
	return false
}

func (u *mailUI) handleEvent(ev vi.Event) {
	switch ev.Kind {
	case vi.EvKeyDown:
		if u.key(ev) {
			u.dirty = true
		}
	case vi.EvMouseDown:
		if ev.Flags&vi.BtnLeft != 0 && u.click(int(ev.Arg0), int(ev.Arg1)) {
			u.dirty = true
		}
	}
}

// hdrLabels renders the envelope list's rows — seq + sender + subject,
// the same shape the 'gomail: hdr' marker spells.
func hdrLabels(hdrs []cachedHdr) []string {
	out := make([]string, 0, len(hdrs))
	for _, h := range hdrs {
		out = append(out, itoa(int64(h.seq))+" "+h.from+": "+h.subject)
	}
	return out
}

func (u *mailUI) draw() {
	cv := u.ta.Canvas()
	tok := theme.Current
	head, rail, hdrsR, bodyR, statusR := u.layout()
	w, h := int(u.ta.W), int(u.ta.H)
	cv.FillRect(widgets.Rect{W: w, H: h}, uidraw.Opaque(tok.Bg))

	v := &u.v
	// Title strip: the app's name and the picked box.
	cv.FillRect(head, uidraw.Opaque(tok.ChromeBg))
	title := "Mail"
	if b := v.curBox(); b != "" {
		title += " — " + b
	}
	widgets.DrawText(cv, head.Inset(tok.PadXS+2), title, uidraw.Opaque(tok.Text))

	// The mailboxes rail.
	bl := widgets.List{
		R: rail, RowH: viewRowH, Items: v.boxes, Sel: v.boxSel,
		Focused: v.pane == paneBoxes,
		Fg:      tok.Ink, Bg: tok.ChromeBg, SelBg: tok.Selection,
	}
	bl.Draw(cv)
	if len(v.boxes) == 0 {
		widgets.DrawText(cv, rail.Inset(tok.PadXS+2), "(no mailboxes — f fetches)", uidraw.Opaque(tok.Muted))
	}

	// The header list.
	cv.FillRect(hdrsR, uidraw.Opaque(tok.Surface))
	hl := widgets.List{
		R: hdrsR, RowH: viewRowH, Items: hdrLabels(v.hdrs), Sel: v.hdrSel,
		Focused: v.pane == paneHdrs,
		Fg:      tok.Text, Bg: tok.Surface, SelBg: tok.Selection,
	}
	hl.Draw(cv)
	if len(v.hdrs) == 0 {
		hint := "(no cached headers"
		if v.curBox() != "" {
			hint += " — f fetches"
		}
		widgets.DrawText(cv, hdrsR.Inset(tok.PadXS+2), hint+")", uidraw.Opaque(tok.Muted))
	}

	// The message pane.
	cv.FillRect(bodyR, uidraw.Opaque(tok.Bg))
	rows := bodyR.H / viewRowH
	for i := 0; i < rows && i+v.bodyAt < len(v.body); i++ {
		line := v.body[i+v.bodyAt]
		r := widgets.Rect{X: bodyR.X, Y: bodyR.Y + i*viewRowH, W: bodyR.W, H: viewRowH}
		widgets.DrawText(cv, r.Inset(tok.PadXS+2), line, uidraw.Opaque(tok.Text))
	}
	if len(v.body) == 0 {
		hint := "(no message)"
		if len(v.hdrs) > 0 {
			hint = "(no cached body — f fetches)"
		}
		widgets.DrawText(cv, bodyR.Inset(tok.PadXS+2), hint, uidraw.Opaque(tok.Muted))
	}

	// The status strip — the last named line: a fetch marker or refusal,
	// a cache-miss, or the navigation hint when nothing has spoken yet.
	cv.FillRect(statusR, uidraw.Opaque(tok.ChromeBg))
	status := v.status
	if status == "" {
		status = "tab/left/right panes · up/down select · f fetch · q quit"
	}
	widgets.DrawText(cv, statusR.Inset(tok.PadXS+2), status, uidraw.Opaque(tok.InkMuted))
}

// runView is the no-argument entry (M208a): window + cached corpus +
// the event loop. cfg is the resolved account Fetch dials with — the
// panes themselves never touch it.
func runView(cfg Config) {
	u := &mailUI{}
	u.v.cfg = cfg
	u.v.fetchFn = fetchRun
	ta := tabapp.Init(tabapp.Config{
		Name: appName,
		// The declare title is the executable's own name — the seat's
		// reopen LIFO re-execs it bare, which is exactly this no-arg
		// path (the GOIRC/GOPAINT discipline).
		Title: appName,
		X:     64, Y: 48, W: viewNatW, H: viewNatH,
	})
	if ta == nil {
		vi.ConsoleLine("gomail: error open -1")
		vi.Exit(exitError)
	}
	u.ta = ta
	vi.ConsoleLine("gomail: open id=" + vi.Itoa64(int64(ta.Win)))
	if ta.TabAware {
		vi.ConsoleLine("gomail: declare accepted")
		if !ta.SetTabTitle("Mail") {
			_ = vi.WinSetTitle(ta.Win, "Mail")
		}
	} else {
		vi.ConsoleLine("gomail: declare refused")
	}
	u.v.load()
	u.draw()
	ta.Present()
	// Printed AFTER the first frame reached the scanout — the gate's
	// chord anchor proves the panes were painted, not queued.
	vi.ConsoleLine("gomail: window")

	for !u.quit {
		ev, r, ok := vi.PollEventRaw()
		if !ok {
			if r < 0 {
				break
			}
			vi.Sleep(1)
			continue
		}
		switch ta.Dispatch(ev) {
		case tabapp.ActionClosed:
			u.quit = true
		case tabapp.ActionResized:
			u.dirty = true
		case tabapp.ActionNone:
			u.handleEvent(ev)
		}
		if u.dirty {
			u.draw()
			ta.Present()
			u.dirty = false
		}
	}
	vi.ConsoleLine("gomail: closed")
	ta.CloseAndExit(0)
}
