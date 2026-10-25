package vi

// M71g (issue #1566): the Go mirror of `sys_net_stats` (ADR 0007 slot 62).
//
// The kernel owns the wire format (kernel/src/syscall.zig `NetStats`,
// marshalled by `handle_net_stats`); the userland mirror lives in
// user/src/lib/netstats.zig. This struct is the Go twin of that mirror, and
// netstats_test.go pins the SAME key offsets and the same 240-byte size, so
// drift on any side fails a test rather than a dashboard reading silently.
//
// Semantics of the call itself, per the kernel handler:
//   - `max < net_stats_bytes` returns 0 — a REFUSAL, not an error (nothing is
//     copied; the caller keeps its previous view).
//   - otherwise it copies the whole snapshot out and returns its byte size.
//   - a bad destination pointer is -EFAULT.
//   - All integers little-endian; IPs are raw network-order bytes ([4]byte);
//     MACs raw bytes ([6]byte). Enum naturals are pinned in the kernel doc.
//
// Off the guest the call degrades to -ENOSYS (vi_host.go), so the decode path
// is exercised through the syscallHook seam.

import "unsafe"

// NetStatsBytes is the pinned snapshot size (`@sizeOf(NetStats)` on both
// other sides; 240 with the padding the layout forces).
const NetStatsBytes = 240

// NetStats is one decoded snapshot.
type NetStats struct {
	MAC       [6]byte
	OwnIP     [4]byte
	Gateway   [4]byte
	DHCPState byte
	LeaseIP   [4]byte
	LeaseMask [4]byte
	// LeaseServer / LeaseSecs
	LeaseServer [4]byte
	LeaseSecs   uint32
	// TCP
	TCPState    byte
	TCPPeerIP   [4]byte
	TCPPeerPort uint16
	// UDP
	UDPCount byte
	UDPPorts [4]uint16
	// ARP
	ARPCount byte
	ARPIPs   [4][4]byte
	ARPMACs  [4][6]byte
	// Counters
	TXFrames   uint64
	TXBytes    uint64
	RXFrames   uint64
	RXBytes    uint64
	RXFiltered uint64
	RXOverflow uint64
	// TCP segment counters: syn_sent, synack_recv, ack_sent, data_sent,
	// data_recv, fin_sent, finack_recv, rst_sent.
	TCPSegs [8]uint64
	// UDP datagram counters: received, sent, loopbacked, dropped.
	UDPDgrams [4]uint64
}

// netStatsOffsets are the layout pins, byte-for-byte the set the kernel
// comment and user/src/lib/netstats.zig pin (the Go compiler inserts the same
// natural-alignment padding as the Zig extern struct, which is what makes a
// field-by-field Go mirror correct rather than merely plausible).
type netStatsOffsets struct {
	OwnIP, Gateway, DHCPState, LeaseSecs uintptr
	TCPState, TCPPeerIP, TCPPeerPort     uintptr
	UDPCount, TXFrames, RXBytes          uintptr
	TCPSegs, UDPDgrams                   uintptr
	Size                                 uintptr
}

func netStatsLayout() netStatsOffsets {
	var s NetStats
	return netStatsOffsets{
		OwnIP:       unsafe.Offsetof(s.OwnIP),
		Gateway:     unsafe.Offsetof(s.Gateway),
		DHCPState:   unsafe.Offsetof(s.DHCPState),
		LeaseSecs:   unsafe.Offsetof(s.LeaseSecs),
		TCPState:    unsafe.Offsetof(s.TCPState),
		TCPPeerIP:   unsafe.Offsetof(s.TCPPeerIP),
		TCPPeerPort: unsafe.Offsetof(s.TCPPeerPort),
		UDPCount:    unsafe.Offsetof(s.UDPCount),
		TXFrames:    unsafe.Offsetof(s.TXFrames),
		RXBytes:     unsafe.Offsetof(s.RXBytes),
		TCPSegs:     unsafe.Offsetof(s.TCPSegs),
		UDPDgrams:   unsafe.Offsetof(s.UDPDgrams),
		Size:        unsafe.Sizeof(s),
	}
}

// netStatsWanted is the offset table the kernel + Zig mirror agree on.
func netStatsWanted() netStatsOffsets {
	return netStatsOffsets{
		OwnIP: 6, Gateway: 10, DHCPState: 14, LeaseSecs: 28,
		TCPState: 32, TCPPeerIP: 33, TCPPeerPort: 38, UDPCount: 40,
		TXFrames: 96, RXBytes: 120, TCPSegs: 144, UDPDgrams: 208,
		Size: 240,
	}
}

// NetStatsSnapshot fetches one live snapshot (slot 62). ok is false when the
// kernel refused (a short buffer / off the guest), in which case the caller
// keeps its previous view — the same contract Zig's read_stats has.
func NetStatsSnapshot() (NetStats, bool) {
	s, rc := NetStatsSnapshotRC()
	return s, rc == NetStatsBytes
}

// NetStatsSnapshotRC is NetStatsSnapshot with the kernel's own return
// preserved: rc is NetStatsBytes on success, 0 on a short-buffer refusal,
// or the negative errno — -EACCES when the caller's grant set lacks the
// `net` word slot 62 belongs to (#3444: a verb that must name WHICH grant
// refused cannot do that off the ok bool).
func NetStatsSnapshotRC() (NetStats, int64) {
	var s NetStats
	buf := unsafe.Slice((*byte)(unsafe.Pointer(&s)), NetStatsBytes)
	r := svc2(SlotNetStats, uintptr(unsafe.Pointer(&buf[0])), uintptr(len(buf)))
	if r != NetStatsBytes {
		return NetStats{}, r
	}
	return decodeNetStats(buf), r
}

// decodeNetStats unpacks a wire snapshot. It is the single decode site, so a
// host test can drive it with a synthesised buffer (the guest only ever hands
// it the kernel's own bytes).
func decodeNetStats(b []byte) NetStats {
	if len(b) < NetStatsBytes {
		return NetStats{}
	}
	var s NetStats
	copy(s.MAC[:], b[0:6])
	copy(s.OwnIP[:], b[6:10])
	copy(s.Gateway[:], b[10:14])
	s.DHCPState = b[14]
	copy(s.LeaseIP[:], b[15:19])
	copy(s.LeaseMask[:], b[20:24])
	copy(s.LeaseServer[:], b[24:28])
	s.LeaseSecs = getU32(b[28:])
	s.TCPState = b[32]
	copy(s.TCPPeerIP[:], b[33:37])
	s.TCPPeerPort = getU16(b[38:])
	s.UDPCount = b[40]
	for i := 0; i < 4; i++ {
		s.UDPPorts[i] = getU16(b[42+2*i:])
	}
	s.ARPCount = b[50]
	for i := 0; i < 4; i++ {
		copy(s.ARPIPs[i][:], b[51+4*i:55+4*i])
	}
	for i := 0; i < 4; i++ {
		copy(s.ARPMACs[i][:], b[67+6*i:73+6*i])
	}
	s.TXFrames = getU64(b[96:])
	s.TXBytes = getU64(b[104:])
	s.RXFrames = getU64(b[112:])
	s.RXBytes = getU64(b[120:])
	s.RXFiltered = getU64(b[128:])
	s.RXOverflow = getU64(b[136:])
	for i := 0; i < 8; i++ {
		s.TCPSegs[i] = getU64(b[144+8*i:])
	}
	for i := 0; i < 4; i++ {
		s.UDPDgrams[i] = getU64(b[208+8*i:])
	}
	return s
}
