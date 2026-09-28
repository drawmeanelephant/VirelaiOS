// Package vsys is the fresh GOOS=virelai syscall/os/net binding (issue #1163,
// phase 2; ADR 0026 D2: "the runtime speaks ADR 0007 directly").
//
// It is authored from the ABI, not ported from Linux: the slot numbers come
// from kernel/src/syscall.zig, an error is the NEGATED magnitude of the
// kernel's ErrorCode enum, and there is no fd table, no errno translation
// layer, no libc, no POSIX and no cgo. A virelai program gets os.File-shaped
// and net.Conn-shaped values from here.
//
// CONCURRENCY: VirelaiOS has ONE TCP socket per process, so Dial/Close are
// not safe for concurrent use — there is no second socket for a lock to hand
// out. One Conn at a time, from one goroutine, is the whole contract.
//
// Host/guest split (the same shape user/go/vi uses): the pure logic —
// path validation, IP-literal parsing, the Conn state machine, error
// mapping — lives in this file and file.go/net.go and is exercised by
// `go test ./...` on the host with an injected syscall. Only
// sys_guest.go/vsys_arm64.s (build tag virelai) touch the kernel.
package vsys

// Slot numbers (ADR 0007; the single source is kernel/src/syscall.zig, and
// user/go/vi/vi.go mirrors the same table).
const (
	SlotPollEvent  uintptr = 21 // sys_poll_event(buf)
	SlotWaitEvent  uintptr = 22 // sys_wait_event(buf)
	SlotFileOpen   uintptr = 23 // sys_file_open(path, len, flags)
	SlotFileRead   uintptr = 24 // sys_file_read(fd, buf, count)
	SlotFileWrite  uintptr = 25 // sys_file_write(fd, buf, count)
	SlotFileClose  uintptr = 26 // sys_file_close(fd)
	SlotDirList    uintptr = 27 // sys_dir_list(path, len, buf, max)
	SlotTCPConnect uintptr = 30 // sys_tcp_connect(ip, port)
	SlotTCPSend    uintptr = 31 // sys_tcp_send(buf, len)
	SlotTCPRecv    uintptr = 32 // sys_tcp_recv(buf, max)
	SlotTCPClose   uintptr = 33 // sys_tcp_close()
	SlotSockReady  uintptr = 76 // sys_sock_ready(op, want, timeout_ns) — phase 2
)

// Kernel error magnitudes (ADR 0007 D3: the kernel returns the negation).
const (
	ErrEINVAL       int64 = 1
	ErrEBADF        int64 = 2
	ErrEFAULT       int64 = 3
	ErrENOSYS       int64 = 4
	ErrENOSPC       int64 = 5
	ErrENOENT       int64 = 6
	ErrEACCES       int64 = 7
	ErrENAMETOOLONG int64 = 8
	ErrENXIO        int64 = 9
	ErrENOMEM       int64 = 10
	ErrEAGAIN       int64 = 11 // ADR 0027 amendment: transient retry
	ErrETIMEDOUT    int64 = 12 // ADR 0027 amendment: deadline expiry
)

// File-open mode flags (kernel/src/file_table.zig, mirrored by
// user/go/vi). flags == 0 is NOT "read" — the kernel refuses it (EINVAL), so
// a reader must ask for ModeRead explicitly.
const (
	ModeRead   uint32 = 0x0001
	ModeWrite  uint32 = 0x0002
	ModeCreate uint32 = 0x0004
	ModeAppend uint32 = 0x0008
	ModeDir    uint32 = 0x0010
)

// ABI bounds, mirrored from the kernel (kernel/src/file_table.zig).
const (
	// MaxPathLen is file_table.max_path_len.
	MaxPathLen = 64
	// MaxHandlesPerProcess is file_table.max_handles_per_process.
	MaxHandlesPerProcess = 8
	// TCPPayloadMax is tcp.payload_max: the largest single send/recv.
	TCPPayloadMax = 1460
)

// Errno is the kernel's error magnitude as a Go error.
type Errno int64

func (e Errno) Error() string {
	if e == 0 {
		return "vsys: ok"
	}
	return "vsys: kernel error " + Itoa64(int64(e))
}

// Sentinel errors raised by this package before any syscall.
var (
	// ErrInvalidPath is an empty or dot-traversal path.
	ErrInvalidPath = errString("vsys: invalid path")
	// ErrNameTooLong is a path longer than MaxPathLen.
	ErrNameTooLong = errString("vsys: path exceeds 64 bytes")
	// ErrNotIPLiteral is a Dial argument that is not an IPv4 literal.
	ErrNotIPLiteral = errString("vsys: not an IPv4 literal (DNS is out of scope)")
	// ErrConnBusy is a second simultaneous Dial (one socket per process).
	ErrConnBusy = errString("vsys: one TCP socket per process — Close before Dial")
	// ErrConnClosed is Read/Write on a closed Conn.
	ErrConnClosed = errString("vsys: connection closed")
	// ErrPeerClosed is a Read that observed the peer's FIN/RST: fail closed.
	ErrPeerClosed = errString("vsys: peer closed the connection")
	// ErrShortWrite is a Write truncated at TCPPayloadMax (the caller loops).
	ErrShortWrite = errString("vsys: short write (payload_max)")
)

type errString string

func (e errString) Error() string { return string(e) }

// syscallFn is the raw 4-argument SVC gateway. It is a variable so the host
// tests can inject a fake kernel; sys_guest.go/vsys_arm64.s supply the real
// one under the virelai build tag, sys_host.go an -ENOSYS stub elsewhere.
var syscallFn = rawSyscall

// syscallResult turns a raw kernel return into (value, error).
func syscallResult(r int64) (int64, error) {
	if r < 0 {
		return 0, Errno(-r)
	}
	return r, nil
}
