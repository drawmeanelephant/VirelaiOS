package vi

// SlotTimeSet is ADR 0007 slot 78 (M83b, #1775): re-anchor the wall clock so
// slot 66 (Now) reads the given Unix seconds. Slot 66 itself stays read-only.
const SlotTimeSet uintptr = 78

// TimeSet steps the guest wall clock to epoch (Unix seconds) and returns 0 or
// a negative errno. The kernel refuses, with EINVAL and the clock untouched,
// an epoch outside 2025-01-01..2100-01-01 UTC; a non-positive epoch is refused
// here without a syscall, because uintptr would wrap it into the range check.
// Works on a boot with no firmware epoch (Now was -ENOSYS). The tick count
// does not move, so Nanos and every deadline built on it are unaffected.
func TimeSet(epoch int64) int64 {
	if epoch <= 0 {
		return -ErrEINVAL
	}
	return svc1(SlotTimeSet, uintptr(epoch))
}

// The UDP seam's fixed bounds (dns.go documents them), exported for the
// one-shot datagram clients that share the seam: the source port slot 10
// always sends from — so it is also the port replies arrive on — and the
// largest payload and datagram (payload + the 8-byte UDP header slot 11
// returns) the seam copies.
const (
	UDPSourcePort  = udpSourcePort
	UDPPayloadMax  = udpPayloadMax
	UDPDatagramMax = udpDatagramMax
)
