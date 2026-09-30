// Package timesync is a one-shot SNTP client (RFC 4330 style) for GOSH's
// `time sync`: one query, one bounded wait, no periodic loop, no daemon.
//
// This file is the pure half — packet encode/decode, the NTP era rule and the
// delay arithmetic — so it is pinned by host tests with no kernel. The impure
// half (resolve, preflight, the UDP exchange, the clock write) is sync.go and
// udp.go, reached through an Env so every failure path is testable too.
//
// Deliberate non-goals: NTP association state, clock discipline or slewing,
// authentication extension fields, and any timezone. The guest clock is a
// whole-second, 1 Hz-tick clock, so the correction is a whole-second step.
package timesync

import "errors"

const (
	// Port is the well-known NTP/SNTP server port.
	Port = 123
	// PacketLen is the fixed SNTP packet size (no extension fields).
	PacketLen = 48

	// ntpUnixOffset is the seconds from 1900-01-01 to 1970-01-01.
	ntpUnixOffset int64 = 2_208_988_800
	// eraSeconds is one 32-bit NTP era.
	eraSeconds int64 = 1 << 32

	versionNumber = 4
	modeClient    = 3
	modeServer    = 4
	leapAlarm     = 3
	maxStratum    = 15
)

// Reply-validation failures. They are protocol conditions, not kernel errnos.
var (
	ErrShort       = errors.New("reply shorter than an SNTP packet")
	ErrNotServer   = errors.New("not a server-mode reply")
	ErrVersion     = errors.New("unsupported NTP version")
	ErrNotOurs     = errors.New("reply does not echo our transmit timestamp")
	ErrStratum     = errors.New("invalid stratum")
	ErrUnsynced    = errors.New("server is unsynchronized (leap indicator 3)")
	ErrNoTime      = errors.New("reply carries no transmit time")
	ErrClockBackup = errors.New("monotonic clock ran backwards during the exchange")
)

// KissOfDeath is a stratum-0 reply: the server refuses to serve us and names
// why in the reference-ID field (RFC 4330 §8), e.g. RATE or DENY.
type KissOfDeath struct{ Code string }

func (k KissOfDeath) Error() string { return "server sent kiss-o'-death " + k.Code }

// Timestamp is a 64-bit NTP timestamp: seconds and 2^-32 s fractions since
// 1900-01-01, with the seconds field wrapping every 2^32 s (an "era").
type Timestamp struct{ Sec, Frac uint32 }

// IsZero reports the all-zero timestamp, which SNTP uses for "unknown".
func (t Timestamp) IsZero() bool { return t.Sec == 0 && t.Frac == 0 }

// UnixNanos converts to nanoseconds since the Unix epoch, resolving the era by
// RFC 4330's rule: a set most-significant bit is era 0 (1968..2036), a clear
// one is era 1 (2036..2104). The wire format cannot say more.
func (t Timestamp) UnixNanos() int64 {
	sec := int64(t.Sec)
	if t.Sec&0x8000_0000 == 0 {
		sec += eraSeconds
	}
	sec -= ntpUnixOffset
	frac := int64(uint64(t.Frac) * 1_000_000_000 >> 32)
	return sec*1_000_000_000 + frac
}

// FromUnixSeconds is the whole-second NTP timestamp for a Unix time. The
// seconds field truncates to 32 bits, as the wire format does.
func FromUnixSeconds(sec int64) Timestamp {
	return Timestamp{Sec: uint32(sec + ntpUnixOffset)}
}

func putTimestamp(b []byte, t Timestamp) {
	put32(b[0:4], t.Sec)
	put32(b[4:8], t.Frac)
}

func getTimestamp(b []byte) Timestamp {
	return Timestamp{Sec: get32(b[0:4]), Frac: get32(b[4:8])}
}

func put32(b []byte, v uint32) {
	b[0], b[1], b[2], b[3] = byte(v>>24), byte(v>>16), byte(v>>8), byte(v)
}

func get32(b []byte) uint32 {
	return uint32(b[0])<<24 | uint32(b[1])<<16 | uint32(b[2])<<8 | uint32(b[3])
}

// BuildRequest encodes a client request: LI 0, version 4, mode 3, and the
// given transmit timestamp. The server echoes that timestamp in the
// originate field, which is what ties a reply to this query. Every other
// field is zero, as RFC 4330 §5 asks of an SNTP client.
func BuildRequest(transmit Timestamp) [PacketLen]byte {
	var p [PacketLen]byte
	p[0] = 0<<6 | versionNumber<<3 | modeClient
	putTimestamp(p[40:48], transmit)
	return p
}

// Response is the part of a validated server reply the client uses.
type Response struct {
	Version uint8
	Stratum uint8
	RefID   [4]byte
	Receive Timestamp
	// Transmit is the server's clock when it sent the reply.
	Transmit Timestamp
}

// ParseReply validates one server packet against RFC 4330 §5 and the query it
// answers. origin is the transmit timestamp the request carried. Stratum 0 is
// checked before the leap indicator because a kiss-o'-death normally sets
// both, and the code is the more useful thing to report.
func ParseReply(p []byte, origin Timestamp) (Response, error) {
	var r Response
	if len(p) < PacketLen {
		return r, ErrShort
	}
	li, vn, mode := p[0]>>6, (p[0]>>3)&7, p[0]&7
	if mode != modeServer {
		return r, ErrNotServer
	}
	if vn < 3 || vn > versionNumber {
		return r, ErrVersion
	}
	if getTimestamp(p[24:32]) != origin {
		return r, ErrNotOurs
	}
	r.Version, r.Stratum = vn, p[1]
	copy(r.RefID[:], p[12:16])
	if r.Stratum == 0 {
		return r, KissOfDeath{Code: kissCode(r.RefID)}
	}
	if r.Stratum > maxStratum {
		return r, ErrStratum
	}
	if li == leapAlarm {
		return r, ErrUnsynced
	}
	r.Receive = getTimestamp(p[32:40])
	r.Transmit = getTimestamp(p[40:48])
	if r.Transmit.IsZero() {
		return r, ErrNoTime
	}
	return r, nil
}

// kissCode renders a reference ID as its ASCII code, keeping only printable
// bytes so a hostile server cannot put control characters on the console.
func kissCode(id [4]byte) string {
	out := make([]byte, 0, 4)
	for _, c := range id {
		if c >= 0x20 && c < 0x7f {
			out = append(out, c)
		}
	}
	return string(out)
}

// Estimate is what one exchange says about the server's clock.
type Estimate struct {
	// ServerNs is the server's clock, in Unix nanoseconds, at the instant the
	// reply arrived: its transmit time plus half the network delay.
	ServerNs int64
	// DelayNs is the round trip minus the time the server held the request.
	DelayNs int64
}

// Measure combines a reply with the client's MONOTONIC send and receive
// instants (Nanos). The client's wall clock is whole seconds only, so the
// offset is not computed from it here: the caller compares ServerNs with its
// own reading. Only durations cross clocks — the round trip is timed on the
// monotonic clock and the server's hold time on the server's own timestamps —
// so a wrong wall clock cannot bias the delay. A server whose receive time is
// missing or after its transmit time is treated as having held the request
// for zero time; a delay that would go negative is clamped to zero.
func Measure(r Response, sentMono, recvMono int64) (Estimate, error) {
	rtt := recvMono - sentMono
	if rtt < 0 {
		return Estimate{}, ErrClockBackup
	}
	tx := r.Transmit.UnixNanos()
	var hold int64
	if !r.Receive.IsZero() {
		if rx := r.Receive.UnixNanos(); tx > rx {
			hold = tx - rx
		}
	}
	delay := rtt - hold
	if delay < 0 {
		delay = 0
	}
	return Estimate{ServerNs: tx + delay/2, DelayNs: delay}, nil
}
