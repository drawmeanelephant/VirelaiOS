package timesync

import (
	"errors"
	"testing"
)

// unix 1_789_043_696 (2026-09-10 12:34:56 UTC) is NTP seconds 3_998_032_496
// (0xEE4D2270): pinned so the era arithmetic cannot drift silently.
const (
	pinUnix = 1_789_043_696
	pinNTP  = 3_998_032_496
)

func TestTimestampConversionPinsTheEraRule(t *testing.T) {
	if got := FromUnixSeconds(pinUnix); got != (Timestamp{Sec: pinNTP}) {
		t.Fatalf("FromUnixSeconds=%+v, want sec %d", got, pinNTP)
	}
	if got := (Timestamp{Sec: pinNTP}).UnixNanos(); got != pinUnix*1e9 {
		t.Fatalf("UnixNanos=%d, want %d", got, int64(pinUnix)*1e9)
	}
	// A half-second fraction is 0x80000000.
	if got := (Timestamp{Sec: pinNTP, Frac: 0x8000_0000}).UnixNanos(); got != pinUnix*1e9+500_000_000 {
		t.Fatalf("half-second UnixNanos=%d", got)
	}
	// MSB set is era 0 (1968..2036); MSB clear is era 1 (2036..2104).
	if got := (Timestamp{Sec: 0x8000_0000}).UnixNanos(); got != (2_147_483_648-2_208_988_800)*1e9 {
		t.Fatalf("era 0 floor=%d", got)
	}
	const rollover = 2_085_978_496 // 2036-02-07 06:28:16 UTC, where the seconds field wraps
	if got := (Timestamp{Sec: 1}).UnixNanos(); got != (rollover+1)*1e9 {
		t.Fatalf("era 1 (Sec=1)=%d, want unix %d", got, rollover+1)
	}
	if got := (Timestamp{Sec: 0x7fff_ffff}).UnixNanos(); got != (rollover+0x7fff_ffff)*1e9 {
		t.Fatalf("era 1 ceiling=%d", got)
	}
	// The wire format truncates: a post-2036 Unix time comes back as Sec=1.
	if got := FromUnixSeconds(rollover + 1); got.Sec != 1 {
		t.Fatalf("FromUnixSeconds past the rollover=%+v, want Sec 1", got)
	}
}

func TestBuildRequestIsAnRFC4330ClientPacket(t *testing.T) {
	tx := Timestamp{Sec: 0x0102_0304, Frac: 0x0506_0708}
	p := BuildRequest(tx)
	if len(p) != 48 {
		t.Fatalf("request is %d bytes, want 48", len(p))
	}
	if p[0] != 0x23 { // LI 0, VN 4, mode 3
		t.Fatalf("byte 0 = %#x, want 0x23", p[0])
	}
	for i, b := range p {
		switch {
		case i == 0:
		case i >= 40 && i < 48:
			if b != byte(i-39) {
				t.Fatalf("transmit byte %d = %#x, want %#x", i, b, i-39)
			}
		default:
			if b != 0 {
				t.Fatalf("byte %d = %#x, want 0 (SNTP leaves every other field zero)", i, b)
			}
		}
	}
}

// server builds a mode-4 packet; each test bends one field.
type serverPkt struct {
	li, vn, mode, stratum byte
	ref                   [4]byte
	origin, rx, tx        Timestamp
}

func goodServer(origin Timestamp) serverPkt {
	return serverPkt{vn: 4, mode: modeServer, stratum: 2, ref: [4]byte{'V', 'I', 'R', 'E'},
		origin: origin, rx: Timestamp{Sec: pinNTP}, tx: Timestamp{Sec: pinNTP, Frac: 1 << 30}}
}

func (s serverPkt) bytes() []byte {
	p := make([]byte, 48)
	p[0] = s.li<<6 | s.vn<<3 | s.mode
	p[1] = s.stratum
	copy(p[12:16], s.ref[:])
	putTimestamp(p[24:32], s.origin)
	putTimestamp(p[32:40], s.rx)
	putTimestamp(p[40:48], s.tx)
	return p
}

func TestParseReplyAcceptsAGoodServerPacket(t *testing.T) {
	origin := Timestamp{Sec: 7, Frac: 9}
	r, err := ParseReply(goodServer(origin).bytes(), origin)
	if err != nil {
		t.Fatalf("good reply refused: %v", err)
	}
	if r.Stratum != 2 || r.Version != 4 || string(r.RefID[:]) != "VIRE" {
		t.Fatalf("decoded %+v", r)
	}
	if r.Transmit != (Timestamp{Sec: pinNTP, Frac: 1 << 30}) || r.Receive != (Timestamp{Sec: pinNTP}) {
		t.Fatalf("timestamps %+v", r)
	}
	// Version 3 servers are still accepted (RFC 4330 interoperates with v3).
	v3 := goodServer(origin)
	v3.vn = 3
	if _, err := ParseReply(v3.bytes(), origin); err != nil {
		t.Fatalf("version 3 refused: %v", err)
	}
}

func TestParseReplyRefusesEveryBadShape(t *testing.T) {
	origin := Timestamp{Sec: 7, Frac: 9}
	cases := []struct {
		name string
		bend func(*serverPkt)
		want error
	}{
		{"client mode echoed back", func(s *serverPkt) { s.mode = modeClient }, ErrNotServer},
		{"broadcast mode", func(s *serverPkt) { s.mode = 5 }, ErrNotServer},
		{"version 2", func(s *serverPkt) { s.vn = 2 }, ErrVersion},
		{"version 5", func(s *serverPkt) { s.vn = 5 }, ErrVersion},
		{"wrong originate", func(s *serverPkt) { s.origin = Timestamp{Sec: 7, Frac: 10} }, ErrNotOurs},
		{"zero originate", func(s *serverPkt) { s.origin = Timestamp{} }, ErrNotOurs},
		{"stratum 16", func(s *serverPkt) { s.stratum = 16 }, ErrStratum},
		{"stratum 255", func(s *serverPkt) { s.stratum = 255 }, ErrStratum},
		{"leap alarm", func(s *serverPkt) { s.li = 3 }, ErrUnsynced},
		{"no transmit time", func(s *serverPkt) { s.tx = Timestamp{} }, ErrNoTime},
	}
	for _, c := range cases {
		s := goodServer(origin)
		c.bend(&s)
		if _, err := ParseReply(s.bytes(), origin); !errors.Is(err, c.want) {
			t.Errorf("%s: err=%v, want %v", c.name, err, c.want)
		}
	}
	for _, n := range []int{0, 1, 47} {
		if _, err := ParseReply(goodServer(origin).bytes()[:n], origin); !errors.Is(err, ErrShort) {
			t.Errorf("%d-byte reply: err=%v, want ErrShort", n, err)
		}
	}
}

func TestParseReplyNamesAKissOfDeath(t *testing.T) {
	origin := Timestamp{Sec: 1, Frac: 1}
	s := goodServer(origin)
	s.stratum, s.li, s.ref = 0, 3, [4]byte{'R', 'A', 'T', 'E'}
	_, err := ParseReply(s.bytes(), origin)
	var kod KissOfDeath
	if !errors.As(err, &kod) || kod.Code != "RATE" {
		t.Fatalf("err=%v, want a RATE kiss-o'-death (checked before the leap alarm)", err)
	}
	if err.Error() != "server sent kiss-o'-death RATE" {
		t.Fatalf("text=%q", err.Error())
	}
	// A hostile code cannot put control bytes on the console.
	s.ref = [4]byte{0x1b, 'D', '\n', 'Y'}
	_, err = ParseReply(s.bytes(), origin)
	if !errors.As(err, &kod) || kod.Code != "DY" {
		t.Fatalf("code=%q, want only the printable bytes", kod.Code)
	}
}

func TestMeasureDelayAndServerClock(t *testing.T) {
	tx := Timestamp{Sec: pinNTP} // server sends at unix 1_789_043_696.000
	rx := Timestamp{Sec: pinNTP - 1, Frac: 0xFF00_0000}
	// The server held the request ~ (1 - 0.99609375) s = 3.90625 ms.
	r := Response{Receive: rx, Transmit: tx}
	est, err := Measure(r, 1_000, 1_000+30_000_000) // 30 ms round trip
	if err != nil {
		t.Fatal(err)
	}
	hold := tx.UnixNanos() - rx.UnixNanos()
	if est.DelayNs != 30_000_000-hold {
		t.Fatalf("delay=%d, want rtt-hold=%d", est.DelayNs, 30_000_000-hold)
	}
	if est.ServerNs != tx.UnixNanos()+est.DelayNs/2 {
		t.Fatalf("server clock=%d, want transmit + delay/2", est.ServerNs)
	}
}

func TestMeasureTreatsAnOddServerClockAsZeroHoldNeverNegativeDelay(t *testing.T) {
	tx := Timestamp{Sec: pinNTP}
	// No receive time: zero hold.
	est, err := Measure(Response{Transmit: tx}, 0, 8_000_000)
	if err != nil || est.DelayNs != 8_000_000 || est.ServerNs != tx.UnixNanos()+4_000_000 {
		t.Fatalf("no receive time: %+v err=%v", est, err)
	}
	// Receive AFTER transmit: zero hold, not a negative one.
	est, _ = Measure(Response{Receive: Timestamp{Sec: pinNTP + 5}, Transmit: tx}, 0, 8_000_000)
	if est.DelayNs != 8_000_000 {
		t.Fatalf("backwards server timestamps: delay=%d", est.DelayNs)
	}
	// A hold longer than the round trip clamps the delay to zero.
	est, _ = Measure(Response{Receive: Timestamp{Sec: pinNTP - 10}, Transmit: tx}, 0, 8_000_000)
	if est.DelayNs != 0 || est.ServerNs != tx.UnixNanos() {
		t.Fatalf("over-long hold: %+v", est)
	}
	// The monotonic clock going backwards is refused, not clamped.
	if _, err := Measure(Response{Transmit: tx}, 100, 99); !errors.Is(err, ErrClockBackup) {
		t.Fatalf("err=%v, want ErrClockBackup", err)
	}
}
