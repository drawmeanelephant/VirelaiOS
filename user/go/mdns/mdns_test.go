package mdns

import (
	"errors"
	"testing"
)

// qname encodes a dotted name uncompressed — the test-side mirror of
// appendName used to hand-build wire packets.
func qname(name string) []byte {
	var out []byte
	start := 0
	for i := 0; i <= len(name); i++ {
		if i == len(name) || name[i] == '.' {
			out = append(out, byte(i-start))
			out = append(out, name[start:i]...)
			start = i + 1
		}
	}
	return append(out, 0)
}

func header(qd, an, ns, ar uint16) []byte {
	return []byte{
		0, 0, // ID
		0x84, 0x00, // QR + AA — a response
		byte(qd >> 8), byte(qd),
		byte(an >> 8), byte(an),
		byte(ns >> 8), byte(ns),
		byte(ar >> 8), byte(ar),
	}
}

func TestQueryRoundTrip(t *testing.T) {
	m := Query("_http._tcp.local", TypePTR, true)
	pkt, err := m.Encode()
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	// A query: ID 0, flags 0 (no QR — a question, not an answer), one
	// question, no records.
	if pkt[2] != 0 || pkt[3] != 0 {
		t.Fatalf("query flags = %#x want 0", uint16(pkt[2])<<8|uint16(pkt[3]))
	}
	got, err := Decode(pkt)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if got.Response || len(got.Questions) != 1 {
		t.Fatalf("decoded as response or wrong qd: %+v", got)
	}
	q := got.Questions[0]
	if q.Name != "_http._tcp.local" || q.Type != TypePTR || !q.QU {
		t.Fatalf("question = %+v", q)
	}
}

func TestResponseRoundTrip(t *testing.T) {
	m := &Message{
		Response:      true,
		Authoritative: true,
		Answers: []Record{
			{Name: "_services._dns-sd._udp.local", Type: TypePTR, TTL: 4500, PTR: "_http._tcp.local"},
			{Name: "Virelai Web._http._tcp.local", Type: TypePTR, TTL: 4500, Flush: true, PTR: "Virelai Web._http._tcp.local"},
		},
		Additionals: []Record{
			{Name: "Virelai Web._http._tcp.local", Type: TypeSRV, TTL: 120, Flush: true,
				SRV: SRV{Priority: 0, Weight: 0, Port: 8080, Target: "virelai.local"}},
			{Name: "Virelai Web._http._tcp.local", Type: TypeTXT, TTL: 4500, Flush: true,
				TXT: []string{"path=/", "app=gohttpd"}},
			{Name: "virelai.local", Type: TypeA, TTL: 120, Flush: true, A: [4]byte{192, 168, 1, 42}},
			{Name: "virelai.local", Type: TypeAAAA, TTL: 120, Flush: true,
				AAAA: [16]byte{0xfe, 0x80, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 1}},
		},
	}
	pkt, err := m.Encode()
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	got, err := Decode(pkt)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if !got.Response || !got.Authoritative {
		t.Fatal("flags lost")
	}
	if len(got.Answers) != 2 || got.Answers[0].PTR != "_http._tcp.local" {
		t.Fatalf("answers: %+v", got.Answers)
	}
	if len(got.Additionals) != 4 {
		t.Fatalf("additionals: %d", len(got.Additionals))
	}
	srv := got.Additionals[0]
	if srv.Type != TypeSRV || srv.SRV.Port != 8080 || srv.SRV.Target != "virelai.local" || !srv.Flush {
		t.Fatalf("srv: %+v", srv)
	}
	txt := got.Additionals[1]
	if len(txt.TXT) != 2 || txt.TXT[0] != "path=/" || txt.TXT[1] != "app=gohttpd" {
		t.Fatalf("txt: %+v", txt.TXT)
	}
	if got.Additionals[2].A != [4]byte{192, 168, 1, 42} {
		t.Fatalf("a: %+v", got.Additionals[2].A)
	}
	if got.Additionals[3].AAAA[15] != 1 || got.Additionals[3].AAAA[0] != 0xfe {
		t.Fatalf("aaaa: %+v", got.Additionals[3].AAAA)
	}
}

// A real Bonjour-shaped packet: the answer NAME is a compression
// pointer back into the question section (the shape dns-sd emits).
func TestDecodeCompression(t *testing.T) {
	pkt := header(1, 1, 0, 0)
	pkt = append(pkt, qname("_services._dns-sd._udp.local")...)
	pkt = append(pkt, 0, byte(TypePTR), 0, 1) // QTYPE PTR, QCLASS IN
	// Answer: NAME = pointer to offset 12 (the question's name), PTR
	// rdata = an uncompressed target name.
	pkt = append(pkt, 0xc0, 0x0c)
	pkt = append(pkt, 0, byte(TypePTR), 0x80, 1) // class IN + cache-flush
	pkt = append(pkt, 0, 0, 0, 120)              // TTL
	target := qname("_http._tcp.local")
	pkt = append(pkt, byte(len(target)>>8), byte(len(target)))
	pkt = append(pkt, target...)

	m, err := Decode(pkt)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(m.Answers) != 1 {
		t.Fatalf("answers: %d", len(m.Answers))
	}
	r := m.Answers[0]
	if r.Name != "_services._dns-sd._udp.local" || !r.Flush || r.PTR != "_http._tcp.local" {
		t.Fatalf("record: %+v", r)
	}
}

// Nested backward pointers: a name ending in a pointer whose target is
// itself a label sequence ending in a pointer — the legal chained shape
// real encoders emit. Laid out inside the question section (names there
// may compress too): Q1 carries "_tcp.local" uncompressed, Q2 carries
// "_http" + ptr→Q1's name, and the answer's NAME is ptr→Q2's name.
func TestDecodeChainedPointers(t *testing.T) {
	pkt := header(2, 1, 0, 0)
	tcpOff := len(pkt) // Q1 name: "_tcp.local" uncompressed
	pkt = append(pkt, qname("_tcp.local")...)
	pkt = append(pkt, 0, byte(TypePTR), 0, 1) // QTYPE/QCLASS
	httpOff := len(pkt)                       // Q2 name: "_http" + ptr→"_tcp.local"
	pkt = append(pkt, 5, '_', 'h', 't', 't', 'p', 0xc0, byte(tcpOff))
	pkt = append(pkt, 0, byte(TypePTR), 0, 1)
	// Answer: NAME = ptr→httpOff (a two-pointer chase), A record.
	pkt = append(pkt, 0xc0, byte(httpOff))
	pkt = append(pkt, 0, byte(TypeA), 0x80, 1, 0, 0, 0, 60, 0, 4)
	pkt = append(pkt, 10, 0, 0, 7)

	m, err := Decode(pkt)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(m.Questions) != 2 || m.Questions[1].Name != "_http._tcp.local" {
		t.Fatalf("questions: %+v", m.Questions)
	}
	if len(m.Answers) != 1 || m.Answers[0].Name != "_http._tcp.local" {
		t.Fatalf("name: %+v", m.Answers)
	}
	if m.Answers[0].A != [4]byte{10, 0, 0, 7} {
		t.Fatalf("a: %+v", m.Answers[0].A)
	}
}

// Every malformed shape refuses by name — never a panic, never a
// partial message.
func TestMalformedRefusals(t *testing.T) {
	cases := []struct {
		name string
		pkt  []byte
		want error
	}{
		{"short-header", []byte{0, 0, 0}, ErrPacketShort},
		{"too-many-questions", header(MaxQuestions+1, 0, 0, 0), ErrTooManyQuestions},
		{"too-many-answers", header(0, MaxSectionRecords+1, 0, 0), ErrTooManyRecords},
		{"truncated-question", append(header(1, 0, 0, 0), 5, '_', 'h'), ErrTruncated},
		{"truncated-record", append(header(0, 1, 0, 0), qname("x.local")...), ErrTruncated},
	}
	for _, c := range cases {
		if _, err := Decode(c.pkt); !errors.Is(err, c.want) {
			t.Errorf("%s: got %v want %v", c.name, err, c.want)
		}
	}

	// A pointer that targets itself (offset 12 -> 12): forward/self
	// pointers refuse by name.
	self := append(header(1, 0, 0, 0), 0xc0, 0x0c, 0, 1, 0, 1)
	if _, err := Decode(self); !errors.Is(err, ErrPointerForward) {
		t.Errorf("self-pointer: got %v", err)
	}

	// A forward pointer (offset 12 -> 20).
	fwd := append(header(1, 0, 0, 0), 0xc0, 0x14, 0, 1, 0, 1)
	fwd = append(fwd, make([]byte, 8)...)
	if _, err := Decode(fwd); !errors.Is(err, ErrPointerForward) {
		t.Errorf("forward-pointer: got %v", err)
	}

	// A pointer missing its second byte.
	pshort := append(header(1, 0, 0, 0), 0xc0)
	if _, err := Decode(pshort); !errors.Is(err, ErrPointerShort) {
		t.Errorf("short-pointer: got %v", err)
	}

	// A reserved label kind (0x40 — the never-assigned half).
	res := append(header(1, 0, 0, 0), 0x40, 'x', 0, 0, 1, 0, 1)
	if _, err := Decode(res); !errors.Is(err, ErrLabelReserved) {
		t.Errorf("reserved-label: got %v", err)
	}

	// An A record with rdlen != 4.
	pkt := header(0, 1, 0, 0)
	pkt = append(pkt, qname("h.local")...)
	pkt = append(pkt, 0, byte(TypeA), 0x80, 1, 0, 0, 0, 60, 0, 3, 1, 2, 3)
	if _, err := Decode(pkt); !errors.Is(err, ErrBadRData) {
		t.Errorf("bad-A: got %v", err)
	}

	// A TXT record whose character-string overruns the rdata.
	pkt = header(0, 1, 0, 0)
	pkt = append(pkt, qname("s._http._tcp.local")...)
	pkt = append(pkt, 0, byte(TypeTXT), 0x80, 1, 0, 0, 0, 60, 0, 2, 9, 'x')
	if _, err := Decode(pkt); !errors.Is(err, ErrBadRData) {
		t.Errorf("bad-TXT: got %v", err)
	}

	// An SRV record too short for its fixed fields.
	pkt = header(0, 1, 0, 0)
	pkt = append(pkt, qname("s._http._tcp.local")...)
	pkt = append(pkt, 0, byte(TypeSRV), 0x80, 1, 0, 0, 0, 60, 0, 3, 0, 0, 0)
	if _, err := Decode(pkt); !errors.Is(err, ErrBadRData) {
		t.Errorf("bad-SRV: got %v", err)
	}

	// A backward-pointer chain deeper than the hop bound: MaxPointerHops+2
	// PTR records, each record's NAME = label 'x' + pointer to the
	// previous record's name — every hop strictly backward, so the walk
	// can only end at the bound, never a loop.
	nrec := MaxPointerHops + 2
	pkt = header(0, uint16(nrec), 0, 0)
	prevNameOff := len(pkt) // record 1's name, uncompressed
	pkt = append(pkt, qname("x.local")...)
	pkt = append(pkt, 0, byte(TypePTR), 0x80, 1, 0, 0, 0, 60, 0, 1, 0)
	for i := 1; i < nrec; i++ {
		prev := prevNameOff
		prevNameOff = len(pkt) // this record's name field offset
		pkt = append(pkt, 1, 'x', 0xc0|byte(prev>>8), byte(prev))
		pkt = append(pkt, 0, byte(TypePTR), 0x80, 1, 0, 0, 0, 60, 0, 1, 0)
	}
	if _, err := Decode(pkt); !errors.Is(err, ErrPointerHops) {
		t.Errorf("hop-chain: got %v", err)
	}
}

func TestEncodeRefusals(t *testing.T) {
	// A record type the codec does not speak refuses by name.
	m := &Message{Response: true, Answers: []Record{
		{Name: "h.local", Type: 5 /* CNAME */, Raw: []byte{1, 2, 3}},
	}}
	if _, err := m.Encode(); !errors.Is(err, ErrUnsupportedType) {
		t.Errorf("unsupported-type: got %v", err)
	}
	// Section counts over the bound refuse.
	over := &Message{Answers: make([]Record, MaxSectionRecords+1)}
	if _, err := over.Encode(); !errors.Is(err, ErrTooManyRecords) {
		t.Errorf("over-records: got %v", err)
	}
	// A name over the wire bound refuses.
	long := make([]byte, 256)
	for i := range long {
		long[i] = 'a'
	}
	bad := Query(string(long)+".local", TypePTR, false)
	if _, err := bad.Encode(); !errors.Is(err, ErrNameTooLong) {
		t.Errorf("long-name: got %v", err)
	}
	// An empty interior label refuses.
	bad = Query("a..local", TypePTR, false)
	if _, err := bad.Encode(); !errors.Is(err, ErrBadName) {
		t.Errorf("empty-label: got %v", err)
	}
	// A label over 63 octets refuses.
	seg := make([]byte, 64)
	for i := range seg {
		seg[i] = 'b'
	}
	bad = Query(string(seg)+".local", TypePTR, false)
	if _, err := bad.Encode(); !errors.Is(err, ErrLabelTooLong) {
		t.Errorf("long-label: got %v", err)
	}
	// A TXT character-string over 255 refuses.
	txt := make([]byte, 256)
	for i := range txt {
		txt[i] = 't'
	}
	m = &Message{Response: true, Answers: []Record{
		{Name: "s._http._tcp.local", Type: TypeTXT, TXT: []string{string(txt)}},
	}}
	if _, err := m.Encode(); !errors.Is(err, ErrBadRData) {
		t.Errorf("long-txt: got %v", err)
	}
}

// An unknown record type (NSEC here) survives decode as Raw — browsing a
// real LAN meets types outside the five, and a codec that refused them
// could not browse Bonjour at all.
func TestUnknownTypePassesThrough(t *testing.T) {
	pkt := header(0, 1, 0, 0)
	pkt = append(pkt, qname("h.local")...)
	pkt = append(pkt, 0, 47 /* NSEC */, 0x80, 1, 0, 0, 0, 60, 0, 5, 0xaa, 0xbb, 0xcc, 0xdd, 0xee)
	m, err := Decode(pkt)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	r := m.Answers[0]
	if r.Type != 47 || len(r.Raw) != 5 || r.Raw[0] != 0xaa {
		t.Fatalf("raw: %+v", r)
	}
}

// A goodbye packet (TTL 0 per RFC 6762 §10.1) decodes like any other —
// the TTL field is the only difference and card-b's advertiser sends it.
func TestGoodbyeTTL(t *testing.T) {
	m := &Message{Response: true, Answers: []Record{
		{Name: "_services._dns-sd._udp.local", Type: TypePTR, TTL: 0, PTR: "_http._tcp.local"},
	}}
	pkt, err := m.Encode()
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	got, err := Decode(pkt)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if got.Answers[0].TTL != 0 {
		t.Fatalf("ttl: %d", got.Answers[0].TTL)
	}
}
