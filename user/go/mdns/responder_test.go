package mdns

import (
	"errors"
	"sync"
	"testing"
	"time"
)

func testService() Service {
	return Service{
		Type:     "_http._tcp.local",
		Instance: "v206b",
		Host:     "v206b.local",
		Port:     8080,
		IPv4:     [4]byte{10, 0, 0, 16},
		IPv6:     [16]byte{0xfe, 0x80},
		TTL:      120,
		TXT:      []string{"txtvers=1", "path=/"},
	}
}

func TestNewResponderRefusals(t *testing.T) {
	for _, svc := range []Service{
		{Type: "", Instance: "v206b", Host: "v206b.local"},
		{Type: "_http._tcp.local", Instance: "", Host: "v206b.local"},
		{Type: "_http._tcp.local", Instance: "v206b", Host: ""},
		{Type: "_http._tcp.local", Instance: "_v206b", Host: "v206b.local"},
	} {
		if _, err := NewResponder(svc); !errors.Is(err, ErrBadService) {
			t.Fatalf("service %+v: err=%v, want ErrBadService", svc, err)
		}
	}
	if _, err := NewResponder(testService()); err != nil {
		t.Fatalf("valid service refused: %v", err)
	}
}

func TestAnswerMatchingPTR(t *testing.T) {
	r, err := NewResponder(testService())
	if err != nil {
		t.Fatal(err)
	}
	q := Query("_http._tcp.local", TypePTR, false)
	pkt, unicast, err := r.Answer(q)
	if err != nil {
		t.Fatalf("answer: %v", err)
	}
	if unicast {
		t.Fatal("a plain question must not ask for a unicast reply")
	}
	if len(pkt) > TransportMax {
		t.Fatalf("answer %d bytes exceeds the transport bound", len(pkt))
	}
	m, err := Decode(pkt)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if !m.Response || !m.Authoritative {
		t.Fatalf("answer flags: response=%v authoritative=%v", m.Response, m.Authoritative)
	}
	var ptr, srv, txt, a bool
	for _, rec := range append(append([]Record{}, m.Answers...), m.Additionals...) {
		switch rec.Type {
		case TypePTR:
			ptr = EqualName(rec.Name, "_http._tcp.local") && EqualName(rec.PTR, "v206b._http._tcp.local")
		case TypeSRV:
			srv = EqualName(rec.Name, "v206b._http._tcp.local") && rec.SRV.Port == 8080 &&
				EqualName(rec.SRV.Target, "v206b.local")
		case TypeTXT:
			txt = EqualName(rec.Name, "v206b._http._tcp.local") && len(rec.TXT) == 2
		case TypeA:
			a = EqualName(rec.Name, "v206b.local") && rec.A == [4]byte{10, 0, 0, 16}
		}
	}
	if !ptr || !srv || !txt || !a {
		t.Fatalf("answer chain incomplete: ptr=%v srv=%v txt=%v a=%v", ptr, srv, txt, a)
	}
}

// A QU question asks for the unicast shape; a question naming the
// instance or host resolves only what was asked (plus its address).
func TestAnswerQUAndInstanceHost(t *testing.T) {
	r, err := NewResponder(testService())
	if err != nil {
		t.Fatal(err)
	}
	_, unicast, err := r.Answer(Query("_http._tcp.local", TypePTR, true))
	if err != nil || !unicast {
		t.Fatalf("QU answer: unicast=%v err=%v", unicast, err)
	}
	mixed := Query("_http._tcp.local", TypePTR, true)
	mixed.Questions = append(mixed.Questions, Question{Name: "_http._tcp.local", Type: TypeANY, Class: ClassIN})
	pkt, unicast, err := r.Answer(mixed)
	if err != nil {
		t.Fatal(err)
	}
	if unicast {
		t.Fatal("a matching plain question must keep the reply multicast")
	}
	if len(pkt) == 0 {
		t.Fatal("no answer for the matching question")
	}
	for _, q := range []*Message{
		{Questions: []Question{{Name: "v206b._http._tcp.local", Type: TypeSRV, Class: ClassIN}}},
		{Questions: []Question{{Name: "v206b.local", Type: TypeA, Class: ClassIN}}},
		{Questions: []Question{{Name: "_services._dns-sd._udp.local", Type: TypePTR, Class: ClassIN}}},
	} {
		if _, _, err := r.Answer(q); err != nil {
			t.Fatalf("%s: %v", q.Questions[0].Name, err)
		}
	}
}

func TestAnswerRefusals(t *testing.T) {
	r, err := NewResponder(testService())
	if err != nil {
		t.Fatal(err)
	}
	badClass := Query("_http._tcp.local", TypePTR, false)
	badClass.Questions[0].Class = 3
	zeroClass := Query("_http._tcp.local", TypePTR, false)
	zeroClass.Questions[0].Class = 0
	badOp := Query("_http._tcp.local", TypePTR, false)
	badOp.Opcode = 5
	resp := Query("_http._tcp.local", TypePTR, false)
	resp.Response = true
	cases := []struct {
		name string
		msg  *Message
		want error
	}{
		{"not a query", resp, ErrNotAQuery},
		{"bad opcode", badOp, ErrBadOpcode},
		{"bad class", badClass, ErrBadClass},
		{"reserved class 0", zeroClass, ErrBadClass},
		{"no questions", &Message{}, ErrNoQuestions},
		{"unrelated", Query("_ssh._tcp.local", TypePTR, false), ErrNoMatch},
		{"wrong type", Query("_http._tcp.local", TypeSRV, false), ErrNoMatch},
	}
	for _, c := range cases {
		if pkt, _, err := r.Answer(c.msg); !errors.Is(err, c.want) || pkt != nil {
			t.Fatalf("%s: pkt=%v err=%v, want %v", c.name, pkt, err, c.want)
		}
	}
}

func TestAnswerTransportBound(t *testing.T) {
	svc := testService()
	for i := 0; i < 4; i++ {
		svc.TXT = append(svc.TXT, string(make([]byte, 200)))
	}
	r, err := NewResponder(svc)
	if err != nil {
		t.Fatal(err)
	}
	if pkt, _, err := r.Answer(Query("_http._tcp.local", TypePTR, false)); !errors.Is(err, ErrAnswerTooLong) {
		t.Fatalf("pkt=%d err=%v, want ErrAnswerTooLong", len(pkt), err)
	}
}

// pumpRecorder feeds scripted slot-85 records and captures sends.
type pumpRecorder struct {
	mu      sync.Mutex
	recv    [][]byte
	sends   []sendRecord
	logs    []string
	release chan struct{}
}

type sendRecord struct {
	dst  [4]byte
	port uint16
	pkt  []byte
}

func (p *pumpRecorder) seam() PumpSeam {
	return PumpSeam{
		Recv: func(_ uint16, buf []byte) int64 {
			p.mu.Lock()
			if len(p.recv) == 0 {
				p.mu.Unlock()
				return 0
			}
			d := p.recv[0]
			p.recv = p.recv[1:]
			p.mu.Unlock()
			return int64(copy(buf, d))
		},
		Send: func(dst [4]byte, _, _ uint16, b []byte) int64 {
			p.mu.Lock()
			p.sends = append(p.sends, sendRecord{dst: dst, pkt: append([]byte(nil), b...)})
			p.mu.Unlock()
			return int64(len(b))
		},
		Sleep: func(uint64) { <-p.release },
		Log: func(l string) {
			p.mu.Lock()
			p.logs = append(p.logs, l)
			p.mu.Unlock()
		},
	}
}

func dgram(src [4]byte, srcPort uint16, payload []byte) []byte {
	d := make([]byte, 12+len(payload))
	copy(d[0:4], src[:])
	d[4] = byte(srcPort >> 8)
	d[5] = byte(srcPort)
	d[6], d[7] = 0, 0
	d[8] = byte((8 + len(payload)) >> 8)
	d[9] = byte(8 + len(payload))
	copy(d[12:], payload)
	return d
}

func TestPumpAnswersAndFallsBackToGroup(t *testing.T) {
	r, err := NewResponder(testService())
	if err != nil {
		t.Fatal(err)
	}
	q, err := Query("_http._tcp.local", TypePTR, true).Encode()
	if err != nil {
		t.Fatal(err)
	}
	p := &pumpRecorder{release: make(chan struct{})}
	own := [4]byte{10, 0, 0, 16}
	peer := [4]byte{10, 0, 0, 17}
	// The first query is answered unicast; the second's unicast send is
	// refused (no ARP) and must fall back to the group. A wrong-source
	// record and a response are consumed.
	refuseUnicast := true
	seam := p.seam()
	inner := seam.Send
	seam.Send = func(dst [4]byte, dstPort, srcPort uint16, b []byte) int64 {
		if dst == peer && refuseUnicast {
			return -22 // -EINVAL: the peer's MAC is not resolved
		}
		return inner(dst, dstPort, srcPort, b)
	}
	p.recv = [][]byte{
		dgram(peer, Port, q),
		dgram(own, Port, q),  // self-source: ignored
		dgram(peer, 9999, q), // not an mDNS source port: ignored
		dgram(peer, Port, q), // answered after unicast refuses
	}
	pump := r.StartPump(seam)
	if pump == nil {
		t.Fatal("StartPump refused a complete seam")
	}
	// Let the loop consume the scripted ring, then join.
	deadline := 0
	for {
		p.mu.Lock()
		n := len(p.sends)
		p.mu.Unlock()
		if n >= 2 || deadline > 2000 {
			break
		}
		deadline++
		time.Sleep(time.Millisecond)
	}
	close(p.release)
	pump.Stop()
	pump.Stop() // idempotent
	p.mu.Lock()
	defer p.mu.Unlock()
	if len(p.sends) != 2 {
		t.Fatalf("sends=%d, want 2 (one unicast-shaped refusal fallback and one answered query)", len(p.sends))
	}
	if p.sends[0].dst != Group4Bytes {
		t.Fatalf("first send dst=%v, want the group fallback", p.sends[0].dst)
	}
	if p.sends[1].dst != Group4Bytes {
		t.Fatalf("second send dst=%v, want the group fallback", p.sends[1].dst)
	}
	if len(p.logs) == 0 || p.logs[len(p.logs)-1] != "mdns answer v206b._http._tcp.local addr=10.0.0.16 port=8080 unicast=0" {
		t.Fatalf("logs=%v", p.logs)
	}
}

func TestPumpUnicastWhenPeerResolved(t *testing.T) {
	r, err := NewResponder(testService())
	if err != nil {
		t.Fatal(err)
	}
	q, err := Query("_http._tcp.local", TypePTR, true).Encode()
	if err != nil {
		t.Fatal(err)
	}
	p := &pumpRecorder{release: make(chan struct{})}
	peer := [4]byte{10, 0, 0, 17}
	p.recv = [][]byte{dgram(peer, Port, q)}
	pump := r.StartPump(p.seam())
	for i := 0; i < 2000; i++ {
		p.mu.Lock()
		n := len(p.sends)
		p.mu.Unlock()
		if n > 0 {
			break
		}
		time.Sleep(time.Millisecond)
	}
	close(p.release)
	pump.Stop()
	p.mu.Lock()
	defer p.mu.Unlock()
	if len(p.sends) != 1 || p.sends[0].dst != peer {
		t.Fatalf("sends=%v, want one unicast send to %v", p.sends, peer)
	}
	if p.logs[len(p.logs)-1] != "mdns answer v206b._http._tcp.local addr=10.0.0.16 port=8080 unicast=1" {
		t.Fatalf("logs=%v", p.logs)
	}
}
