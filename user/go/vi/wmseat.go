package vi

import "unsafe"

// M57a (issue #1313): the WM-SEAT half of the slot-65 seam.
//
// wmclient.go is the tab-CLIENT half: an app asks the registered WM over the
// mailbox and never touches slot 65. A window manager is the SERVER: it is the
// process the kernel accepts as the render-server seat (slot 65 cmd 1
// REGISTER) and the one that drives the composite cadence with
// REQUEST_PRESENT (cmd 3), once per kind-18 COMPOSITE_TICK the kernel delivers
// to the seat. This file is that server-side surface — deliberately tiny, the
// mirror of wmclient.go, and host-safe: every call degrades to -ENOSYS off the
// guest, exactly like the rest of the SDK.

const (
	// SlotWmctl is the render-server control slot (ADR 0015 seam A, frozen
	// by WMS1 in the ADR 0007 amendment).
	SlotWmctl uintptr = 65
	// WmctlRegisterCmd is slot-65 subcommand 1: accept the CALLER as the
	// active compositor (one seat; a second registration is refused EACCES).
	WmctlRegisterCmd uint64 = 1
	// WmctlRequestPresentCmd is slot-65 subcommand 3: transfer+flush the
	// scanout now and advance the kernel's present sequence/count.
	WmctlRequestPresentCmd uint64 = 3
	// WmctlContentPtrCmd is slot-65 subcommand 15 (#1688, ADR 0015
	// additive, changelog 2026-09-23): forward one CONTENT pointer sample
	// into the kernel's local path (terminal text selection + mouse-
	// tracking reports). a0 = x|(y<<16), a1 = button mask (bit0 left,
	// bit1 right); the kernel derives press/release edges from the
	// seat-serialized stream itself. Chrome the seat consumed (start
	// surface, rail, launcher) is never forwarded.
	WmctlContentPtrCmd uint64 = 15
	// WmctlSeatPidCmd is slot-65 subcommand 16 (#2079, M97g seat gate):
	// return the kernel-registered WM pid, or -ENOENT when no seat is
	// held. Unprivileged and read-only — the seat pid was always
	// observable; the fix is that clients now read the kernel's own
	// register instead of resolving a forgeable process name.
	WmctlSeatPidCmd uint64 = 16

	// M33MapShared is the frozen seam-B flag (ADR 0016, bit 16 of the mmap
	// flags word): a shared-anonymous mapping. The scanout bind is one.
	M33MapShared uint64 = 0x10000
	// M33ScanoutTag is the mmap `addr` tag that asks the kernel for the
	// REGISTERED WM's WRITABLE view of the virtio-gpu framebuffer (the
	// compose-N target). Seat-only, full-frame only.
	M33ScanoutTag uintptr = 0x4000000000000000
	// M33WindowTag binds the low-eight-bit window id to a shared, writable
	// owner surface. Unlike a normal mmap address the tag is not page-aligned.
	M33WindowTag uintptr = 0x8000000000000000

	// The framebuffer geometry the scanout bind is full-frame against
	// (kernel/src/virtio_gpu.zig: fb_width x fb_height x fb_bpp).
	ScanoutWidth   = 1280
	ScanoutHeight  = 720
	ScanoutBPP     = 4
	ScanoutFbBytes = ScanoutWidth * ScanoutHeight * ScanoutBPP // 3,686,400
)

// EvCompositeTick is event kind 18 (ADR 0009 numbering, reserved by WMS1): the
// pacing tick delivered ONLY to the registered WM's process event queue, once
// per scheduler tick.
const EvCompositeTick uint16 = 18

// WM input-seam kinds (ADR 0009 numbering, WMS5). SDK mirrors of the kernel
// constants in kernel/src/events.zig — not a new ABI. Routing-restricted:
// the kernel pushes them ONLY to the registered WM, never in shim mode.
const (
	// EvWmPointer is kind 19: raw absolute pointer. arg0 = x|(y<<16) in
	// framebuffer pixels, flags low byte = HID button byte (0x01 left).
	EvWmPointer uint16 = 19
	// EvWmWindow is kind 20: a window-registry mirror (id / rect / focus).
	EvWmWindow uint16 = 20
	// EvWmKey is kind 21: raw keyboard. arg0 = HID usage, flags = ADR 0009
	// modifier bits.
	EvWmKey uint16 = 21
)

// WmctlRegister asks the kernel to accept the calling process as the active
// render-server seat (slot 65 cmd 1). Returns 0 on success; -EACCES when a
// seat is already taken (the one-seat discipline); -ENXIO when the compositor
// is not armed.
func WmctlRegister() int64 { return syscall1(SlotWmctl, uintptr(WmctlRegisterCmd)) }

// WmctlRequestPresent transfers+flushes the scanout now (slot 65 cmd 3).
// Returns 0 on success; -ENOSYS when no seat is registered; -EACCES from any
// process other than the seat.
func WmctlRequestPresent() int64 { return syscall1(SlotWmctl, uintptr(WmctlRequestPresentCmd)) }

// WmctlSeatPid returns the kernel-registered WM pid (slot 65 cmd 16), or a
// negative errno: -ENOENT when no seat is held, -ENOSYS off the guest. It
// is the #2079 seat-resolution primitive — the answer is the kernel's
// register, so a rogue process named like a WM cannot capture the seat id
// another app's WM_RPC traffic routes to. It routes through svc1 so the
// host-test hook sees it (unlike the seat-side wrappers above, which only
// a live seat ever calls).
func WmctlSeatPid() int64 { return svc1(SlotWmctl, uintptr(WmctlSeatPidCmd)) }

// WmctlContentPtr forwards one pointer sample for the kernel's local content
// path — terminal text selection (M49 SD5) and mouse-tracking reports — the
// path WMS5 made dormant under a seat. The seat calls it for every pointer
// event that is not launcher or rail chrome; press/release edges derive from
// this serialized stream itself, so the kernel cannot tell it from the raw
// path. Host-safe: degrades to -ENOSYS off the guest like the rest of the SDK.
func WmctlContentPtr(x, y uint32, buttons uint8) int64 {
	return syscall6(SlotWmctl, uintptr(WmctlContentPtrCmd),
		uintptr(uint32(x)|(y<<16)), uintptr(buttons), 0, 0, 0)
}

// MmapScanout maps the virtio-gpu framebuffer WRITABLE into the registered
// WM's root — the compose-N target. Seat-only and full-frame only: the kernel
// refuses a non-seat with EACCES, a non-full-frame length with EINVAL, and an
// unarmed framebuffer with ENXIO. Off the guest this degrades to -ENOSYS.
func MmapScanout() ([]byte, error) {
	return MmapHint(M33ScanoutTag, ScanoutFbBytes,
		ProtRead|ProtWrite, MapAnonymous|M33MapShared)
}

// MmapWindowSurface binds the caller's window backing buffer (M33 SB3).
// The kernel validates the window owner and size and selects the actual VA.
// Unlike MmapHint this tag has a window id in its low bits, so it must not
// pass the ordinary page-alignment check.
func MmapWindowSurface(id, size int) ([]byte, error) {
	if id <= 0 || id > 255 || size <= 0 || size > int(^uint(0)>>1)-(PageSize-1) {
		return nil, errno(ErrEINVAL)
	}
	n := (size + PageSize - 1) &^ (PageSize - 1)
	r := syscall4(SlotMmap, M33WindowTag|uintptr(id), uintptr(n),
		uintptr(ProtRead|ProtWrite), uintptr(MapAnonymous|M33MapShared))
	if r < 0 {
		return nil, errno(-r)
	}
	return mmapSlice(uintptr(r), n), nil
}

// ErrnoOf maps a negative syscall result to the kernel's own error magnitude
// (for callers that branch on it), or 0 when the result is not an error.
func ErrnoOf(r int64) int64 {
	if r < 0 {
		return -r
	}
	return 0
}

// ---------------------------------------------------------------------------
// M57b (issue #1317): the window-lifecycle half of the seat seam.
// ---------------------------------------------------------------------------

// Slot-65 window-lifecycle subcommands (kernel/src/wm_server.zig).
const (
	// WmctlSetWindowCmd is slot-65 subcommand 2: the WM proposes a window
	// rect (a1 = x|(y<<16), a2 = w|(h<<16)) and/or submits a chrome
	// descriptor (a4 = ptr, a5 = len; len 0 = chrome unchanged). The kernel
	// applies the rect through its own clamped layout primitive — WM
	// proposes, kernel clamps to the scanout.
	WmctlSetWindowCmd uint64 = 2
	// WmctlWinCloseCmd is slot-65 subcommand 13: the WM asks the kernel to
	// release a user window. The kernel runs its OWN release primitive, so
	// the OWNER receives the real WIN_CLOSE event (kind 8).
	WmctlWinCloseCmd uint64 = 13
	// ChromeDescBytes is the frozen v1 chrome-descriptor wire length (the
	// kernel refuses any other non-zero length).
	ChromeDescBytes = 40
)

// WmctlSetWindowRect proposes the rect (x,y,w,h) for window id. The kernel
// clamps it to the scanout and pushes WIN_RESIZE to the owner when the
// clamped size differs. Returns 0, or -EINVAL/-EACCES/-ENOSYS.
func WmctlSetWindowRect(id, x, y, w, h uint32) int64 {
	xy := (x & 0xffff) | (y&0xffff)<<16
	wh := (w & 0xffff) | (h&0xffff)<<16
	// Six arguments, not four: the kernel reads the chrome pointer/length out
	// of a4/a5 and refuses a non-zero `len` that is not a frozen descriptor
	// length, so a4/a5 MUST be explicitly zero on a geometry-only call.
	return syscall6(SlotWmctl, uintptr(WmctlSetWindowCmd), uintptr(id), uintptr(xy), uintptr(wh), 0, 0)
}

// WmctlSetWindowChrome submits a 40-byte chrome descriptor for window id.
// desc MUST live in a mapped page: the kernel reads it with uaccess.copy_in
// through the caller's own page map, and a Go heap/stack buffer is not
// guaranteed mapped for EL1 (ADR 0026 D8). MmapHint a page and write it there.
func WmctlSetWindowChrome(id uint32, desc []byte) int64 {
	if len(desc) != ChromeDescBytes {
		return -ErrEINVAL
	}
	return syscall6(SlotWmctl, uintptr(WmctlSetWindowCmd), uintptr(id), 0, 0,
		uintptr(unsafe.Pointer(&desc[0])), uintptr(len(desc)))
}

// WmctlWinClose releases window id through the WM seam (slot 65 cmd 13). The
// owning process receives WIN_CLOSE. Returns 0, or -EINVAL/-EACCES/-ENOSYS.
func WmctlWinClose(id uint32) int64 {
	return syscall2(SlotWmctl, uintptr(WmctlWinCloseCmd), uintptr(id))
}

// WmctlTaskbarClickCmd is slot-65 subcommand 12: the WM asks the kernel for
// the taskbar-click decision on window id — the kernel's own focus+raise
// primitive, so the owner receives the real WIN_FOCUS event.
const WmctlTaskbarClickCmd uint64 = 12

// WmctlTaskbarClick focuses and raises window id through the WM seam (slot 65
// cmd 12). The owning process receives WIN_FOCUS. Returns 0, or
// -EINVAL/-EACCES/-ENOSYS.
func WmctlTaskbarClick(id uint32) int64 {
	return syscall2(SlotWmctl, uintptr(WmctlTaskbarClickCmd), uintptr(id))
}
