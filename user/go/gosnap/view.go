// view.go — M120d (issue #2561): the windowed face of GOSNAP.ELF.
//
// A bare `GOSNAP.ELF` (no argv verb) opens this tabapp window: the stored
// snapshots as a list, a name field + Save for the ctl `snapshot` verb,
// and Restore/Delete actions on the selected row. Every verb drives the
// SAME cmd* path the argv face runs, so the serial markers a gate greps
// (`gosnap: save X ok`, `gosnap: restore X staged`, `gosnap: rm X ok`)
// are one vocabulary for both faces.
//
// The two-step confirm IS the argv `confirm` token's seat-shaped twin:
// Restore and Delete are arm buttons — the first gesture marks the
// button Confirm? and prints `gosnap: consent <verb> <name>` (the same
// ask line the argv path prints); the second gesture on the armed
// button runs the verb. Esc, a click on any other control, a moved
// selection, or a changed listing disarms — no destructive verb can
// land on a single press.
//
// The window re-lists on a ~1 Hz poll, so a snapshot minted out of band
// — the host Machine menu's menu-<stamp>.vzsave (M120c), the seat's
// CRASH<n> slots (M125f) — appears while the window is open. The `ls`
// markers print only when the listing actually changed.
package main

import (
	"strings"

	"virelai/art"
	"virelai/draw"
	"virelai/tabapp"
	"virelai/theme"
	"virelai/vi"
	"virelai/widgets"
)

const (
	appName  = "GOSNAP.ELF"
	appTitle = "Snapshots"
	natW     = 480
	natH     = 300

	// markerSavedMascot is the one-shot decode receipt for the staged
	// celebrate pose (MSC_CELB) standing on the save confirmation —
	// the seat's mascot markers' latch shape in this app's namespace.
	markerSavedMascot = "gosnap: saved mascot qoi "
)

// The HID usage codes the window understands — the per-app copy every
// tabapp keeps (goset controls.go, widgets' own table); no appkit pull.
const (
	keyEnter  = 0x28
	keyEscape = 0x29
	keyTab    = 0x2b
	keyHome   = 0x4a
	keyEnd    = 0x4d
	keyRight  = 0x4f
	keyLeft   = 0x50
	keyDown   = 0x51
	keyUp     = 0x52
)

// The focus ring's slots, Tab order: list -> name field -> Save ->
// Restore -> Delete -> Pin -> Prune. The field leads because naming is
// the window's most common gesture.
const (
	fList = iota
	fField
	fSave
	fRest
	fDel
	fPin
	fPrune
	fCount
)

// pollNanos is the listing's re-read cadence — one listDir a second.
const pollNanos = 1_000_000_000

type snapView struct {
	ta *tabapp.TabApp

	rows []snapRow
	sel  int // -1 while the listing is empty

	// The two-step state: armed is the pending verb ("restore"/"rm"),
	// armedName the row it named. An empty armed is disarmed. The name —
	// not an index — is bound so a re-ordered listing can never point
	// the second gesture at a different snapshot.
	armed     string
	armedName string

	focus  int
	status string
	quit   bool
	pollAt int64

	headTxt   widgets.Text
	list      widgets.List
	field     widgets.TextInput
	saveBtn   widgets.Button
	restBtn   widgets.Button
	delBtn    widgets.Button
	pinBtn    widgets.Button
	pruneBtn  widgets.Button
	statusTxt widgets.Text
}

func newSnapView(ta *tabapp.TabApp) *snapView {
	v := &snapView{ta: ta, sel: -1, focus: fField}
	v.headTxt = widgets.Text{Label: "Snapshots", Fg: theme.Current.Text, Bg: theme.Current.Surface}
	v.list = widgets.List{
		RowH:  18,
		Sel:   -1,
		Fg:    theme.Current.Text,
		Bg:    theme.Current.Bg,
		SelBg: theme.Current.Surface,
	}
	v.field = widgets.TextInput{Max: nameMax}
	v.field.OnSubmit = func(string) { v.save() }
	v.field.Focused = true
	v.saveBtn = widgets.Button{Label: "Save"}
	v.restBtn = widgets.Button{Label: "Restore"}
	v.delBtn = widgets.Button{Label: "Delete"}
	v.pinBtn = widgets.Button{Label: "Pin"}
	v.pruneBtn = widgets.Button{Label: "Prune"}
	v.statusTxt = widgets.Text{Fg: theme.Current.Muted, Bg: theme.Current.Bg}
	v.status = "type a name + Enter to save; Restore/Delete/Prune ask twice; Pin exempts"
	return v
}

// selName is the selected row's name, or ("",false) with no live selection.
func (v *snapView) selName() (string, bool) {
	if v.sel < 0 || v.sel >= len(v.rows) {
		return "", false
	}
	return v.rows[v.sel].name, true
}

// refresh re-lists the share and rebuilds the row labels. The argv `ls`
// marker vocabulary prints exactly as the verb would print it, so a gate
// cannot tell the faces apart by their lines. The selection is clamped
// to the new listing (a deleted row leaves the next row selected).
func (v *snapView) refresh() {
	rows, err := snapRows()
	if err != nil {
		if err.Error() == "ENOENT" {
			rows = nil
		} else {
			logLine(markerPrefix + "ls refused " + err.Error())
			v.status = "list refused " + err.Error()
			return
		}
	}
	v.rows = rows
	if v.sel >= len(rows) {
		v.sel = len(rows) - 1
	}
	v.list.Items = make([]string, len(rows))
	for i, r := range rows {
		tag := ""
		if r.pinned {
			tag += " [pin]"
		}
		if crashName(r.name) {
			tag += " [crash]"
		}
		v.list.Items[i] = r.name + "  " + vi.Itoa64(int64(r.bytes)) + " B" + tag
	}
	v.list.Sel = v.sel
	v.ensureVisible()
	logSnapRows(rows)
	v.syncWidgets()
}

// poll is the 1 Hz re-list: cheap (one bounded dir read) and only a
// listing that CHANGED repaints — the marker lines stay event-shaped.
// A changed listing also disarms: a confirm armed against a listing
// that has since moved under it must be re-armed by the person.
func (v *snapView) poll(now int64) bool {
	if now-v.pollAt < pollNanos {
		return false
	}
	v.pollAt = now
	rows, err := snapRows()
	if err != nil || sameRows(rows, v.rows) {
		return false
	}
	v.disarm()
	v.refresh()
	v.status = "snapshot list changed"
	return true
}

// sameRows compares two listings name+size — the poll's change check.
func sameRows(a, b []snapRow) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// setFocus moves the focus ring and mirrors it onto the widgets.
func (v *snapView) setFocus(i int) {
	for i < 0 {
		i += fCount
	}
	v.focus = i % fCount
	v.list.Focused = v.focus == fList
	v.field.Focused = v.focus == fField
	v.saveBtn.Focused = v.focus == fSave
	v.restBtn.Focused = v.focus == fRest
	v.delBtn.Focused = v.focus == fDel
	v.pinBtn.Focused = v.focus == fPin
	v.pruneBtn.Focused = v.focus == fPrune
}

// arm is a destructive verb's first gesture: print the consent ask and
// mark the button. Nothing runs until the same verb is activated again
// against the same name.
func (v *snapView) arm(verb, name string) {
	v.armed, v.armedName = verb, name
	logLine(markerPrefix + "consent " + verb + " " + name)
	v.status = verb + " " + name + " — click again to confirm; Esc cancels"
	v.syncWidgets()
}

func (v *snapView) disarm() {
	if v.armed == "" {
		return
	}
	v.armed, v.armedName = "", ""
	v.syncWidgets()
}

// activate is the two-step itself: the first call for a (verb, name)
// pair arms, the second runs the verb — the windowed spelling of the
// argv `confirm` token. The verb's own cmd* path prints the verdict
// marker; a success re-lists so the row set stays honest.
func (v *snapView) activate(verb string) {
	name, ok := v.selName()
	if !ok {
		v.status = "select a snapshot first"
		return
	}
	if v.armed != verb || v.armedName != name {
		v.arm(verb, name)
		return
	}
	v.disarm()
	var rc int
	if verb == "restore" {
		rc = cmdRestore(name, []string{confirmToken})
	} else {
		rc = cmdRm(name, []string{confirmToken})
	}
	if rc == exitOK {
		if verb == "restore" {
			v.status = name + " staged — the next --resume boots it"
		} else {
			v.status = name + " deleted"
		}
		v.refresh()
	} else {
		v.status = verb + " " + name + " refused — see serial"
	}
}

// activatePrune is the Prune button's two-step: the armed name is the
// computed victim — the oldest unpinned named snapshot, the same one
// the host's cap eviction would take — so the confirm gesture names
// exactly what dies. No selection needed: the victim is the store's
// own answer, and nothing qualifies when the store holds only pins and
// crash slots.
func (v *snapView) activatePrune() {
	victim, ok := pruneVictim(v.rows)
	if !ok {
		v.disarm()
		v.status = "nothing unpinned to prune"
		v.syncWidgets()
		return
	}
	if v.armed != "prune" || v.armedName != victim {
		v.arm("prune", victim)
		return
	}
	v.disarm()
	if cmdPrune([]string{confirmToken}) == exitOK {
		v.status = victim + " pruned"
		v.refresh()
	} else {
		v.status = "prune refused — see serial"
	}
}

// togglePin is the Pin button's single-gesture flip on the selected
// row — additive metadata, never destructive, so it asks once like
// Save does. The label mirrors the row's state ("Unpin" when pinned).
func (v *snapView) togglePin() {
	name, ok := v.selName()
	if !ok {
		v.status = "select a snapshot first"
		return
	}
	v.disarm()
	pinned := v.rows[v.sel].pinned
	var rc int
	if pinned {
		rc = cmdUnpin(name)
	} else {
		rc = cmdPin(name)
	}
	if rc == exitOK {
		if pinned {
			v.status = name + " unpinned"
		} else {
			v.status = name + " pinned — cap eviction skips it"
		}
		v.refresh()
	} else {
		v.status = "pin refused — see serial"
		v.syncWidgets()
	}
}

// save is the name field's submit and the Save button's activation: the
// ctl `snapshot` verb through cmdSave, then a re-list on success. An
// empty field is a status hint, never a verb.
func (v *snapView) save() {
	v.disarm()
	name := v.field.Value()
	if name == "" {
		v.status = "type a snapshot name first"
		v.syncWidgets()
		return
	}
	if cmdSave(name) == exitOK {
		v.field.Clear()
		v.status = name + " saved"
		v.refresh()
	} else {
		v.status = "save " + name + " refused — see serial"
		v.syncWidgets()
	}
}

// savedMascotShown latches the one-shot decode receipt for the celebrate
// pose on the save confirmation — the same latch shape the seat's mascot
// markers keep.
var savedMascotShown bool

// statusSaved reports whether the standing status line is the save
// verb's confirmation — the one line the staged celebrate pose dresses
// (#3348). Every other status (refusals, the restore/rm verdicts, the
// consent asks) keeps the text-only band.
func (v *snapView) statusSaved() bool {
	return strings.HasSuffix(v.status, " saved")
}

// statusArtYield blits the staged celebrate pose at the status band's
// left when the band carries the save confirmation, returning the rect
// the status text keeps. The degrade contract is the fleet's: an absent
// or refused asset — or a canvas that is not a pixel buffer — returns r
// unchanged, so the band stays byte-identical to its text-only form.
func statusArtYield(c widgets.RectCanvas, r widgets.Rect, saved bool) widgets.Rect {
	if !saved {
		return r
	}
	bc, ok := c.(*draw.BufferCanvas)
	if !ok {
		return r
	}
	m := art.Load(art.MascotCelebrate)
	if m == nil || r.H < 8 {
		return r
	}
	s := r.H - 4
	if s > 48 {
		s = 48
	}
	draw.BlitScaled(bc, m, widgets.Rect{X: r.X, Y: r.Y + (r.H-s)/2, W: s, H: s})
	if !savedMascotShown {
		savedMascotShown = true
		logLine(markerSavedMascot +
			vi.Itoa64(int64(m.W)) + "x" + vi.Itoa64(int64(m.H)))
	}
	r.X += s + 8
	r.W -= s + 8
	return r
}

// handle applies one input event, reporting whether the frame changed.
func (v *snapView) handle(ev vi.Event) bool {
	switch ev.Kind {
	case vi.EvKeyDown:
		return v.key(ev)
	case vi.EvMouseDown:
		if ev.Flags&vi.BtnLeft == 0 {
			return false
		}
		return v.click(int(ev.Arg0), int(ev.Arg1))
	}
	return false
}

// key is the keyboard path. Esc is global: it cancels a pending confirm
// before it can quit the window. Tab walks the focus ring; Up/Down
// always move the list (the field has no use for them); every other key
// goes to the focused control.
func (v *snapView) key(ev vi.Event) bool {
	usage, ch := ev.Arg0, byte(ev.Arg1)
	if usage == keyEscape || (usage == 0 && ch == 0x1b) {
		if v.armed != "" {
			v.disarm()
			v.status = "confirm cancelled"
			v.syncWidgets()
			return true
		}
		v.quit = true
		return true
	}
	if usage == keyTab || (usage == 0 && ch == '\t') {
		d := 1
		if ev.Flags&vi.ModShift != 0 {
			d = -1
		}
		v.setFocus(v.focus + d)
		return true
	}
	if usage == keyUp || usage == keyDown {
		return v.moveSel(usage)
	}
	switch v.focus {
	case fList:
		switch usage {
		case keyHome:
			return v.selTo(0)
		case keyEnd:
			return v.selTo(len(v.rows) - 1)
		case keyEnter, keyRight, keyLeft:
			// A row's Enter is its first verb gesture? No — Enter on a
			// list row does nothing: Restore/Delete are the buttons, so
			// a stray key can never arm a destructive verb.
		}
	case fField:
		return v.field.HandleKey(usage, ch)
	case fSave:
		if usage == keyEnter || (usage == 0 && (ch == '\r' || ch == '\n')) {
			v.save()
			return true
		}
	case fRest:
		if usage == keyEnter || (usage == 0 && (ch == '\r' || ch == '\n')) {
			v.activate("restore")
			return true
		}
	case fDel:
		if usage == keyEnter || (usage == 0 && (ch == '\r' || ch == '\n')) {
			v.activate("rm")
			return true
		}
	case fPin:
		if usage == keyEnter || (usage == 0 && (ch == '\r' || ch == '\n')) {
			v.togglePin()
			return true
		}
	case fPrune:
		if usage == keyEnter || (usage == 0 && (ch == '\r' || ch == '\n')) {
			v.activatePrune()
			return true
		}
	}
	return false
}

// moveSel is the Up/Down list step — reachable from every focus slot so
// the list answers the arrows even while a name is being typed.
func (v *snapView) moveSel(usage uint32) bool {
	if len(v.rows) == 0 {
		return false
	}
	d := 1
	if usage == keyUp {
		d = -1
	}
	return v.selTo(v.sel + d)
}

func (v *snapView) selTo(i int) bool {
	if i < 0 {
		i = 0
	}
	if i >= len(v.rows) {
		i = len(v.rows) - 1
	}
	if i == v.sel || i < 0 {
		return false
	}
	v.sel, v.list.Sel = i, i
	v.disarm()
	v.ensureVisible()
	return true
}

func (v *snapView) ensureVisible() {
	vis := v.list.VisibleRows()
	if vis < 1 {
		return
	}
	top := v.list.ScrollTop
	if top > len(v.list.Items)-vis {
		top = len(v.list.Items) - vis
	}
	if v.sel >= 0 && v.sel < top {
		top = v.sel
	}
	if v.sel >= top+vis {
		top = v.sel - vis + 1
	}
	if top < 0 {
		top = 0
	}
	v.list.ScrollTop = top
}

// click is the pointer path. Every control hit disarms a pending
// confirm for another verb — the second gesture must land on the SAME
// armed button.
func (v *snapView) click(x, y int) bool {
	if i := v.list.ItemAt(x, y); i >= 0 {
		v.sel, v.list.Sel = i, i
		v.disarm()
		v.setFocus(fList)
		return true
	}
	if v.field.HitTest(x, y) {
		v.disarm()
		v.setFocus(fField)
		return true
	}
	if v.saveBtn.HitTest(x, y) {
		v.setFocus(fSave)
		v.save()
		return true
	}
	if v.restBtn.HitTest(x, y) {
		v.setFocus(fRest)
		v.activate("restore")
		return true
	}
	if v.delBtn.HitTest(x, y) {
		v.setFocus(fDel)
		v.activate("rm")
		return true
	}
	if v.pinBtn.HitTest(x, y) {
		v.setFocus(fPin)
		v.togglePin()
		return true
	}
	if v.pruneBtn.HitTest(x, y) {
		v.setFocus(fPrune)
		v.activatePrune()
		return true
	}
	v.disarm()
	return false
}

// syncWidgets mirrors model state onto the controls: disabled flags,
// the armed button's Confirm? face, and the status line. draw() calls
// it, so it must also run wherever the state moved without a repaint
// yet — the verb paths all end through it.
func (v *snapView) syncWidgets() {
	_, ok := v.selName()
	v.restBtn.Disabled = !ok
	v.delBtn.Disabled = !ok
	v.pinBtn.Disabled = !ok
	_, hasVictim := pruneVictim(v.rows)
	v.pruneBtn.Disabled = !hasVictim
	v.restBtn.Label = "Restore"
	if v.armed == "restore" && ok {
		v.restBtn.Label = "Confirm?"
	}
	v.delBtn.Label = "Delete"
	if v.armed == "rm" && ok {
		v.delBtn.Label = "Confirm?"
	}
	v.pinBtn.Label = "Pin"
	if ok && v.rows[v.sel].pinned {
		v.pinBtn.Label = "Unpin"
	}
	v.pruneBtn.Label = "Prune"
	if v.armed == "prune" {
		v.pruneBtn.Label = "Confirm?"
	}
	v.list.Sel = v.sel
	v.statusTxt.Label = v.status
}

// layout maps the native 480x300 design onto the live canvas — the
// same ta.Layout rescale every migrated app runs on WIN_RESIZE.
func (v *snapView) layout() {
	sc := func(r widgets.Rect) widgets.Rect {
		s := v.ta.Layout(tabapp.Rect{X: r.X, Y: r.Y, W: r.W, H: r.H}, natW, natH)
		return widgets.Rect{X: s.X, Y: s.Y, W: s.W, H: s.H}
	}
	v.headTxt.R = sc(widgets.Rect{X: 12, Y: 10, W: 456, H: 18})
	v.list.R = sc(widgets.Rect{X: 12, Y: 34, W: 456, H: 152})
	v.list.RowH = sc(widgets.Rect{X: 0, Y: 0, W: 1, H: 18}).H
	v.field.R = sc(widgets.Rect{X: 12, Y: 196, W: 336, H: 22})
	v.saveBtn.R = sc(widgets.Rect{X: 356, Y: 196, W: 112, H: 22})
	v.restBtn.R = sc(widgets.Rect{X: 12, Y: 228, W: 104, H: 22})
	v.delBtn.R = sc(widgets.Rect{X: 124, Y: 228, W: 104, H: 22})
	v.pinBtn.R = sc(widgets.Rect{X: 236, Y: 228, W: 104, H: 22})
	v.pruneBtn.R = sc(widgets.Rect{X: 348, Y: 228, W: 120, H: 22})
	v.statusTxt.R = sc(widgets.Rect{X: 12, Y: 260, W: 456, H: 30})
}

func (v *snapView) draw() {
	v.syncWidgets()
	v.layout()
	cv := v.ta.Canvas()
	cv.FillRect(widgets.Rect{W: int(v.ta.W), H: int(v.ta.H)}, draw.Opaque(theme.Current.Bg))
	v.headTxt.Draw(cv)
	v.list.Draw(cv)
	v.field.Draw(cv)
	v.saveBtn.Draw(cv)
	v.restBtn.Draw(cv)
	v.delBtn.Draw(cv)
	v.pinBtn.Draw(cv)
	v.pruneBtn.Draw(cv)
	// #3348 (#1250): a saved snapshot stands the staged celebrate pose
	// at the status band's left, the status text yielding it the room —
	// an upgrade on the confirmation, never a dependency.
	v.statusTxt.R = statusArtYield(cv, v.statusTxt.R, v.statusSaved())
	v.statusTxt.Draw(cv)
}

func (v *snapView) present() { v.draw(); v.ta.Present() }

// --- the M195c (#3308) appver-1 state envelope -----------------------------
//
// The windowed face's half of the M125a (#2586) seat-mediated
// save/restore protocol — the same shape NOTE's state.go and
// GOWELCOME's tour keep: the seat stores the blob verbatim on the tab's
// session row (kind-20 save, kind-21 restore) and hands it back on a
// reopen, so a close/reopen mid-gesture resumes with the same row
// selected and the same half-typed name in the field. The seat owns
// the lifecycle; the face owns the format:
//
//	u8 fmtVer (=stateFmtVer) — the blob's own version byte; also the
//	   Config's StateVersion, so the envelope's appver and the payload
//	   agree (a stale serialization is refused twice over)
//	u8 flags (=0, reserved)
//	u8 fieldLen | fieldLen bytes — the name field's in-progress text
//	u8 selLen   | selLen bytes   — the selected row's NAME
//
// The selection rides as a name, not an index: the listing re-reads on
// a 1 Hz poll and a re-ordered share must never point the restore at a
// different snapshot. A name that no longer lists degrades to no
// selection — the GOTOP clamp rule, a stale-but-well-formed blob never
// refuses the session.
//
// The two-step arm is deliberately NOT in the envelope: a half-finished
// destructive gesture must be re-armed by the person after any restore,
// never inherited by a window the seat rebuilt.
//
// Decode is total: a bad length, a foreign fmtVer, nonzero flags,
// truncated or trailing bytes, or non-printable text refuses the WHOLE
// blob — a torn handoff can never land half-applied.

const (
	stateFmtVer = 1
	stateAppVer = 1
)

// snapState is one decoded session blob.
type snapState struct {
	field string
	sel   string
}

// encode serializes the face's durable state. Bounded by construction:
// the field caps at nameMax and a row name is a dir entry — the blob can
// never approach the seat's 64 KiB envelope ceiling.
func (v *snapView) encode() []byte {
	out := []byte{stateFmtVer, 0}
	out = appendStateName(out, v.field.Value())
	name, _ := v.selName()
	return appendStateName(out, name)
}

// appendStateName length-prefixes one string into the envelope — a name
// over the u8 bound degrades to empty rather than writing a truncated
// length decode could never have produced.
func appendStateName(out []byte, s string) []byte {
	if len(s) > 255 {
		s = ""
	}
	out = append(out, byte(len(s)))
	return append(out, s...)
}

// decodeSnapState is encode's strict inverse. ok=false means stale or
// foreign — refused whole, never half-applied; the caller names the
// refusal. Every byte must be printable ASCII, the alphabet the field's
// insert filter and the row names share; a stored name the fresh
// listing lacks is the apply side's degrade, not a decode refusal.
func decodeSnapState(b []byte) (snapState, bool) {
	var st snapState
	if len(b) < 4 || b[0] != stateFmtVer || b[1] != 0 {
		return st, false
	}
	o := 2
	field, ok := takeStateName(b, &o)
	if !ok {
		return st, false
	}
	sel, ok := takeStateName(b, &o)
	if !ok || o != len(b) {
		return st, false
	}
	st.field, st.sel = field, sel
	return st, true
}

// takeStateName reads one u8-length-prefixed printable string out of the
// blob, advancing o — false on a truncation or a byte outside the
// field/listing alphabet.
func takeStateName(b []byte, o *int) (string, bool) {
	if *o >= len(b) {
		return "", false
	}
	n := int(b[*o])
	*o++
	if len(b)-*o < n {
		return "", false
	}
	s := b[*o : *o+n]
	*o += n
	for _, c := range s {
		if c < 0x20 || c > 0x7e {
			return "", false
		}
	}
	return string(s), true
}

// stateDesc is the envelope's marker rendering — '-' for an empty piece
// so the line never carries a bare `sel=`/`field=`.
func stateDesc(sel, field string) string {
	if sel == "" {
		sel = "-"
	}
	if field == "" {
		field = "-"
	}
	return "sel=" + sel + " field=" + field
}

// applyRestored lands a decoded envelope on the live view and returns
// the marker to print — "" when the seat handed nothing over. It runs
// after the first refresh so the selection resolves against real rows:
// the stored name re-selects its row wherever the fresh listing sorted
// it, and a name the share has since dropped reports `gone`. The
// envelope's appver is the face's own stale check: a foreign version or
// a torn blob is refused by name, never silently misread.
func (v *snapView) applyRestored(st snapState, ok bool) string {
	if v.ta == nil || !v.ta.Restored {
		return ""
	}
	if v.ta.RestoredVersion != stateAppVer {
		return markerState + "refused ver=" + vi.Itoa64(int64(v.ta.RestoredVersion))
	}
	if !ok {
		return markerState + "refused bad"
	}
	v.field.SetValue(st.field)
	sel := st.sel
	if sel != "" {
		found := false
		for i, r := range v.rows {
			if r.name == sel {
				v.sel, v.list.Sel = i, i
				v.ensureVisible()
				found = true
				break
			}
		}
		if !found {
			sel = "gone"
		}
	}
	return markerState + "restored " + stateDesc(sel, st.field)
}

// runWindowed is the bare-argv entry: open the tab window, host the
// event loop until the window closes. A refused open falls back to the
// usage text — a serial-only caller still learns the verbs, and the
// exit stays nonzero (a window that could not open is not a success).
func runWindowed() int {
	// The M195c state hooks, the GOTOP shape: SaveState reads the live
	// view (the closure captures the variable, so an ask always
	// serializes the current selection); RestoreState fires inside
	// tabapp.Init before the view exists, so it only decodes — the
	// apply lands after the first refresh, once real rows exist to
	// re-select against.
	var v *snapView
	var restored snapState
	var restoredOK bool
	ta := tabapp.Init(tabapp.Config{
		Name: appName, Title: appTitle, X: 64, Y: 64, W: natW, H: natH,
		StateVersion: stateAppVer,
		SaveState: func() []byte {
			if v == nil {
				return nil
			}
			return v.encode()
		},
		RestoreState: func(b []byte) { restored, restoredOK = decodeSnapState(b) },
	})
	if ta == nil {
		logLine("gosnap: error open -1")
		usage()
		return exitRefused
	}
	logLine("gosnap: open id=" + vi.Itoa64(int64(ta.Win)))
	if ta.TabAware {
		logLine("gosnap: declare accepted")
	} else {
		logLine("gosnap: declare refused")
	}

	v = newSnapView(ta)
	v.refresh()
	if line := v.applyRestored(restored, restoredOK); line != "" {
		logLine(line)
	}
	v.present()
	logLine("gosnap: present")
	logLine("gosnap: ready n=" + vi.Itoa64(int64(len(v.rows))))
	for {
		dirty := false
		if v.poll(vi.Nanos()) {
			dirty = true
		}
		// The seat's save ask is mailbox traffic, not an input event:
		// answer it once per pass before polling, the same discipline
		// the other hooked apps keep. The saved marker prints only on
		// an acked handoff — never on silence or a refused post.
		if v.ta.PollStateAsk() {
			name, _ := v.selName()
			logLine(markerState + "saved " + stateDesc(name, v.field.Value()))
		}
		ev, rc, ok := vi.PollEventRaw()
		if ok {
			switch ta.Dispatch(ev) {
			case tabapp.ActionClosed:
				v.quit = true
			case tabapp.ActionResized:
				dirty = true
			case tabapp.ActionNone:
				dirty = v.handle(ev) || dirty
			}
		} else if rc < 0 {
			return 0
		}
		if v.quit {
			logLine("gosnap: close")
			logLine("gosnap OK")
			ta.CloseAndExit(0)
			return exitOK
		}
		if dirty {
			v.present()
		}
		if !ok {
			vi.Sleep(1)
		}
	}
}
