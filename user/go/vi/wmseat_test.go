package vi

import "testing"

// The seat's wire constants are ABI: they must match kernel/src/wm_server.zig
// (opcodes), user/src/tabwm.zig, and kernel/src/syscall.zig (the tags). Pinned
// here so a drift is a host-test failure, not a silent live-gate mismatch.
func TestWmSeatWireConstants(t *testing.T) {
	if SlotWmctl != 65 {
		t.Fatalf("SlotWmctl = %d want 65", SlotWmctl)
	}
	if WmctlRegisterCmd != 1 {
		t.Fatalf("WmctlRegisterCmd = %d want 1", WmctlRegisterCmd)
	}
	if WmctlRequestPresentCmd != 3 {
		t.Fatalf("WmctlRequestPresentCmd = %d want 3", WmctlRequestPresentCmd)
	}
	if EvCompositeTick != 18 {
		t.Fatalf("EvCompositeTick = %d want 18", EvCompositeTick)
	}
	if EvWmPointer != 19 {
		t.Fatalf("EvWmPointer = %d want 19", EvWmPointer)
	}
	if EvWmWindow != 20 {
		t.Fatalf("EvWmWindow = %d want 20", EvWmWindow)
	}
	if EvWmKey != 21 {
		t.Fatalf("EvWmKey = %d want 21", EvWmKey)
	}
	// Distinct from the app-side MOUSE_*/KEY_* kinds (ADR 0009 D2): while a
	// seat is registered the kernel fans 19/21 to the WM, not 3/1.
	if EvWmPointer == EvMouseMove || EvWmPointer == EvMouseDown {
		t.Fatal("EvWmPointer collided with an app mouse kind")
	}
	if EvWmKey == EvKeyDown || EvWmKey == EvKeyUp {
		t.Fatal("EvWmKey collided with an app key kind")
	}
	if M33ScanoutTag != 0x4000000000000000 {
		t.Fatalf("M33ScanoutTag = %#x want 0x4000000000000000", uint64(M33ScanoutTag))
	}
	if M33MapShared != 0x10000 {
		t.Fatalf("M33MapShared = %#x want 0x10000", M33MapShared)
	}
	// The scanout bind is full-frame: the length must be the framebuffer's
	// exact byte size and a whole number of pages.
	if ScanoutFbBytes != 3686400 {
		t.Fatalf("ScanoutFbBytes = %d want 3686400", ScanoutFbBytes)
	}
	if ScanoutFbBytes%PageSize != 0 {
		t.Fatalf("ScanoutFbBytes %d is not page-aligned", ScanoutFbBytes)
	}
}

// The scanout hint itself must survive MmapHint's alignment pre-check (the
// scan tag is a power of two, hence page-aligned) and reach the syscall, which
// degrades to ENOSYS off the guest.
func TestMmapScanoutHostDegrades(t *testing.T) {
	b, err := MmapScanout()
	if err != errno(ErrENOSYS) || b != nil {
		t.Fatalf("host MmapScanout = (%v,%v) want (nil,ENOSYS)", b, err)
	}
}

func TestMmapWindowSurfaceHostDegrades(t *testing.T) {
	if _, err := MmapWindowSurface(0, 1024); err != errno(ErrEINVAL) {
		t.Fatalf("invalid window: %v", err)
	}
	if _, err := MmapWindowSurface(1, 0); err != errno(ErrEINVAL) {
		t.Fatalf("empty surface: %v", err)
	}
	if _, err := MmapWindowSurface(1, 1024); err != errno(ErrENOSYS) {
		t.Fatalf("host window surface: %v", err)
	}
}

func TestErrnoOf(t *testing.T) {
	if ErrnoOf(0) != 0 {
		t.Fatalf("ErrnoOf(0) = %d want 0", ErrnoOf(0))
	}
	if ErrnoOf(-ErrEACCES) != ErrEACCES {
		t.Fatalf("ErrnoOf(-EACCES) = %d want %d", ErrnoOf(-ErrEACCES), ErrEACCES)
	}
	if ErrnoOf(-ErrENXIO) != ErrENXIO {
		t.Fatalf("ErrnoOf(-ENXIO) = %d want %d", ErrnoOf(-ErrENXIO), ErrENXIO)
	}
}
