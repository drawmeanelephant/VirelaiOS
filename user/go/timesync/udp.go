package timesync

import (
	"errors"

	"virelai/vi"
	"virelai/vsys"
)

// SNTP is UDP, so it cannot ride vi.Dial (kernel TCP, slots 30-33). It uses
// the same UDP seam DNS does (slots 9/10/11), with the same bounds dns.go
// documents: slot 10 always sends from the fixed source port, so the client
// must be listening on it BEFORE the query goes out; slot 10 does not resolve
// ARP, so an unresolved peer is a refusal the caller reports, never a retry
// hidden here; and a 48-byte packet plus the 8-byte header slot 11 prepends
// fits the 72-byte datagram bound.

var (
	// ErrSendRefused: the transport refused the send (EINVAL — the seam's
	// .no_peer/.not_ready mapping, usually the server missing from ARP).
	ErrSendRefused = errors.New("send refused — resolve the peer first, then retry")
	// ErrNoReply: nothing matching arrived inside the budget.
	ErrNoReply = errors.New("no reply within the budget")
)

// sysErr is a kernel errno from the UDP seam that has no sentence of its own.
type sysErr int64

func (e sysErr) Error() string { return "errno " + vsys.Itoa64(int64(e)) }

const (
	// spinPolls is how many empty polls yield the CPU instead of parking. A
	// LAN reply lands in milliseconds and a park is a whole scheduler tick
	// (about a second), which would land in the delay estimate.
	spinPolls = 64
	// maxPolls is a belt-and-braces bound: even with a clock that never
	// advances, the wait terminates (the vi.Conn.Recv precedent).
	maxPolls = 3600
	udpHdr   = 8
)

// exchangeUDP sends one request datagram to ip:Port and returns the payload of
// the first datagram from source port 123 that echoes the request's transmit
// timestamp. Anything else arriving on the shared port (a stray DNS reply, a
// late answer to an earlier query) is consumed and the wait continues. The
// wait is bounded by budgetNs on the monotonic clock: an unbounded wait is not
// expressible.
func exchangeUDP(ip [4]byte, req []byte, budgetNs int64) ([]byte, error) {
	if len(req) != PacketLen {
		return nil, ErrShort
	}
	// EINVAL means the port is already bound (there is no unlisten, and a
	// prior DNS lookup in this process binds the same port): that is fine.
	_ = vi.UDPListen(vi.UDPSourcePort)
	if rc := vi.UDPSend(ip, Port, req); rc < 0 {
		if -rc == vi.ErrEINVAL {
			return nil, ErrSendRefused
		}
		return nil, sysErr(-rc)
	}
	deadline := vi.Nanos() + budgetNs
	buf := make([]byte, vi.UDPDatagramMax)
	for polls := 0; ; polls++ {
		n := vi.UDPRecv(vi.UDPSourcePort, buf)
		if n < 0 {
			return nil, sysErr(-n)
		}
		if n > 0 {
			if p, ok := matchReply(buf[:n], req); ok {
				return p, nil
			}
		}
		if vi.Nanos() >= deadline || polls >= maxPolls {
			return nil, ErrNoReply
		}
		if n > 0 {
			continue // a stray was consumed; there may be more queued
		}
		if polls < spinPolls {
			vi.Yield()
		} else {
			vi.Sleep(1)
		}
	}
}

// matchReply extracts the SNTP payload from a datagram (8-byte UDP header +
// payload) when it comes from the NTP port and echoes req's transmit
// timestamp in its originate field. The check is byte equality on the echoed
// field; ParseReply validates the rest.
func matchReply(dgram, req []byte) ([]byte, bool) {
	if len(dgram) < udpHdr+PacketLen {
		return nil, false
	}
	if uint16(dgram[0])<<8|uint16(dgram[1]) != Port {
		return nil, false
	}
	p := dgram[udpHdr:]
	for i := 0; i < 8; i++ {
		if p[24+i] != req[40+i] {
			return nil, false
		}
	}
	out := make([]byte, len(p))
	copy(out, p)
	return out, true
}
