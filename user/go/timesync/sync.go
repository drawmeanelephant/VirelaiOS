package timesync

import (
	"errors"

	"virelai/vi"
	"virelai/vsys"
)

const (
	// DefaultServer is what `time sync` asks when no server is named.
	DefaultServer = "time.apple.com"
	// DefaultBudgetNs bounds the whole wait for a reply, and the DNS lookup in
	// front of it. One query, one wait; there is no retry and no loop.
	DefaultBudgetNs = 5_000_000_000

	// tickDeadbandSecs is the correction, in whole seconds, at or under which
	// the clock is left alone. The kernel clock advances on a 1 Hz tick with an
	// unknown phase, so a reading can be a second behind a perfect clock; a
	// "correction" of one second is that phase, not drift, and applying it
	// would make repeated syncs jitter.
	tickDeadbandSecs = 1
)

// Status is how one sync attempt ended. Only StatusSet ever wrote the clock;
// every failure leaves it exactly as it was.
type Status int

const (
	// StatusSet: a valid reply arrived and the clock was stepped.
	StatusSet Status = iota
	// StatusInSync: a valid reply arrived and the clock was already within the
	// one-second tick; nothing was written.
	StatusInSync
	// StatusBadName: the server name did not resolve.
	StatusBadName
	// StatusOffline: no own IP is configured.
	StatusOffline
	// StatusNoRoute: an own IP exists but the server is not ARP-resolved.
	StatusNoRoute
	// StatusExchangeFailed: the transport refused the query, or the receive
	// side failed with an errno.
	StatusExchangeFailed
	// StatusNoReply: nothing valid arrived inside the budget.
	StatusNoReply
	// StatusBadReply: a reply arrived and failed validation.
	StatusBadReply
	// StatusKernelRefused: the kernel refused the epoch (outside its range).
	StatusKernelRefused
)

// Result is one sync attempt, complete enough to print without re-deriving.
type Result struct {
	Status  Status
	Server  string
	IP      [4]byte
	Stratum uint8
	DelayNs int64
	// HadClock is false on a boot with no firmware epoch (slot 66 -ENOSYS).
	HadClock bool
	// Before is the wall clock in whole seconds when the reply arrived; only
	// meaningful when HadClock.
	Before int64
	// Epoch is the server's time, in whole seconds, that the clock was set to
	// (or would have been, for a refusal).
	Epoch int64
	// Correction is Epoch - Before; only meaningful when HadClock.
	Correction int64
	// Now is the clock read back after the step (or the unchanged reading).
	Now      int64
	BudgetNs int64
	Err      error
}

// OK reports a sync that left the clock correct: stepped, or already in sync.
func (r Result) OK() bool { return r.Status == StatusSet || r.Status == StatusInSync }

// Env carries every side effect, so tests can drive each outcome. DefaultEnv
// is the guest wiring.
type Env struct {
	// Nanos is the monotonic clock.
	Nanos func() int64
	// Now is wall-clock Unix seconds, or a negative errno (-ENOSYS = no epoch).
	Now func() int64
	// SetClock steps the wall clock and returns 0 or a negative errno.
	SetClock func(epoch int64) int64
	// Resolve maps a name to an IPv4 address.
	Resolve func(name string, budgetNs int64) ([4]byte, error)
	// Preflight classifies the route to ip (the N13/N14 shape).
	Preflight func(ip [4]byte) vi.NetDiagnosis
	// Exchange sends req to ip and returns the matching reply payload.
	Exchange func(ip [4]byte, req []byte, budgetNs int64) ([]byte, error)
	// Random fills p with entropy for the request's transmit timestamp.
	Random func(p []byte) (int, error)
	// BudgetNs bounds the wait; <= 0 means DefaultBudgetNs.
	BudgetNs int64
}

// DefaultEnv wires SyncWith to the guest: the vi UDP seam, vi DNS, the slot-66
// clock and the slot-78 write.
func DefaultEnv() Env {
	return Env{
		Nanos:    vi.Nanos,
		Now:      vi.Now,
		SetClock: vi.TimeSet,
		Resolve: func(name string, budgetNs int64) ([4]byte, error) {
			return vi.ResolveDNS(name, vi.DefaultDNSServer, budgetNs)
		},
		Preflight: func(ip [4]byte) vi.NetDiagnosis {
			d, _ := vi.NetPreflight(ip)
			return d
		},
		Exchange: exchangeUDP,
		Random:   vi.Random,
		BudgetNs: DefaultBudgetNs,
	}
}

// Sync runs one SNTP exchange against server on the guest.
func Sync(server string) Result { return SyncWith(DefaultEnv(), server) }

// SyncWith runs one SNTP exchange: resolve, preflight, one query, one bounded
// wait, validate, and — only then — step the clock by the whole-second
// difference. It never retries and never loops. Every failure returns before
// SetClock is reached.
func SyncWith(env Env, server string) Result {
	budget := env.BudgetNs
	if budget <= 0 {
		budget = DefaultBudgetNs
	}
	res := Result{Server: server, BudgetNs: budget}

	ip, err := vsys.ParseIPv4(server)
	if err != nil {
		// A name needs the resolver, and with no own IP its send is refused
		// with a message about ARP. The plain answer is that we are offline.
		if env.Preflight([4]byte{}) == vi.NetOfflineNoIP {
			res.Status = StatusOffline
			return res
		}
		if ip, err = env.Resolve(server, budget); err != nil {
			res.Status, res.Err = StatusBadName, err
			return res
		}
	}
	if ip == ([4]byte{}) {
		res.Status, res.Err = StatusBadName, errors.New("0.0.0.0 is not a server address")
		return res
	}
	res.IP = ip

	// NetUnknown (a refused snapshot) proceeds: the send reports honestly.
	switch env.Preflight(ip) {
	case vi.NetOfflineNoIP:
		res.Status = StatusOffline
		return res
	case vi.NetNoRoute:
		res.Status = StatusNoRoute
		return res
	}

	tx := transmitStamp(env)
	req := BuildRequest(tx)
	sent := env.Nanos()
	reply, err := env.Exchange(ip, req[:], budget)
	recv := env.Nanos()
	// Read the wall clock at the same instant as the receive stamp: its
	// whole-second phase is then as close to the estimate as it can be.
	wall := env.Now()
	if err != nil {
		switch {
		case errors.Is(err, ErrNoReply):
			res.Status = StatusNoReply
		default:
			res.Status = StatusExchangeFailed
		}
		res.Err = err
		return res
	}

	resp, err := ParseReply(reply, tx)
	if err != nil {
		res.Status, res.Err = StatusBadReply, err
		return res
	}
	res.Stratum = resp.Stratum
	est, err := Measure(resp, sent, recv)
	if err != nil {
		res.Status, res.Err = StatusBadReply, err
		return res
	}
	res.DelayNs = est.DelayNs

	// The server's clock now: its estimate at the receive instant plus what
	// elapsed since, floored to the clock's whole second.
	res.Epoch = (est.ServerNs + (env.Nanos() - recv)) / 1_000_000_000

	res.HadClock = wall > 0
	if res.HadClock {
		res.Before = wall
		res.Correction = res.Epoch - wall
		if res.Correction >= -tickDeadbandSecs && res.Correction <= tickDeadbandSecs {
			res.Status, res.Now = StatusInSync, wall
			return res
		}
	}
	if rc := env.SetClock(res.Epoch); rc < 0 {
		res.Status, res.Err = StatusKernelRefused, sysErr(-rc)
		return res
	}
	res.Status, res.Now = StatusSet, env.Now()
	return res
}

// transmitStamp is the request's transmit timestamp: the wall-clock seconds
// when there is a clock, and random low-order bits either way, so a reply can
// only be an answer to this query (RFC 4330 §5). Never all-zero.
func transmitStamp(env Env) Timestamp {
	var rnd [8]byte
	if env.Random != nil {
		_, _ = env.Random(rnd[:])
	}
	ts := Timestamp{Sec: get32(rnd[0:4]), Frac: get32(rnd[4:8])}
	if now := env.Now(); now > 0 {
		ts.Sec = FromUnixSeconds(now).Sec
	}
	if ts.IsZero() {
		ts.Frac = 1
	}
	return ts
}

// Line renders the outcome as the console text `time sync` prints: one line,
// and every failure names that the clock was left untouched.
func (r Result) Line() string {
	ip := vi.FormatIPv4(r.IP)
	switch r.Status {
	case StatusSet:
		return "time sync: synced server=" + ip + " stratum=" + vsys.Itoa64(int64(r.Stratum)) +
			" delay=" + ms(r.DelayNs) + " correction=" + r.correctionText() +
			" epoch=" + vsys.Itoa64(r.Now) + "\n"
	case StatusInSync:
		return "time sync: in sync server=" + ip + " stratum=" + vsys.Itoa64(int64(r.Stratum)) +
			" delay=" + ms(r.DelayNs) + " correction=" + signed(r.Correction) +
			"s (within the 1 s clock tick; not applied) epoch=" + vsys.Itoa64(r.Now) + "\n"
	case StatusBadName:
		return "time sync: cannot resolve " + r.Server + ": " + errText(r.Err) + "; clock untouched\n"
	case StatusOffline:
		return vi.NetDiagnosisMessage("time sync", vi.NetOfflineNoIP, r.IP) + "time sync: clock untouched\n"
	case StatusNoRoute:
		return vi.NetDiagnosisMessage("time sync", vi.NetNoRoute, r.IP) + "time sync: clock untouched\n"
	case StatusExchangeFailed:
		return "time sync: exchange with " + ip + " failed (" + errText(r.Err) + "); clock untouched\n"
	case StatusNoReply:
		return "time sync: no reply from " + ip + " within " + vsys.Itoa64(r.BudgetNs/1_000_000_000) +
			"s; clock untouched\n"
	case StatusBadReply:
		return "time sync: bad reply from " + ip + ": " + errText(r.Err) + "; clock untouched\n"
	case StatusKernelRefused:
		return "time sync: kernel refused epoch " + vsys.Itoa64(r.Epoch) + " from " + ip +
			" (" + errText(r.Err) + "); clock untouched\n"
	}
	return "time sync: unknown outcome; clock untouched\n"
}

func (r Result) correctionText() string {
	if !r.HadClock {
		return "none (the clock was unset)"
	}
	return signed(r.Correction) + "s"
}

func errText(err error) string {
	if err == nil {
		return "unknown error"
	}
	return err.Error()
}

// signed renders an explicit-sign decimal: +3600, -30, +0.
func signed(v int64) string {
	if v < 0 {
		return vsys.Itoa64(v)
	}
	return "+" + vsys.Itoa64(v)
}

// ms renders nanoseconds as whole milliseconds with the unit.
func ms(ns int64) string { return vsys.Itoa64(ns/1_000_000) + "ms" }
