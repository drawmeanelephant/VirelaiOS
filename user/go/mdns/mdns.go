// Package mdns is the M198a (#3349, index #3313) bounded multicast-DNS
// codec: RFC 6762 messages on the RFC 1035 wire format, query and
// response directions, the five record types the LAN arc speaks
// (PTR/SRV/TXT/A/AAAA) plus raw pass-through for everything else a real
// Bonjour responder sends (NSEC/OPT live in Raw, never refused — a
// browser that chokes on them could not browse a real LAN).
//
// Bounds, all named and all enforced before any wire I/O:
//
//   - a message never exceeds MaxPacket — it is one UDP datagram and
//     this link's MTU makes anything beyond ~1.5 KiB theoretical;
//   - a wire name never exceeds 255 bytes (RFC 1035), a label 63;
//   - questions and each record section are count-bounded
//     (MaxQuestions/MaxSectionRecords);
//   - compression pointers are decode-only here (encoding is always
//     uncompressed, which is legal) and must point strictly BACKWARD —
//     a forward, self, or looping pointer refuses by name
//     (ErrPointerForward), never a runaway walk.
//
// Malformed input never panics and never returns partial truth: Decode
// fails with a named error, Encode refuses a shape it cannot speak.
package mdns

import "errors"

const (
	// Port is the mDNS UDP port both directions use (RFC 6762 §3).
	Port = 5353
	// Group4/Group6 are the link-local multicast groups queries and
	// responses ride — 224.0.0.251 (Ethernet 01:00:5e:00:00:fb) and
	// ff02::fb (33:33:00:00:00:fb), the pair the M198a MAC filter admits.
	Group4 = uint32(224)<<24 | 0<<16 | 0<<8 | 251
)

// Group6Bytes is ff02::fb — the v6 mDNS multicast address.
var Group6Bytes = [16]byte{0xff, 0x02, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0xfb}

// Group4Bytes is 224.0.0.251 — the v4 mDNS multicast address.
var Group4Bytes = [4]byte{224, 0, 0, 251}

// Record types this codec speaks natively (RFC 1035 / RFC 2782 / RFC 6763).
const (
	TypeA    uint16 = 1
	TypePTR  uint16 = 12
	TypeTXT  uint16 = 16
	TypeAAAA uint16 = 28
	TypeSRV  uint16 = 33
	// TypeANY is query-side only (a browser asks ANY on a PTR target);
	// the codec encodes it in a Question but never in a Record.
	TypeANY uint16 = 255
)

// ClassIN is the only class this link speaks; the top bit carries the
// QU question flag (queries) and the cache-flush bit (responses).
const (
	ClassIN  uint16 = 0x0001
	bitQU    uint16 = 0x8000 // question: unicast-response requested
	bitFlush uint16 = 0x8000 // record: cache-flush (unique record set)
)

// The packet/section/name bounds. Every refusal names itself.
const (
	// MaxPacket bounds one message — a single UDP datagram. Real mDNS
	// messages live well under one Ethernet MTU; the bound is generous
	// (jumbo-safe) while staying finite.
	MaxPacket = 9000
	// MaxNameWire is RFC 1035's 255-octet name bound.
	MaxNameWire = 255
	maxLabel    = 63
	// MaxPointerHops bounds one name's compression chase; every hop must
	// also target an offset strictly before its pointer, so a loop is
	// impossible and the budget is almost never spent.
	MaxPointerHops = 32
	// MaxQuestions / MaxSectionRecords bound the header's counts: a
	// message claiming more refuses before walking anything.
	MaxQuestions      = 16
	MaxSectionRecords = 64
	headerLen         = 12
)

// The named refusals. Decode rejects malformed wire input, Encode
// rejects shapes the codec does not speak — each by name.
var (
	ErrPacketShort      = errors.New("mdns: packet shorter than its own header")
	ErrPacketTooLong    = errors.New("mdns: packet over the bound")
	ErrNameTooLong      = errors.New("mdns: name over the wire bound")
	ErrLabelTooLong     = errors.New("mdns: label over 63 octets")
	ErrLabelReserved    = errors.New("mdns: reserved label kind (0x40/0x80)")
	ErrPointerShort     = errors.New("mdns: compression pointer missing its low byte")
	ErrPointerForward   = errors.New("mdns: compression pointer not strictly backward")
	ErrPointerHops      = errors.New("mdns: compression chase over the hop bound")
	ErrTooManyQuestions = errors.New("mdns: question count over the bound")
	ErrTooManyRecords   = errors.New("mdns: record count over the bound")
	ErrTruncated        = errors.New("mdns: message runs past its datagram")
	ErrBadRData         = errors.New("mdns: rdata malformed for its type")
	ErrUnsupportedType  = errors.New("mdns: record type outside PTR/SRV/TXT/A/AAAA")
	ErrBadName          = errors.New("mdns: name is not encodable")
)

// Question is one decoded or to-be-encoded question. QU is the
// unicast-response bit a question may set on the wire's class field.
type Question struct {
	Name string
	Type uint16
	QU   bool
}

// SRV is a decoded service record body (RFC 2782): priority, weight,
// port, target host name.
type SRV struct {
	Priority uint16
	Weight   uint16
	Port     uint16
	Target   string
}

// Record is one decoded or to-be-encoded resource record. The typed
// fields carry the five supported types; Raw carries the rdata of any
// OTHER type the wire delivered (decode keeps it, encode refuses it).
type Record struct {
	Name  string
	Type  uint16
	TTL   uint32
	Flush bool
	PTR   string   // TypePTR: the name pointed at
	SRV   SRV      // TypeSRV
	TXT   []string // TypeTXT: the character-strings
	A     [4]byte  // TypeA
	AAAA  [16]byte // TypeAAAA
	Raw   []byte   // any other type: opaque rdata
}

// Message is one decoded or to-be-encoded mDNS message.
type Message struct {
	ID            uint16 // conventionally 0 on the mDNS wire
	Response      bool   // QR
	Authoritative bool   // AA
	Truncated     bool   // TC
	RCode         int    // low nibble of the flags word
	Questions     []Question
	Answers       []Record
	Authorities   []Record
	Additionals   []Record
}

// Query builds the conventional one-question browse/lookup message:
// ID 0, flags 0, the asked name and type. QU asks responders for a
// unicast reply (RFC 6762 §5.4 — the bounded-latency path a browser
// prefers).
func Query(name string, qtype uint16, qu bool) *Message {
	return &Message{
		Questions: []Question{{Name: name, Type: qtype, QU: qu}},
	}
}

// ---------------------------------------------------------------------------
// Encode
// ---------------------------------------------------------------------------

// Encode serializes the message uncompressed (legal per RFC 1035 —
// compression is a MAY, never a MUST) and bounded: the refusal names
// what does not fit or what the codec does not speak.
func (m *Message) Encode() ([]byte, error) {
	if len(m.Questions) > MaxQuestions {
		return nil, ErrTooManyQuestions
	}
	if len(m.Answers) > MaxSectionRecords ||
		len(m.Authorities) > MaxSectionRecords ||
		len(m.Additionals) > MaxSectionRecords {
		return nil, ErrTooManyRecords
	}
	out := make([]byte, 0, 256)
	out = append(out, byte(m.ID>>8), byte(m.ID))
	var flags uint16
	if m.Response {
		flags |= 0x8000
	}
	if m.Authoritative {
		flags |= 0x0400
	}
	if m.Truncated {
		flags |= 0x0200
	}
	flags |= uint16(m.RCode) & 0x000f
	out = append(out, byte(flags>>8), byte(flags))
	out = appendCount(out, len(m.Questions))
	out = appendCount(out, len(m.Answers))
	out = appendCount(out, len(m.Authorities))
	out = appendCount(out, len(m.Additionals))
	var err error
	for _, q := range m.Questions {
		if out, err = appendName(out, q.Name); err != nil {
			return nil, err
		}
		out = append(out, byte(q.Type>>8), byte(q.Type))
		class := ClassIN
		if q.QU {
			class |= bitQU
		}
		out = append(out, byte(class>>8), byte(class))
	}
	for _, sec := range [][]Record{m.Answers, m.Authorities, m.Additionals} {
		for _, r := range sec {
			if out, err = appendRecord(out, r); err != nil {
				return nil, err
			}
		}
	}
	if len(out) > MaxPacket {
		return nil, ErrPacketTooLong
	}
	return out, nil
}

func appendCount(b []byte, n int) []byte {
	return append(b, byte(n>>8), byte(n))
}

// appendName encodes a dotted name as RFC 1035 labels (no compression —
// the decoder reads pointers, the encoder never writes them). The empty
// name is the DNS root (a single zero byte) — legal on the wire, though
// mDNS never emits it.
func appendName(b []byte, name string) ([]byte, error) {
	if len(name) > MaxNameWire {
		return nil, ErrNameTooLong
	}
	if name == "" {
		return append(b, 0), nil
	}
	start := 0
	for i := 0; i <= len(name); i++ {
		if i == len(name) || name[i] == '.' {
			label := name[start:i]
			if len(label) > maxLabel {
				return nil, ErrLabelTooLong
			}
			if len(label) == 0 {
				return nil, ErrBadName // empty interior label ("a..b")
			}
			b = append(b, byte(len(label)))
			b = append(b, label...)
			start = i + 1
		}
	}
	return append(b, 0), nil
}

// appendRecord writes one RR: name, TYPE, CLASS (cache-flush on the top
// bit), TTL, RDLENGTH, then the typed rdata. Raw records refuse — the
// codec encodes the five named types only.
func appendRecord(b []byte, r Record) ([]byte, error) {
	var err error
	if b, err = appendName(b, r.Name); err != nil {
		return nil, err
	}
	b = append(b, byte(r.Type>>8), byte(r.Type))
	class := ClassIN
	if r.Flush {
		class |= bitFlush
	}
	b = append(b, byte(class>>8), byte(class))
	b = append(b, byte(r.TTL>>24), byte(r.TTL>>16), byte(r.TTL>>8), byte(r.TTL))
	rdAt := len(b)
	b = append(b, 0, 0) // RDLENGTH patched after the body lands
	rdStart := len(b)
	switch r.Type {
	case TypeA:
		b = append(b, r.A[:]...)
	case TypeAAAA:
		b = append(b, r.AAAA[:]...)
	case TypePTR:
		if b, err = appendName(b, r.PTR); err != nil {
			return nil, err
		}
	case TypeSRV:
		b = append(b, byte(r.SRV.Priority>>8), byte(r.SRV.Priority),
			byte(r.SRV.Weight>>8), byte(r.SRV.Weight),
			byte(r.SRV.Port>>8), byte(r.SRV.Port))
		if b, err = appendName(b, r.SRV.Target); err != nil {
			return nil, err
		}
	case TypeTXT:
		for _, s := range r.TXT {
			if len(s) > 255 {
				return nil, ErrBadRData // a character-string is one length byte
			}
			b = append(b, byte(len(s)))
			b = append(b, s...)
		}
	default:
		return nil, ErrUnsupportedType
	}
	rd := len(b) - rdStart
	b[rdAt] = byte(rd >> 8)
	b[rdAt+1] = byte(rd)
	return b, nil
}

// ---------------------------------------------------------------------------
// Decode
// ---------------------------------------------------------------------------

// Decode parses one datagram into a Message. The header's counts are
// bound-checked before anything is walked; every structure that overruns
// the datagram refuses whole — no partial-truth returns.
func Decode(pkt []byte) (*Message, error) {
	if len(pkt) > MaxPacket {
		return nil, ErrPacketTooLong
	}
	if len(pkt) < headerLen {
		return nil, ErrPacketShort
	}
	m := &Message{
		ID: uint16(pkt[0])<<8 | uint16(pkt[1]),
	}
	flags := uint16(pkt[2])<<8 | uint16(pkt[3])
	m.Response = flags&0x8000 != 0
	m.Authoritative = flags&0x0400 != 0
	m.Truncated = flags&0x0200 != 0
	m.RCode = int(flags & 0x000f)
	qd := int(uint16(pkt[4])<<8 | uint16(pkt[5]))
	an := int(uint16(pkt[6])<<8 | uint16(pkt[7]))
	ns := int(uint16(pkt[8])<<8 | uint16(pkt[9]))
	ar := int(uint16(pkt[10])<<8 | uint16(pkt[11]))
	if qd > MaxQuestions {
		return nil, ErrTooManyQuestions
	}
	if an > MaxSectionRecords || ns > MaxSectionRecords || ar > MaxSectionRecords {
		return nil, ErrTooManyRecords
	}
	off := headerLen
	var err error
	for i := 0; i < qd; i++ {
		var q Question
		if q.Name, off, err = decodeName(pkt, off); err != nil {
			return nil, err
		}
		if off+4 > len(pkt) {
			return nil, ErrTruncated
		}
		q.Type = uint16(pkt[off])<<8 | uint16(pkt[off+1])
		class := uint16(pkt[off+2])<<8 | uint16(pkt[off+3])
		q.QU = class&bitQU != 0
		off += 4
		m.Questions = append(m.Questions, q)
	}
	if m.Answers, off, err = decodeSection(pkt, off, an); err != nil {
		return nil, err
	}
	if m.Authorities, off, err = decodeSection(pkt, off, ns); err != nil {
		return nil, err
	}
	if m.Additionals, off, err = decodeSection(pkt, off, ar); err != nil {
		return nil, err
	}
	return m, nil
}

// decodeName reads the name at off, following compression pointers that
// point strictly backward (the only direction a real encoder emits; it
// also makes loops structurally impossible). Returns the dotted name
// and the offset AFTER the name — at the first pointer, not after the
// chase. Pointer-hopping keeps its own hop budget.
func decodeName(pkt []byte, off int) (string, int, error) {
	var name []byte
	next := -1
	hops := 0
	for {
		if off >= len(pkt) {
			return "", 0, ErrTruncated
		}
		b := int(pkt[off])
		if b == 0 {
			if next < 0 {
				next = off + 1
			}
			break
		}
		if b&0xC0 == 0xC0 {
			if off+2 > len(pkt) {
				return "", 0, ErrPointerShort
			}
			target := (b&0x3f)<<8 | int(pkt[off+1])
			if target >= off {
				return "", 0, ErrPointerForward // a loop can only live forward/sideways
			}
			if next < 0 {
				next = off + 2
			}
			hops++
			if hops > MaxPointerHops {
				return "", 0, ErrPointerHops
			}
			off = target
			continue
		}
		if b&0xC0 != 0 {
			return "", 0, ErrLabelReserved
		}
		if off+1+b > len(pkt) {
			return "", 0, ErrTruncated
		}
		if len(name)+b+1 > MaxNameWire {
			return "", 0, ErrNameTooLong
		}
		if len(name) > 0 {
			name = append(name, '.')
		}
		name = append(name, pkt[off+1:off+1+b]...)
		off += 1 + b
	}
	return string(name), next, nil
}

// decodeSection walks n records starting at off. A section that runs
// past the datagram refuses whole.
func decodeSection(pkt []byte, off, n int) ([]Record, int, error) {
	recs := make([]Record, 0, n)
	for i := 0; i < n; i++ {
		var r Record
		var err error
		if r.Name, off, err = decodeName(pkt, off); err != nil {
			return nil, 0, err
		}
		if off+10 > len(pkt) {
			return nil, 0, ErrTruncated // TYPE(2) CLASS(2) TTL(4) RDLENGTH(2)
		}
		r.Type = uint16(pkt[off])<<8 | uint16(pkt[off+1])
		class := uint16(pkt[off+2])<<8 | uint16(pkt[off+3])
		r.Flush = class&bitFlush != 0
		r.TTL = uint32(pkt[off+4])<<24 | uint32(pkt[off+5])<<16 |
			uint32(pkt[off+6])<<8 | uint32(pkt[off+7])
		rdlen := int(uint16(pkt[off+8])<<8 | uint16(pkt[off+9]))
		off += 10
		if off+rdlen > len(pkt) {
			return nil, 0, ErrTruncated
		}
		rd := pkt[off : off+rdlen]
		switch r.Type {
		case TypeA:
			if rdlen != 4 {
				return nil, 0, ErrBadRData
			}
			copy(r.A[:], rd)
		case TypeAAAA:
			if rdlen != 16 {
				return nil, 0, ErrBadRData
			}
			copy(r.AAAA[:], rd)
		case TypePTR:
			var ptr string
			if ptr, _, err = decodeName(pkt, off); err != nil {
				return nil, 0, err
			}
			r.PTR = ptr
		case TypeSRV:
			if rdlen < 7 {
				return nil, 0, ErrBadRData // 6 fixed bytes + at least a root label
			}
			r.SRV.Priority = uint16(rd[0])<<8 | uint16(rd[1])
			r.SRV.Weight = uint16(rd[2])<<8 | uint16(rd[3])
			r.SRV.Port = uint16(rd[4])<<8 | uint16(rd[5])
			var target string
			if target, _, err = decodeName(pkt, off+6); err != nil {
				return nil, 0, err
			}
			r.SRV.Target = target
		case TypeTXT:
			for j := 0; j < rdlen; {
				l := int(rd[j])
				if j+1+l > rdlen {
					return nil, 0, ErrBadRData // a character-string overruns its rdata
				}
				r.TXT = append(r.TXT, string(rd[j+1:j+1+l]))
				j += 1 + l
			}
		default:
			r.Raw = append([]byte(nil), rd...)
		}
		off += rdlen
		recs = append(recs, r)
	}
	return recs, off, nil
}
