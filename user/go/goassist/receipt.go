// receipt.go — the M205d act-receipt lane (#3811, ADR 0052 amendment):
// every act GOASSIST resolves leaves a verifiable receipt — a
// RCPT-<epoch>-<seq> file in the app's own lane, <store>/AUDIT/
// (/host/ASSIST/AUDIT/ on the default root, /disk/ASSIST/AUDIT/ under
// the data_root opt-in). The record is the same two-line shape AGENTD's
// audit lane carries — the JSON receipt, then 64 lowercase hex of
// HMAC-SHA256 over line 1 — plus a caller field naming GOASSIST.ELF,
// MAC'd under `assist-hmac`: the receipt key GOASSIST and GOTOP hold
// and AGENTD does not (the channel's agent-hmac row stays AGENTD's
// alone, ADR 0044). Files under AUDIT/ stay unreachable through fs.* —
// the flat NAME grammar still has no `/`.
//
// The lane is fail-closed like the daemon's: a missing key row refuses
// the act by name BEFORE the consent dialog (no_receipt_key — nothing
// runs, no dialog), and a refused receipt write marks the lane dead so
// the next act refuses audit_full rather than run unreceipted. One
// receipt covers every resolved outcome — allow's ok/refuse, the deny's
// declined, and a cap-gated call's named refusal when the lane is armed
// to write it.
package main

import (
	"sort"
	"strings"

	"virelai/agentverbs"
	"virelai/shlib"
	"virelai/vi"
)

const (
	// assistStem is the receipt filename prefix — the epoch+seq suffix
	// keeps lexical order chronological within one epoch, the daemon's
	// VHI1 shape.
	assistStem = "RCPT-"
	// assistFileMax is the lane's receipt bound (the auditFileMax
	// contract): at the cap the writer drops the lexically smallest
	// name from an earlier epoch before publishing the next.
	assistFileMax = 64
	// assistKeyMax bounds the assist-hmac row read — the value bytes
	// are the HMAC key verbatim, never decoded (the a5x32 precedent).
	assistKeyMax = 256
	// assistSecret is the SECRETS.TXT row GOASSIST.ELF and GOTOP.ELF
	// share: one uid_user credential on a two-app allowlist (the
	// kernel's store dedupes on name+uid, so a second uid-1000 row
	// would overwrite this row's binding — the M173d split only
	// applies across uids).
	assistSecret = "assist-hmac"
	// assistLaneDir is the lane's directory name under the store.
	assistLaneDir = "AUDIT"
)

// The lane gate's named refusals — the words actPass records and the
// go-assist spec asserts on.
const (
	refuseNoKey   = "no_receipt_key" // no assist-hmac row was served
	refuseEntropy = "no_entropy"     // the epoch draw failed
	refuseLane    = "audit_full"     // mkdir/write refused: the lane is dead
)

// Serial markers for the receipt legs (asserted AFTER the write they
// name — the same discipline the act markers keep).
const (
	markerRcpt      = "goassist: rcpt "
	markerAuditFull = "goassist: audit full"
	markerAuditDrop = "goassist: audit drop "
)

// assistLane is GOASSIST's receipt state: the loaded key, this boot's
// epoch, the next seq, and the sorted lane names the bound rotates
// over. armed marks that the posture ran once this boot; dead is the
// named-degraded mark a refused write sets — the lane fails closed and
// acts refuse audit_full rather than run unreceipted.
type assistLane struct {
	armed  bool
	hasKey bool
	noRand bool
	dead   bool
	key    []byte
	epoch  uint32
	seq    uint32
	names  []string
}

var assist assistLane

// Seams for the receipt lane's kernel reads beyond the host interface:
// the secret row, the epoch draw, and the wall clock (the same seam
// discipline the act loop's actSeams keeps — tests fake the lot).
var (
	assistKeyFn  = shlib.StoreGet
	assistRandFn = vi.Random
	assistTimeFn = vi.Time
)

// refusal renders the lane's posture as the named refusal actPass
// records, "" when armed.
func (l *assistLane) refusal() string {
	switch {
	case !l.hasKey:
		return refuseNoKey
	case l.noRand:
		return refuseEntropy
	case l.dead:
		return refuseLane
	}
	return ""
}

// assistLaneArm resolves the lane's posture once per boot — the key
// row, the epoch draw, the lane dir, and the names the rotation bound
// reads — and reports the named refusal the act gate records. "" means
// armed: the act may proceed to consent. The dir is created like the
// store's own (MkdirShare); a refusal dead-marks the lane at once.
func (v *view) assistLaneArm() string {
	l := &assist
	if l.armed {
		return l.refusal()
	}
	l.armed = true
	var kb [assistKeyMax]byte
	if n := assistKeyFn(assistSecret, kb[:]); n > 0 {
		l.key = append([]byte(nil), kb[:n]...)
		l.hasKey = true
	}
	if !l.hasKey {
		return refuseNoKey
	}
	var e [4]byte
	if m, err := assistRandFn(e[:]); err != nil || m != len(e) {
		l.noRand = true
		return refuseEntropy
	}
	l.epoch = uint32(e[0])<<24 | uint32(e[1])<<16 | uint32(e[2])<<8 | uint32(e[3])
	if err := v.h.MkdirShare(v.assistDir()); err != nil {
		l.dead = true
		return refuseLane
	}
	l.names = v.assistList()
	return ""
}

// assistDir resolves the lane under this boot's store: /host/ASSIST/
// AUDIT or /disk/ASSIST/AUDIT — the app's own data root, never AGENT's.
func (v *view) assistDir() string { return v.dir + "/" + assistLaneDir }

// assistList returns the lane's file names sorted ascending — the
// drop candidate is always names[0]. Every file counts toward the
// bound, receipt-shaped or not (the F8 discipline: a planted name is
// lane garbage the rotation evicts like any other). A listing failure
// returns nil — the write still publishes, the bound just has nothing
// to rotate against.
func (v *view) assistList() []string {
	names, err := v.h.ListDir(v.assistDir())
	if err != nil {
		return nil
	}
	sort.Strings(names)
	return names
}

// actReceipt emits one act's receipt: the JSON line, its MAC line, then
// the publish — a no-op while the lane is unarmed, so a cap-refused
// call still receipts when the lane can write and costs nothing when
// the act was already refused for want of a lane. agent names the open
// conversation ("-" when there is none); caller always names
// GOASSIST.ELF — GOTOP reads both fields off the same flat shape it
// verifies for AGENTD's lane.
func (v *view) actReceipt(verb string, fields map[string]agentverbs.Field, result string) {
	l := &assist
	if !l.armed || l.refusal() != "" {
		return
	}
	l.seq++
	agent := "-"
	if v.cur != nil && v.cur.name != "" {
		agent = v.cur.name
	}
	var sb strings.Builder
	sb.WriteString(`{"seq":`)
	sb.WriteString(vi.Itoa64(int64(l.seq)))
	sb.WriteString(`,"epoch":`)
	rcptJStr(&sb, hex8(l.epoch))
	sb.WriteString(`,"agent":`)
	rcptJStr(&sb, agent)
	sb.WriteString(`,"caller":`)
	rcptJStr(&sb, appName)
	sb.WriteString(`,"verb":`)
	rcptJStr(&sb, verb)
	sb.WriteString(`,"args":`)
	sb.WriteString(rcptArgs(fields))
	sb.WriteString(`,"ts":`)
	sb.WriteString(vi.Itoa64(assistTimeFn()))
	sb.WriteString(`,"mono":`)
	sb.WriteString(vi.Itoa64(v.h.Nanos()))
	sb.WriteString(`,"result":`)
	rcptJStr(&sb, result)
	sb.WriteByte('}')
	line := sb.String()
	mac := shlib.HMACSHA256(l.key, []byte(line))
	name := assistStem + hex8(l.epoch) + "-" + hex8(l.seq)
	dir := v.assistDir()
	body := line + "\n" + hexStr(mac[:]) + "\n"
	if err := v.h.WriteShareFile(dir+"/"+name, []byte(body)); err != nil {
		l.dead = true
		v.h.ConsoleLine(markerAuditFull)
		return
	}
	v.h.ConsoleLine(markerRcpt + name)
	l.names = v.assistList()
	// The D4 lexical rotation, verbatim from the daemon's lane: the
	// victim is the smallest name from an EARLIER epoch — a boot's own
	// receipts are always its chronologically newest, so the fresh
	// epoch never eats its own evidence. When every name is this
	// epoch's the smallest is also the chronologically oldest; the
	// just-written name can never be the victim either way.
	prefix := assistStem + hex8(l.epoch)
	for len(l.names) > assistFileMax {
		victim := 0
		for i, n := range l.names {
			if !strings.HasPrefix(n, prefix) {
				victim = i
				break
			}
		}
		if err := v.h.DeleteFile(dir + "/" + l.names[victim]); err != nil {
			v.h.ConsoleLine("goassist: audit io_error")
			break
		}
		v.h.ConsoleLine(markerAuditDrop + l.names[victim])
		l.names = append(l.names[:victim], l.names[victim+1:]...)
	}
}

// rcptArgs renders the receipt's args object from the call's flat
// fields — names sorted for a stable line, each value in its grammar
// kind (string quoted, int bare, bool literal). Only bounded metadata:
// the names the @@call grammar already proved, values clipped at the
// receipt's 192-byte field bound.
func rcptArgs(fields map[string]agentverbs.Field) string {
	if len(fields) == 0 {
		return "{}"
	}
	keys := make([]string, 0, len(fields))
	for k := range fields {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var sb strings.Builder
	sb.WriteByte('{')
	for i, k := range keys {
		if i > 0 {
			sb.WriteByte(',')
		}
		rcptJStr(&sb, k)
		sb.WriteByte(':')
		switch f := fields[k]; f.Kind {
		case agentverbs.FInt:
			sb.WriteString(vi.Itoa64(f.Num))
		case agentverbs.FBool:
			sb.WriteString(f.Str)
		default:
			rcptJStr(&sb, clipRcpt(f.Str, 192))
		}
	}
	sb.WriteByte('}')
	return sb.String()
}

// clipRcpt bounds a verbatim receipt field — the receipt ceiling holds
// regardless of what the call line carried.
func clipRcpt(s string, max int) string {
	if len(s) > max {
		return s[:max]
	}
	return s
}

// hex8 renders v as 8 lowercase hex digits — the epoch and seq fields
// share the writer's uint32 shape with the daemon's lane so one
// verifier reads both.
func hex8(v uint32) string {
	const digits = "0123456789abcdef"
	var b [8]byte
	for i := 0; i < 8; i++ {
		b[i] = digits[(v>>uint((7-i)*4))&0xf]
	}
	return string(b[:])
}

// hexStr renders bytes as lowercase hex — the receipt's MAC line.
func hexStr(b []byte) string {
	const digits = "0123456789abcdef"
	out := make([]byte, 2*len(b))
	for i, c := range b {
		out[2*i] = digits[c>>4]
		out[2*i+1] = digits[c&0xf]
	}
	return string(out)
}

// rcptJStr appends a JSON string literal: '"', '\' and control bytes
// are escaped (the wire encoder's shape — a receipt's fields carry
// names and codes only, never payload bytes).
func rcptJStr(sb *strings.Builder, s string) {
	const digits = "0123456789abcdef"
	sb.WriteByte('"')
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case c == '"' || c == '\\':
			sb.WriteByte('\\')
			sb.WriteByte(c)
		case c < 0x20:
			sb.WriteString(`\u00`)
			sb.WriteByte(digits[c>>4])
			sb.WriteByte(digits[c&0xf])
		default:
			sb.WriteByte(c)
		}
	}
	sb.WriteByte('"')
}
