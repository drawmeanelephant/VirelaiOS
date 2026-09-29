package timesync

import (
	"errors"
	"strings"
	"testing"

	"virelai/vi"
)

// fake is a scripted guest: a wall clock, a monotonic clock that steps 10 ms
// per read, and a server that answers with a chosen time.
type fake struct {
	t *testing.T

	wall     int64 // slot 66 reading; negative = -ENOSYS
	setRC    int64
	sets     []int64
	mono     int64
	diag     vi.NetDiagnosis
	resolved map[string][4]byte

	// serverUnix is the server's clock, in whole seconds, when it replies.
	serverUnix int64
	// reply, when set, replaces the normally-built server packet.
	reply    func(req []byte) []byte
	exchErr  error
	exchIP   [4]byte
	exchWait int64
	resolves int
}

func (f *fake) env() Env {
	return Env{
		Nanos: func() int64 { f.mono += 10_000_000; return f.mono },
		Now:   func() int64 { return f.wall },
		SetClock: func(epoch int64) int64 {
			f.sets = append(f.sets, epoch)
			if f.setRC == 0 {
				f.wall = epoch
			}
			return f.setRC
		},
		Resolve: func(name string, budgetNs int64) ([4]byte, error) {
			f.resolves++
			if ip, ok := f.resolved[name]; ok {
				return ip, nil
			}
			return [4]byte{}, errors.New("dns: name not found (NXDOMAIN)")
		},
		Preflight: func(ip [4]byte) vi.NetDiagnosis { return f.diag },
		Exchange: func(ip [4]byte, req []byte, budgetNs int64) ([]byte, error) {
			f.exchIP, f.exchWait = ip, budgetNs
			if f.exchErr != nil {
				return nil, f.exchErr
			}
			if f.reply != nil {
				return f.reply(req), nil
			}
			origin := getTimestamp(req[40:48])
			s := goodServer(origin)
			s.rx = FromUnixSeconds(f.serverUnix)
			s.tx = FromUnixSeconds(f.serverUnix)
			return s.bytes(), nil
		},
		Random: func(p []byte) (int, error) {
			for i := range p {
				p[i] = byte(0xA0 + i)
			}
			return len(p), nil
		},
	}
}

func newFake(t *testing.T) *fake {
	return &fake{t: t, wall: pinUnix, diag: vi.NetReady, serverUnix: pinUnix + 3600}
}

func (f *fake) noClockWrite(res Result) {
	f.t.Helper()
	if len(f.sets) != 0 {
		f.t.Fatalf("the clock was written (%v) on a %v outcome", f.sets, res.Status)
	}
}

func TestSyncStepsADriftedClockAndReportsTheCorrection(t *testing.T) {
	f := newFake(t)
	res := SyncWith(f.env(), "10.0.0.2")
	if res.Status != StatusSet || !res.OK() {
		t.Fatalf("status=%v err=%v", res.Status, res.Err)
	}
	if len(f.sets) != 1 || f.sets[0] != pinUnix+3600 {
		t.Fatalf("clock writes=%v, want exactly one, to %d", f.sets, pinUnix+3600)
	}
	if !res.HadClock || res.Before != pinUnix || res.Correction != 3600 || res.Now != pinUnix+3600 {
		t.Fatalf("result=%+v", res)
	}
	if f.exchIP != [4]byte{10, 0, 0, 2} || f.exchWait != DefaultBudgetNs {
		t.Fatalf("exchange to %v with budget %d", f.exchIP, f.exchWait)
	}
	want := "time sync: synced server=10.0.0.2 stratum=2 delay=10ms correction=+3600s epoch=" +
		"1789047296\n"
	if got := res.Line(); got != want {
		t.Fatalf("line:\n got %q\nwant %q", got, want)
	}
}

func TestSyncStepsBackwardsWithAnExplicitSign(t *testing.T) {
	f := newFake(t)
	f.serverUnix = pinUnix - 30
	res := SyncWith(f.env(), "10.0.0.2")
	if res.Status != StatusSet || res.Correction != -30 {
		t.Fatalf("status=%v correction=%d", res.Status, res.Correction)
	}
	if !strings.Contains(res.Line(), "correction=-30s") {
		t.Fatalf("line=%q", res.Line())
	}
}

func TestSyncGivesANoEpochBootItsFirstClock(t *testing.T) {
	f := newFake(t)
	f.wall = -4 // slot 66: -ENOSYS, no firmware epoch
	res := SyncWith(f.env(), "10.0.0.2")
	if res.Status != StatusSet || res.HadClock {
		t.Fatalf("status=%v hadClock=%v", res.Status, res.HadClock)
	}
	if len(f.sets) != 1 || f.sets[0] != pinUnix+3600 {
		t.Fatalf("clock writes=%v", f.sets)
	}
	want := "time sync: synced server=10.0.0.2 stratum=2 delay=10ms " +
		"correction=none (the clock was unset) epoch=1789047296\n"
	if got := res.Line(); got != want {
		t.Fatalf("line:\n got %q\nwant %q", got, want)
	}
}

func TestSyncLeavesAClockWithinTheTickAlone(t *testing.T) {
	for _, off := range []int64{-1, 0, 1} {
		f := newFake(t)
		f.wall = f.serverUnix - off
		res := SyncWith(f.env(), "10.0.0.2")
		if res.Status != StatusInSync || !res.OK() {
			t.Fatalf("offset %d: status=%v", off, res.Status)
		}
		f.noClockWrite(res)
		if res.Correction != off || res.Now != f.wall {
			t.Fatalf("offset %d: result=%+v", off, res)
		}
		if !strings.Contains(res.Line(), "not applied") {
			t.Fatalf("offset %d: line=%q", off, res.Line())
		}
	}
	// Two seconds is past the tick's phase: that is drift, and it is stepped.
	f := newFake(t)
	f.wall = f.serverUnix - 2
	if res := SyncWith(f.env(), "10.0.0.2"); res.Status != StatusSet {
		t.Fatalf("2 s off: status=%v", res.Status)
	}
}

func TestSyncResolvesANameThroughTheBoundedResolver(t *testing.T) {
	f := newFake(t)
	f.resolved = map[string][4]byte{"time.example": {93, 184, 216, 34}}
	res := SyncWith(f.env(), "time.example")
	if res.Status != StatusSet || f.resolves != 1 || f.exchIP != [4]byte{93, 184, 216, 34} {
		t.Fatalf("status=%v resolves=%d exchange ip=%v", res.Status, f.resolves, f.exchIP)
	}
	// A dotted quad never touches DNS.
	f = newFake(t)
	SyncWith(f.env(), "10.0.0.2")
	if f.resolves != 0 {
		t.Fatalf("a literal address resolved %d times", f.resolves)
	}
}

func TestSyncOfflineWithANameSaysOfflineWithoutTouchingDNS(t *testing.T) {
	f := newFake(t)
	f.diag = vi.NetOfflineNoIP
	f.resolved = map[string][4]byte{DefaultServer: {17, 253, 4, 125}}
	res := SyncWith(f.env(), DefaultServer)
	if res.Status != StatusOffline || f.resolves != 0 {
		t.Fatalf("status=%v resolves=%d, want offline and no DNS query", res.Status, f.resolves)
	}
	want := "time sync: offline — no IP address (set one: net ip <a.b.c.d> or net dhcp)\ntime sync: clock untouched\n"
	if got := res.Line(); got != want {
		t.Fatalf("line = %q, want %q", got, want)
	}
	f.noClockWrite(res)
}

func TestSyncFailuresNameThemselvesAndNeverWriteTheClock(t *testing.T) {
	cases := []struct {
		name   string
		server string
		bend   func(*fake)
		status Status
		line   string
	}{
		{"name does not resolve", "nowhere.example", func(f *fake) {},
			StatusBadName, "time sync: cannot resolve nowhere.example: dns: name not found (NXDOMAIN); clock untouched\n"},
		{"the all-zeros address", "0.0.0.0", func(f *fake) {},
			StatusBadName, "time sync: cannot resolve 0.0.0.0: 0.0.0.0 is not a server address; clock untouched\n"},
		{"offline", "10.0.0.2", func(f *fake) { f.diag = vi.NetOfflineNoIP },
			StatusOffline, "time sync: offline — no IP address (set one: net ip <a.b.c.d> or net dhcp)\ntime sync: clock untouched\n"},
		{"no route", "10.0.0.2", func(f *fake) { f.diag = vi.NetNoRoute },
			StatusNoRoute, "time sync: no route to 10.0.0.2 (resolve first: net arp <a.b.c.d>)\ntime sync: clock untouched\n"},
		{"send refused", "10.0.0.2", func(f *fake) { f.exchErr = ErrSendRefused },
			StatusExchangeFailed, "time sync: exchange with 10.0.0.2 failed (send refused — resolve the peer first, then retry); clock untouched\n"},
		{"receive errno", "10.0.0.2", func(f *fake) { f.exchErr = sysErr(9) },
			StatusExchangeFailed, "time sync: exchange with 10.0.0.2 failed (errno 9); clock untouched\n"},
		{"no reply", "10.0.0.2", func(f *fake) { f.exchErr = ErrNoReply },
			StatusNoReply, "time sync: no reply from 10.0.0.2 within 5s; clock untouched\n"},
		{"kiss of death", "10.0.0.2", func(f *fake) {
			f.reply = func(req []byte) []byte {
				s := goodServer(getTimestamp(req[40:48]))
				s.stratum, s.li, s.ref = 0, 3, [4]byte{'D', 'E', 'N', 'Y'}
				return s.bytes()
			}
		}, StatusBadReply, "time sync: bad reply from 10.0.0.2: server sent kiss-o'-death DENY; clock untouched\n"},
		{"reply to some other query", "10.0.0.2", func(f *fake) {
			f.reply = func(req []byte) []byte { return goodServer(Timestamp{Sec: 1, Frac: 2}).bytes() }
		}, StatusBadReply, "time sync: bad reply from 10.0.0.2: reply does not echo our transmit timestamp; clock untouched\n"},
		{"unsynchronized server", "10.0.0.2", func(f *fake) {
			f.reply = func(req []byte) []byte {
				s := goodServer(getTimestamp(req[40:48]))
				s.li = 3
				return s.bytes()
			}
		}, StatusBadReply, "time sync: bad reply from 10.0.0.2: server is unsynchronized (leap indicator 3); clock untouched\n"},
		{"truncated reply", "10.0.0.2", func(f *fake) {
			f.reply = func(req []byte) []byte { return goodServer(getTimestamp(req[40:48])).bytes()[:20] }
		}, StatusBadReply, "time sync: bad reply from 10.0.0.2: reply shorter than an SNTP packet; clock untouched\n"},
		{"kernel refuses the epoch", "10.0.0.2", func(f *fake) { f.setRC = -int64(vi.ErrEINVAL) },
			StatusKernelRefused, "time sync: kernel refused epoch 1789047296 from 10.0.0.2 (errno 1); clock untouched\n"},
	}
	for _, c := range cases {
		f := newFake(t)
		f.wall = pinUnix
		c.bend(f)
		res := SyncWith(f.env(), c.server)
		if res.Status != c.status || res.OK() {
			t.Errorf("%s: status=%v ok=%v err=%v, want %v", c.name, res.Status, res.OK(), res.Err, c.status)
			continue
		}
		if got := res.Line(); got != c.line {
			t.Errorf("%s: line:\n got %q\nwant %q", c.name, got, c.line)
		}
		if c.status != StatusKernelRefused {
			f.noClockWrite(res)
		}
		if f.wall != pinUnix {
			t.Errorf("%s: the clock moved from %d to %d", c.name, pinUnix, f.wall)
		}
	}
}

func TestSyncUsesTheBudgetOnTheEnvAndDefaultsIt(t *testing.T) {
	f := newFake(t)
	env := f.env()
	env.BudgetNs = 1_500_000_000
	res := SyncWith(env, "10.0.0.2")
	if f.exchWait != 1_500_000_000 || res.BudgetNs != 1_500_000_000 {
		t.Fatalf("budget %d / %d", f.exchWait, res.BudgetNs)
	}
	f = newFake(t)
	f.exchErr = ErrNoReply
	if res := SyncWith(f.env(), "10.0.0.2"); res.BudgetNs != DefaultBudgetNs {
		t.Fatalf("default budget=%d", res.BudgetNs)
	}
}

func TestTransmitStampCarriesTheWallSecondsAndRandomLowBitsAndIsNeverZero(t *testing.T) {
	f := newFake(t)
	ts := transmitStamp(f.env())
	if ts.Sec != uint32(pinNTP) || ts.Frac != 0xA4A5A6A7 {
		t.Fatalf("stamp=%+v, want wall seconds + random fraction", ts)
	}
	f.wall = -4 // no clock: the random seconds stand
	ts = transmitStamp(f.env())
	if ts.Sec != 0xA0A1A2A3 {
		t.Fatalf("no-clock stamp=%+v", ts)
	}
	env := f.env()
	env.Random = func(p []byte) (int, error) { return 0, errors.New("no entropy") }
	if ts := transmitStamp(env); ts.IsZero() {
		t.Fatal("an entropy failure produced the all-zero (unknown) timestamp")
	}
}
