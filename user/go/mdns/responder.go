// Bounded mDNS query responder (M206b residual, #3814).
//
// The M198b advertiser only announced: a peer browsing after the startup
// burst had nothing to receive, because the stack had no responder and
// the IPv4 gate admitted no multicast. This file is the SHARED half the
// architecture asks for — HTTPD supplies a service description and the
// existing codec plus this responder answer matching DNS-SD queries;
// FLINTD reuses it later in its own staged lifecycle. No per-app protocol
// copy, and no generic discovery daemon.
//
// Bounds, all named:
//
//   - one query datagram decodes through the existing codec (MaxPacket,
//     MaxQuestions, MaxSectionRecords, backward-only pointers);
//   - only opcode 0, RCode 0, class IN queries with at least one question
//     are considered — everything else refuses by name and is ignored;
//   - the answer is one datagram bounded at TransportMax (512 B, the
//     owned UDP seam's payload bound), never the codec's 9000 B
//     MaxPacket — a larger answer refuses rather than truncating;
//   - a QU question gets a unicast reply; the pump falls back to the
//     group when the unicast send has no resolvable peer (a real mDNS
//     responder's permitted fallback), still one answer per query;
//   - the pump drains at most DrainMax datagrams per poll tick and parks
//     one scheduler tick between polls, so a burst cannot monopolize the
//     guest; Stop closes the pump and joins before the port is released.
package mdns

import (
	"errors"
)

const (
	// TransportMax is the owned UDP seam's payload bound (udp.payload_max
	// in the kernel / vi's udpFromPayloadMax): an answer larger than this
	// cannot be sent whole and is refused, never handed to a truncated
	// transport.
	TransportMax = 512
	// DrainMax bounds the datagrams one pump tick consumes; the rest wait
	// in the kernel ring for the next tick.
	DrainMax = 8
)

// The responder's named refusals. None of them is a live-service answer:
// the pump logs nothing and consumes the datagram.
var (
	ErrNotAQuery     = errors.New("mdns: not a query (response flag set)")
	ErrBadOpcode     = errors.New("mdns: query opcode outside 0")
	ErrBadClass      = errors.New("mdns: question class outside IN")
	ErrNoQuestions   = errors.New("mdns: query carries no questions")
	ErrNoMatch       = errors.New("mdns: no question names this service")
	ErrAnswerTooLong = errors.New("mdns: answer exceeds the 512-byte transport bound")
	ErrBadService    = errors.New("mdns: service description is not encodable")
)

// Service is one DNS-SD instance a responder answers for. Type is the
// service type ("_http._tcp.local"), Name the instance label
// ("v206b"), Host the SRV target/host owner ("v206b.local"). IPv6 is
// omitted when zero.
type Service struct {
	Type     string
	Instance string
	Host     string
	Port     uint16
	IPv4     [4]byte
	IPv6     [16]byte
	TTL      uint32
	TXT      []string
}

// Responder is the pure matcher/encoder: Answer takes a decoded query
// and returns the one bounded response datagram (or a named refusal).
// It holds no I/O state, so host tests exercise every policy directly.
type Responder struct {
	svc Service
}

// NewResponder validates the service description. A description the
// wire cannot carry refuses by name before anything binds.
func NewResponder(svc Service) (*Responder, error) {
	if !ValidName(svc.Type) || !ValidName(svc.Host) {
		return nil, ErrBadService
	}
	if svc.Instance == "" || !ValidName(svc.Instance) {
		return nil, ErrBadService
	}
	if svc.Instance[0] == '_' {
		return nil, ErrBadService
	}
	r := &Responder{svc: svc}
	if _, err := r.records(); err != nil {
		return nil, err
	}
	return r, nil
}

// serviceName is the fully qualified instance name the SRV/TXT records
// are owned by: "v206b._http._tcp.local".
func (r *Responder) serviceName() string { return r.svc.Instance + "." + r.svc.Type }

// records builds the record set once per call (cheap, bounded).
func (r *Responder) records() ([]Record, error) {
	instance := r.serviceName()
	recs := []Record{
		{Name: r.svc.Type, Type: TypePTR, TTL: r.svc.TTL, PTR: instance},
		{Name: instance, Type: TypeSRV, TTL: r.svc.TTL, Flush: true,
			SRV: SRV{Port: r.svc.Port, Target: r.svc.Host}},
		{Name: instance, Type: TypeTXT, TTL: r.svc.TTL, Flush: true, TXT: r.svc.TXT},
		{Name: r.svc.Host, Type: TypeA, TTL: r.svc.TTL, Flush: true, A: r.svc.IPv4},
	}
	if r.svc.IPv6 != [16]byte{} {
		recs = append(recs, Record{Name: r.svc.Host, Type: TypeAAAA, TTL: r.svc.TTL, Flush: true, AAAA: r.svc.IPv6})
	}
	for _, rec := range recs {
		if _, err := (&Message{Answers: []Record{rec}}).Encode(); err != nil {
			return nil, ErrBadService
		}
	}
	return recs, nil
}

// Answer matches one decoded query against the service and returns the
// bounded response. unicast reports the QU decision: true when every
// matching question asked for a unicast reply (RFC 6762 §5.4).
func (r *Responder) Answer(m *Message) (pkt []byte, unicast bool, err error) {
	if m == nil {
		return nil, false, ErrNotAQuery
	}
	if m.Response {
		return nil, false, ErrNotAQuery
	}
	if m.Opcode != 0 || m.RCode != 0 {
		return nil, false, ErrBadOpcode
	}
	if len(m.Questions) == 0 {
		return nil, false, ErrNoQuestions
	}
	recs, err := r.records()
	if err != nil {
		return nil, false, err
	}
	var (
		answers   []Record
		extras    []Record
		matched   bool
		quCount   int
		plainQ    int
		seen      = map[string]bool{}
		metaName  = "_services._dns-sd._udp.local"
		instance  = r.serviceName()
		metaRec   = Record{Name: metaName, Type: TypePTR, TTL: r.svc.TTL, PTR: r.svc.Type}
		haveBadCl = false
	)
	add := func(rec Record, extra bool) {
		key := rec.Name + "\x00" + string([]byte{byte(rec.Type >> 8), byte(rec.Type)})
		if seen[key] {
			return
		}
		seen[key] = true
		matched = true
		if extra {
			extras = append(extras, rec)
		} else {
			answers = append(answers, rec)
		}
	}
	for _, q := range m.Questions {
		// Only the Internet class is a service query. A zero class is
		// what the ENCODER writes for an unset struct field; on the wire
		// it is the reserved class 0 and must not become an answer.
		cls := q.Class
		if cls != ClassIN {
			haveBadCl = true
			continue
		}
		hit := false
		switch {
		case EqualName(q.Name, r.svc.Type):
			if q.Type == TypePTR || q.Type == TypeANY {
				add(recs[0], false) // PTR
				add(recs[1], true)  // SRV
				add(recs[2], true)  // TXT
				for _, rec := range recs[3:] {
					add(rec, true) // A/AAAA
				}
				hit = true
			}
		case EqualName(q.Name, instance):
			if q.Type == TypeSRV || q.Type == TypeANY {
				add(recs[1], false)
				hit = true
			}
			if q.Type == TypeTXT || q.Type == TypeANY {
				add(recs[2], false)
				hit = true
			}
			if hit {
				for _, rec := range recs[3:] {
					add(rec, true)
				}
			}
		case EqualName(q.Name, r.svc.Host):
			for _, rec := range recs[3:] {
				if q.Type == TypeANY || q.Type == rec.Type {
					add(rec, false)
					hit = true
				}
			}
		case EqualName(q.Name, metaName):
			if q.Type == TypePTR || q.Type == TypeANY {
				add(metaRec, false)
				hit = true
			}
		}
		if hit {
			if q.QU {
				quCount++
			} else {
				plainQ++
			}
		}
	}
	if !matched {
		if haveBadCl && len(m.Questions) == 1 {
			return nil, false, ErrBadClass
		}
		return nil, false, ErrNoMatch
	}
	resp := &Message{Response: true, Authoritative: true, Answers: answers, Additionals: extras}
	pkt, err = resp.Encode()
	if err != nil {
		return nil, false, err
	}
	if len(pkt) > TransportMax {
		return nil, false, ErrAnswerTooLong
	}
	return pkt, quCount > 0 && plainQ == 0, nil
}

// PumpSeam is the syscall surface the pump drives — injected so host
// tests run the whole receive/answer/fallback loop without the guest.
// Recv is the slot-85 shape (4-byte source IPv4 + 8-byte UDP header +
// payload); Send is the explicit-source-port slot-84 shape.
type PumpSeam struct {
	Recv  func(port uint16, buf []byte) int64
	Send  func(dst [4]byte, dstPort, srcPort uint16, b []byte) int64
	Sleep func(ticks uint64)
	Log   func(line string)
}

// Pump is one responder loop over an ALREADY-BOUND :5353 port. The
// advertiser owns the bind; the pump never binds or closes it.
type Pump struct {
	resp *Responder
	seam PumpSeam
	stop chan struct{}
	done chan struct{}
}

// StartPump launches the bounded receive/answer loop. The caller must
// have bound Port already; StartPump refuses a seam that cannot recv.
func (r *Responder) StartPump(seam PumpSeam) *Pump {
	if seam.Recv == nil || seam.Send == nil || seam.Sleep == nil {
		return nil
	}
	p := &Pump{resp: r, seam: seam, stop: make(chan struct{}), done: make(chan struct{})}
	go p.run()
	return p
}

// Stop signals the pump and JOINS it before returning, so a caller may
// release the port and stage the next owner immediately.
func (p *Pump) Stop() {
	if p == nil {
		return
	}
	select {
	case <-p.stop:
	default:
		close(p.stop)
	}
	<-p.done
}

func (p *Pump) run() {
	defer close(p.done)
	buf := make([]byte, 4+8+TransportMax)
	for {
		select {
		case <-p.stop:
			return
		default:
		}
		for i := 0; i < DrainMax; i++ {
			n := p.seam.Recv(Port, buf)
			if n <= 0 {
				break
			}
			p.handle(buf[:n])
		}
		p.seam.Sleep(1)
	}
}

// handle validates one slot-85 record and answers it. A datagram that
// is not an mDNS query from a unicast :5353 source is consumed silently
// (or with one refusal line when the seam names it) — never answered.
func (p *Pump) handle(d []byte) {
	if len(d) < 4+8 {
		return
	}
	src := [4]byte{d[0], d[1], d[2], d[3]}
	srcPort := uint16(d[4])<<8 | uint16(d[5])
	if srcPort != Port {
		return
	}
	if src[0] == 0 || src[0] == 255 || src[0] >= 224 || src == p.resp.svc.IPv4 {
		return // not a unicast peer source (group/broadcast/self/zero)
	}
	m, err := Decode(d[4+8:])
	if err != nil {
		return
	}
	pkt, unicast, err := p.resp.Answer(m)
	if err != nil {
		return // unrelated/malformed/refused — never a service answer
	}
	if p.seam.Log != nil {
		p.seam.Log("mdns query " + p.resp.svc.Type)
	}
	if unicast {
		if rc := p.seam.Send(src, Port, Port, pkt); rc < 0 {
			// A QU reply needs the peer's MAC. When it is not resolved
			// yet, the group send needs no ARP — RFC 6762's permitted
			// multicast answer, still only after this query.
			unicast = false
		}
	}
	if !unicast {
		if rc := p.seam.Send(Group4Bytes, Port, Port, pkt); rc < 0 {
			return
		}
	}
	if p.seam.Log != nil {
		line := "mdns answer " + p.resp.serviceName() +
			" addr=" + ipv4String(p.resp.svc.IPv4) +
			" port=" + itoa(p.resp.svc.Port)
		if unicast {
			line += " unicast=1"
		} else {
			line += " unicast=0"
		}
		p.seam.Log(line)
	}
}

// ipv4String/itoa keep the responder free of fmt and net (the guest's
// stdlib subset) — the same dotted-quad shape vi.FormatIPv4 prints.
func ipv4String(a [4]byte) string {
	out := make([]byte, 0, 15)
	for i, b := range a {
		if i != 0 {
			out = append(out, '.')
		}
		out = appendUint(out, uint32(b))
	}
	return string(out)
}

func itoa(v uint16) string {
	return string(appendUint(nil, uint32(v)))
}

func appendUint(out []byte, v uint32) []byte {
	if v == 0 {
		return append(out, '0')
	}
	var tmp [10]byte
	i := len(tmp)
	for v != 0 {
		i--
		tmp[i] = byte('0' + v%10)
		v /= 10
	}
	return append(out, tmp[i:]...)
}
