package main

import (
	"testing"

	"virelai/mdns"
	"virelai/vi"
)

// fakeAdvSeam gives the unit tests a deterministic attachment snapshot and
// console capture without the guest's net seam. NewAdvertiser only
// pre-encodes, so the I/O functions stay nil here; Start/Stop are covered by
// the mdns package's lifecycle tests.
func fakeAdvSeam(ip [4]byte, rc int64, logs *[]string) advSeam {
	return advSeam{
		stats: func() (vi.NetStats, int64) {
			return vi.NetStats{OwnIP: ip, MAC: [6]byte{0x02, 0, 0, 0, 0, 0x0a}}, rc
		},
		log: func(line string) { *logs = append(*logs, line) },
	}
}

func TestNewAdvDescribesFlintService(t *testing.T) {
	var logs []string
	seam := fakeAdvSeam([4]byte{10, 0, 0, 16}, vi.NetStatsBytes, &logs)
	adv := newAdv("hubflint", 4748, seam)
	if adv == nil {
		t.Fatalf("bridged-shaped stats refused the advertiser: %v", logs)
	}
	svcName := "hubflint._flint._tcp.local"
	if adv.ServiceName() != svcName {
		t.Fatalf("service name = %q, want %q", adv.ServiceName(), svcName)
	}

	// The announcement carries the DNS-SD chain a browsing peer walks.
	m, err := mdns.Decode(adv.Announce())
	if err != nil {
		t.Fatalf("announce decode: %v", err)
	}
	if !m.Response || !m.Authoritative || len(m.Questions) != 0 {
		t.Fatalf("announce envelope = %+v", m)
	}
	var ptr, srv *mdns.Record
	for i := range m.Answers {
		switch m.Answers[i].Type {
		case mdns.TypePTR:
			ptr = &m.Answers[i]
		case mdns.TypeSRV:
			srv = &m.Answers[i]
		}
	}
	if ptr == nil || ptr.Name != "_flint._tcp.local" || ptr.PTR != svcName || ptr.TTL != mdns.AdvTTL {
		t.Fatalf("PTR = %+v", ptr)
	}
	if srv == nil || srv.Name != svcName || srv.SRV.Target != "hubflint.local" ||
		srv.SRV.Port != 4748 || srv.TTL != mdns.AdvTTL {
		t.Fatalf("SRV = %+v", srv)
	}
	var arec *mdns.Record
	for i := range m.Additionals {
		if m.Additionals[i].Type == mdns.TypeA {
			arec = &m.Additionals[i]
		}
	}
	if arec == nil || arec.Name != "hubflint.local" || arec.A != [4]byte{10, 0, 0, 16} {
		t.Fatalf("A = %+v", arec)
	}

	// A post-burst PTR query resolves to the same instance/port/address.
	q := mdns.Query("_flint._tcp.local", mdns.TypePTR, true)
	pkt, unicast, err := adv.Responder().Answer(q)
	if err != nil || !unicast {
		t.Fatalf("query answer: unicast=%v err=%v", unicast, err)
	}
	got, err := mdns.Decode(pkt)
	if err != nil {
		t.Fatalf("answer decode: %v", err)
	}
	// The answer section carries the PTR; the instance SRV/A chain rides
	// the additionals, as in the shared responder's query path.
	var gotSRV *mdns.Record
	for _, recs := range [][]mdns.Record{got.Answers, got.Additionals} {
		for i := range recs {
			if recs[i].Type == mdns.TypeSRV {
				gotSRV = &recs[i]
			}
		}
	}
	if gotSRV == nil || gotSRV.SRV.Port != 4748 || gotSRV.SRV.Target != "hubflint.local" {
		t.Fatalf("answer SRV = %+v", gotSRV)
	}

	// The goodbye is the same names at TTL=0.
	bye, err := mdns.Decode(adv.Goodbye())
	if err != nil {
		t.Fatalf("goodbye decode: %v", err)
	}
	for _, recs := range [][]mdns.Record{bye.Answers, bye.Additionals} {
		for _, r := range recs {
			if r.TTL != 0 {
				t.Fatalf("goodbye record carries TTL %d", r.TTL)
			}
		}
	}
}

func TestNewAdvRefusals(t *testing.T) {
	cases := []struct {
		name string
		ip   [4]byte
		rc   int64
		want string
	}{
		{"a..b", [4]byte{10, 0, 0, 16}, vi.NetStatsBytes, "flintd: advertise bad name"},
		{"hubflint", [4]byte{10, 0, 0, 16}, -vi.ErrEACCES, "flintd: advertise needs net"},
		{"hubflint", [4]byte{192, 168, 64, 5}, vi.NetStatsBytes, "flintd: advertise needs bridged"},
		{"hubflint", [4]byte{}, vi.NetStatsBytes, "flintd: advertise needs bridged"},
		{"hubflint", [4]byte{}, 0, "flintd: advertise needs bridged"},
	}
	for _, c := range cases {
		var logs []string
		seam := fakeAdvSeam(c.ip, c.rc, &logs)
		if adv := newAdv(c.name, 4748, seam); adv != nil {
			t.Fatalf("%+v: advertiser must be refused", c)
		}
		if len(logs) != 1 || logs[0] != c.want {
			t.Fatalf("%+v: logs = %v, want [%q]", c, logs, c.want)
		}
	}
}
