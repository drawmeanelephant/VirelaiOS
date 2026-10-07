package vi

import (
	"errors"
	"testing"
	"time"
)

// The TCP host fake: captures the slot-30..33 + 76 traffic the Conn type
// generates and scripts the answers (probe masks, recv bytes, error rows).
type connFake struct {
	connectCalls int
	connectIP    uint32
	connectPort  uintptr
	connectRC    int64
	sendCalls    int
	sentSizes    []int
	sentBytes    []byte
	recvCalls    int
	closeCalls   int
	probeCalls   int
	sleeps       int
	other        int
	// probeMask is returned by every slot-76 probe; probeScript, when set,
	// maps the 1-based probe call to a mask instead.
	probeMask   int64
	probeScript func(call int) int64
	// recvScript maps the 1-based recv call to (bytes, rc).
	recvScript func(call int) ([]byte, int64)
	sendRC     int64 // what every send returns (default: the offered length)
}

func (f *connFake) hook(num uintptr, a0, a1, a2, a3 uintptr) int64 {
	switch num {
	case SlotTCPConnect:
		f.connectCalls++
		f.connectIP = uint32(a0)
		f.connectPort = a1
		return f.connectRC
	case SlotTCPSend:
		f.sendCalls++
		b := hookBytes(a0, a1)
		f.sentSizes = append(f.sentSizes, len(b))
		f.sentBytes = append(f.sentBytes, b...)
		if f.sendRC != 0 {
			return f.sendRC
		}
		return int64(a1)
	case SlotTCPRecv:
		f.recvCalls++
		if f.recvScript == nil {
			return 0
		}
		b, rc := f.recvScript(f.recvCalls)
		if b != nil {
			copy(hookBytes(a0, uintptr(len(b))), b)
		}
		return rc
	case SlotTCPClose:
		f.closeCalls++
		return 0
	case SlotSockReady:
		f.probeCalls++
		if f.probeScript != nil {
			return f.probeScript(f.probeCalls)
		}
		return f.probeMask
	case SlotSleep:
		f.sleeps++
		return 0
	}
	f.other++
	return -ErrENOSYS
}

func startConnFake(t *testing.T) *connFake {
	t.Helper()
	f := &connFake{}
	prev := SetSyscallHookForTest(f.hook)
	t.Cleanup(func() { SetSyscallHookForTest(prev) })
	t.Cleanup(func() { slotHeld.Store(false) })
	return f
}

func TestDial_LiteralConnects(t *testing.T) {
	f := startConnFake(t)
	c, err := Dial("10.0.0.2", 8080)
	if err != nil {
		t.Fatalf("Dial: %v", err)
	}
	if f.connectCalls != 1 || f.connectIP != 0x0a000002 || f.connectPort != 8080 {
		t.Fatalf("connect = %d calls to %08x:%d, want 1 call to 0a000002:8080",
			f.connectCalls, f.connectIP, f.connectPort)
	}
	if c.IP() != [4]byte{10, 0, 0, 2} || c.Port() != 8080 {
		t.Fatalf("peer = %v:%d, want 10.0.0.2:8080", c.IP(), c.Port())
	}
}

func TestDialTimeout_EncodesMilliseconds(t *testing.T) {
	for _, d := range []time.Duration{time.Nanosecond, time.Millisecond, 5 * time.Second, 5*time.Second + time.Nanosecond, 0xffffffff * time.Millisecond} {
		t.Run(d.String(), func(t *testing.T) {
			f := startConnFake(t)
			c, err := DialTimeout("10.0.0.2", 8080, d)
			if err != nil {
				t.Fatal(err)
			}
			ms := uint64((d + time.Millisecond - 1) / time.Millisecond)
			if f.connectCalls != 1 || f.connectIP != 0x0a000002 || uint64(f.connectPort) != 8080|ms<<16 {
				t.Fatalf("connect = %d %08x %x", f.connectCalls, f.connectIP, f.connectPort)
			}
			if c.Port() != 8080 || c.IP() != [4]byte{10, 0, 0, 2} {
				t.Fatalf("peer = %v:%d", c.IP(), c.Port())
			}
		})
	}
}

func TestDialTimeout_InvalidDurationDoesNotConnect(t *testing.T) {
	for _, d := range []time.Duration{-1, 0, 0xffffffff*time.Millisecond + 1, time.Duration(1<<63 - 1)} {
		f := startConnFake(t)
		if c, err := DialTimeout("10.0.0.2", 8080, d); c != nil || !errors.Is(err, errno(ErrEINVAL)) {
			t.Fatalf("DialTimeout(%v) = %v, %v", d, c, err)
		}
		if f.connectCalls != 0 {
			t.Fatal("invalid duration reached slot 30")
		}
	}
}

func TestDialTimeout_RefusalReleasesSlot(t *testing.T) {
	f := startConnFake(t)
	f.connectRC = -ErrETIMEDOUT
	if c, err := DialTimeout("10.0.0.2", 8081, 5*time.Second); c != nil || !errors.Is(err, errno(ErrETIMEDOUT)) {
		t.Fatalf("timeout = %v, %v", c, err)
	}
	if f.closeCalls != 0 || slotHeld.Load() {
		t.Fatal("failed connect retained its slot or called Close")
	}
	f.connectRC = 0
	if _, err := Dial("10.0.0.2", 8080); err != nil {
		t.Fatal(err)
	}
	if f.connectCalls != 2 || f.connectPort != 8080 {
		t.Fatal("legacy reconnect did not use its unchanged port word")
	}
}

func TestDialTimeout_UsesDialGuards(t *testing.T) {
	f := startConnFake(t)
	if _, err := DialTimeout("10.0.0.2", 0, time.Second); !errors.Is(err, errno(ErrEINVAL)) {
		t.Fatal(err)
	}
	if _, err := DialTimeout("0.0.0.0", 8080, time.Second); !errors.Is(err, ErrServerDial) {
		t.Fatal(err)
	}
	if f.connectCalls != 0 {
		t.Fatal("invalid peer reached slot 30")
	}
	if _, err := Dial("10.0.0.2", 8080); err != nil {
		t.Fatal(err)
	}
	if _, err := DialTimeout("10.0.0.2", 8080, time.Second); !errors.Is(err, ErrConnBusy) {
		t.Fatal(err)
	}
	if f.connectCalls != 1 {
		t.Fatal("busy dial reached slot 30")
	}
}

func TestDial_NameResolvesThenConnects(t *testing.T) {
	// A name dials the RESOLVED address: the DNS seam runs first (the fake
	// answers myhost.local -> 10.0.0.2), then connect carries the octets.
	dns := &dnsFake{}
	dns.recvScript = func(int) []byte {
		return udpDgram(DNSPort, dnsReplyFor(dns.sentQuery, [4]byte{10, 0, 0, 2}))
	}
	f := &connFake{}
	prev := SetSyscallHookForTest(func(num uintptr, a0, a1, a2, a3 uintptr) int64 {
		if num == SlotUDPListen || num == SlotUDPSend || num == SlotUDPRecv {
			return dns.hook(num, a0, a1, a2, a3)
		}
		return f.hook(num, a0, a1, a2, a3)
	})
	defer SetSyscallHookForTest(prev)
	t.Cleanup(func() { dnsPortBound = false; slotHeld.Store(false) })

	c, err := Dial("myhost.local", 8080)
	if err != nil {
		t.Fatalf("Dial by name: %v", err)
	}
	if f.connectCalls != 1 || f.connectIP != 0x0a000002 {
		t.Fatalf("connect = %d calls to %08x, want the resolved 0a000002",
			f.connectCalls, f.connectIP)
	}
	if c.IP() != [4]byte{10, 0, 0, 2} {
		t.Fatalf("peer IP = %v, want the resolved address", c.IP())
	}
}

func TestDialTimeout_NameResolvesThenConnects(t *testing.T) {
	dns := &dnsFake{}
	dns.recvScript = func(int) []byte {
		return udpDgram(DNSPort, dnsReplyFor(dns.sentQuery, [4]byte{10, 0, 0, 2}))
	}
	f := &connFake{}
	prev := SetSyscallHookForTest(func(num uintptr, a0, a1, a2, a3 uintptr) int64 {
		if num == SlotUDPListen || num == SlotUDPSend || num == SlotUDPRecv {
			return dns.hook(num, a0, a1, a2, a3)
		}
		return f.hook(num, a0, a1, a2, a3)
	})
	defer SetSyscallHookForTest(prev)
	t.Cleanup(func() { dnsPortBound = false; slotHeld.Store(false) })
	c, err := DialTimeout("myhost.local", 8080, 5*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	if c.IP() != [4]byte{10, 0, 0, 2} || f.connectCalls != 1 || f.connectPort != 8080|5000<<16 {
		t.Fatalf("resolved connect = %v, %d calls, word=%x", c.IP(), f.connectCalls, f.connectPort)
	}
}

func TestDial_SecondLiveDialIsBusy(t *testing.T) {
	// The kernel keeps ONE TCP socket per process; the second live Dial
	// must fail in userland WITHOUT touching the connect slot.
	f := startConnFake(t)
	c1, err := Dial("10.0.0.2", 8080)
	if err != nil {
		t.Fatalf("first Dial: %v", err)
	}
	if _, err := Dial("10.0.0.3", 8081); !errors.Is(err, ErrConnBusy) {
		t.Fatalf("second live Dial = %v, want ErrConnBusy", err)
	}
	if f.connectCalls != 1 {
		t.Fatalf("connect called %d times, want 1 (the busy check is userland)", f.connectCalls)
	}
	// Close-then-Dial is the legal reconnect path.
	if err := c1.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if f.closeCalls != 1 {
		t.Fatalf("close calls = %d, want 1", f.closeCalls)
	}
	if _, err := Dial("10.0.0.3", 8081); err != nil {
		t.Fatalf("Dial after Close: %v", err)
	}
	if f.connectCalls != 2 || f.connectIP != 0x0a000003 {
		t.Fatalf("reconnect = %d calls to %08x, want 2nd call to 0a000003", f.connectCalls, f.connectIP)
	}
}

func TestDial_PortZeroRefused(t *testing.T) {
	f := startConnFake(t)
	if _, err := Dial("10.0.0.2", 0); !errors.Is(err, error(errno(ErrEINVAL))) {
		t.Fatalf("Dial port 0 = %v, want Errno(EINVAL)", err)
	}
	if f.connectCalls != 0 {
		t.Fatalf("port 0 reached the kernel (%d calls)", f.connectCalls)
	}
}

func TestDial_ZeroIPIsTheServerAddress(t *testing.T) {
	// The kernel treats ip == 0 as PASSIVE open (listen mode). Dial must
	// refuse 0.0.0.0 in userland so a hostile DNS reply cannot open a
	// listener; Listen is the inbound path.
	f := startConnFake(t)
	if _, err := Dial("0.0.0.0", 8080); !errors.Is(err, ErrServerDial) {
		t.Fatalf("Dial(0.0.0.0) = %v, want ErrServerDial", err)
	}
	if f.connectCalls != 0 {
		t.Fatalf("the zero address reached the kernel (%d calls)", f.connectCalls)
	}
}

func TestListen_PassiveOpenOnZeroIP(t *testing.T) {
	f := startConnFake(t)
	c, err := Listen(2222)
	if err != nil {
		t.Fatalf("Listen: %v", err)
	}
	if f.connectCalls != 1 || f.connectIP != 0 || f.connectPort != 2222 {
		t.Fatalf("connect = %d calls to %08x:%d, want 1 call to 0:2222",
			f.connectCalls, f.connectIP, f.connectPort)
	}
	if !c.listening {
		t.Fatal("Listen did not mark the Conn as listening")
	}
	if _, err := Dial("10.0.0.2", 8080); !errors.Is(err, ErrConnBusy) {
		t.Fatalf("Dial while listening = %v, want ErrConnBusy", err)
	}
	if f.connectCalls != 1 {
		t.Fatalf("Dial while listening reached the kernel (%d calls)", f.connectCalls)
	}
	if err := c.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if _, err := Dial("10.0.0.2", 8080); err != nil {
		t.Fatalf("Dial after Listen+Close: %v", err)
	}
}

func TestListen_PortZeroRefused(t *testing.T) {
	f := startConnFake(t)
	if _, err := Listen(0); !errors.Is(err, error(errno(ErrEINVAL))) {
		t.Fatalf("Listen(0) = %v, want EINVAL", err)
	}
	if f.connectCalls != 0 {
		t.Fatalf("Listen(0) reached the kernel (%d calls)", f.connectCalls)
	}
}

func TestAccept_WaitsUntilEstablished(t *testing.T) {
	f := startConnFake(t)
	c, err := Listen(2222)
	if err != nil {
		t.Fatalf("Listen: %v", err)
	}
	// First two probes still listening (mask 0); third is established
	// (writable bit 1). Accept must park between probes rather than
	// spinning, and must not touch recv.
	f.probeScript = func(call int) int64 {
		if call >= 3 {
			return 2
		}
		return 0
	}
	if err := c.Accept(); err != nil {
		t.Fatalf("Accept: %v", err)
	}
	if c.listening {
		t.Fatal("Accept left the Conn in listening state")
	}
	if f.probeCalls < 3 {
		t.Fatalf("probes = %d, want at least 3", f.probeCalls)
	}
	if f.sleeps < 2 {
		t.Fatalf("sleeps = %d, want parks between empty probes", f.sleeps)
	}
	if f.recvCalls != 0 {
		t.Fatalf("Accept recv'd %d times, want 0", f.recvCalls)
	}
}

func TestDial_DNSResolvingToZeroRefused(t *testing.T) {
	// The same guard covers the resolved path: a (hostile) reply naming
	// 0.0.0.0 as the A record must not turn a name dial into a server.
	dns := &dnsFake{}
	dns.recvScript = func(int) []byte {
		return udpDgram(DNSPort, dnsReplyFor(dns.sentQuery, [4]byte{0, 0, 0, 0}))
	}
	f := &connFake{}
	prev := SetSyscallHookForTest(func(num uintptr, a0, a1, a2, a3 uintptr) int64 {
		if num == SlotUDPListen || num == SlotUDPSend || num == SlotUDPRecv {
			return dns.hook(num, a0, a1, a2, a3)
		}
		return f.hook(num, a0, a1, a2, a3)
	})
	defer SetSyscallHookForTest(prev)
	t.Cleanup(func() { dnsPortBound = false; slotHeld.Store(false) })

	if _, err := Dial("zero.host", 8080); !errors.Is(err, ErrServerDial) {
		t.Fatalf("Dial of a name resolving to 0.0.0.0 = %v, want ErrServerDial", err)
	}
	if f.connectCalls != 0 {
		t.Fatalf("the resolved zero address reached the kernel (%d calls)", f.connectCalls)
	}
}

func TestDial_ConcurrentDialsStaySingle(t *testing.T) {
	// Programs run concurrent goroutines (the fixtures' own heartbeat), so
	// two racing Dials must not both pass the nil check: exactly one wins
	// the socket, the rest see the busy bound — and the kernel sees ONE
	// connect.
	f := startConnFake(t)
	const n = 4
	errs := make(chan error, n)
	for i := 0; i < n; i++ {
		go func() {
			_, err := Dial("10.0.0.2", 8080)
			errs <- err
		}()
	}
	wins, busy := 0, 0
	for i := 0; i < n; i++ {
		switch err := <-errs; err {
		case nil:
			wins++
		case ErrConnBusy:
			busy++
		default:
			t.Fatalf("unexpected Dial error: %v", err)
		}
	}
	if wins != 1 || busy != n-1 {
		t.Fatalf("racing Dials = %d wins / %d busy, want 1 / %d", wins, busy, n-1)
	}
	if f.connectCalls != 1 {
		t.Fatalf("connect called %d times, want exactly 1", f.connectCalls)
	}
}

func TestClose_FailureKeepsTheSlotClaimed(t *testing.T) {
	// On a kernel close refusal the socket is still live in-kernel: the
	// userland slot must stay claimed, or the next Dial would eat a
	// confusing EINVAL from the seam instead of the honest busy bound.
	f := startConnFake(t)
	c, _ := Dial("10.0.0.2", 8080)
	closeScript := []int64{-ErrEACCES, 0} // first Close refused, second: idle → 0
	f.other = 0
	prev := SetSyscallHookForTest(func(num uintptr, a0, a1, a2, a3 uintptr) int64 {
		if num == SlotTCPClose {
			rc := closeScript[0]
			closeScript = closeScript[1:]
			if len(closeScript) == 0 {
				closeScript = []int64{0}
			}
			f.closeCalls++
			return rc
		}
		t.Fatalf("unexpected slot %d", num)
		return -ErrENOSYS
	})
	defer SetSyscallHookForTest(prev)

	if err := c.Close(); !errors.Is(err, error(errno(ErrEACCES))) {
		t.Fatalf("refused Close = %v, want Errno(EACCES)", err)
	}
	if !slotHeld.Load() {
		t.Fatal("a refused Close must NOT release the one-socket slot")
	}
	if _, err := Dial("10.0.0.3", 8081); !errors.Is(err, ErrConnBusy) {
		t.Fatalf("Dial after a refused Close = %v, want ErrConnBusy", err)
	}
	// The kernel's "nothing owned" row (idle → 0) DOES release.
	if err := c.Close(); err != nil {
		t.Fatalf("Close on an idle socket = %v, want nil", err)
	}
	if slotHeld.Load() {
		t.Fatal("an accepted Close must release the slot")
	}
}

func TestSend_ChunksByConfirmedCounts(t *testing.T) {
	// A body longer than one segment is a run of honest sends, each
	// advancing by the CONFIRMED count: two maximal segments and a tail.
	f := startConnFake(t)
	c, _ := Dial("10.0.0.2", 8080)
	body := make([]byte, 2*TCPPayloadMax+116)
	for i := range body {
		body[i] = byte(i)
	}
	n, err := c.Send(body)
	if err != nil || n != len(body) {
		t.Fatalf("Send = %d, %v want %d, nil", n, err, len(body))
	}
	if len(f.sentSizes) != 3 || f.sentSizes[0] != TCPPayloadMax || f.sentSizes[1] != TCPPayloadMax || f.sentSizes[2] != 116 {
		t.Fatalf("segment plan = %v, want %d/%d/116", f.sentSizes, TCPPayloadMax, TCPPayloadMax)
	}
	for i := range body {
		if f.sentBytes[i] != byte(i) {
			t.Fatalf("sent byte %d = %d, want %d", i, f.sentBytes[i], byte(i))
			break
		}
	}
}

func TestSend_StopsOnKernelError(t *testing.T) {
	// A mid-stream failure reports the CONFIRMED prefix and the errno.
	f := startConnFake(t)
	c, _ := Dial("10.0.0.2", 8080)
	f.sendRC = -ErrEFAULT
	n, err := c.Send(make([]byte, 400))
	if n != 0 || !errors.Is(err, error(errno(ErrEFAULT))) {
		t.Fatalf("Send = %d, %v want 0, Errno(EFAULT)", n, err)
	}
}

func TestSend_AdvancesByConfirmedCounts(t *testing.T) {
	// A kernel that confirms FEWER bytes than offered (a defensive pin: the
	// handler returns len or an error today) must advance the stream by the
	// confirmed count and finish the body, never fail it whole.
	calls := 0
	prev := SetSyscallHookForTest(func(num uintptr, a0, a1, a2, a3 uintptr) int64 {
		switch num {
		case SlotTCPConnect:
			return 0
		case SlotTCPSend:
			calls++
			if calls == 1 {
				return 64 // confirms 64 of the offered TCPPayloadMax
			}
			return int64(a1)
		}
		t.Fatalf("unexpected slot %d", num)
		return 0
	})
	defer SetSyscallHookForTest(prev)
	t.Cleanup(func() { slotHeld.Store(false) })

	c, err := Dial("10.0.0.2", 8080)
	if err != nil {
		t.Fatalf("Dial: %v", err)
	}
	body := make([]byte, TCPPayloadMax+65)
	n, err := c.Send(body)
	if err != nil || n != len(body) {
		t.Fatalf("Send over a short confirm = %d, %v want %d, nil", n, err, len(body))
	}
	if calls < 3 {
		t.Fatalf("sends = %d, want the shortfall re-sent (>=3)", calls)
	}
}

func TestRecv_DeliversQueuedBytes(t *testing.T) {
	f := startConnFake(t)
	c, _ := Dial("10.0.0.2", 8080)
	f.probeMask = 1 // readable on the first probe
	f.recvScript = func(int) ([]byte, int64) { return []byte("hello world"), 11 }
	buf := make([]byte, 64)
	n, err := c.Recv(buf)
	if err != nil || n != 11 || string(buf[:n]) != "hello world" {
		t.Fatalf("Recv = %d, %v, %q want 11, nil, hello world", n, err, buf[:n])
	}
	// The recv asks for at most len(p) (the caller's bound is the wire's).
	if f.recvCalls != 1 {
		t.Fatalf("recv calls = %d, want 1", f.recvCalls)
	}
}

func TestRecv_ParksWhileNotReadable(t *testing.T) {
	// Not-readable probes must PARK (sys_sleep) before re-probing — the
	// M65 pacing that lets the other goroutines run while this one waits.
	f := startConnFake(t)
	c, _ := Dial("10.0.0.2", 8080)
	f.probeScript = func(call int) int64 {
		if call < 3 {
			return 0 // two empty polls...
		}
		return 1 // ...then readable
	}
	f.recvScript = func(int) ([]byte, int64) { return []byte("x"), 1 }
	n, err := c.Recv(make([]byte, 8))
	if err != nil || n != 1 {
		t.Fatalf("Recv = %d, %v want 1, nil", n, err)
	}
	if f.sleeps != 2 {
		t.Fatalf("parks = %d, want 2 (one per empty poll)", f.sleeps)
	}
}

func TestRecv_ReadableButEmptyIsPeerGone(t *testing.T) {
	// Readable with a 0-byte drain is the consumed FIN/RST: fail closed
	// once, then every further Recv fails without another syscall.
	f := startConnFake(t)
	c, _ := Dial("10.0.0.2", 8080)
	f.probeMask = 1
	f.recvScript = func(int) ([]byte, int64) { return nil, 0 }
	if _, err := c.Recv(make([]byte, 8)); !errors.Is(err, ErrPeerGone) {
		t.Fatalf("readable-but-empty Recv = %v, want ErrPeerGone", err)
	}
	calls := f.recvCalls
	if _, err := c.Recv(make([]byte, 8)); !errors.Is(err, ErrPeerGone) {
		t.Fatalf("Recv after peer gone = %v, want ErrPeerGone", err)
	}
	if f.recvCalls != calls {
		t.Fatal("Recv after peer gone must fail closed without a recv")
	}
}

func TestRecv_TimesOutWhenNeverReadable(t *testing.T) {
	f := startConnFake(t)
	c, _ := Dial("10.0.0.2", 8080)
	c.SetRecvDeadline(2_000_000) // 2 ms of real wall clock
	start := time.Now()
	_, err := c.Recv(make([]byte, 8))
	if !errors.Is(err, error(errno(ErrETIMEDOUT))) {
		t.Fatalf("Recv on a silent peer = %v, want Errno(ETIMEDOUT)", err)
	}
	if elapsed := time.Since(start); elapsed > 5*time.Second {
		t.Fatalf("a 2 ms budget took %v — the wait is not bounded", elapsed)
	}
	if f.sleeps == 0 {
		t.Fatal("the bounded wait must park between probes, not spin")
	}
}

func TestRecv_ExpiredDeadlineFailsOnFirstProbe(t *testing.T) {
	// The deadline is absolute: one that has already elapsed costs ONE
	// probe and ZERO parks, instead of a whole tick.
	f := startConnFake(t)
	c, _ := Dial("10.0.0.2", 8080)
	c.SetRecvDeadline((1 * time.Millisecond).Nanoseconds())
	time.Sleep(5 * time.Millisecond) // the clock is already past the deadline
	_, err := c.Recv(make([]byte, 8))
	if !errors.Is(err, error(errno(ErrETIMEDOUT))) {
		t.Fatalf("Recv with an elapsed deadline = %v, want Errno(ETIMEDOUT)", err)
	}
	if f.probeCalls != 1 || f.sleeps != 0 {
		t.Fatalf("elapsed deadline cost %d probes / %d parks, want 1 / 0", f.probeCalls, f.sleeps)
	}
}

func TestRecv_ProbeEAGAINMeansClosed(t *testing.T) {
	// The probe's EAGAIN is the kernel saying the process owns no socket:
	// the Conn is dead, not merely empty.
	f := startConnFake(t)
	c, _ := Dial("10.0.0.2", 8080)
	f.other = 0
	prev := SetSyscallHookForTest(func(num uintptr, a0, a1, a2, a3 uintptr) int64 {
		if num == SlotSockReady {
			f.probeCalls++
			return -ErrEAGAIN
		}
		t.Fatalf("unexpected slot %d after EAGAIN", num)
		return -ErrENOSYS
	})
	defer SetSyscallHookForTest(prev)
	if _, err := c.Recv(make([]byte, 8)); !errors.Is(err, ErrConnClosed) {
		t.Fatalf("Recv after EAGAIN = %v, want ErrConnClosed", err)
	}
}

func TestClose_ClearsTheLiveSlot(t *testing.T) {
	f := startConnFake(t)
	c, _ := Dial("10.0.0.2", 8080)
	if err := c.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if slotHeld.Load() {
		t.Fatal("Close must clear the one-socket slot")
	}
	if err := c.Close(); err != nil {
		t.Fatalf("double Close = %v, want nil", err)
	}
	if f.closeCalls != 1 {
		t.Fatalf("close calls = %d, want 1 (the second is a no-op)", f.closeCalls)
	}
	if _, err := c.Recv(make([]byte, 8)); !errors.Is(err, ErrConnClosed) {
		t.Fatalf("Recv after Close = %v, want ErrConnClosed", err)
	}
	if _, err := c.Send([]byte("x")); !errors.Is(err, ErrConnClosed) {
		t.Fatalf("Send after Close = %v, want ErrConnClosed", err)
	}
}

func TestRecv_EmptyBufferIsNoop(t *testing.T) {
	f := startConnFake(t)
	c, _ := Dial("10.0.0.2", 8080)
	if n, err := c.Recv(nil); n != 0 || err != nil {
		t.Fatalf("Recv(nil) = %d, %v want 0, nil", n, err)
	}
	if f.recvCalls != 0 {
		t.Fatal("an empty recv must not touch the kernel")
	}
}
