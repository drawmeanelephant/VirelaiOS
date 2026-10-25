package main

import (
	"strings"
	"testing"

	"virelai/mdns"
	"virelai/vi"
)

// fakeDiscSeam plays the kernel back: a scripted inbox of slot-85
// records, a send log, and the refusal knobs the named-refusal tests
// flip. Every send's dst/ports are recorded so the wire shape (multicast
// group, 5353->5353) is asserted, not assumed.
type fakeDisc struct {
	snap      vi.NetStats
	snapRC    int64
	listenRC  int64
	sendRC    int64
	inbox     [][]byte
	listened  uint16
	closed    uint16
	sends     int
	sendDst   [4]byte
	sendPorts [2]uint16
	wire      [][]byte
	logs      []string
	sleeps    uint64
}

func (f *fakeDisc) seam() discSeam {
	return discSeam{
		listen: func(port uint16) int64 {
			if f.listenRC != 0 {
				return f.listenRC
			}
			f.listened = port
			return 0
		},
		send: func(dst [4]byte, dstPort, srcPort uint16, b []byte) int64 {
			if f.sendRC != 0 {
				return f.sendRC
			}
			f.sends++
			f.sendDst = dst
			f.sendPorts = [2]uint16{srcPort, dstPort}
			f.wire = append(f.wire, append([]byte(nil), b...))
			return int64(len(b))
		},
		recv: func(port uint16, buf []byte) int64 {
			if len(f.inbox) == 0 {
				return 0
			}
			d := f.inbox[0]
			f.inbox = f.inbox[1:]
			return int64(copy(buf, d))
		},
		close: func(port uint16) int64 { f.closed = port; return 0 },
		stats: func() (vi.NetStats, int64) { return f.snap, f.snapRC },
		sleep: func(ticks uint64) { f.sleeps += ticks },
		log:   func(line string) { f.logs = append(f.logs, line) },
	}
}

func (f *fakeDisc) has(sub string) bool {
	for _, l := range f.logs {
		if strings.Contains(l, sub) {
			return true
		}
	}
	return false
}

// bridgedFake is the passing attachment: the --net capture shape (a
// real wire, a real own IP, no NAT signature).
func bridgedFake() *fakeDisc {
	return &fakeDisc{
		snap:   vi.NetStats{OwnIP: [4]byte{10, 0, 0, 1}, MAC: [6]byte{2, 0, 0, 0, 0, 1}},
		snapRC: vi.NetStatsBytes,
	}
}

// discDatagram wraps a message payload in the slot-85 record shape:
// [4-byte src IPv4][8-byte UDP header][payload].
func discDatagram(srcIP [4]byte, srcPort, dstPort uint16, payload []byte) []byte {
	d := make([]byte, 4+8+len(payload))
	copy(d[:4], srcIP[:])
	d[4] = byte(srcPort >> 8)
	d[5] = byte(srcPort)
	d[6] = byte(dstPort >> 8)
	d[7] = byte(dstPort)
	copy(d[12:], payload)
	return d
}

// gateServiceMsg is the fake _workstation._tcp responder's answer: the
// PTR in answers with the SRV/TXT chain, the host's A in additionals —
// the RFC 6763 shape a real responder stuffs.
func gateServiceMsg(t *testing.T) []byte {
	t.Helper()
	m := &mdns.Message{
		Response: true, Authoritative: true,
		Answers: []mdns.Record{
			{Name: "_workstation._tcp.local", Type: mdns.TypePTR, TTL: 120,
				PTR: "gate-ws._workstation._tcp.local"},
			{Name: "gate-ws._workstation._tcp.local", Type: mdns.TypeSRV, TTL: 120, Flush: true,
				SRV: mdns.SRV{Port: 9999, Target: "gate-host.local"}},
			{Name: "gate-ws._workstation._tcp.local", Type: mdns.TypeTXT, TTL: 120, Flush: true,
				TXT: []string{"txtvers=1", "role=gate"}},
		},
		Additionals: []mdns.Record{
			{Name: "gate-host.local", Type: mdns.TypeA, TTL: 120, Flush: true,
				A: [4]byte{10, 0, 0, 2}},
		},
	}
	pkt, err := m.Encode()
	if err != nil {
		t.Fatalf("fixture encode: %v", err)
	}
	return pkt
}

func TestDiscNormName(t *testing.T) {
	for arg, want := range map[string]string{
		"":                        "",
		"_workstation._tcp":       "_workstation._tcp.local",
		"_workstation._tcp.local": "_workstation._tcp.local",
		"printer._sub._http._tcp": "printer._sub._http._tcp.local",
	} {
		if got := discNormName(arg); got != want {
			t.Fatalf("discNormName(%q) = %q, want %q", arg, got, want)
		}
	}
}

func TestDiscoverRefusals(t *testing.T) {
	// No snapshot at all — the guest cannot even read its own stats.
	f := &fakeDisc{snapRC: 0}
	if rc := runDiscover([]string{"_workstation._tcp"}, f.seam()); rc != 1 || !f.has("netstat: discover offline") {
		t.Fatalf("no-snapshot: rc=%d logs=%v", rc, f.logs)
	}
	// A snapshot with no own IP — nothing to source a query from.
	f = &fakeDisc{snap: vi.NetStats{}, snapRC: vi.NetStatsBytes}
	if rc := runDiscover([]string{"_workstation._tcp"}, f.seam()); rc != 1 || !f.has("netstat: discover offline") {
		t.Fatalf("no-IP: rc=%d logs=%v", rc, f.logs)
	}
	// The observed VZ NAT signature — multicast has no LAN segment.
	f = &fakeDisc{snap: vi.NetStats{OwnIP: [4]byte{192, 168, 64, 5}}, snapRC: vi.NetStatsBytes}
	if rc := runDiscover([]string{"_workstation._tcp"}, f.seam()); rc != 1 || !f.has("netstat: discover needs bridged") {
		t.Fatalf("NAT: rc=%d logs=%v", rc, f.logs)
	}
	if f.listened != 0 || f.sends != 0 {
		t.Fatal("a refused browse must not bind or send")
	}
	// #3444: grant refusals name the missing word — the stats read rides
	// the `net` slot group, the :5353 bind/sends the `net-mdns` service
	// right.
	f = &fakeDisc{snapRC: -vi.ErrEACCES}
	if rc := runDiscover([]string{"_workstation._tcp"}, f.seam()); rc != 1 || !f.has("netstat: discover needs net") {
		t.Fatalf("net denied: rc=%d logs=%v", rc, f.logs)
	}
	f = bridgedFake()
	f.listenRC = -vi.ErrEACCES
	if rc := runDiscover([]string{"_workstation._tcp"}, f.seam()); rc != 1 || !f.has("netstat: discover needs net-mdns") {
		t.Fatalf("net-mdns denied at bind: rc=%d logs=%v", rc, f.logs)
	}
	f = bridgedFake()
	f.sendRC = -vi.ErrEACCES
	if rc := runDiscover([]string{"_workstation._tcp"}, f.seam()); rc != 1 || !f.has("netstat: discover needs net-mdns") {
		t.Fatalf("net-mdns denied at send: rc=%d logs=%v", rc, f.logs)
	}
	// The bind is refused — a running advertiser holds 5353.
	f = bridgedFake()
	f.listenRC = -22 // EINVAL
	if rc := runDiscover([]string{"_workstation._tcp"}, f.seam()); rc != 1 || !f.has("netstat: discover bind refused rc=-22") {
		t.Fatalf("bind: rc=%d logs=%v", rc, f.logs)
	}
	// The send is refused.
	f = bridgedFake()
	f.sendRC = -22
	if rc := runDiscover([]string{"_workstation._tcp"}, f.seam()); rc != 1 || !f.has("netstat: discover send refused rc=-22") {
		t.Fatalf("send: rc=%d logs=%v", rc, f.logs)
	}
	// A bad service-type argument is a usage refusal, not a query.
	f = bridgedFake()
	if rc := runDiscover([]string{"a..b"}, f.seam()); rc != 1 || !f.has("usage: discover") {
		t.Fatalf("usage: rc=%d logs=%v", rc, f.logs)
	}
}

func TestDiscoverBrowse(t *testing.T) {
	f := bridgedFake()
	f.inbox = append(f.inbox,
		discDatagram([4]byte{10, 0, 0, 2}, mdns.Port, mdns.Port, gateServiceMsg(t)))
	rc := runDiscover([]string{"_workstation._tcp"}, f.seam())
	if rc != 0 {
		t.Fatalf("rc=%d logs=%v", rc, f.logs)
	}
	if f.listened != mdns.Port || f.closed != mdns.Port {
		t.Fatalf("port lifecycle: listened=%d closed=%d", f.listened, f.closed)
	}
	// The bounded window: exactly two sends (t=0 and the resend tick),
	// each to the mDNS group:port from the bound 5353.
	if f.sends != 2 {
		t.Fatalf("sends=%d, want the bounded two", f.sends)
	}
	if f.sendDst != mdns.Group4Bytes || f.sendPorts != [2]uint16{mdns.Port, mdns.Port} {
		t.Fatalf("wire shape dst=%v ports=%v", f.sendDst, f.sendPorts)
	}
	for i, pkt := range f.wire {
		m, err := mdns.Decode(pkt)
		if err != nil {
			t.Fatalf("sent query %d does not decode: %v", i, err)
		}
		if m.Response || len(m.Questions) != 1 {
			t.Fatalf("sent query %d is not a one-question query: %+v", i, m)
		}
		q := m.Questions[0]
		if q.Name != "_workstation._tcp.local" || q.Type != mdns.TypePTR || !q.QU {
			t.Fatalf("question %+v, want QU PTR _workstation._tcp.local", q)
		}
	}
	// The deduped print: the instance resolved through SRV -> host -> A.
	want := "netstat: discover service gate-ws._workstation._tcp.local " +
		"host=gate-host.local port=9999 addr=10.0.0.2 txt=txtvers=1;role=gate"
	if !f.has(want) {
		t.Fatalf("service line missing %q in %v", want, f.logs)
	}
	if !f.has("netstat: discover done types=0 services=1 replies=1 ignored=0") {
		t.Fatalf("done line missing in %v", f.logs)
	}
}

func TestDiscoverDedupAndGoodbye(t *testing.T) {
	f := bridgedFake()
	// The same instance announced twice — the cache holds one entry.
	dup := discDatagram([4]byte{10, 0, 0, 2}, mdns.Port, mdns.Port, gateServiceMsg(t))
	// A second instance, announced then withdrawn inside the window.
	gone, err := (&mdns.Message{
		Response: true,
		Answers: []mdns.Record{
			{Name: "_workstation._tcp.local", Type: mdns.TypePTR, TTL: 120,
				PTR: "gone._workstation._tcp.local"},
		},
	}).Encode()
	if err != nil {
		t.Fatal(err)
	}
	bye, err := (&mdns.Message{
		Response: true,
		Answers: []mdns.Record{
			{Name: "_workstation._tcp.local", Type: mdns.TypePTR, TTL: 0,
				PTR: "gone._workstation._tcp.local"},
		},
	}).Encode()
	if err != nil {
		t.Fatal(err)
	}
	f.inbox = append(f.inbox, dup, dup,
		discDatagram([4]byte{10, 0, 0, 2}, mdns.Port, mdns.Port, gone),
		discDatagram([4]byte{10, 0, 0, 2}, mdns.Port, mdns.Port, bye))
	if rc := runDiscover([]string{"_workstation._tcp"}, f.seam()); rc != 0 {
		t.Fatalf("rc=%d logs=%v", rc, f.logs)
	}
	var svcLines int
	for _, l := range f.logs {
		if strings.Contains(l, "netstat: discover service ") {
			svcLines++
		}
		if strings.Contains(l, "gone._workstation._tcp.local") {
			t.Fatalf("withdrawn service printed: %q", l)
		}
	}
	if svcLines != 1 {
		t.Fatalf("deduped service lines = %d, want 1", svcLines)
	}
	if !f.has("services=1 replies=4 ignored=0") {
		t.Fatalf("done line wrong in %v", f.logs)
	}
}

func TestDiscoverMetaBrowse(t *testing.T) {
	f := bridgedFake()
	// The meta answer: the type PTR plus the instance's whole chain in
	// one response — the additionals a real responder stuffs.
	meta, err := (&mdns.Message{
		Response: true, Authoritative: true,
		Answers: []mdns.Record{
			{Name: discMetaName, Type: mdns.TypePTR, TTL: 120,
				PTR: "_workstation._tcp.local"},
			{Name: "_workstation._tcp.local", Type: mdns.TypePTR, TTL: 120,
				PTR: "gate-ws._workstation._tcp.local"},
		},
		Additionals: []mdns.Record{
			{Name: "gate-ws._workstation._tcp.local", Type: mdns.TypeSRV, TTL: 120,
				SRV: mdns.SRV{Port: 9999, Target: "gate-host.local"}},
		},
	}).Encode()
	if err != nil {
		t.Fatal(err)
	}
	f.inbox = append(f.inbox, discDatagram([4]byte{10, 0, 0, 2}, mdns.Port, mdns.Port, meta))
	if rc := runDiscover(nil, f.seam()); rc != 0 {
		t.Fatalf("rc=%d logs=%v", rc, f.logs)
	}
	if !f.has("netstat: discover type _workstation._tcp.local") {
		t.Fatalf("type line missing in %v", f.logs)
	}
	if !f.has("netstat: discover service gate-ws._workstation._tcp.local host=gate-host.local port=9999") {
		t.Fatalf("service line missing in %v", f.logs)
	}
	// The bare verb's second phase bounded at discMaxTypes: 2 meta
	// sends + 2 per browsed type.
	if f.sends != 4 {
		t.Fatalf("meta browse sends=%d, want 4", f.sends)
	}
	if !f.has("types=1 services=1 replies=1") {
		t.Fatalf("done line wrong in %v", f.logs)
	}
}

func TestDiscoverIgnoresNoise(t *testing.T) {
	f := bridgedFake()
	good := gateServiceMsg(t)
	// A non-mDNS datagram (wrong source port), a query (QR=0 — not an
	// answer), a truncated message, and the good reply.
	q, err := mdns.Query("_workstation._tcp.local", mdns.TypePTR, true).Encode()
	if err != nil {
		t.Fatal(err)
	}
	f.inbox = append(f.inbox,
		discDatagram([4]byte{10, 0, 0, 2}, 65000, mdns.Port, good),
		discDatagram([4]byte{10, 0, 0, 2}, mdns.Port, mdns.Port, q),
		discDatagram([4]byte{10, 0, 0, 2}, mdns.Port, mdns.Port, good[:len(good)/2]),
		discDatagram([4]byte{10, 0, 0, 2}, mdns.Port, mdns.Port, good))
	if rc := runDiscover([]string{"_workstation._tcp"}, f.seam()); rc != 0 {
		t.Fatalf("rc=%d logs=%v", rc, f.logs)
	}
	if !f.has("services=1 replies=1 ignored=3") {
		t.Fatalf("noise accounting wrong in %v", f.logs)
	}
}

func TestDiscoverUnresolved(t *testing.T) {
	f := bridgedFake()
	// A PTR with no SRV following — the honest print is "unresolved",
	// never an invented address.
	ptr, err := (&mdns.Message{
		Response: true,
		Answers: []mdns.Record{
			{Name: "_workstation._tcp.local", Type: mdns.TypePTR, TTL: 120,
				PTR: "ghost._workstation._tcp.local"},
		},
	}).Encode()
	if err != nil {
		t.Fatal(err)
	}
	f.inbox = append(f.inbox, discDatagram([4]byte{10, 0, 0, 2}, mdns.Port, mdns.Port, ptr))
	if rc := runDiscover([]string{"_workstation._tcp"}, f.seam()); rc != 0 {
		t.Fatalf("rc=%d logs=%v", rc, f.logs)
	}
	if !f.has("netstat: discover service ghost._workstation._tcp.local unresolved") {
		t.Fatalf("unresolved line missing in %v", f.logs)
	}
}
