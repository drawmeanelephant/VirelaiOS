package flintstore

import (
	"bytes"
	"encoding/binary"
	"encoding/json"
	"unicode/utf8"

	"virelai/ed25519"
	"virelai/sshlib"
)

// Canonical signature domains (ADR 0049 §Canonical bytes).
const (
	DomainOps    = "FLINT-OPS/1\x00"
	DomainChange = "FLINT-CHANGE/1\x00"
)

func parseLowerHex(s string, wantBytes int) ([]byte, bool) {
	if len(s) != wantBytes*2 {
		return nil, false
	}
	for i := 0; i < len(s); i++ {
		c := s[i]
		if !((c >= '0' && c <= '9') || (c >= 'a' && c <= 'f')) {
			return nil, false
		}
	}
	return sshlib.ParseHex(s)
}

func parseHex32Lower(s string) ([32]byte, bool) {
	var out [32]byte
	b, ok := parseLowerHex(s, 32)
	if !ok {
		return out, false
	}
	copy(out[:], b)
	return out, true
}

// ParseHex32Lower decodes exactly 64 lowercase hex characters into 32 bytes.
func ParseHex32Lower(s string) ([32]byte, bool) { return parseHex32Lower(s) }

func parseHex64Lower(s string) ([64]byte, bool) {
	var out [64]byte
	b, ok := parseLowerHex(s, 64)
	if !ok {
		return out, false
	}
	copy(out[:], b)
	return out, true
}

func writeU32LE(buf *bytes.Buffer, v uint32) {
	var b [4]byte
	binary.LittleEndian.PutUint32(b[:], v)
	buf.Write(b[:])
}

func writeU64LE(buf *bytes.Buffer, v uint64) {
	var b [8]byte
	binary.LittleEndian.PutUint64(b[:], v)
	buf.Write(b[:])
}

func writeStr(buf *bytes.Buffer, s string) {
	writeU32LE(buf, uint32(len(s)))
	buf.WriteString(s)
}

func writeOptStr(buf *bytes.Buffer, s string) {
	if s == "" {
		buf.WriteByte(0x00)
		return
	}
	buf.WriteByte(0x01)
	writeStr(buf, s)
}

func writeOptSpan(buf *bytes.Buffer, sp *Span) {
	if sp == nil {
		buf.WriteByte(0x00)
		return
	}
	buf.WriteByte(0x01)
	writeStr(buf, sp.File)
	writeU32LE(buf, sp.Start)
	writeU32LE(buf, sp.End)
}

func writeOptHash32(buf *bytes.Buffer, hexStr string) bool {
	if hexStr == "" {
		buf.WriteByte(0x00)
		return true
	}
	h, ok := parseHex32Lower(hexStr)
	if !ok {
		return false
	}
	buf.WriteByte(0x01)
	buf.Write(h[:])
	return true
}

func writeOptUUID(buf *bytes.Buffer, u *string) bool {
	if u == nil {
		buf.WriteByte(0x00)
		return true
	}
	id, ok := ParseUUID(*u)
	if !ok {
		return false
	}
	buf.WriteByte(0x01)
	buf.Write(id[:])
	return true
}

// OpBytes encodes a single Op into its ADR 0049 §Canonical bytes representation.
func OpBytes(op Op) ([]byte, error) {
	code := OpTypeCode(op.Type)
	if code == 0 {
		return nil, Refuse("change_malformed", "")
	}
	atomID, ok := ParseUUID(op.AtomID)
	if !ok {
		return nil, Refuse("change_malformed", "")
	}
	var buf bytes.Buffer
	buf.WriteByte(code)
	buf.Write(atomID[:])
	writeOptStr(&buf, op.Kind)
	writeOptStr(&buf, op.NewName)
	writeOptSpan(&buf, op.NewSpan)
	if !writeOptHash32(&buf, op.NewHash) {
		return nil, Refuse("change_malformed", "")
	}
	if !writeOptHash32(&buf, op.NewSigHash) {
		return nil, Refuse("change_malformed", "")
	}
	if !writeOptHash32(&buf, op.OldHash) {
		return nil, Refuse("change_malformed", "")
	}
	return buf.Bytes(), nil
}

// OpsHash computes
// SHA-256("FLINT-OPS/1\x00" || u32le(n) || op_bytes(op_1) || ... || op_bytes(op_n))
// with ops sorted by (atom_id bytes, type code).
func OpsHash(ops []Op) ([32]byte, error) {
	var zero [32]byte
	sorted := make([]Op, len(ops))
	copy(sorted, ops)
	SortOps(sorted)

	var buf bytes.Buffer
	buf.WriteString(DomainOps)
	writeU32LE(&buf, uint32(len(sorted)))
	for _, op := range sorted {
		ob, err := OpBytes(op)
		if err != nil {
			return zero, err
		}
		buf.Write(ob)
	}
	return sshlib.SHA256Sum(buf.Bytes()), nil
}

// SigMsg computes the canonical Change preimage signed by Ed25519 per ADR 0049
// §Canonical bytes:
// "FLINT-CHANGE/1\x00" || id[16] || opt(parent[16]) || stream[16]
// || u64le(ts) || author[32] || str(message) || opt(merged[16]) || ops_hash[32]
func SigMsg(c *Change) ([]byte, error) {
	if c == nil {
		return nil, Refuse("change_malformed", "")
	}
	idBytes, ok := ParseUUID(c.ID)
	if !ok {
		return nil, Refuse("change_malformed", "")
	}
	streamBytes, ok := ParseUUID(c.Stream)
	if !ok {
		return nil, Refuse("change_malformed", "")
	}
	if c.TS < 0 {
		return nil, Refuse("change_malformed", "")
	}
	authorBytes, ok := parseHex32Lower(c.Author)
	if !ok {
		return nil, Refuse("change_malformed", "")
	}
	if !utf8.ValidString(c.Message) {
		return nil, Refuse("change_malformed", "")
	}

	var buf bytes.Buffer
	buf.WriteString(DomainChange)
	buf.Write(idBytes[:])
	if !writeOptUUID(&buf, c.Parent) {
		return nil, Refuse("change_malformed", "")
	}
	buf.Write(streamBytes[:])
	writeU64LE(&buf, uint64(c.TS))
	buf.Write(authorBytes[:])
	writeStr(&buf, c.Message)
	if !writeOptUUID(&buf, c.Merged) {
		return nil, Refuse("change_malformed", "")
	}
	oh, err := OpsHash(c.Ops)
	if err != nil {
		return nil, err
	}
	buf.Write(oh[:])
	return buf.Bytes(), nil
}

// MarshalChangeLine serializes a Change as a single JSON line with sorted Ops
// and a trailing '\n'.
func MarshalChangeLine(c Change) ([]byte, error) {
	sorted := make([]Op, len(c.Ops))
	copy(sorted, c.Ops)
	SortOps(sorted)
	c.Ops = sorted
	line, err := json.Marshal(c)
	if err != nil {
		return nil, err
	}
	return append(line, '\n'), nil
}

// ParseChange unmarshals a single JSONL line into a Change and validates all
// field encodings.
func ParseChange(line []byte) (Change, error) {
	if len(bytes.TrimSpace(line)) == 0 {
		return Change{}, Refuse("change_malformed", "")
	}
	var c Change
	if err := json.Unmarshal(line, &c); err != nil {
		return Change{}, Refuse("change_malformed", "")
	}
	if c.Ops == nil || c.TS <= 0 {
		return Change{}, Refuse("change_malformed", "")
	}
	if _, ok := parseHex64Lower(c.Sig); !ok {
		return Change{}, Refuse("change_malformed", "")
	}
	if _, err := SigMsg(&c); err != nil {
		return Change{}, Refuse("change_malformed", "")
	}
	return c, nil
}

// VerifyChange recomputes sig_msg from c's parsed fields and verifies c.Sig
// against c.Author.
func VerifyChange(c *Change) (bool, error) {
	msg, err := SigMsg(c)
	if err != nil {
		return false, Refuse("change_malformed", "")
	}
	author, ok := parseHex32Lower(c.Author)
	if !ok {
		return false, Refuse("change_malformed", "")
	}
	sig, ok := parseHex64Lower(c.Sig)
	if !ok {
		return false, Refuse("change_malformed", "")
	}
	return ed25519.EdVerify(sig, msg, author), nil
}
