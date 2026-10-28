// The seat-facing verbs: ui.windows, ui.screenshot, ui.input and
// ui.geometry. All four resolve the window strip through the daemon's
// own lazily bound kernel window (M97g-F2 binds kind-16/29/30 requests
// by window id, so an authenticated query requires a window the daemon
// provably owns — the ownership challenge echoes through its title).
// The bind is never declared to the seat, so it stays off the strip it
// reads (D5); it is opened at the kernel's resize floor and hidden at
// once — an undersized window is outside the compositor's tested
// geometry (observed: a 1x1 open faulted the kernel's paint path), and
// the daemon's plumbing has no pixels to show.
package agentverbs

import (
	"strings"

	"virelai/vi"
)

// bindWindow lazily opens the daemon's kernel window — the strip query
// and every kind-29/30 frame ride it. "" on success, else the refusal.
func (d *Dispatcher) bindWindow() string {
	if d.agentWindow < 0 {
		id, raw := d.cfg.Seams.WinOpen(0, 0, 128, 64)
		if id < 0 || raw < 0 {
			return "seat_unsupported"
		}
		_ = d.cfg.Seams.WinHide(id, false)
		d.agentWindow = id
	}
	return ""
}

// stripRows queries the window strip over kind 16 against the bound
// window — the seat's unsupported verdict is named, silence is
// seat_timeout.
func (d *Dispatcher) stripRows() ([]vi.WmWindowRow, string) {
	rows, err := d.cfg.Seams.WmWindows(uint32(d.agentWindow), d.cfg.Self)
	if err != nil {
		if err == vi.ErrSeatUnsupported {
			return nil, "seat_unsupported"
		}
		return nil, "seat_timeout"
	}
	return rows, ""
}

// --- ui.windows -----------------------------------------------------------------

// UIWindows answers ui.windows: the seat's focused index and the strip
// rows. No registered seat is a normal answer, not a refusal.
func (d *Dispatcher) UIWindows() (string, string) {
	if d.cfg.Seams.SeatPid() <= 0 {
		return `{"seat":"none","focused":null,"windows":[]}`, ""
	}
	if code := d.bindWindow(); code != "" {
		return "", code
	}
	rows, code := d.stripRows()
	if code != "" {
		return "", code
	}
	var sb strings.Builder
	sb.WriteString(`{"seat":"gotabwm","focused":`)
	focused := -1
	for i := range rows {
		if rows[i].Focused {
			focused = rows[i].Index
		}
	}
	if focused >= 0 {
		sb.WriteString(vi.Itoa64(int64(focused)))
	} else {
		sb.WriteString("null")
	}
	sb.WriteString(`,"windows":[`)
	for i := range rows {
		if i > 0 {
			sb.WriteByte(',')
		}
		w := &rows[i]
		sb.WriteString(`{"index":`)
		sb.WriteString(vi.Itoa64(int64(w.Index)))
		sb.WriteString(`,"id":`)
		sb.WriteString(vi.Itoa64(int64(w.ID)))
		sb.WriteString(`,"title":`)
		jwriteStr(&sb, w.Title)
		sb.WriteString(`,"focused":`)
		sb.WriteString(jbool(w.Focused))
		sb.WriteString(`,"pinned":`)
		sb.WriteString(jbool(w.Pinned))
		sb.WriteString(`,"frozen":`)
		sb.WriteString(jbool(w.Frozen))
		sb.WriteByte('}')
	}
	sb.WriteString(`]}`)
	return sb.String(), ""
}

// --- ui.screenshot ---------------------------------------------------------

// The screenshot is a bounded RGB565 little-endian read of the scanout
// through slot 88 (vi.ScanoutRead — the kernel's bounded read-only copy
// of the framebuffer; the writable bind stays seat-only). The default
// scale 8 is the ThumbW x ThumbH thumbnail: it keeps the 1280x720
// frame's exact 16:9 at an 8:1 stride, so one sampled row band per
// output row suffices and the payload is fixed at ThumbW*ThumbH*2 =
// 28,800 bytes before chunking.
//
// Card #2977 (ADR 0044 D3 amendment): an optional `scale` argument —
// 8 is the thumbnail, 1 is the full 1280x720 frame. scale=1 reads the
// scanout in FullShotRows-row bands (the largest band one slot-88 call
// serves inside ScanoutReadMax, and 720 is a multiple of it), converts
// each band and streams it through the same emit path — 60 bands,
// 1,843,200 bytes. A full-resolution read is rate-bound to one per
// FullShotMinNs per session; a second request inside the bound answers
// rate_limited. Any other scale value is bad_request.
const (
	ThumbW = 160
	ThumbH = 90
	// FullShotRows is the scale=1 band height: 12 rows of 1280*4 =
	// 61,440 bytes fit ScanoutReadMax (64 KiB); 13 rows would not.
	FullShotRows = 12
	// FullShotMinNs is the per-session bound on scale=1 captures: one
	// full-resolution read per 2 s.
	FullShotMinNs = int64(2) * 1e9
)

// UIScreenshot answers ui.screenshot. The seat is the semantic anchor:
// "screenshot" means the desktop the human sees, so a daemon with no
// registered seat refuses seat_absent by name before touching the
// scanout (the card's named-refusal bar). Pixels arrive as stream
// chunks through s.Emit; the result carries only the shape.
func (d *Dispatcher) UIScreenshot(req *Request, s *Session) (string, string) {
	scale, present, ok := req.argInt("scale", 1, intMax)
	if present && (!ok || (scale != 1 && scale != 8)) {
		return "", "bad_request"
	}
	if !present {
		scale = 8
	}
	if scale == 1 && d.cfg.Seams.Nanos() < s.NextFullShotNs {
		// The bound refuses before the seat query — a rate-limited
		// request pays no syscall and consumes no further budget.
		return "", "rate_limited"
	}
	if d.cfg.Seams.SeatPid() <= 0 {
		return "", "seat_absent"
	}
	if scale == 1 {
		return d.uiScreenshotFull(s)
	}
	var row [vi.ScanoutWidth * vi.ScanoutBPP]byte
	out := make([]byte, 0, ThumbW*ThumbH*2)
	stride := vi.ScanoutWidth / ThumbW // 8 — the geometry is fixed 16:9
	for ty := 0; ty < ThumbH; ty++ {
		sy := uint32(ty * (vi.ScanoutHeight / ThumbH))
		r := d.cfg.Seams.Scanout(0, sy, uint32(vi.ScanoutWidth), 1, row[:])
		if r < 0 {
			return shotErr(r)
		}
		for tx := 0; tx < ThumbW; tx++ {
			px := row[tx*stride*vi.ScanoutBPP:]
			out = append565(out, px)
		}
	}
	emitChunks(s, out)
	return shotResult(ThumbW, ThumbH), ""
}

// uiScreenshotFull is the scale=1 path: the whole frame in
// FullShotRows-row bands, each converted to RGB565 and streamed as it
// lands — the running payload stays at one band, never the full
// 1.8 MB. The first landed band arms the session's rate bound: a
// request refused on the read itself consumed no budget, so ENXIO
// still answers seat_absent without rate-limiting the retry.
func (d *Dispatcher) uiScreenshotFull(s *Session) (string, string) {
	var band [FullShotRows * vi.ScanoutWidth * vi.ScanoutBPP]byte
	out := make([]byte, 0, FullShotRows*vi.ScanoutWidth*2)
	armed := false
	for y := uint32(0); y < vi.ScanoutHeight; y += FullShotRows {
		r := d.cfg.Seams.Scanout(0, y, uint32(vi.ScanoutWidth), FullShotRows, band[:])
		if r < 0 {
			return shotErr(r)
		}
		if !armed {
			s.NextFullShotNs = d.cfg.Seams.Nanos() + FullShotMinNs
			armed = true
		}
		for px := 0; px < FullShotRows*vi.ScanoutWidth; px++ {
			out = append565(out, band[px*vi.ScanoutBPP:])
		}
		emitChunks(s, out)
		out = out[:0]
	}
	return shotResult(vi.ScanoutWidth, vi.ScanoutHeight), ""
}

// append565 converts one B8G8R8X8 little-endian pixel to RGB565
// little-endian — the single conversion both scales share.
func append565(out []byte, px []byte) []byte {
	v := uint16(px[2]>>3)<<11 | uint16(px[1]>>2)<<5 | uint16(px[0]>>3)
	return append(out, byte(v), byte(v>>8))
}

// shotErr maps a scanout_read result onto the verb's named refusals:
// an unarmed framebuffer reads the same as no seat (nothing to see).
func shotErr(r int64) (string, string) {
	switch vi.ErrnoName(r) {
	case "ENXIO":
		return "", "seat_absent"
	case "ENOSYS":
		return "", "seat_unsupported"
	}
	return ioErr(r)
}

// shotResult renders the ui.screenshot result object: the frame's
// shape and its RGB565 little-endian byte count.
func shotResult(w, h int) string {
	var sb strings.Builder
	sb.WriteString(`{"w":`)
	sb.WriteString(vi.Itoa64(int64(w)))
	sb.WriteString(`,"h":`)
	sb.WriteString(vi.Itoa64(int64(h)))
	sb.WriteString(`,"format":"rgb565le","bytes":`)
	sb.WriteString(vi.Itoa64(int64(w * h * 2)))
	sb.WriteByte('}')
	return sb.String()
}

// --- ui.input -------------------------------------------------------------------

// M139b (#2780): ui.input {win, events} injects input into the focused
// pane over WM_RPC kind 29 (ADR 0007 R2). The parts:
//
//   - `win` is the strip index the request believes is focused — the
//     same integer ui.windows reports as `focused`. The verb re-queries
//     the strip and refuses agent_input_no_focus when it doesn't match;
//     the seat re-checks focus at dispatch anyway, so a race between
//     the two still lands the seat's own no_focus refusal verbatim.
//   - `events` is a bounded batch (<= InputEventsMax) of flat objects:
//     {"type":"key","usage":N,"mods":N?} for a key press, or
//     {"type":"ptr","x":N,"y":N,"buttons":N?} for an absolute pointer
//     sample. mods/buttons default to 0.
//   - One event rides one kind-29 request — the R2 frame carries a
//     single packed title — so the batch is sent sequentially and the
//     first refusal stops it.
//   - The seat's refusal name is the verdict: agent_input_denied and
//     agent_input_no_focus surface verbatim; a bare applied=0 (a legacy
//     seat with no kind-29 handler) is seat_unsupported.
//
// Refuse before touching: the whole batch validates BEFORE the first
// kind-29 frame goes out — a malformed event N never causes events
// 1..N-1 to fire.

// inputEvent is one validated batch element.
type inputEvent struct {
	ptr   bool   // false = key press; true = absolute pointer sample
	bits  uint8  // mods (key) / buttons (ptr)
	usage uint8  // key: HID usage
	x, y  uint16 // ptr: framebuffer coordinates
}

// parseInputEvent validates one events[] element against its per-type
// field set; every value must be a flat integer (or "type", a string).
// Member order carries no meaning — "type" is resolved before the rest.
func parseInputEvent(v Val) (ev inputEvent, ok bool) {
	if v.Kind != KObj {
		return ev, false
	}
	seen := map[string]bool{}
	typeFound := false
	for _, m := range v.Membs {
		if seen[m.Key] {
			return ev, false // a duplicated member is malformed
		}
		seen[m.Key] = true
		if m.Key == "type" {
			if m.Val.Kind != KStr {
				return ev, false
			}
			switch m.Val.Str {
			case "key":
				ev.ptr = false
			case "ptr":
				ev.ptr = true
			default:
				return ev, false
			}
			typeFound = true
		}
	}
	if !typeFound {
		return ev, false
	}
	for _, m := range v.Membs {
		if m.Key == "type" {
			continue
		}
		if m.Val.Kind != KNum {
			return ev, false
		}
		n, ok2 := flatInt(m.Val.Str)
		if !ok2 {
			return ev, false
		}
		switch m.Key {
		case "usage":
			if ev.ptr || n > 0xff {
				return ev, false
			}
			ev.usage = uint8(n)
		case "mods":
			if ev.ptr || n > 0xff {
				return ev, false
			}
			ev.bits = uint8(n)
		case "buttons":
			if !ev.ptr || n > 0xff {
				return ev, false
			}
			ev.bits = uint8(n)
		case "x":
			if !ev.ptr || n > 0xffff {
				return ev, false
			}
			ev.x = uint16(n)
		case "y":
			if !ev.ptr || n > 0xffff {
				return ev, false
			}
			ev.y = uint16(n)
		default:
			return ev, false // an unknown member is malformed
		}
	}
	if !ev.ptr {
		return ev, seen["usage"]
	}
	return ev, seen["x"] && seen["y"]
}

// UIInput answers ui.input. Seat absence is the named refusal first
// (same anchor as ui.screenshot), then the strip's focus answers `win`,
// then each event rides its own bound kind-29 frame in order.
func (d *Dispatcher) UIInput(req *Request) (string, string) {
	win, present, ok := req.argInt("win", 0, intMax)
	if !present || !ok {
		return "", "bad_request"
	}
	elems, present, ok := req.argArray("events")
	if !present || !ok {
		return "", "bad_request"
	}
	if len(elems) > InputEventsMax {
		return "", "too_large"
	}
	evs := make([]inputEvent, len(elems))
	for i, e := range elems {
		if evs[i], ok = parseInputEvent(e); !ok {
			return "", "bad_request"
		}
	}
	if d.cfg.Seams.SeatPid() <= 0 {
		return "", "seat_absent"
	}
	if code := d.bindWindow(); code != "" {
		return "", code
	}
	rows, code := d.stripRows()
	if code != "" {
		return "", code
	}
	focused := -1
	for i := range rows {
		if rows[i].Focused {
			focused = rows[i].Index
		}
	}
	if focused < 0 || int64(focused) != win {
		return "", vi.AgentInputNoFocus
	}
	sent := 0
	for _, e := range evs {
		var rep vi.WmRpc
		var answered bool
		if e.ptr {
			rep, answered = d.cfg.Seams.AgentInputPtr(uint32(d.agentWindow), e.bits, e.x, e.y, d.cfg.Self)
		} else {
			rep, answered = d.cfg.Seams.AgentInputKey(uint32(d.agentWindow), e.bits, e.usage, d.cfg.Self)
		}
		if !answered {
			return "", "seat_timeout"
		}
		if rep.Applied == 0 {
			switch rep.TitleString() {
			case vi.AgentInputDenied, vi.AgentInputNoFocus:
				return "", rep.TitleString()
			}
			return "", "seat_unsupported"
		}
		sent++
	}
	return `{"sent":` + vi.Itoa64(int64(sent)) + `}`, ""
}

// --- ui.geometry ----------------------------------------------------------------

// Card #2985 (ADR 0044 D5c): ui.geometry {win, op, space?|percent?}
// arranges the desktop the way the seat arranges it, over WM_RPC kind
// 30. `win` is the strip index ui.windows reports; the verb resolves it
// to the kernel window id through the same kind-16 query ui.input uses
// for its focus check. `op` is one of:
//
//   - "query" — where the window is: `{"space":N,"pane":N|null,
//     "split":"none"|"h"|"v","rect":{"x":N,"y":N,"w":N,"h":N}}`. The
//     `window` cap serves it — same grant as ui.windows.
//   - "move" — `space`: space index; the tab joins that space's strip
//     (creating it at the seat's own cap). `{"space":N}`.
//   - "sash" — `percent`: the window's parent split's divider at 10-90 %
//     of its rect. `{"percent":N}`.
//
// move and sash take the `input` cap — they drive the desktop, so the
// M139b rule applies; the refusal names cap_required:input before the
// verb touches the seat. The seat's own refusal names surface verbatim
// (win_geometry_denied / win_geometry_no_window / win_geometry_bad_op);
// a bare applied=0 is seat_unsupported, silence is seat_timeout, and an
// index no strip row holds is win_geometry_no_window resolved here.
func (d *Dispatcher) UIGeometry(req *Request, s *Session) (string, string) {
	win, present, ok := req.argInt("win", 0, intMax)
	if !present || !ok {
		return "", "bad_request"
	}
	op, present, ok := req.argStr("op")
	if !present || !ok {
		return "", "bad_request"
	}
	var opCode uint8
	switch op {
	case "query":
		opCode = vi.WmGeomOpQuery
	case "move":
		opCode = vi.WmGeomOpMove
	case "sash":
		opCode = vi.WmGeomOpSash
	default:
		return "", "bad_request"
	}
	// The cap gate runs before the op's own args are weighed, the same
	// order dispatch uses — a session without the drive grant learns
	// cap_required:input, not which field was malformed.
	if opCode != vi.WmGeomOpQuery && s.Caps&CapInput == 0 {
		return "", "cap_required:input"
	}
	space, spPresent, spOK := req.argInt("space", 0, 255)
	pct, pcPresent, pcOK := req.argInt("percent", 0, 255)
	if !spOK || !pcOK {
		return "", "bad_request"
	}
	var arg uint8
	switch opCode {
	case vi.WmGeomOpQuery:
		if spPresent || pcPresent {
			return "", "bad_request"
		}
	case vi.WmGeomOpMove:
		if !spPresent || pcPresent {
			return "", "bad_request"
		}
		arg = uint8(space)
	case vi.WmGeomOpSash:
		if !pcPresent || spPresent {
			return "", "bad_request"
		}
		arg = uint8(pct)
	}
	if d.cfg.Seams.SeatPid() <= 0 {
		return "", "seat_absent"
	}
	if code := d.bindWindow(); code != "" {
		return "", code
	}
	rows, code := d.stripRows()
	if code != "" {
		return "", code
	}
	var target uint32
	found := false
	for i := range rows {
		if int64(rows[i].Index) == win {
			target = rows[i].ID
			found = true
			break
		}
	}
	if !found {
		return "", vi.AgentGeomNoWindow
	}
	rep, answered := d.cfg.Seams.AgentGeometry(uint32(d.agentWindow), opCode, target, arg, d.cfg.Self)
	if !answered {
		return "", "seat_timeout"
	}
	if rep.Applied == 0 {
		switch rep.TitleString() {
		case vi.AgentGeomDenied, vi.AgentGeomNoWindow, vi.AgentGeomBadOp:
			return "", rep.TitleString()
		}
		return "", "seat_unsupported"
	}
	switch opCode {
	case vi.WmGeomOpMove:
		return `{"space":` + vi.Itoa64(int64(arg)) + `}`, ""
	case vi.WmGeomOpSash:
		return `{"percent":` + vi.Itoa64(int64(arg)) + `}`, ""
	}
	v, ok := vi.WmGeomDecode(rep)
	if !ok {
		return "", "seat_unsupported"
	}
	var sb strings.Builder
	sb.WriteString(`{"space":`)
	sb.WriteString(vi.Itoa64(int64(v.Space)))
	sb.WriteString(`,"pane":`)
	if v.Pane == vi.WmGeomUnplaced {
		sb.WriteString("null")
	} else {
		sb.WriteString(vi.Itoa64(int64(v.Pane)))
	}
	sb.WriteString(`,"split":`)
	switch v.Split {
	case 1:
		jwriteStr(&sb, "h")
	case 2:
		jwriteStr(&sb, "v")
	default:
		jwriteStr(&sb, "none")
	}
	sb.WriteString(`,"rect":{"x":`)
	sb.WriteString(vi.Itoa64(int64(v.X)))
	sb.WriteString(`,"y":`)
	sb.WriteString(vi.Itoa64(int64(v.Y)))
	sb.WriteString(`,"w":`)
	sb.WriteString(vi.Itoa64(int64(v.W)))
	sb.WriteString(`,"h":`)
	sb.WriteString(vi.Itoa64(int64(v.H)))
	sb.WriteString(`}}`)
	return sb.String(), ""
}
