package mdns

import (
	"sync"
	"testing"
	"time"
)

func TestValidInstance(t *testing.T) {
	for _, c := range []struct {
		name string
		want bool
	}{
		{"v198b-share", true},
		{"hubflint", true},
		{"two.part.name", true},
		{"", false},
		{".", false},
		{"a.", false},
		{".a", false},
		{"a..b", false},
		{string(make([]byte, 64)), false},
	} {
		if got := ValidInstance(c.name); got != c.want {
			t.Fatalf("ValidInstance(%q) = %v, want %v", c.name, got, c.want)
		}
	}
}

func TestLinkLocal6(t *testing.T) {
	a := LinkLocal6([6]byte{0x02, 0, 0, 0, 0, 0x01})
	want := [16]byte{0xfe, 0x80, 0, 0, 0, 0, 0, 0, 0x00, 0x00, 0x00, 0xff, 0xfe, 0x00, 0x00, 0x01}
	if a != want {
		t.Fatalf("LinkLocal6 = %x, want %x", a, want)
	}
}

func TestOnNatSubnet(t *testing.T) {
	for _, ip := range [][4]byte{{192, 168, 64, 5}, {192, 168, 64, 1}, {192, 168, 64, 255}} {
		if !OnNatSubnet(ip) {
			t.Fatalf("%v must be the NAT signature", ip)
		}
	}
	for _, ip := range [][4]byte{{192, 168, 78, 5}, {192, 168, 1, 40}, {10, 0, 0, 16}, {192, 168, 65, 1}} {
		if OnNatSubnet(ip) {
			t.Fatalf("%v must not be the NAT signature", ip)
		}
	}
}

// advHarness is a scripted AdvSeam: recorded binds/sends/closes and a
// recv queue the query test feeds.
type advHarness struct {
	mu     sync.Mutex
	binds  []uint16
	closes []uint16
	sends  [][2]interface{}
	pkts   [][]byte
	logs   []string
}

func newAdvHarness() *advHarness { return &advHarness{} }

func (h *advHarness) log(line string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.logs = append(h.logs, line)
}

func (h *advHarness) listen(port uint16) int64 {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.binds = append(h.binds, port)
	return 0
}

func (h *advHarness) close(port uint16) int64 {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.closes = append(h.closes, port)
	return 0
}

func (h *advHarness) send(dst [4]byte, dstPort, srcPort uint16, b []byte) int64 {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.sends = append(h.sends, [2]interface{}{dst, append([]byte(nil), b...)})
	return int64(len(b))
}

func (h *advHarness) recv(port uint16, buf []byte) int64 {
	h.mu.Lock()
	defer h.mu.Unlock()
	if len(h.pkts) == 0 {
		return 0
	}
	d := h.pkts[0]
	h.pkts = h.pkts[1:]
	return int64(copy(buf, d))
}

func (h *advHarness) seam() AdvSeam {
	return AdvSeam{
		Listen: h.listen,
		Send:   h.send,
		Recv:   h.recv,
		Close:  h.close,
		Sleep:  func(uint64) { time.Sleep(2 * time.Millisecond) },
		Log:    h.log,
	}
}

func advTestService() Service {
	return Service{
		Type:     "_flint._tcp.local",
		Instance: "hubflint",
		Host:     "hubflint.local",
		Port:     4748,
		IPv4:     [4]byte{10, 0, 0, 16},
		IPv6:     LinkLocal6([6]byte{0x02, 0, 0, 0, 0, 0x0a}),
		TTL:      AdvTTL,
		TXT:      []string{"txtvers=1", "proto=FLINT-SYNC/1"},
	}
}

func TestAdvertiserLifecycle(t *testing.T) {
	h := newAdvHarness()
	a := NewAdvertiser(advTestService(), h.seam())
	if a == nil {
		t.Fatal("NewAdvertiser refused a valid service")
	}
	if !a.Start() {
		t.Fatal("Start failed on a live seam")
	}
	select {
	case <-a.BurstDone():
	case <-time.After(5 * time.Second):
		t.Fatal("startup burst never completed")
	}
	a.Stop()

	h.mu.Lock()
	defer h.mu.Unlock()
	if len(h.binds) != 1 || h.binds[0] != Port {
		t.Fatalf("binds = %v, want [%d]", h.binds, Port)
	}
	if len(h.closes) != 1 || h.closes[0] != Port {
		t.Fatalf("closes = %v, want [%d]", h.closes, Port)
	}
	// Three announcements (initial + two burst) then the goodbye pair.
	if len(h.sends) != 1+(AdvBurstN-1)+2 {
		t.Fatalf("sends = %d, want %d", len(h.sends), 1+(AdvBurstN-1)+2)
	}
	first := h.sends[0][1].([]byte)
	if m, err := Decode(first); err != nil || m.Answers[0].TTL != AdvTTL {
		t.Fatalf("first datagram is not the live announce: %v %v", err, m)
	}
	for _, s := range h.sends[len(h.sends)-2:] {
		m, err := Decode(s[1].([]byte))
		if err != nil {
			t.Fatalf("goodbye decode: %v", err)
		}
		for _, r := range m.Answers {
			if r.TTL != 0 {
				t.Fatal("goodbye datagram carries a nonzero TTL")
			}
		}
	}
	for _, want := range []string{
		"advertising hubflint._flint._tcp.local port=4748",
		"advertise sent",
		"advertise burst done",
		"advertise goodbye sent",
	} {
		found := false
		for _, l := range h.logs {
			if l == want {
				found = true
				break
			}
		}
		if !found {
			t.Fatalf("missing log %q", want)
		}
	}
}

func TestAdvertiserAnswersQuery(t *testing.T) {
	h := newAdvHarness()
	q, err := Query("_flint._tcp.local", TypePTR, true).Encode()
	if err != nil {
		t.Fatal(err)
	}
	peer := [4]byte{10, 0, 0, 17}
	rec := make([]byte, 4+8+len(q))
	copy(rec[0:4], peer[:])
	rec[4], rec[5] = 0x14, 0xe9 // source port 5353
	rec[8] = byte((8 + len(q)) >> 8)
	rec[9] = byte(8 + len(q))
	copy(rec[12:], q)
	h.pkts = [][]byte{rec}

	a := NewAdvertiser(advTestService(), h.seam())
	if a == nil {
		t.Fatal("NewAdvertiser refused a valid service")
	}
	if !a.Start() {
		t.Fatal("Start failed")
	}
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		h.mu.Lock()
		found := 0
		for _, s := range h.sends {
			if s[0].([4]byte) == peer {
				found++
			}
		}
		h.mu.Unlock()
		if found > 0 {
			break
		}
		time.Sleep(time.Millisecond)
	}
	a.Stop()

	h.mu.Lock()
	defer h.mu.Unlock()
	var answered *Message
	for _, s := range h.sends {
		if s[0].([4]byte) != peer {
			continue
		}
		m, err := Decode(s[1].([]byte))
		if err != nil {
			t.Fatalf("answer decode: %v", err)
		}
		answered = m
	}
	if answered == nil || !answered.Response || len(answered.Answers) == 0 {
		t.Fatalf("answer = %+v", answered)
	}
	if answered.Answers[0].Type != TypePTR || answered.Answers[0].PTR != "hubflint._flint._tcp.local" {
		t.Fatalf("answer PTR = %+v", answered.Answers[0])
	}
	var srv *Record
	for _, recs := range [][]Record{answered.Answers, answered.Additionals} {
		for i := range recs {
			if recs[i].Type == TypeSRV {
				srv = &recs[i]
			}
		}
	}
	if srv == nil || srv.SRV.Port != 4748 || srv.SRV.Target != "hubflint.local" {
		t.Fatalf("answer SRV = %+v", srv)
	}
	for _, want := range []string{
		"mdns query _flint._tcp.local",
		"mdns answer hubflint._flint._tcp.local addr=10.0.0.16 port=4748 unicast=1",
	} {
		found := false
		for _, l := range h.logs {
			if l == want {
				found = true
				break
			}
		}
		if !found {
			t.Fatalf("missing log %q", want)
		}
	}
}

func TestAdvertiserBindRefusal(t *testing.T) {
	h := newAdvHarness()
	seam := h.seam()
	seam.Listen = func(uint16) int64 { return -13 }
	a := NewAdvertiser(advTestService(), seam)
	if a == nil {
		t.Fatal("NewAdvertiser refused a valid service")
	}
	if a.Start() {
		t.Fatal("a refused bind must fail Start")
	}
	found := false
	for _, l := range h.logs {
		if l == "advertise bind failed" {
			found = true
		}
	}
	if !found {
		t.Fatalf("logs = %v", h.logs)
	}
}
