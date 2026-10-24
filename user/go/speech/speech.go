// Package speech is the SPEAK lane's guest-app half (ADR 0051 V2-D8):
// the /host/SAY/ directory, the SAY-<8hex> intent-marker grammar, and
// the staging gesture GOSH `say` and GOA11Y's -say hook share.
//
// Intent and provenance are split, the OUTBOX REQ pattern: the app
// writes an UNSIGNED SAY marker whose whole content is the utterance;
// the seat (the only signer) emits one signed SPEAK record per marker
// while the adopted mask carries lane_speech. Unlike OUTBOX the payload
// needs no second file — a bounded text fits the marker itself, so the
// marker IS the utterance. An unmarked file in SAY/ is inert; the seat
// reads only the marker grammar.
//
// This is a leaf package on purpose: it imports only vi and utf8, so the
// shell (which hostint's codec cannot see — hostint imports shlib for
// its MAC) can stage utterances without an import cycle.
package speech

import (
	"unicode/utf8"

	"virelai/vi"
)

const (
	// Dir is the speech lane's share directory in kernel path form — the
	// runner creates it at arm (the SDK has no mkdir), the seat polls it
	// beside the lane dir once per tick.
	Dir = "/host/SAY"
	// TextMax bounds one utterance — the VHI1 payload bound (SPEAK's
	// grammar is plen 1..512, one utterance per record).
	TextMax = 512
	// SayPrefix is the intent marker's stem: SAY-<8hex>, the utterance
	// text verbatim as its whole content.
	SayPrefix = "SAY-"
)

// The refusal words Say returns — the lane vocabulary (hostint's refusal
// table). The apps only ever surface them; the seat and the host name
// the same reasons on their own refusal lines.
const (
	// RefuseLaneDisabled is the flags-off answer: SAY/ does not exist
	// because the runner never armed the lane — the byte-silence rule
	// means the dir's absence IS the lane's posture.
	RefuseLaneDisabled = "lane_disabled"
	RefuseNotText      = "payload_not_text"
	RefuseTooLarge     = "payload_too_large"
	RefuseIO           = "io_error"
)

const hexDigits = "0123456789abcdef"

// hex8 renders v as exactly 8 lowercase hex digits — the SAY suffix.
func hex8(v uint32) string {
	var b [8]byte
	for i := 0; i < 8; i++ {
		b[i] = hexDigits[(v>>uint((7-i)*4))&0xf]
	}
	return string(b[:])
}

// SayName is the intent marker's filename for an app-chosen 32-bit
// suffix. The suffix needs only per-app uniqueness — every marker's
// content is authoritative and a collision just coalesces two
// utterances (the REQ collision rule).
func SayName(id uint32) string { return SayPrefix + hex8(id) }

// IsSayName reports whether name is a SAY-<8hex> intent marker. Hex case
// is accepted like the lane's record-name parse (the share's filesystem
// is case-insensitive anyway).
func IsSayName(name string) bool {
	if len(name) != len(SayPrefix)+8 || name[:len(SayPrefix)] != SayPrefix {
		return false
	}
	for i := len(SayPrefix); i < len(name); i++ {
		c := name[i]
		if (c < '0' || c > '9') && (c < 'a' || c > 'f') && (c < 'A' || c > 'F') {
			return false
		}
	}
	return true
}

// Clamp cuts text at the TextMax byte bound on a rune boundary — a
// programmatic utterance (GOA11Y's outline report) is shortened, never
// refused: a reader's report has no user to answer payload_too_large
// to. Interactive callers use Say directly and get the named refusal.
func Clamp(text string) string {
	if len(text) <= TextMax {
		return text
	}
	cut := TextMax
	for cut > 0 && !utf8.RuneStart(text[cut]) {
		cut--
	}
	return text[:cut]
}

// IO is the file-op seam set the staging gesture takes — the share's
// file ABI plus a uniqueness source for the SAY suffix. The production
// value wires vi; tests drive the whole gesture in memory.
type IO struct {
	// WriteSafe publishes b at path through the share's temp+rename rule.
	WriteSafe func(path string, b []byte) int64
	// IsDir reports whether path lists as a directory — the lane's
	// armed check (SAY/ absent = lane_disabled).
	IsDir func(path string) bool
	// Rand fills p with the SAY suffix entropy (vi.Random; time is the
	// caller's fallback when it fails).
	Rand func(p []byte) (int, error)
}

// Guest is the production seam set: the vi file ABI, verbatim.
var Guest = IO{
	WriteSafe: vi.WriteFileSafe,
	Rand:      vi.Random,
	IsDir: func(path string) bool {
		var rows [vi.MaxDirEntries]vi.DirEntry
		_, rc := vi.DirList(path, rows[:])
		return rc >= 0
	},
}

// Say stages one utterance: the text verbatim as a SAY-<8hex> marker's
// whole content, temp+rename per the share's atomicity rule. Returns ""
// on success or a refusal word: payload_not_text (empty or invalid
// UTF-8), payload_too_large (> TextMax), lane_disabled (SAY/ absent —
// the lane is not armed), io_error.
func (io IO) Say(text string) string {
	if len(text) == 0 || !utf8.ValidString(text) {
		return RefuseNotText
	}
	if len(text) > TextMax {
		return RefuseTooLarge
	}
	if io.IsDir != nil && !io.IsDir(Dir) {
		return RefuseLaneDisabled
	}
	var suf [4]byte
	if io.Rand != nil {
		_, _ = io.Rand(suf[:])
	}
	v := uint32(suf[0]) | uint32(suf[1])<<8 | uint32(suf[2])<<16 | uint32(suf[3])<<24
	if v == 0 {
		v = uint32(vi.Time()) | 1 // entropy-free fallback stays nonzero
	}
	if rc := io.WriteSafe(Dir+"/"+SayName(v), []byte(text)); rc < 0 {
		return RefuseIO
	}
	return ""
}
