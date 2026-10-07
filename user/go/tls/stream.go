// The TCP → TLS stream adapter (the Go mirror of user/src/lib/tls/stream.zig).
//
// The kernel TCP seam (slots 30–33) is a single connection with no reassembly
// and a one-slot RX buffer of tcpChunkMax (192) bytes. A TLS record is
// routinely much larger, so this adapter:
//
//   - accumulates reads: recv returns at most 192 bytes, so read loops until
//     it can satisfy the caller;
//   - treats 0 as "nothing yet", not EOF — a zero poll is retried until the
//     idle bound (host tests) or the recv budget (guest: drain via tcp_recv,
//     then park one scheduler tick);
//   - paces writes: each ≤192-byte send is followed by a drain so arriving
//     RX bytes are stashed rather than dropped on the full one-slot buffer
//     (kernel/src/tcp.zig: "net tcp recv first");
//   - fails closed: overflow or a dead seam is an error, never a truncate.
//
// Slot 76 (sys_sock_ready) does not drain virtio. vi.Conn.Recv probes it
// first, so a Recv that never calls tcp_recv will sit until the 30 s budget
// while ServerHello bytes wait in the device ring. That was observed on
// go-fetch-https run 01 (handshake error ETIMEDOUT; relay had already
// forwarded 1428 bytes). This adapter calls tcp_recv (which drains) and
// uses slot 76 only to tell a peer FIN from an empty poll.
//
// The seam is injected so the chunking is host-testable without a NIC.

package tls

import "virelai/vi"

const (
	tcpChunkMax    = 192
	streamStashCap = 4096
	streamMaxPolls = 3600
	streamRecvNs   = vi.DefaultRecvBudgetNs
)

// tcpStream is the bounded accumulator. recv/send are the injected seam
// (kernel slots on the guest; a fake in host tests).
type tcpStream struct {
	recv    func([]byte) (int, error)
	send    func([]byte) (int, error)
	peekEOF func() bool
	buf     []byte
	n       int
	// idleLimit > 0: busy-poll this many empty recvs (host tests). 0: guest
	// wait — drain, then Sleep(1) until the budget or a FIN.
	idleLimit  int
	deadlineAt int64        // absolute caller deadline; zero selects legacy bounds
	now        func() int64 // injected clock in host tests; nil uses vi.Nanos
}

func (s *tcpStream) nanos() int64 {
	if s.now != nil {
		return s.now()
	}
	return vi.Nanos()
}

func (s *tcpStream) checkDeadline() error {
	if s.deadlineAt > 0 && s.nanos() >= s.deadlineAt {
		return errStreamTimeout
	}
	return nil
}

func (s *tcpStream) read(out []byte) (int, error) {
	got := 0
	for got < len(out) {
		if err := s.checkDeadline(); err != nil {
			return got, err
		}
		if s.n == 0 {
			if err := s.pumpOnce(); err != nil {
				// A short read is success when some bytes were already
				// delivered (stream.zig: pumpOnce failure after got>0
				// returns the prefix, not the error).
				if got > 0 && !(s.deadlineAt > 0 && err == errStreamTimeout) {
					return got, nil
				}
				return 0, err
			}
		}
		take := s.n
		if take > len(out)-got {
			take = len(out) - got
		}
		copy(out[got:got+take], s.buf[:take])
		copy(s.buf[:], s.buf[take:s.n])
		s.n -= take
		got += take
	}
	return got, nil
}

func (s *tcpStream) write(data []byte) (int, error) {
	off := 0
	for off < len(data) {
		if err := s.checkDeadline(); err != nil {
			return off, err
		}
		take := len(data) - off
		if take > tcpChunkMax {
			take = tcpChunkMax
		}
		n, err := s.send(data[off : off+take])
		if err != nil || n <= 0 {
			if off > 0 {
				return off, err
			}
			if err == nil {
				return 0, errTransport
			}
			return 0, err
		}
		off += n
		if err := s.drainOnce(); err != nil {
			return off, err
		}
	}
	return off, nil
}

func (s *tcpStream) pumpOnce() error {
	var chunk [tcpChunkMax]byte
	idle := 0
	deadline := s.nanos() + streamRecvNs
	if s.deadlineAt > 0 && s.deadlineAt < deadline {
		deadline = s.deadlineAt
	}
	for polls := 0; ; polls++ {
		if s.nanos() >= deadline {
			return errStreamTimeout
		}
		n, err := s.recv(chunk[:])
		if err != nil {
			return err
		}
		if n > 0 {
			if n > tcpChunkMax {
				return errStreamOverflow
			}
			if s.n+n > len(s.buf) {
				return errStreamOverflow
			}
			copy(s.buf[s.n:], chunk[:n])
			s.n += n
			return nil
		}
		if s.idleLimit > 0 {
			idle++
			if idle >= s.idleLimit {
				return errStreamClosed
			}
			continue
		}
		if s.peekEOF != nil && s.peekEOF() {
			return errStreamClosed
		}
		if s.nanos() >= deadline || polls >= streamMaxPolls {
			return errStreamTimeout
		}
		if s.deadlineAt > 0 {
			// A scheduler tick is much coarser than the browser's stall
			// deadline. Yield briefly so ACK-paced records can be drained
			// without turning every empty poll into a whole-tick delay.
			// Two milliseconds also keeps the unchanged 3600-poll guard
			// from expiring ahead of a five-second caller deadline.
			until := s.nanos() + 2_000_000
			if deadline < until {
				until = deadline
			}
			for yields := 0; yields < 1024 && s.nanos() < until; yields++ {
				vi.Yield()
			}
		} else {
			vi.Sleep(1)
		}
	}
}

func (s *tcpStream) drainOnce() error {
	if err := s.checkDeadline(); err != nil {
		return err
	}
	var chunk [tcpChunkMax]byte
	n, err := s.recv(chunk[:])
	if err != nil {
		return err
	}
	if n == 0 {
		return nil
	}
	if n > tcpChunkMax {
		return errStreamOverflow
	}
	if s.n+n > len(s.buf) {
		return errStreamOverflow
	}
	copy(s.buf[s.n:], chunk[:n])
	s.n += n
	return nil
}
