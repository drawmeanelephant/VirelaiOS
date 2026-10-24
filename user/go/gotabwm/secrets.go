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
// Markers (each printed only after the step it names):
//
//	gotabwm: secret-ask name=<IMG> key=<k>     — the prompt is standing
//	gotabwm: secret grant name=<IMG> key=<k>   — consent landed the entry
//	gotabwm: secret decline name=<IMG> key=<k> — refusal gesture, no write
//	gotabwm: secret refuse name=<IMG> key=<k> <err> — the write was refused
package main

import (
	"strconv"
	"strings"

	
	"virelai/theme"
	"virelai/vi"
	"virelai/widgets"
)

const (
	MarkerSecretAsk     = "gotabwm: secret-ask name="
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
	secretLog = vi.ConsoleLine
)

// secretAsk is the standing consent prompt. One at a time, oldest first:
// a second staged ask waits behind the answered one — kernel LIST order.
type secretAsk struct {
	open  bool
	id    uint32
	image string
	key   string
	apps  string
	sink  uint32
}

var secretAskModal secretAsk

// parseSecretAskLine decodes one LIST marshal —
// `id\tpid\tuid\timage\tkey[\tapps]` — or fails. The pid/uid fields are
// kernel-recorded facts the modal does not render; the image is what the
// asker exec'd, the key is the name the entry would take.
func parseSecretAskLine(line string) (a secretAsk, ok bool) {
	f := strings.Split(line, "\t")
	if len(f) != 5 && len(f) != 6 {
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
	if len(f) == 6 {
		a.apps = f[5]
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
	secretAskModal = a
	secretAskModal.open = true
	secretAskModal.sink = 0
	acquireSecretFocus()
	secretLog(MarkerSecretAsk + a.image + " key=" + a.key)
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
// kernel reaps a dead window out from under the seat.
func ensureSecretFocus() bool {
	if secretAskModal.sink == 0 {
		return false
	}
	_, r := queryWindow(int(secretAskModal.sink))
	return r >= 0
}

func closeSecretAsk() {
	if secretAskModal.sink != 0 {
		_ = closeWin(secretAskModal.sink)
	}
	secretAskModal = secretAsk{}
}

// handleSecretAskKey answers the standing prompt. `g` commits the staged
// line through the seat's authority; `d`/Esc declines. A refused write
// is named, never silent. Returns true whenever the prompt owns the key.
func handleSecretAskKey(e vi.Event) bool {
	if !secretAskModal.open {
		return false
	}
	name := secretAskModal.image
	key := secretAskModal.key
	switch {
	case e.Arg0 == 0x29: // Escape
		answerSecretAsk(secretAskModal.id, false)
		secretLog(MarkerSecretDecline + name + " key=" + key)
	case e.Arg1 == 'g':
		if rc := answerSecretAsk(secretAskModal.id, true); rc < 0 {
			secretLog(MarkerSecretRefuse + name + " key=" + key + " " + vi.ErrnoName(rc))
		} else {
			secretLog(MarkerSecretGrant + name + " key=" + key)
		}
	case e.Arg1 == 'd':
		answerSecretAsk(secretAskModal.id, false)
		secretLog(MarkerSecretDecline + name + " key=" + key)
	}
	return true
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

// paintSecretAsk renders the standing prompt — the asker's image, the
// key name, and the two gestures. The value is never rendered anywhere.
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
	label := secretAskModal.image + " wants to store " + secretAskModal.key
	hint := "[g]rant [d]eny"
	clip := plate.Inset(1)
	seatText(c, clip, x+8, y+4, seatSmallPx, label, tok.Ink)
	seatText(c, clip, x+w-8-7*len(hint), y+4, seatSmallPx, hint, tok.InkMuted)
	return n
}
