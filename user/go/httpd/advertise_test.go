package main

import (
	"sync"
	"testing"
	"time"

	"virelai/mdns"
	"virelai/vi"
)

func TestValidateAdvertiseName(t *testing.T) {
	for _, ok := range []struct {
		name string
		want bool
	}{
		{"v198b-share", true},
		{"a", true},
		{"two.part.name", true},
		{"", false},
		{".", false},
		{"a.", false},
		{".a", false},
		{"a..b", false},
		{string(make([]byte, 64)), false}, // over the 63-octet label bound
	} {
		if got := validateAdvertiseName(ok.name); got != ok.want {
			t.Fatalf("validateAdvertiseName(%q) = %v, want %v", ok.name, got, ok.want)
		}
	}
	long := ""
	for len(long) <= mdns.MaxNameWire {
		long += "aaaaaaaa."
	}
	if validateAdvertiseName(long) {
		t.Fatal("over-255-octet name accepted")
	}
}

func TestOnNatAttachment(t *testing.T) {
	// A refused snapshot and an unset own IP both refuse — nothing to
	// announce from, nothing to prove a LAN by.
	if !onNatAttachment(vi.NetStats{}, false) {
		t.Fatal("refused snapshot must refuse")
	}
	if !onNatAttachment(vi.NetStats{}, true) {
		t.Fatal("unset own IP must refuse")
	}
	// The observed VZ NAT signature.
	for _, ip := range [][4]byte{{192, 168, 64, 5}, {192, 168, 64, 1}, {192, 168, 64, 255}} {
		if !onNatAttachment(vi.NetStats{OwnIP: ip}, true) {
			t.Fatalf("NAT subnet %v must refuse", ip)
		}
	}
	// vmnet-pinned, bridged-LAN, and --net capture addresses all proceed.
	for _, ip := range [][4]byte{{192, 168, 78, 5}, {192, 168, 1, 40}, {10, 0, 0, 1}, {192, 168, 65, 1}, {193, 168, 64, 5}} {
		if onNatAttachment(vi.NetStats{OwnIP: ip}, true) {
			t.Fatalf("non-NAT %v must not refuse", ip)
		}
	}
}

func TestLinkLocal6(t *testing.T) {
	// The kernel's iid_from_mac on the fixed guest MAC: fe80::ff:fe00:1.
	a := linkLocal6([6]byte{0x02, 0x00, 0x00, 0x00, 0x00, 0x01})
	want := [16]byte{0xfe, 0x80, 0, 0, 0, 0, 0, 0, 0x00, 0x00, 0x00, 0xff, 0xfe, 0x00, 0x00, 0x01}
	if a != want {
		t.Fatalf("linkLocal6 = %x, want %x", a, want)
	}
}

func TestNewAdvertiserRefusals(t *testing.T) {
	var logs []string
	seam := advSeam{
		log: func(l string) { logs = append(logs, l) },
		stats: func() (vi.NetStats, int64) {
			return vi.NetStats{OwnIP: [4]byte{192, 168, 64, 5}}, vi.NetStatsBytes
		},
	}
	if a := newAdvertiser("v198b", 8080, seam); a != nil {
		t.Fatal("NAT-signature own IP must refuse the advertiser")
	}
	seam.stats = func() (vi.NetStats, int64) {
		return vi.NetStats{OwnIP: [4]byte{192, 168, 78, 5}}, vi.NetStatsBytes
	}
	if a := newAdvertiser("a..b", 8080, seam); a != nil {
		t.Fatal("a bad name must refuse the advertiser")
	}
	a := newAdvertiser("v198b", 8080, seam)
	if a == nil {
		t.Fatal("a bridged-shaped boot must accept the advertiser")
	}
	if a.svcName != "v198b._http._tcp.local" || a.hostName != "v198b.local" {
		t.Fatalf("names = %q / %q", a.svcName, a.hostName)
	}
	if a.ip4 != [4]byte{192, 168, 78, 5} {
		t.Fatalf("advertised A = %v", a.ip4)
	}
}

// decodeAnswers decodes one advertiser datagram and returns its sections
// for field checks — the wire shape a real browser walks.
func decodeAnswers(t *testing.T, pkt []byte) *mdns.Message {
	t.Helper()
	m, err := mdns.Decode(pkt)
	if err != nil {
		t.Fatalf("advertise datagram does not decode: %v", err)
	}
	return m
}

func TestAdvertiseWireShape(t *testing.T) {
	var logs []string
	seam := advSeam{log: func(l string) { logs = append(logs, l) }, stats: func() (vi.NetStats, int64) {
		return vi.NetStats{OwnIP: [4]byte{192, 168, 78, 5}, MAC: [6]byte{0x02, 0, 0, 0, 0, 1}}, vi.NetStatsBytes
	}}
	a := newAdvertiser("v198b", 8080, seam)
	if a == nil {
		t.Fatal("newAdvertiser refused a bridged-shaped boot")
	}

	m := decodeAnswers(t, a.announce)
	if !m.Response || !m.Authoritative {
		t.Fatal("announcement must be an authoritative response")
	}
	if len(m.Questions) != 0 {
		t.Fatal("announcement carries a question")
	}
	var ptr, srv, txt *mdns.Record
	for i := range m.Answers {
		switch m.Answers[i].Type {
		case mdns.TypePTR:
			ptr = &m.Answers[i]
		case mdns.TypeSRV:
			srv = &m.Answers[i]
		case mdns.TypeTXT:
			txt = &m.Answers[i]
		}
	}
	if ptr == nil || ptr.Name != "_http._tcp.local" || ptr.PTR != "v198b._http._tcp.local" || ptr.TTL != advTTL {
		t.Fatalf("PTR record wrong: %+v", ptr)
	}
	if srv == nil || srv.Name != "v198b._http._tcp.local" || srv.SRV.Target != "v198b.local" || srv.SRV.Port != 8080 || srv.TTL != advTTL {
		t.Fatalf("SRV record wrong: %+v", srv)
	}
	if txt == nil || len(txt.TXT) == 0 {
		t.Fatalf("TXT record wrong: %+v", txt)
	}
	var arec, aaa *mdns.Record
	for i := range m.Additionals {
		switch m.Additionals[i].Type {
		case mdns.TypeA:
			arec = &m.Additionals[i]
		case mdns.TypeAAAA:
			aaa = &m.Additionals[i]
		}
	}
	if arec == nil || arec.Name != "v198b.local" || arec.A != [4]byte{192, 168, 78, 5} {
		t.Fatalf("A record wrong: %+v", arec)
	}
	if aaa == nil || aaa.Name != "v198b.local" || aaa.AAAA != linkLocal6([6]byte{0x02, 0, 0, 0, 0, 1}) {
		t.Fatalf("AAAA record wrong: %+v", aaa)
	}

	// The goodbye is the SAME names at TTL=0 — a browser expires the set.
	g := decodeAnswers(t, a.goodbye)
	for _, sec := range [][]mdns.Record{g.Answers, g.Additionals} {
		for _, r := range sec {
			if r.TTL != 0 {
				t.Fatalf("goodbye record carries TTL %d", r.TTL)
			}
		}
	}
}

func TestAdvertiserStartStopSeam(t *testing.T) {
	var mu sync.Mutex
	var binds, closes []uint16
	var sends [][]byte
	seam := advSeam{
		log:    func(string) {},
		listen: func(p uint16) int64 { binds = append(binds, p); return 0 },
		recv:   func(uint16, []byte) int64 { return 0 },
		close:  func(p uint16) int64 { closes = append(closes, p); return 0 },
		send: func(dst [4]byte, dp, sp uint16, b []byte) int64 {
			if dst != mdns.Group4Bytes || dp != mdns.Port || sp != mdns.Port {
				return -1
			}
			cp := make([]byte, len(b))
			copy(cp, b)
			mu.Lock()
			sends = append(sends, cp)
			mu.Unlock()
			return int64(len(b))
		},
		stats: func() (vi.NetStats, int64) {
			return vi.NetStats{OwnIP: [4]byte{192, 168, 78, 5}}, vi.NetStatsBytes
		},
		// A quantum long enough that stop() (called immediately below)
		// closes the stop channel before the burst completes: the emitted
		// set is announce + the goodbye pair, deterministic.
		sleep: func(uint64) { time.Sleep(200 * time.Millisecond) },
	}
	a := newAdvertiser("v198b", 8080, seam)
	if a == nil {
		t.Fatal("newAdvertiser refused")
	}
	if !a.start() {
		t.Fatal("start failed on a live seam")
	}
	if len(binds) != 1 || binds[0] != mdns.Port {
		t.Fatalf("binds = %v, want [5353]", binds)
	}
	mu.Lock()
	n := len(sends)
	mu.Unlock()
	if n != 1 {
		t.Fatalf("start emitted %d datagrams, want the first announce only", n)
	}
	a.stop()
	if len(closes) != 1 || closes[0] != mdns.Port {
		t.Fatalf("closes = %v, want [5353]", closes)
	}
	mu.Lock()
	defer mu.Unlock()
	if len(sends) != 3 {
		t.Fatalf("stop left %d total datagrams, want announce + 2 goodbyes", len(sends))
	}
	if m := decodeAnswers(t, sends[0]); m.Answers[0].TTL != advTTL {
		t.Fatal("first datagram is not the live announce")
	}
	for _, pkt := range sends[1:] {
		g := decodeAnswers(t, pkt)
		for _, r := range g.Answers {
			if r.TTL != 0 {
				t.Fatal("a goodbye datagram carries a nonzero TTL")
			}
		}
	}
}

// M206b residual (#3814): the advertiser's own :5353 bind also serves
// matching queries — a QU browse gets a bounded unicast answer on the
// same owner, and the console names the query and answer.
func TestAdvertiseAnswersQuery(t *testing.T) {
	var mu sync.Mutex
	var logs []string
	type sent struct {
		dst [4]byte
		pkt []byte
	}
	var sends []sent
	q, err := mdns.Query("_http._tcp.local", mdns.TypePTR, true).Encode()
	if err != nil {
		t.Fatal(err)
	}
	peer := [4]byte{10, 0, 0, 17}
	record := make([]byte, 4+8+len(q))
	copy(record[0:4], peer[:])
	record[4], record[5] = 0x14, 0xe9 // source port 5353
	record[6], record[7] = 0, 0
	record[8] = byte((8 + len(q)) >> 8)
	record[9] = byte(8 + len(q))
	copy(record[12:], q)
	pending := [][]byte{record}
	seam := advSeam{
		log:    func(l string) { mu.Lock(); logs = append(logs, l); mu.Unlock() },
		listen: func(uint16) int64 { return 0 },
		recv: func(_ uint16, buf []byte) int64 {
			mu.Lock()
			defer mu.Unlock()
			if len(pending) == 0 {
				return 0
			}
			d := pending[0]
			pending = pending[1:]
			return int64(copy(buf, d))
		},
		close: func(uint16) int64 { return 0 },
		send: func(dst [4]byte, dp, sp uint16, b []byte) int64 {
			if dp != mdns.Port || sp != mdns.Port {
				return -1
			}
			mu.Lock()
			sends = append(sends, sent{dst: dst, pkt: append([]byte(nil), b...)})
			mu.Unlock()
			return int64(len(b))
		},
		stats: func() (vi.NetStats, int64) {
			return vi.NetStats{OwnIP: [4]byte{10, 0, 0, 16}}, vi.NetStatsBytes
		},
		sleep: func(uint64) {},
	}
	a := newAdvertiser("v206b", 8080, seam)
	if a == nil {
		t.Fatal("newAdvertiser refused")
	}
	if a.responder == nil {
		t.Fatal("advertiser has no query responder")
	}
	if !a.start() {
		t.Fatal("start failed")
	}
	found := false
	for i := 0; i < 2000 && !found; i++ {
		mu.Lock()
		for _, s := range sends {
			if s.dst == peer {
				found = true
			}
		}
		mu.Unlock()
		if !found {
			time.Sleep(time.Millisecond)
		}
	}
	burstDone := false
	for i := 0; i < 2000 && !burstDone; i++ {
		mu.Lock()
		for _, l := range logs {
			if l == "httpd: advertise burst done" {
				burstDone = true
			}
		}
		mu.Unlock()
		if !burstDone {
			time.Sleep(time.Millisecond)
		}
	}
	a.stop()
	if !found {
		t.Fatal("no unicast answer reached the querying peer")
	}
	mu.Lock()
	defer mu.Unlock()
	var answered *mdns.Message
	for _, s := range sends {
		if s.dst != peer {
			continue
		}
		m, err := mdns.Decode(s.pkt)
		if err != nil {
			t.Fatalf("answer decode: %v", err)
		}
		answered = m
	}
	if answered == nil || !answered.Response || len(answered.Answers) == 0 {
		t.Fatalf("answer message = %+v", answered)
	}
	if answered.Answers[0].Type != mdns.TypePTR ||
		answered.Answers[0].PTR != "v206b._http._tcp.local" {
		t.Fatalf("answer PTR = %+v", answered.Answers[0])
	}
	need := []string{
		"httpd: mdns query _http._tcp.local",
		"httpd: mdns answer v206b._http._tcp.local addr=10.0.0.16 port=8080 unicast=1",
		"httpd: advertise burst done",
		"httpd: advertise goodbye sent",
	}
	for _, wantLine := range need {
		ok := false
		for _, l := range logs {
			if l == wantLine {
				ok = true
			}
		}
		if !ok {
			t.Fatalf("missing log %q in %v", wantLine, logs)
		}
	}
}

func TestAdvertiserBindRefusal(t *testing.T) {
	var logs []string
	seam := advSeam{
		log:    func(l string) { logs = append(logs, l) },
		listen: func(uint16) int64 { return -1 },
		stats: func() (vi.NetStats, int64) {
			return vi.NetStats{OwnIP: [4]byte{192, 168, 78, 5}}, vi.NetStatsBytes
		},
	}
	a := newAdvertiser("v198b", 8080, seam)
	if a.start() {
		t.Fatal("a refused bind must fail start, not run unadvertised")
	}
}

// #3444: a grant refusal names the missing word — `net` when the stats
// read is denied, `net-mdns` when the :5353 bind is.
func TestAdvertiseGrantRefusals(t *testing.T) {
	var logs []string
	seam := advSeam{
		log: func(l string) { logs = append(logs, l) },
		stats: func() (vi.NetStats, int64) {
			return vi.NetStats{}, -vi.ErrEACCES
		},
	}
	if a := newAdvertiser("v198d", 8080, seam); a != nil {
		t.Fatal("a net-denied stats read must refuse the advertiser")
	}
	if len(logs) == 0 || logs[len(logs)-1] != "httpd: advertise needs net" {
		t.Fatalf("logs=%v", logs)
	}

	logs = nil
	seam.stats = func() (vi.NetStats, int64) {
		return vi.NetStats{OwnIP: [4]byte{192, 168, 78, 5}}, vi.NetStatsBytes
	}
	seam.listen = func(uint16) int64 { return -vi.ErrEACCES }
	a := newAdvertiser("v198d", 8080, seam)
	if a == nil {
		t.Fatal("bridged-shaped stats must accept the advertiser")
	}
	if a.start() {
		t.Fatal("a net-mdns-denied bind must fail start")
	}
	found := false
	for _, l := range logs {
		if l == "httpd: advertise needs net-mdns" {
			found = true
		}
	}
	if !found {
		t.Fatalf("logs=%v", logs)
	}
}

func TestParseArgsAdvertise(t *testing.T) {
	cfg := parseArgs([]string{"GOHTTPD.ELF", "--advertise", "v198b"})
	if cfg.advertise != "v198b" || cfg.port != defaultPort {
		t.Fatalf("cfg = %+v", cfg)
	}
	cfg = parseArgs([]string{"GOHTTPD.ELF", "--advertise", "v198b", "9090"})
	if cfg.advertise != "v198b" || cfg.port != 9090 {
		t.Fatalf("cfg = %+v", cfg)
	}
	cfg = parseArgs([]string{"GOHTTPD.ELF", "--advertise"})
	if cfg.advertise != "" {
		t.Fatalf("dangling --advertise must not advertise: %+v", cfg)
	}
	cfg = parseArgs([]string{"GOHTTPD.ELF"})
	if cfg.advertise != "" {
		t.Fatalf("no flag must not advertise: %+v", cfg)
	}
}
