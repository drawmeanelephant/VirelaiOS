package main

import (
	"sync"
	"testing"

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
		// Every sleep parks — the cadence goroutine blocks at its first
		// burst wait and never emits again, so the sends list is
		// deterministic: announce, then the goodbye pair.
		sleep: func(uint64) { select {} },
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
