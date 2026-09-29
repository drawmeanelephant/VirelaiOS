// GOTABWM.ELF — M82d2 (issue #1785): the notifications center's POLICY half.
// M82d1 (#1771) built the center as an in-memory history; this file is what
// makes it a place rather than a flash. Two policies, both extending that one
// center (there is no second queue anywhere):
//
//   - Do-not-disturb. One settings key (`notify_dnd`, on|off — the GOSET row),
//     one seat control (the center's header toggle), one state (notifyDND).
//     While it is on, a NEW notice still lands in the center's history — that
//     is the whole point: suppression must never lose a notice — but it raises
//     no toast. The app cannot tell: the kind-12 ack is applied either way, so
//     an app never grows a "the user is away" code path. Toasts already on the
//     strip when DND turns on run out normally; only NEW notices are held.
//   - Persistence. The history survives a seat restart through the M66b path:
//     one file, published by temp + fsync + delete/rename (vi.WriteFileSafe),
//     so the live path only ever holds the old bytes, the new bytes, or none.
//     The bytes carry their own integrity (magic, count, length-framed
//     records, an FNV-1a trailer), because the promise on the read side is
//     stronger than "the write is atomic": a file that fails ANY check is
//     refused WHOLE — never a partial history — and heals to a valid empty
//     one, so a bad file costs the user their history once and is never
//     re-reported on the boot after.
//
// The write is coalesced to the composite tick. A notice is an app's to send
// at IPC speed, unlike a tab open or a user's click (session.go writes through
// on every mutation and says why that is fine there). One fsync'd publish per
// notice would put a stalled share on the seat's request path under a flood,
// which is exactly what the queue's drop-oldest bound exists to prevent. So a
// mutation only marks the history dirty; the next tick publishes ONCE however
// many changed, and a clean exit publishes the tail. A hard kill can lose the
// changes of the last tick, never corrupt the file.
//
// Restored entries name senders that no longer exist (window ids die with the
// seat's process), so every one is restored with its source LATCHED closed —
// the same latch M82d1 uses so a reused id can never turn a historical item
// into a link to an unrelated app.
package main

import (
	"virelai/settings"
	"virelai/vi"
)

const (
	// notifyHistoryPath is the persisted center history on the share.
	notifyHistoryPath = "/host/NOTIFY.HIST"

	// MarkerNotifyDND names a do-not-disturb transition and who asked for
	// it: `via=boot` (the persisted key, read at start — printed only when
	// it is ON, so a default boot's log is unchanged), `via=seat` (the
	// center's header control; carries `persisted=1|0`), `via=settings`
	// (GOSET's publish, applied live).
	MarkerNotifyDND = "gotabwm: notify dnd="
	// MarkerNotifyHeld replaces MarkerNotify for a notice DND kept off the
	// strip. `id=` is the SENDER's tab id, like every other notify marker.
	// The notice is in the center's history by the time this prints.
	MarkerNotifyHeld = "gotabwm: notify held id="
	// MarkerNotifyHistRestore is printed at seat start, before the loop and
	// therefore before the first composite paint, once a valid history file
	// was decoded into the center.
	MarkerNotifyHistRestore = "gotabwm: notify history restore n="
	// MarkerNotifyHistBad names a history file refused whole; the center
	// starts empty. MarkerNotifyHistHealed follows once the valid empty
	// replacement is published.
	MarkerNotifyHistBad    = "gotabwm: notify history bad"
	MarkerNotifyHistHealed = "gotabwm: notify history healed"
	// MarkerNotifyHistWrite is printed after a publish returned; the fail
	// marker prints once per failure streak, not once per retrying tick.
	MarkerNotifyHistWrite = "gotabwm: notify history write n="
	MarkerNotifyHistFail  = "gotabwm: notify history write fail"

	// Who asked for a do-not-disturb change (the marker's via= word).
	notifyViaBoot     = "boot"
	notifyViaSeat     = "seat"
	notifyViaSettings = "settings"
)

// notifyDND is the seat's do-not-disturb state. Off is the zero value and the
// compiled default, so a host test that never configures it is unaffected.
var notifyDND bool

// saveNotifyDND is the persistence seam for the seat control, a var so a host
// test can observe it: settings.Load/Save degrade to ENOSYS off the guest.
var saveNotifyDND = persistNotifyDND

// persistNotifyDND writes the choice into SETTINGS.TXT through the codec's
// crash-safe save, the same publish GOSET uses. A corrupt file is refused
// whole (settings.File.Save returns SaveRefused), exactly as it is for GOSET:
// the seat control must not launder a file the kernel rejected.
func persistNotifyDND(on bool) bool {
	f := settings.Load()
	if f.State == settings.StateCorrupt {
		return false
	}
	val := "off"
	if on {
		val = "on"
	}
	f.Rows = settings.Set(f.Rows, settings.NotifyDNDKey, val)
	f.State = settings.StateOK
	return f.Save() == 0
}

// configureNotifyDND applies the persisted key at seat start. Only an intact
// file can turn DND on: a corrupt file is refused whole (compiled default,
// off), and a value that is not exactly `on`/`off` is the default too, so a
// typo can never silence the user's notifications.
func configureNotifyDND(f settings.File) {
	notifyDND = false
	if f.State != settings.StateOK {
		return
	}
	value, _ := f.Effective(settings.NotifyDNDKey)
	on, _ := settings.NotifyDND(value)
	if on {
		setNotifyDND(true, notifyViaBoot)
	}
}

// setNotifyDND applies a do-not-disturb state and reports it. The state
// change comes first and the marker after, so the line can never claim a mode
// the seat is not in. A seat-originated change is persisted and offered to
// the settings bus (an app subscribed to the key through appkit hears it);
// the other two origins already have the file and the bus value.
func setNotifyDND(on bool, via string) {
	notifyDND = on
	word := "off"
	if on {
		word = "on"
	}
	line := MarkerNotifyDND + word + " via=" + via
	if via == notifyViaSeat {
		persisted := "0"
		if saveNotifyDND(on) {
			persisted = "1"
		}
		line += " persisted=" + persisted
		if updateSettingsBusValue(settings.NotifyDNDKey, word) {
			broadcastSettingsChange(settings.NotifyDNDKey)
		}
	}
	vi.ConsoleLine(line)
}

// toggleNotifyDND is the seat control: the center header's toggle.
func toggleNotifyDND() { setNotifyDND(!notifyDND, notifyViaSeat) }

// applyNotifyDNDSetting is the GOSET publish path (settings_bus.go): the
// value is already persisted and already recorded on the bus. A value that is
// not exactly on/off means the default, like the boot read.
func applyNotifyDNDSetting(value string) {
	on, _ := settings.NotifyDND(value)
	setNotifyDND(on, notifyViaSettings)
}

// --- the persisted history --------------------------------------------------

// The file: "VNH" + version 1, a count byte, that many records of
// (source length, text length, source, text), then the FNV-1a 32 of every
// byte before it, little-endian. Guest-safe by construction (no fmt, no os,
// no hash package), like the `.tabs` codec beside it.
const (
	notifyHistMagic    = "VNH\x01"
	notifyHistSrcMax   = 48 // the center's own display bound (centerText)
	notifyHistTextMax  = vi.WmRpcTitleMax
	notifyHistMaxBytes = len(notifyHistMagic) + 1 +
		NotifyCenterMax*(2+notifyHistSrcMax+notifyHistTextMax) + 4
)

func fnv32(b []byte) uint32 {
	h := uint32(2166136261)
	for _, c := range b {
		h ^= uint32(c)
		h *= 16777619
	}
	return h
}

func clampNotifyField(s string, max int) string {
	if len(s) > max {
		return s[:max]
	}
	return s
}

// encodeNotifyHistory serializes the newest NotifyCenterMax entries. Sender
// ids and the closed latch are not written: neither means anything to the
// next seat process (see the file comment).
func encodeNotifyHistory(h []centerNotification) []byte {
	if len(h) > NotifyCenterMax {
		h = h[len(h)-NotifyCenterMax:]
	}
	buf := make([]byte, 0, notifyHistMaxBytes)
	buf = append(buf, notifyHistMagic...)
	buf = append(buf, byte(len(h)))
	for _, e := range h {
		src := clampNotifyField(e.source, notifyHistSrcMax)
		text := clampNotifyField(e.text, notifyHistTextMax)
		buf = append(buf, byte(len(src)), byte(len(text)))
		buf = append(buf, src...)
		buf = append(buf, text...)
	}
	sum := fnv32(buf)
	return append(buf, byte(sum), byte(sum>>8), byte(sum>>16), byte(sum>>24))
}

// decodeNotifyHistory is all-or-nothing: any check that fails — size, magic,
// checksum, count, a field bound, an empty text (the seat never accepts one),
// or bytes left over after the last record — returns ok=false and NO entries.
// The checksum is verified before a single record is read, so a torn or
// bit-flipped file cannot yield a plausible-looking prefix.
func decodeNotifyHistory(b []byte) ([]centerNotification, bool) {
	head := len(notifyHistMagic)
	if len(b) < head+1+4 || len(b) > notifyHistMaxBytes || string(b[:head]) != notifyHistMagic {
		return nil, false
	}
	body, tail := b[:len(b)-4], b[len(b)-4:]
	want := uint32(tail[0]) | uint32(tail[1])<<8 | uint32(tail[2])<<16 | uint32(tail[3])<<24
	if fnv32(body) != want {
		return nil, false
	}
	n := int(body[head])
	if n > NotifyCenterMax {
		return nil, false
	}
	out := make([]centerNotification, 0, n)
	off := head + 1
	for i := 0; i < n; i++ {
		if off+2 > len(body) {
			return nil, false
		}
		srcLen, textLen := int(body[off]), int(body[off+1])
		off += 2
		if textLen == 0 || srcLen > notifyHistSrcMax || textLen > notifyHistTextMax ||
			off+srcLen+textLen > len(body) {
			return nil, false
		}
		source := string(body[off : off+srcLen])
		off += srcLen
		text := string(body[off : off+textLen])
		off += textLen
		out = append(out, centerNotification{source: source, text: text, sourceGone: true})
	}
	if off != len(body) {
		return nil, false
	}
	return out, true
}

type notifyHistState uint8

const (
	notifyHistMissing notifyHistState = iota
	notifyHistRestored
	notifyHistCorrupt
)

// The file seams: host tests inject stubs, since vi.WriteFileSafe and
// vi.ReadFileAll degrade to ENOSYS off the guest. Deliberately NOT the
// session's writeHostFile, so a test that stubs that seam cannot have a
// history publish land in its capture.
var (
	readNotifyFile  = vi.ReadFileAll
	writeNotifyFile = func(path string, data []byte) bool {
		return vi.WriteFileSafe(path, data) == 0
	}
)

var (
	notifyHistDirty      bool
	notifyHistFailLogged bool
)

// restoreNotifyHistoryBytes keeps the missing/corrupt/present decision
// testable on the host. Missing is the first boot (or a share that never held
// a notice) and is silent. A file that opened but holds nothing is NOT
// missing: it is a file that failed every check, which is corrupt.
func restoreNotifyHistoryBytes(b []byte, readResult int64) ([]centerNotification, notifyHistState) {
	if readResult < 0 {
		return nil, notifyHistMissing
	}
	h, ok := decodeNotifyHistory(b)
	if !ok {
		return nil, notifyHistCorrupt
	}
	return h, notifyHistRestored
}

// loadNotifyHistory restores the center at seat start. It runs before the
// composite loop, so the history is in place before the first frame is
// painted (the clock panel's hint reads it) and before the user can open the
// panel: there is no flash of an empty center that then fills in.
func loadNotifyHistory() notifyHistState {
	b, r := readNotifyFile(notifyHistoryPath, notifyHistMaxBytes+1)
	h, state := restoreNotifyHistoryBytes(b, r)
	switch state {
	case notifyHistRestored:
		notifyCenter = h
		// Fresh ids, continuing past the restored ones so a later push can
		// never collide with an entry a toast could name.
		for i := range notifyCenter {
			notifyCenter[i].id = uint32(i + 1)
		}
		notifyCenterNextID = uint32(len(notifyCenter))
		notifyHistDirty = false
		vi.ConsoleLine(MarkerNotifyHistRestore + vi.Itoa64(int64(len(notifyCenter))))
	case notifyHistCorrupt:
		// Refused WHOLE: nothing from the file is trusted, and the center
		// is explicitly emptied rather than left to whatever it held. The
		// heal publishes a valid empty history immediately, so the file is
		// not re-refused on every later boot.
		notifyCenter = nil
		notifyCenterNextID = 0
		vi.ConsoleLine(MarkerNotifyHistBad)
		notifyHistDirty = true
		if publishNotifyHistory() {
			vi.ConsoleLine(MarkerNotifyHistHealed)
		}
	}
	return state
}

// notifyHistoryChanged marks the center's history as needing a publish. Every
// mutation of the history itself calls it; the closed latch does not (it is
// not persisted).
func notifyHistoryChanged() { notifyHistDirty = true }

// flushNotifyHistory publishes a changed history: once per tick however many
// mutations landed, and at a clean exit. A failed publish stays dirty and is
// retried on the next tick.
func flushNotifyHistory() {
	if notifyHistDirty {
		publishNotifyHistory()
	}
}

func publishNotifyHistory() bool {
	if !writeNotifyFile(notifyHistoryPath, encodeNotifyHistory(notifyCenter)) {
		if !notifyHistFailLogged {
			notifyHistFailLogged = true
			vi.ConsoleLine(MarkerNotifyHistFail)
		}
		return false
	}
	notifyHistDirty = false
	notifyHistFailLogged = false
	vi.ConsoleLine(MarkerNotifyHistWrite + vi.Itoa64(int64(len(notifyCenter))))
	return true
}
