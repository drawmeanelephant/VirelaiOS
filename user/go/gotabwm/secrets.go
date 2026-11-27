// GOTABWM — M196a (issue #3351, ADR 0024 D8 amendment): the seat's
// consent surface for the secret store. `sys_secret_set` (slot 96)
// stages any EL0 app's `key\tuid\tvalue[\tapps]` line in the kernel's
// bounded pending table; the value never leaves kernel memory uncommitted
// and nothing lands in the store until a seat gesture says so. The seat
// polls LIST once per loop pass (one small op — ENOENT when the table is
// empty, so an idle desktop pays one refused syscall), stands a modal on
// the first pending ask — name and key only, never the value — and the
// typed gesture answers it: `g` commits (the entry lands + SECRETS.TXT
// persists), `d`/Esc declines (the asker's status read says `dropped`).
// Every other key is swallowed while the ask stands — the M116c modal
// discipline: an unanswered consent is not a background state.
//
// M216l (issue #3951, second D8 amendment): the consent cannot be typed
// or clicked through. The asking app picks the moment, so a `g` (or a
// pointer press on Grant) that lands before the user could have seen the
// prompt is swallowed whole: grant gestures stay dead until the first
// heartbeat after a frame carrying the prompt presented — the seat's
// only clock is 1 Hz, and "could have seen it" is a presented frame plus
// one heartbeat (card n's sub-second timer narrows this; until then the
// heartbeat is the honest bound). Decline is live from the first key —
// refusing a prompt you have not read is always safe. The pointer is
// modal too: two buttons inside the plate answer, every other press on
// or off the plate is swallowed and dismisses nothing, and no seat chord
// acts while the ask stands — except lock, which cannot wait on consent.
//
// Markers (each printed only after the step it names):
//
//	gotabwm: secret-ask name=<IMG> key=<k>     — the prompt is standing
//	gotabwm: secret armed name=<IMG> key=<k>   — grant gestures are live
//	gotabwm: secret grant name=<IMG> key=<k>   — consent landed the entry
//	gotabwm: secret decline name=<IMG> key=<k> — refusal gesture, no write
//	gotabwm: secret refuse name=<IMG> key=<k> <err> — the write was refused
package main

import (
	"strconv"
	"strings"

	"virelai/appkit"
	"virelai/draw"
	"virelai/theme"
	"virelai/vi"
	"virelai/widgets"
)

const (
	MarkerSecretAsk     = "gotabwm: secret-ask name="
	MarkerSecretArm     = "gotabwm: secret armed name="
	MarkerSecretGrant   = "gotabwm: secret grant name="
	MarkerSecretDecline = "gotabwm: secret decline name="
	MarkerSecretRefuse  = "gotabwm: secret refuse name="
)

// The slot-96 seams — function vars like every other guest-coupled call
// in this package, so host tests drive the whole flow without a kernel.
var (
	secretAskList = func(i uint32, buf []byte) (int, int64) {
		return vi.SecretAskList(i, buf)
	}
	secretAskCommit = func(id uint32) int64 { return vi.SecretAskCommit(id) }
	secretAskDrop   = func(id uint32) int64 { return vi.SecretAskDrop(id) }
	secretLog       = vi.ConsoleLine
)

// secretAsk is the standing consent prompt. One at a time, oldest first:
// a second staged ask waits behind the answered one — kernel LIST order.
// seen/armed are the M216l arm delay: seen records that a frame carrying
// the prompt actually presented somewhere the user could read it (never
// under a curtain), armed flips grant gestures live on the next heartbeat
// after that. sel is the keyboard's button focus — Deny is its zero
// value on purpose: an unaimed Enter declines, the direction that cannot
// commit a write.
type secretAsk struct {
	open     bool
	id       uint32
	image    string
	key      string
	apps     string
	replaces bool
	sink     uint32
	seen     bool
	armed    bool
	sel      int
}

// The button identities sel and the pointer hit-test share.
const (
	secretBtnDeny  = 0
	secretBtnGrant = 1
)

var secretAskModal secretAsk

// parseSecretAskLine decodes one LIST marshal —
// `id\tpid\tuid\timage\tkey\tmode[\tapps]` — or fails. The pid/uid fields
// are kernel-recorded facts the modal does not render; the image is what
// the asker exec'd, the key is the name the entry would take. mode is the
// kernel's add-vs-replace signal (#3407): "replace" means the ask's
// (uid,key) already holds an entry, so consent is asked with the decisive
// fact instead of a silent overwrite posing as an ordinary add.
func parseSecretAskLine(line string) (a secretAsk, ok bool) {
	f := strings.Split(line, "\t")
	if len(f) != 6 && len(f) != 7 {
		return a, false
	}
	id, err := strconv.ParseUint(f[0], 10, 32)
	if err != nil || id == 0 || f[3] == "" || f[4] == "" {
		return a, false
	}
	if _, err := strconv.ParseUint(f[2], 10, 32); err != nil {
		return a, false
	}
	a = secretAsk{id: uint32(id), image: f[3], key: f[4]}
	switch f[5] {
	case "new":
	case "replace":
		a.replaces = true
	default:
		return secretAsk{}, false
	}
	if len(f) == 7 {
		a.apps = f[6]
	}
	return a, true
}

// pollSecretAsks runs once per seat loop pass (next to serviceRPC). With
// no modal standing it walks the pending table and stands a prompt on
// the first ask it finds — a dropped ask is out of LIST, so a refused
// prompt can never re-open itself.
func pollSecretAsks() {
	if secretAskModal.open {
		return
	}
	var buf [vi.SecretAskMetaMax]byte
	for i := uint32(0); i < 8; i++ {
		n, r := secretAskList(i, buf[:])
		if r < 0 || n <= 0 {
			return
		}
		a, ok := parseSecretAskLine(string(buf[:n]))
		if !ok {
			continue
		}
		openSecretAsk(a)
		return
	}
}

func openSecretAsk(a secretAsk) {
	// M216l: the consent takes every in-flight gesture latch — an armed
	// drag, a rail reorder, a sash pull, a held content press or a stale
	// hover face cannot keep running under the prompt, and its release
	// must never land later as if it were the answer. The zeroed
	// seen/armed/sel fields are the fail-safe: grant is dead until a
	// presented frame plus a heartbeat, and an unaimed Enter declines.
	dragAbort("chrome")
	railDragFrom, contentDown, sashDragging = -1, false, false
	railHover, railCloseHover, godButtonHover = -1, -1, false
	// M216p (#3955): close any dismissible modal before acquiring the
	// consent sink so a modal borrowing the reserve parks it first.
	closeDismissibleModals(overlaySecretAsk)
	secretAskModal = a
	secretAskModal.open = true
	secretAskModal.sink = 0
	acquireSecretFocus()
	m := MarkerSecretAsk + a.image + " key=" + a.key
	if a.replaces {
		m += " replace"
	}
	if a.apps != "" {
		m += " apps=" + a.apps
	}
	secretLog(m)
}

// acquireSecretFocus steals focus for the prompt the way the power menu
// does: an invisible input sink over the panel rect, so its keys are the
// seat's alone and no hosted app sees the gesture.
func acquireSecretFocus() bool {
	x, y, w, h := secretAskRect(vi.ScanoutWidth, vi.ScanoutHeight)
	if w <= 0 {
		return false
	}
	id, r := openInputSink(uint32(x), uint32(y), uint32(w), uint32(h))
	if r < 0 {
		return false
	}
	secretAskModal.sink = uint32(id)
	st, qr := queryWindow(int(secretAskModal.sink))
	if qr < 0 || (st[5] == 0 && focusRaise(secretAskModal.sink) != 0) {
		_ = closeWin(secretAskModal.sink)
		secretAskModal.sink = 0
		return false
	}
	return true
}

// ensureSecretFocus reports whether the prompt's sink still exists — the
// kernel reaps a dead window out from under the seat. M216l: the sink
// OWNS the keys while the prompt stands, so a focus loss is repaired on
// the same pass (the launcher's ensure pattern) — otherwise a hosted app
// would see the consent gesture. The lock curtain is exempt: it owns the
// frame while it is up and both sinks are seat windows, so nothing leaks
// either way, and its own unlock path hands the keys back to this sink.
func ensureSecretFocus() bool {
	if secretAskModal.sink == 0 {
		return false
	}
	st, r := queryWindow(int(secretAskModal.sink))
	if r < 0 {
		return false
	}
	if !lockActive && st[5] == 0 && focusRaise(secretAskModal.sink) != 0 {
		return false
	}
	return true
}

func closeSecretAsk() {
	if secretAskModal.sink != 0 {
		_ = closeWin(secretAskModal.sink)
	}
	secretAskModal = secretAsk{}
}

// armSecretAsk flips the prompt's grant gestures live on the first
// heartbeat after a frame carrying it presented — compositeTick calls it
// ahead of the frame it may join, so the heartbeat that first shows the
// prompt can never also arm it. The marker prints once, at the
// transition: the gate's `--input-chords-after` anchor for a grant that
// must not fire early.
func armSecretAsk() {
	if !secretAskModal.open || secretAskModal.armed || !secretAskModal.seen {
		return
	}
	secretAskModal.armed = true
	secretLog(MarkerSecretArm + secretAskModal.image + " key=" + secretAskModal.key)
}

// noteSecretFrame records whether this composite pass presented a frame
// the user could actually read the prompt on. The ask paints UNDER the
// lock and idle curtains, so a presented frame that carried it covered
// counts for nothing — an ask staged while locked would otherwise arm
// sight-unseen behind the curtain.
func noteSecretFrame(painted int, presented bool) {
	if painted <= 0 || !presented || lockActive || idleState == idleCurtain {
		return
	}
	secretAskModal.seen = true
}

// handleSecretAskKey answers the standing prompt. `g` commits the staged
// line through the seat's authority once the prompt is armed; `d`/Esc
// declines at any time. Tab cycles the button focus and Enter runs the
// focused button — the pointer affordance's keyboard twin. Every other
// key is swallowed except the lock chord: a consent can wait for an
// answer, physical safety cannot — every other seat action would treat
// the unanswered ask as background. A refused write is named, never
// silent. Returns true whenever the prompt owns the key.
// The seat sees the kind-21 WM_KEY mirror, not an app-facing KEY_DOWN:
// the gesture decodes out of Arg0's raw HID usage (arg1 is always 0 on
// that wire — the codepoint contract is the focused window's, never the
// seat's). M196d (#3433): the gate leg was the first class-B exercise of
// this path — decoding Arg1 'g'/'d' could never fire on the real wire.
func handleSecretAskKey(e vi.Event) bool {
	if !secretAskModal.open {
		return false
	}
	// M216l (#3951): the lock chord stays live while the ask stands. It
	// calls requestLock directly rather than through runSeatAction — a
	// parked focus cycle must not queue physical safety behind a drain.
	if c := chordEvent(e); seatChords[c] == "lock" {
		requestLock()
		return true
	}
	switch e.Arg0 {
	case 0x29: // Escape — decline, live at all times
		declineSecretAsk()
	case 0x2b: // Tab — cycle the button focus; never an answer by itself
		secretAskModal.sel = secretBtnGrant - secretAskModal.sel
	case 0x28: // Enter — the focused button's gesture (Deny by default)
		if secretAskModal.sel == secretBtnGrant {
			grantSecretAsk()
		} else {
			declineSecretAsk()
		}
	case 0x0a: // 'g' — grant, armed only
		grantSecretAsk()
	case uint32(hidUsageD): // 'd' — decline, live at all times
		declineSecretAsk()
	}
	return true
}

// handleSecretAskPointer answers the prompt to a left press on a button.
// hid.go's modal branch reaches it only on a down edge — every other
// sample while the ask stands is swallowed before it gets here.
func handleSecretAskPointer(px, py uint32) {
	switch secretAskButtonAt(px, py, vi.ScanoutWidth, vi.ScanoutHeight) {
	case secretBtnGrant:
		grantSecretAsk()
	case secretBtnDeny:
		declineSecretAsk()
	}
}

// declineSecretAsk drops the staged ask — the refusal gesture, live from
// the moment the prompt opens.
func declineSecretAsk() {
	name, key := secretAskModal.image, secretAskModal.key
	answerSecretAsk(secretAskModal.id, false)
	secretLog(MarkerSecretDecline + name + " key=" + key)
}

// grantSecretAsk commits the staged ask — armed gestures only. A grant
// that lands before the arm delay is swallowed whole: the prompt keeps
// standing, and nothing about it changes. A refused write is named.
func grantSecretAsk() {
	if !secretAskModal.armed {
		return
	}
	name, key := secretAskModal.image, secretAskModal.key
	if rc := answerSecretAsk(secretAskModal.id, true); rc < 0 {
		secretLog(MarkerSecretRefuse + name + " key=" + key + " " + vi.ErrnoName(rc))
	} else {
		secretLog(MarkerSecretGrant + name + " key=" + key)
	}
}

// answerSecretAsk runs the gesture and closes the prompt. The commit/drop
// result comes back so the caller can name a refusal.
func answerSecretAsk(id uint32, grant bool) (rc int64) {
	if grant {
		rc = secretAskCommit(id)
	} else {
		rc = secretAskDrop(id)
	}
	closeSecretAsk()
	return rc
}

// secretAskRect is the prompt's plate: centered on the rail band, the
// power-menu plate's width class.
func secretAskRect(width, height int) (x, y, w, h int) {
	w = 336
	h = 30
	x = (width - w) / 2
	if x < chromeInset {
		x = chromeInset
	}
	y = RailHeight + 8
	if y+h > height-8 {
		y = height - h - 8
	}
	return
}

// The answer buttons' geometry inside the plate — right-aligned,
// vertically centered, Deny last. secretAskButtonRects is the one source
// the paint and the pointer hit-test share.
const (
	secretBtnW   = 56
	secretBtnH   = 22
	secretBtnGap = 6
)

func secretAskButtonRects(width, height int) (grant, deny widgets.Rect) {
	x, y, w, h := secretAskRect(width, height)
	if w <= 0 {
		return
	}
	deny = widgets.Rect{X: x + w - 8 - secretBtnW, Y: y + (h-secretBtnH)/2, W: secretBtnW, H: secretBtnH}
	grant = widgets.Rect{X: deny.X - secretBtnGap - secretBtnW, Y: deny.Y, W: secretBtnW, H: secretBtnH}
	return
}

// secretAskButtonAt maps a point to a button index, or -1 anywhere else —
// inside the plate but off the buttons included (a press there is
// swallowed like every other press, never an answer).
func secretAskButtonAt(px, py uint32, width, height int) int {
	g, d := secretAskButtonRects(width, height)
	in := func(r widgets.Rect) bool {
		return r.W > 0 && int(px) >= r.X && int(px) < r.X+r.W &&
			int(py) >= r.Y && int(py) < r.Y+r.H
	}
	if in(g) {
		return secretBtnGrant
	}
	if in(d) {
		return secretBtnDeny
	}
	return -1
}

// paintSecretAsk renders the standing prompt — the asker's image, the
// key name, and the two answer buttons. The value is never rendered
// anywhere.
func paintSecretAsk(scan []byte, width, height int) int {
	if !secretAskModal.open {
		return 0
	}
	x, y, w, h := secretAskRect(width, height)
	if w <= 0 || len(scan)/4/width < y+h {
		return 0
	}
	c := seatCanvas(scan, width, height)
	if c == nil {
		return 0
	}
	tok := theme.Current
	plate := widgets.Rect{X: x, Y: y, W: w, H: h}
	n := seatPlate(c, plate, radiusControl, tok.Accent, tok.ChromeBg)
	label := secretAskLabel()
	grant, deny := secretAskButtonRects(width, height)
	clip := plate.Inset(1)
	label = seatFitText(label, grant.X-x-16, seatSmallPx)
	seatText(c, clip, x+8, y+4, seatSmallPx, label, tok.Ink)
	paintSecretButton(c, grant, "Grant",
		secretAskModal.sel == secretBtnGrant, secretAskModal.armed, tok)
	paintSecretButton(c, deny, "Deny",
		secretAskModal.sel == secretBtnDeny, true, tok)
	return n
}

// paintSecretButton draws one answer button: the keyboard-focused button
// carries the selection face, and a dead gesture's ink stays muted —
// Grant's dimmed look until the arm delay passes IS the affordance.
func paintSecretButton(c *draw.BufferCanvas, r widgets.Rect, label string, sel, live bool, tok theme.Tokens) {
	face := tok.ChromeBg
	if sel {
		face = tok.Selection
	}
	seatPlate(c, r, radiusControl, tok.Accent, face)
	ink := tok.Ink
	if !live {
		ink = tok.InkMuted
	}
	tw := seatTextWidth(label, seatSmallPx)
	seatText(c, r.Inset(1), r.X+(r.W-tw)/2, r.Y+(r.H-8)/2, seatSmallPx, label, ink)
}

// secretAskLabel is the consent sentence the plate paints — "IMAGE
// wants to store KEY for APPS" — the asker's names only, never the
// staged value. The a11y node reuses it untruncated.
func secretAskLabel() string {
	verb := " wants to store "
	if secretAskModal.replaces {
		verb = " wants to REPLACE "
	}
	label := secretAskModal.image + verb + secretAskModal.key
	if secretAskModal.apps != "" {
		label += " for " + secretAskModal.apps
	}
	return label
}

// secretAskA11yNodes is the plate's outline fragment, the launcher's
// shape for the consent surface: the prompt node holds 'f' while the
// sink owns the keys, and each answer is a button child. Grant carries
// 'd' until armed — the tree must not offer a control that cannot fire —
// and 's' rides the button Tab/Enter would act on (Deny by default).
func secretAskA11yNodes(depth int) []appkit.Node {
	var grant, deny appkit.State
	if !secretAskModal.armed {
		grant |= appkit.StateDisabled
	}
	if secretAskModal.sel == secretBtnGrant {
		grant |= appkit.StateSelected
	} else {
		deny |= appkit.StateSelected
	}
	return []appkit.Node{
		{Depth: depth, Role: appkit.RoleMenu, Name: secretAskLabel(),
			State: appkit.StateFocused | appkit.StateExpanded},
		{Depth: depth + 1, Role: appkit.RoleButton, Name: "Grant", State: grant},
		{Depth: depth + 1, Role: appkit.RoleButton, Name: "Deny", State: deny},
	}
}
