package vi

// M56a (issue #1311): the IPC mailbox + process-table wire over ADR 0007
// slots 5/6 (`sys_ipc_send`/`sys_ipc_recv`) and slot 7 (`sys_procs`).
//
// The kernel is the single source of truth for every bound below
// (kernel/src/syscall.zig):
//   - mailbox.message_max = 64: a send longer than this is TRUNCATED to the
//     slot bound and a recv whose `max` exceeds it is CLAMPED. Both are
//     documented truncations, not errors.
//   - the ring holds max_messages = 8 slots per process; a full ring refuses
//     the send with ENOSPC BEFORE any bytes are copied, so a refusal never
//     touches user memory.
//   - a send to a target outside the registry, to a free slot, or to an
//     EXITED process is EINVAL (a mailbox is reachable only while live).
//   - a bad user pointer is EFAULT; on recv the message stays QUEUED.
//   - recv always reads the CALLER's own ring (the kernel resolves the pid
//     from the current task), so there is no self-pid argument on the wire.
//
// On the host every call degrades to -ENOSYS (vi_host.go) unless a test
// installs syscallHook — the same inject-a-fake-kernel seam the TCP path
// uses. That is what keeps these paths unit-testable off the guest.

import "unsafe"

const (
	// MailboxMessageMax is the per-message byte bound (mailbox.zig
	// `message_max`). Sends truncate to it; recvs clamp to it.
	MailboxMessageMax = 64
	// MailboxMaxMessages is the per-process ring depth (mailbox.zig
	// `max_messages`): the slot a full-ring ENOSPC guards.
	MailboxMaxMessages = 8
	// ProcRowSize is the fixed width of one `sys_procs` snapshot row
	// (process.zig `snapshot_row_bytes`).
	ProcRowSize = 40
	// ProcNameBytes is the NUL-padded name field width inside a row.
	ProcNameBytes = 16
)

// Process state codes as marshalled by the kernel snapshot (process.zig
// `State`): a `free` descriptor is never emitted.
const (
	ProcCreated uint64 = 1
	ProcRunning uint64 = 2
	ProcExited  uint64 = 3
)

// ProcRow is one decoded `sys_procs` row: u64 pid@0, u64 state@8, u64
// exit_status@16, name[16] NUL-padded@24 (all little-endian).
type ProcRow struct {
	PID        uint64
	State      uint64
	ExitStatus uint64
	NameBuf    [ProcNameBytes]byte
}

// Name returns the row's process name with the NUL padding trimmed.
func (r ProcRow) Name() string {
	n := 0
	for n < len(r.NameBuf) && r.NameBuf[n] != 0 {
		n++
	}
	return string(r.NameBuf[:n])
}

// IpcSend copies up to MailboxMessageMax bytes of b into process `target`'s
// mailbox ring (slot 5). It returns the number of bytes sent (>0) or the raw
// negative kernel error: -EINVAL (target not a live process), -EFAULT (bad
// source pointer), -ENOSPC (ring full). A zero-length send is a no-op
// returning 0.
func IpcSend(target uint32, b []byte) int64 {
	if len(b) == 0 {
		return 0
	}
	if len(b) > MailboxMessageMax {
		b = b[:MailboxMessageMax]
	}
	return svc3(SlotIPCSend, uintptr(target), uintptr(unsafe.Pointer(&b[0])), uintptr(len(b)))
}

// IpcRecv copies the caller's own oldest mailbox message into buf (slot 6),
// returning (n, raw). n == 0 with raw >= 0 means the queue was empty; a
// negative raw is a kernel error (-ENOSYS off-guest, -EFAULT when the
// destination pointer is bad — the kernel then leaves the message queued).
// len(buf) > MailboxMessageMax is clamped to the bound.
func IpcRecv(buf []byte) (int, int64) {
	if len(buf) == 0 {
		return 0, 0
	}
	if len(buf) > MailboxMessageMax {
		buf = buf[:MailboxMessageMax]
	}
	r := svc2(SlotIPCRecv, uintptr(unsafe.Pointer(&buf[0])), uintptr(len(buf)))
	if r < 0 {
		return 0, r
	}
	return int(r), r
}

// Procs reads up to len(dst) rows of the process table (slot 7). It returns
// the number of rows written and the raw result. `sys_procs` floors the
// request to whole rows and returns the ROW COUNT, not a byte count. Off the
// guest this is (0, -ENOSYS).
func Procs(dst []ProcRow) (int, int64) {
	if len(dst) == 0 {
		return 0, 0
	}
	buf := make([]byte, len(dst)*ProcRowSize)
	r := svc2(SlotProcs, uintptr(unsafe.Pointer(&buf[0])), uintptr(len(buf)))
	if r < 0 {
		return 0, r
	}
	rows := int(r)
	if rows > len(dst) {
		rows = len(dst)
	}
	for i := 0; i < rows; i++ {
		off := i * ProcRowSize
		dst[i] = ProcRow{
			PID:        getU64(buf[off:]),
			State:      getU64(buf[off+8:]),
			ExitStatus: getU64(buf[off+16:]),
		}
		copy(dst[i].NameBuf[:], buf[off+24:off+24+ProcNameBytes])
	}
	return rows, r
}

// WmSeat is a resolved WM seat: the registered WM process id and THIS
// process's own id, both found in one `sys_procs` scan (acks route by pid).
type WmSeat struct {
	WM   uint32
	Self uint32
	// Go is true when the resolved seat is the Go WM (GOTABWM.ELF): the seat
	// that speaks the M97g-F2 authenticated bind protocol, where requests
	// carry a session token and acks arrive Pad-marked. The Zig seats answer
	// bare applied acks and are served by the legacy path.
	Go bool
}

// wmGoSeatName is the seat process name that runs the authenticated WM_RPC
// protocol (M97g-F2 #2080).
const wmGoSeatName = "GOTABWM.ELF"

// WMProcNames are the two seats an app's WM_RPC client resolves: the floating
// WND.BIN desktop and the tabbed TABWM.BIN desktop. At most ONE is registered
// at a time (sys_wmctl REGISTER is one-seat), so matching either is safe.
var WMProcNames = [...]string{"WND.BIN", "TABWM.BIN", wmGoSeatName}

// wmPeersScanAttempts bounds the `sys_procs` re-reads (M56d #1315). It is
// kept at the historical 8, but only a persistently suspect scan can spend
// them now: an intact scan returns on the spot.
const wmPeersScanAttempts = 8

// WmPeers finds the WM pid and this process's pid in a single `sys_procs`
// scan (the M56a `wm_peers` helper). A zero field means "not found". Only
// RUNNING rows with a non-empty name match.
//
// A scan that reads back INTACT (no RUNNING row whose name bytes came back
// zeroed) and knows this process is TRUSTED: a missing WM then means there is
// genuinely no seat, and the answer stands. Only a SUSPECT scan is retried --
// on the guest `sys_procs` intermittently returns its row count while the name
// bytes read back zeroed (observed on VZ, alternating scans; M56d #1315), and
// the row that got zeroed may be the WM's own, so a suspect scan cannot answer
// "no seat" at all.
//
// That distinction is not cosmetic. Between attempts the loop yields, and on
// VZ a yield parks the caller until the next scheduler tick, which is a full
// second (kernel/src/timer.zig `period_ns` = 1e9). Retrying the no-seat answer
// therefore cost ~7 s per call (#1586) -- twice per browser boot, which is
// most of the 10 s that put WEB over its startup budget on EVERY boot while
// only three boots asserted it. A suspect scan gets one immediate re-read
// first, because the flake alternates per scan; only a persistent one pays a
// tick. On the host (every syscall -ENOSYS) the loop is a cheap no-op that
// still returns the zero seat.
func WmPeers(selfName string) WmSeat {
	var out WmSeat
	if selfName == "" {
		return out
	}
	rows := make([]ProcRow, 64)
	for attempt := 0; attempt < wmPeersScanAttempts; attempt++ {
		out = WmSeat{}
		suspect := false
		if n, r := Procs(rows); r > 0 && n > 0 {
			for i := 0; i < n; i++ {
				if rows[i].State != ProcRunning {
					continue
				}
				name := rows[i].Name()
				if name == "" {
					// A RUNNING row with no readable name: the M56d flake.
					// This row could BE the WM seat, so this scan cannot
					// answer "no seat".
					suspect = true
					continue
				}
				if out.Self == 0 && name == selfName {
					out.Self = uint32(rows[i].PID)
				}
				if out.WM == 0 {
					for _, w := range WMProcNames {
						if name == w {
							out.WM = uint32(rows[i].PID)
							out.Go = name == wmGoSeatName
							break
						}
					}
				}
			}
		}
		// Both peers resolved (the seat answered for itself), or an intact
		// scan that knows this process (so the seat answer is the truth).
		if out.Self != 0 && (out.WM != 0 || !suspect) {
			return out
		}
		if attempt == 0 {
			// The flake alternates per scan, so one immediate re-read lands
			// an intact scan far more often than a tick-long sleep would.
			continue
		}
		Yield()
	}
	return out
}

// --- wire helpers (shared with wmclient.go) --------------------------------

func getU16(b []byte) uint16 { return uint16(b[0]) | uint16(b[1])<<8 }

func putU16(b []byte, v uint16) {
	b[0] = byte(v)
	b[1] = byte(v >> 8)
}

func getU32(b []byte) uint32 {
	return uint32(b[0]) | uint32(b[1])<<8 | uint32(b[2])<<16 | uint32(b[3])<<24
}

func getU64(b []byte) uint64 {
	return uint64(b[0]) | uint64(b[1])<<8 | uint64(b[2])<<16 | uint64(b[3])<<24 |
		uint64(b[4])<<32 | uint64(b[5])<<40 | uint64(b[6])<<48 | uint64(b[7])<<56
}

func putU64(b []byte, v uint64) {
	for i := 0; i < 8; i++ {
		b[i] = byte(v >> (8 * i))
	}
}
