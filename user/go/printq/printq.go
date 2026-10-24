// Package printq is the PRINT lane's guest-app half (M202c, ADR 0051):
// the /host/PRINT/ directory, the PREQ-<8hex> intent-marker grammar, and
// the staging gesture the print verbs in GOEDIT/WEB/GOREADER share.
//
// Intent and provenance are split, the OUTBOX REQ pattern carried
// verbatim: the app writes the payload and an UNSIGNED PREQ marker
// naming it; the seat (the only signer) emits one signed PRINT record
// per PREQ-marked payload while the adopted mask carries lane_print. An
// unmarked file in PRINT/ is inert — the mirror of an unannounced INBOX
// plant.
//
// A print payload IS a PDF — every in-tree producer is pdfwrite-shaped
// (GOEDIT/WEB render through virelai/pdfwrite; GOREADER's document is a
// PDF already) — so the lane pins it twice: the name must end .pdf and
// the bytes must open %PDF-. The host re-checks both at consume; the
// app-side check is the fail-fast half, before a byte lands.
//
// This is a leaf package on purpose: it imports only vi, so the apps
// (which hostint's codec cannot see — hostint imports shlib for its MAC)
// can stage jobs without an import cycle.
package printq

import "virelai/vi"

const (
	// Dir is the print lane's share directory in kernel path form — the
	// runner creates it at arm (the SDK has no mkdir), the seat polls it
	// beside the lane dir once per tick.
	Dir = "/host/PRINT"
	// JobMax bounds one print payload — the tree's own PDF bound:
	// pdfwrite's output cap and the pdf engine's MaxSource are both
	// 4 MiB, so a job larger than that is nothing any in-tree producer
	// or reader could have made.
	JobMax = 4 << 20
	// NameMax bounds a payload basename — the D4 rule carried symmetric
	// (48 keeps "/host/PRINT/<name>" inside the path bound).
	NameMax = 48
	// ReqPrefix is the intent marker's stem: PREQ-<8hex>, the payload
	// basename verbatim as its whole content.
	ReqPrefix = "PREQ-"
)

// The refusal words Print returns — the lane vocabulary (hostint's
// refusal table). The apps only ever surface them; the seat and the host
// name the same reasons on their own refusal lines.
const (
	// RefuseLaneDisabled is the flags-off answer: PRINT/ does not exist
	// because the runner never armed the lane — the byte-silence rule
	// means the dir's absence IS the lane's posture.
	RefuseLaneDisabled = "lane_disabled"
	RefuseNameRejected = "name_rejected"
	RefuseUnsupported  = "unsupported_kind"
	RefuseTooLarge     = "payload_too_large"
	RefuseIO           = "io_error"
)

// alnumByte reports the D4 charset's leading-byte rule.
func alnumByte(c byte) bool {
	return (c >= 'A' && c <= 'Z') || (c >= 'a' && c <= 'z') || (c >= '0' && c <= '9')
}

// nameByte reports the D4 charset: [A-Za-z0-9._-].
func nameByte(c byte) bool {
	return alnumByte(c) || c == '.' || c == '_' || c == '-'
}

// isPDFName reports whether name ends in the .pdf suffix, ASCII
// case-insensitive — hand-folded like edit's exportPath so this package
// stays free of the unicode case tables.
func isPDFName(name string) bool {
	if len(name) < 4 {
		return false
	}
	s := name[len(name)-4:]
	return s[0] == '.' &&
		(s[1] == 'p' || s[1] == 'P') &&
		(s[2] == 'd' || s[2] == 'D') &&
		(s[3] == 'f' || s[3] == 'F')
}

// IsPDF reports whether b opens with the %PDF- magic — the lane's
// payload-kind check, applied app-side at staging and host-side at
// consume. Empty input is not a PDF.
func IsPDF(b []byte) bool {
	return len(b) >= 5 && b[0] == '%' && b[1] == 'P' && b[2] == 'D' &&
		b[3] == 'F' && b[4] == '-'
}

// Name renders a document basename as a lawful PRINT payload name: the
// D4 fold (bytes outside the charset to '_', a leading non-alphanumeric
// gains an 'F' prefix) and the lane's .pdf rule — a name with no .pdf
// suffix gains ".PDF", the base cut first so the total stays inside
// NameMax. ok=false when no honest name exists (an empty basename).
func Name(base string) (string, bool) {
	out := make([]byte, 0, len(base)+5)
	for i := 0; i < len(base); i++ {
		c := base[i]
		if !nameByte(c) {
			c = '_'
		}
		out = append(out, c)
	}
	if len(out) == 0 {
		return "", false
	}
	if !alnumByte(out[0]) {
		out = append([]byte{'F'}, out...)
	}
	name := string(out)
	if !isPDFName(name) {
		if len(name) > NameMax-4 {
			name = name[:NameMax-4]
		}
		name += ".PDF"
	}
	if len(name) > NameMax {
		return "", false
	}
	return name, true
}

// NameValid is the strict check the seat applies to a PREQ's content —
// the payload basename must already satisfy the rule the apps fold to:
// ≤ NameMax of the charset, leading alnum, and the .pdf suffix (the
// lane's kind in the name, so a REQ naming a non-document is a
// name_rejected before any bytes move).
func NameValid(name string) bool {
	if len(name) == 0 || len(name) > NameMax || !alnumByte(name[0]) ||
		!isPDFName(name) {
		return false
	}
	for i := 0; i < len(name); i++ {
		if !nameByte(name[i]) {
			return false
		}
	}
	return true
}

const hexDigits = "0123456789abcdef"

// hex8 renders v as exactly 8 lowercase hex digits — the PREQ suffix.
func hex8(v uint32) string {
	var b [8]byte
	for i := 0; i < 8; i++ {
		b[i] = hexDigits[(v>>uint((7-i)*4))&0xf]
	}
	return string(b[:])
}

// ReqName is the intent marker's filename for an app-chosen 32-bit
// suffix. The suffix needs only per-app uniqueness — the content is
// authoritative and a collision just coalesces two requests for the same
// intent (the REQ collision rule).
func ReqName(id uint32) string { return ReqPrefix + hex8(id) }

// IsReqName reports whether name is a PREQ-<8hex> intent marker. Hex case
// is accepted like the lane's record-name parse (the share's filesystem
// is case-insensitive anyway).
func IsReqName(name string) bool {
	if len(name) != len(ReqPrefix)+8 || name[:len(ReqPrefix)] != ReqPrefix {
		return false
	}
	for i := len(ReqPrefix); i < len(name); i++ {
		c := name[i]
		if (c < '0' || c > '9') && (c < 'a' || c > 'f') && (c < 'A' || c > 'F') {
			return false
		}
	}
	return true
}

// IO is the file-op seam set the staging gesture takes — the share's
// file ABI plus a uniqueness source for the PREQ suffix. The production
// value wires vi; tests drive the whole gesture in memory.
type IO struct {
	// WriteSafe publishes b at path through the share's temp+rename rule.
	WriteSafe func(path string, b []byte) int64
	// IsDir reports whether path lists as a directory — the lane's
	// armed check (PRINT/ absent = lane_disabled).
	IsDir func(path string) bool
	// Rand fills p with the PREQ suffix entropy (vi.Random; time is the
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

// Print stages one print job: the PDF bytes as PRINT/<name> (temp+rename
// per the share's atomicity rule), then the PREQ marker naming it — the
// seat signs only what is both present and PREQ-marked. Returns "" on
// success or a refusal word: name_rejected (no honest name),
// unsupported_kind (the bytes are not a PDF), payload_too_large
// (> JobMax), lane_disabled (PRINT/ absent — the lane is not armed),
// io_error. A failed marker write leaves the payload unmarked — inert by
// rule, never attributed.
func (io IO) Print(name string, pdf []byte) string {
	if !NameValid(name) {
		return RefuseNameRejected
	}
	if !IsPDF(pdf) {
		return RefuseUnsupported
	}
	if len(pdf) > JobMax {
		return RefuseTooLarge
	}
	if io.IsDir != nil && !io.IsDir(Dir) {
		return RefuseLaneDisabled
	}
	if rc := io.WriteSafe(Dir+"/"+name, pdf); rc < 0 {
		return RefuseIO
	}
	var suf [4]byte
	if io.Rand != nil {
		_, _ = io.Rand(suf[:])
	}
	v := uint32(suf[0]) | uint32(suf[1])<<8 | uint32(suf[2])<<16 | uint32(suf[3])<<24
	if v == 0 {
		v = uint32(vi.Time()) | 1 // entropy-free fallback stays nonzero
	}
	if rc := io.WriteSafe(Dir+"/"+ReqName(v), []byte(name)); rc < 0 {
		return RefuseIO
	}
	return ""
}
