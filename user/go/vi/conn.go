package vi

import (
	"sync/atomic"
	"unsafe"

	"virelai/vsys"
)

// The M67a/M70g socket surface (#1446, #1491): dial/send/recv over the
// kernel's TCP slots 30-33, with DNS name resolution in front of the dial
// (dns.go), plus Listen over the same slot-30 passive-open (ip==0) that
// httpd.zig already uses. Zero kernel work — every call lands on an
// existing ADR 0007 row; the kernel keeps its ONE-TCP-socket-per-process
// law (kernel/src/tcp.zig is a process-wide singleton keyed by
// tcp.owner_pid) and this type mirrors it so a second live Dial or Listen
// fails in userland before the kernel has to.
//
// Blocking semantics: Dial's connect waits IN the kernel (slot 30 parks the
// caller until ESTABLISHED or the kernel's 30 s connect timeout); Recv is
// blocking-with-poll — probe the readiness mask (slot 76), take the bytes
// when readable, otherwise check the deadline and PARK one scheduler tick
// (slot 4) before re-probing. A parked Recv therefore never starves its
// peers: the M65 runtime's other goroutines keep running while this task is
// parked, which is the pacing the go-net gate pins with its heartbeat.
// Every wait is bounded (the deadline or DefaultRecvBudgetNs): a peer that
// goes dark fails closed instead of parking forever.

const (
	// TCPPayloadMax is tcp.payload_max (kernel/src/tcp.zig): the largest
	// single segment the seam accepts. Send chunks at this size.
	TCPPayloadMax = 1460
	// DefaultRecvBudgetNs is the bounded-wait ceiling used when no recv
	// deadline is set. An unbounded wait is deliberately not expressible.
	DefaultRecvBudgetNs = 30_000_000_000
	// maxRecvPolls is a belt-and-braces iteration bound: even with a clock
	// that never advances, the recv loop still terminates.
	maxRecvPolls = 3600
)

// Connection-domain failures (the errno rows carry the kernel's codes; the
// sentinels carry this type's own contract).
var (
	// ErrConnBusy: a second live Dial. One TCP socket per process — Close
	// the current Conn before dialing again.
	ErrConnBusy = errString("vi: one TCP socket per process — Close before Dial")
	// ErrConnClosed: the Conn was closed (or is nil) and cannot carry traffic.
	ErrConnClosed = errString("vi: connection closed")
	// ErrPeerGone: the peer's FIN/RST was observed. Fail closed, never spin.
	ErrPeerGone = errString("vi: peer closed the connection")
)

// ErrServerDial: a dial to the all-zeros address. The kernel treats
// ip == 0 as PASSIVE open (listen mode, kernel/src/syscall.zig
// handle_tcp_connect). Dial refuses the address in userland so a
// hostile DNS reply resolving to 0.0.0.0 cannot accidentally open a
// listener; Listen is the inbound path (M70g G1, #1491).
var ErrServerDial = errString("vi: 0.0.0.0 is the passive-open address — Dial refuses it; use Listen")

// errString is a constant error carried as a string (the mirror of vsys's
// sentinel style; the errno type covers kernel codes, these cover the
// userland contract).
type errString string

func (e errString) Error() string { return string(e) }

// slotHeld is the one-socket bound's enforcement point. The claim is a
// CAS on an atomic bool, NOT a sync.Mutex: a racing Dial must never slip
// a second socket past the check (programs run heartbeat goroutines, so
// Dial-vs-Dial is a live race), and the sync package costs ~3 KiB of text
// — enough to push the gate fixtures out of the kernel's fixed text gap.
// Dial claims before touching the seam (a racing Dial's CAS fails and it
// sees the busy bound) and releases on every failure path; Close releases
// only after the kernel confirms (a refusal leaves the socket live
// in-kernel, so the slot must stay claimed). Identity needs no second
// variable: only the successful Close of the OWNING Conn sets its open
// flag false before releasing, and a released Conn cannot re-release.
var slotHeld atomic.Bool

// Conn is one outbound TCP connection. Create it with Dial; it is the
// process's only live socket until Close. Concurrent Dial and Close calls
// are safe (the slotHeld CAS arbitrates); a single Conn's Recv/Send/Close
// are not — one goroutine owns the traffic, the same contract vsys.Conn
// states.
type Conn struct {
	ip        [4]byte
	port      uint16
	open      bool
	listening bool
	peerGone  bool
	// deadlineAt is an ABSOLUTE monotonic deadline in Nanos() nanoseconds
	// for Recv (0 = unset — DefaultRecvBudgetNs applies). Absolute, so a
	// budget shorter than one scheduler tick is not rounded up to a whole
	// tick and a late tick cannot stretch it.
	deadlineAt int64
}

// Dial connects to host:port. host may be a dotted-quad IPv4 literal
// (dialed directly) or a name (resolved via ResolveDNS against
// DefaultDNSServer first). The kernel's connect blocks until the handshake
// lands or its 30 s timeout expires; a refusal maps to the kernel's errno.
// The argument checks and the resolution are pure userland work and run
// before the claim; the CAS guards only the seam, so a racing Dial can
// never slip a second connect past it.
func Dial(host string, port uint16) (*Conn, error) {
	if port == 0 {
		return nil, errno(ErrEINVAL)
	}
	ip, perr := vsys.ParseIPv4(host)
	if perr != nil {
		resolved, err := ResolveDNS(host, DefaultDNSServer, 0)
		if err != nil {
			return nil, err
		}
		ip = resolved
	}
	if ip == ([4]byte{}) {
		return nil, ErrServerDial
	}
	if !slotHeld.CompareAndSwap(false, true) {
		return nil, ErrConnBusy
	}
	rc := svc2(SlotTCPConnect, ipv4Word(ip), uintptr(port))
	if rc < 0 {
		slotHeld.Store(false)
		return nil, errno(-rc)
	}
	return &Conn{ip: ip, port: port, open: true}, nil
}

// Listen passively opens port (slot 30, ip==0). The kernel's TCP is a
// single-connection machine: one listen_port, one handshake, one RX
// slot. Accept waits until that handshake reaches ESTABLISHED. Close
// releases the process-wide socket so the next Listen or Dial can run.
func Listen(port uint16) (*Conn, error) {
	if port == 0 {
		return nil, errno(ErrEINVAL)
	}
	if !slotHeld.CompareAndSwap(false, true) {
		return nil, ErrConnBusy
	}
	rc := svc2(SlotTCPConnect, 0, uintptr(port))
	if rc < 0 {
		slotHeld.Store(false)
		return nil, errno(-rc)
	}
	return &Conn{port: port, open: true, listening: true}, nil
}

// Accept waits until the listened socket is ESTABLISHED. Bit 1 of the
// readiness mask is writable-when-established; bit 0 is readable (the
// peer's first segment may land before we observe writable). A bounded
// wait fails closed rather than parking forever.
func (c *Conn) Accept() error {
	if c == nil || !c.open {
		return ErrConnClosed
	}
	if !c.listening {
		return nil
	}
	deadline := Nanos() + DefaultRecvBudgetNs
	for polls := 0; ; polls++ {
		mask, rc := TCPReady()
		if rc < 0 {
			if -rc == ErrEAGAIN {
				return ErrConnClosed
			}
			return errno(-rc)
		}
		// Established (writable) or the peer already sent bytes/FIN.
		if mask&3 != 0 {
			c.listening = false
			return nil
		}
		if Nanos() >= deadline || polls >= maxRecvPolls {
			return errno(ErrETIMEDOUT)
		}
		svc1(SlotSleep, 1)
	}
}

// IP returns the peer's address (the resolved one when Dial was given a
// name). Port returns the peer's port.
func (c *Conn) IP() [4]byte {
	if c == nil {
		return [4]byte{}
	}
	return c.ip
}

// Port returns the peer's port.
func (c *Conn) Port() uint16 {
	if c == nil {
		return 0
	}
	return c.port
}

// SetRecvDeadline bounds a blocking Recv by a monotonic budget in
// nanoseconds, measured on Nanos. ns <= 0 clears the deadline and restores
// DefaultRecvBudgetNs. A bounded recv is what makes a peer that goes dark
// fail closed instead of parking forever.
func (c *Conn) SetRecvDeadline(ns int64) {
	if c == nil {
		return
	}
	if ns <= 0 {
		c.deadlineAt = 0
		return
	}
	c.deadlineAt = Nanos() + ns
}

// Send writes ALL of p, chunked to TCPPayloadMax (the kernel's one-segment
// bound) and advancing ONLY by the confirmed count each slot-31 call
// reports — the FileWriteAll contract applied to the socket, so a longer
// body is a run of honest syscalls that can never silently truncate.
// Returns the confirmed total (== len(p) on success, the confirmed prefix
// after a mid-stream failure).
func (c *Conn) Send(p []byte) (int, error) {
	if c == nil || !c.open {
		return 0, ErrConnClosed
	}
	if c.peerGone {
		return 0, ErrPeerGone
	}
	sent := 0
	for sent < len(p) {
		take := len(p) - sent
		if take > TCPPayloadMax {
			take = TCPPayloadMax
		}
		r := svc2(SlotTCPSend, bytePtr(p[sent:sent+take]), uintptr(take))
		if r < 0 {
			return sent, errno(-r)
		}
		if r == 0 {
			// A zero-count acceptance cannot advance the stream; refuse
			// instead of spinning (the kernel never reports one for a
			// non-empty segment, so this is a defensive stop).
			return sent, errno(ErrEINVAL)
		}
		sent += int(r)
	}
	return sent, nil
}

// Recv reads up to len(p) bytes, blocking-with-poll: probe readiness (slot
// 76), take the segment when readable, otherwise check the deadline and
// park one scheduler tick (slot 4) before re-probing. A readable socket
// whose recv then drains 0 bytes means the peer's FIN/RST was consumed —
// fail closed with ErrPeerGone (the kernel reports a received FIN as
// readable and returns 0 for both "no segment yet" and EOF; readiness is
// the only way to tell them apart, and readable-but-empty IS the EOF).
func (c *Conn) Recv(p []byte) (int, error) {
	if c == nil || !c.open {
		return 0, ErrConnClosed
	}
	if c.peerGone {
		return 0, ErrPeerGone
	}
	if len(p) == 0 {
		return 0, nil
	}
	max := len(p)
	if max > TCPPayloadMax {
		max = TCPPayloadMax
	}
	deadline := c.deadlineAt
	if deadline == 0 {
		deadline = Nanos() + DefaultRecvBudgetNs
	}
	for polls := 0; ; polls++ {
		mask, rc := TCPReady()
		if rc < 0 {
			// EAGAIN = the process owns no socket any more (released or
			// never connected): the Conn is dead, not merely empty.
			if -rc == ErrEAGAIN {
				return 0, ErrConnClosed
			}
			return 0, errno(-rc)
		}
		if mask&1 != 0 {
			r := svc2(SlotTCPRecv, bytePtr(p[:max]), uintptr(max))
			if r < 0 {
				return 0, errno(-r)
			}
			if r == 0 {
				c.peerGone = true
				return 0, ErrPeerGone
			}
			return int(r), nil
		}
		// An already-expired budget fails on the FIRST probe instead of
		// paying a full tick for it.
		if Nanos() >= deadline || polls >= maxRecvPolls {
			return 0, errno(ErrETIMEDOUT)
		}
		// The park point: one scheduler tick, so other goroutines run. The
		// loop resumes, re-probes, and re-checks the deadline.
		svc1(SlotSleep, 1)
	}
}

// Close tears the connection down (FIN, slot 33). The userland slot is
// released ONLY when the kernel confirms: on a refusal (the kernel keeps
// the socket — its only close error is EACCES, not-owner) the slot stays
// claimed so the next Dial fails with the honest busy bound instead of a
// confusing EINVAL from the seam. The kernel returns 0 for "nothing
// owned" (idle), which releases too. Close on an already-closed (or nil)
// Conn is a no-op.
func (c *Conn) Close() error {
	if c == nil || !c.open {
		return nil
	}
	rc := svc0(SlotTCPClose)
	if rc < 0 {
		return errno(-rc)
	}
	c.open = false
	slotHeld.Store(false) // identity needs no check: c.open was still true,
	// and only the owner's conn can be open (Dial cannot succeed while held)
	return nil
}

// ipv4Word packs an IPv4 address into the slot-30/10 argument word
// (big-endian in the low 32 bits — the kernel extracts the octets
// byte-explicitly, never a bitcast).
func ipv4Word(ip [4]byte) uintptr {
	return uintptr(uint32(ip[0])<<24 | uint32(ip[1])<<16 | uint32(ip[2])<<8 | uint32(ip[3]))
}

// bytePtr is the &b[0] gateway for a non-empty byte slice (0 for empty —
// the kernel refuses a bad pointer, and an empty call has nothing to copy).
func bytePtr(b []byte) uintptr {
	if len(b) == 0 {
		return 0
	}
	return uintptr(unsafe.Pointer(&b[0]))
}
