// Package abi is the gsport adapter layer's one injectable seam: the table
// of virelai/vi entry points (ADR 0007 syscall slots) every adapter speaks.
//
// Guest builds resolve each entry to the real vi call — the variable holds
// the function value, nothing is indirected through a second mechanism.
// Host tests get slot evidence two ways:
//
//   - swap a table entry for a recorder to prove WHICH entry point an
//     adapter path uses (each is annotated with its slot), or
//   - install vi.SetSyscallHookForTest to record the actual slot numbers
//     the real vi wrappers emit — the stronger proof, since it observes
//     the SVC the kernel would see.
//
// The gsport guard (gsport/guard) refuses a virelai/vi import anywhere else
// in the adapter tree, so no adapter can bypass this seam.
package abi

import (
	"errors"
	"fmt"
	"io/fs"

	"virelai/vi"
)

// Kernel constants the adapters need, re-exported so no adapter imports
// virelai/vi itself — the guard refuses that import outside this package.
const (
	// ADR 0010 file-channel flags.
	ModeRead   = vi.ModeRead
	ModeWrite  = vi.ModeWrite
	ModeCreate = vi.ModeCreate
	ModeAppend = vi.ModeAppend
	ModeDir    = vi.ModeDir

	MaxDirEntries = vi.MaxDirEntries // slot-27 window

	ExecMaxArgs = vi.ExecMaxArgs // slot-28 argv bound
	ExecArgMax  = vi.ExecArgMax  // slot-28 per-arg byte bound

	ProbeAbsent  = vi.ProbeAbsent
	ProbeRunning = vi.ProbeRunning
	ProbeExited  = vi.ProbeExited
)

// ADR 0007 D3 errno magnitudes.
const (
	ErrEINVAL       = vi.ErrEINVAL
	ErrEBADF        = vi.ErrEBADF
	ErrEFAULT       = vi.ErrEFAULT
	ErrENOSYS       = vi.ErrENOSYS
	ErrENOSPC       = vi.ErrENOSPC
	ErrENOENT       = vi.ErrENOENT
	ErrEACCES       = vi.ErrEACCES
	ErrENAMETOOLONG = vi.ErrENAMETOOLONG
	ErrENXIO        = vi.ErrENXIO
	ErrENOMEM       = vi.ErrENOMEM
	ErrEAGAIN       = vi.ErrEAGAIN
	ErrETIMEDOUT    = vi.ErrETIMEDOUT

	ErrFileNotFound   = vi.ErrFileNotFound
	ErrFileIsDir      = vi.ErrFileIsDir
	ErrFileExists     = vi.ErrFileExists
	ErrFileHandleFull = vi.ErrFileHandleFull
)

// DirEntry is the slot-27 row shape.
type DirEntry = vi.DirEntry

// The vi entry points, annotated with the ADR 0007 slot each one issues.
// A "slots" line naming several numbers means the wrapper is composite —
// the kernel still sees exactly those slots.
var (
	// Process table + lifecycle.
	Procs = vi.Procs // slot 7  sys_procs
	Probe = vi.Probe // slot 7  sys_procs (one registry scan)
	Wait  = vi.Wait  // slots 7+4 sys_procs polled between sys_sleep ticks
	Exec  = vi.Exec  // slot 28 sys_exec
	Kill  = vi.Kill  // slot 29 sys_kill

	// File primitives (ADR 0010).
	FileOpen      = vi.FileOpen      // slot 23 sys_file_open
	FileRead      = vi.FileRead      // slot 24 sys_file_read
	FileWrite     = vi.FileWrite     // slot 25 sys_file_write
	FileWriteAll  = vi.FileWriteAll  // slot 25 sys_file_write, chunked
	FileClose     = vi.FileClose     // slot 26 sys_file_close
	DirList       = vi.DirList       // slot 27 sys_dir_list
	FileDelete    = vi.FileDelete    // slot 34 sys_file_delete
	FileRename    = vi.FileRename    // slot 35 sys_file_rename
	FileTruncate  = vi.FileTruncate  // slot 36 sys_file_truncate
	FileSync      = vi.FileSync      // slot 77 sys_file_sync
	FileExists    = vi.FileExists    // slot 23 sys_file_open (probe open)
	ReadFileAll   = vi.ReadFileAll   // slots 23+24+26
	WriteFileSafe = vi.WriteFileSafe // slots 23+25+77+26+34+35 (safe publish)
	FileAppend    = vi.FileAppend    // slots 23+25+26

	// Clock: wall time is slot 66 sys_time; the monotonic clock is the
	// runtime's CNTPCT_EL0 read behind vi.Nanos — no slot at all.
	Time  = vi.Time  // slot 66 sys_time
	Nanos = vi.Nanos // CNTPCT_EL0, not a syscall

	// Scheduler cooperation.
	Sleep = vi.Sleep // slot 4 sys_sleep
	Yield = vi.Yield // slot 2 sys_yield
	Exit  = vi.Exit  // slot 3 sys_exit

	// M95c (#2011): the terminal/window entries gsport/tty speaks — the
	// bound window tty, the ADR 0009 event queue and the zoom rung.
	PollEventRaw = vi.PollEventRaw // slot 21 sys_poll_event
	TtyAttach    = vi.TtyAttach    // slot 67 sys_tty_attach (detach/serial/window/net by arg)
	// TerminalCell issues no event/window slot of its own: it re-derives the
	// font_size rung from /host/SETTINGS.TXT (slots 23+24+26 inside
	// vi.ReadFileAll) — the same read the kernel's own reflow used.
	TerminalCell = vi.TerminalCell
)

// TtyAttachWindow binds the caller's own .user window as its controlling
// terminal — slot 67 with the TtyWindow (2) front-end selector.
func TtyAttachWindow(windowID int) int64 {
	return vi.TtyAttachWindow(windowID)
}

// Event is the ADR 0009 queue row.
type Event = vi.Event

// Event kinds the terminal adapter consumes (ADR 0009).
const (
	EvWinClose  = vi.EvWinClose  // kind 8
	EvWinResize = vi.EvWinResize // kind 10
)

// TtyDetach is the slot-67 selector that releases the controlling terminal.
const TtyDetach = vi.TtyDetach

// Error is a kernel refusal surfaced as a Go error: Op and Path name the
// operation, Code is the positive ADR 0007 errno magnitude (vi.Err*).
type Error struct {
	Op   string
	Path string
	Code int64
}

func (e *Error) Error() string {
	return e.Op + " " + e.Path + ": " + errnoName(e.Code)
}

// Is maps the kernel's errno magnitudes onto the io/fs sentinels a caller
// legitimately branches on. ErrFileExists (-9) has no fs sentinel, so it
// maps to fs.ErrExist by name.
func (e *Error) Is(target error) bool {
	switch e.Code {
	case ErrENOENT:
		return target == fs.ErrNotExist
	case ErrEACCES:
		return target == fs.ErrPermission
	case ErrEINVAL:
		return target == fs.ErrInvalid
	case -ErrFileExists:
		return target == fs.ErrExist
	}
	return false
}

// Check turns a vi raw result into an error: negative r is a kernel errno.
func Check(op, path string, r int64) error {
	if r >= 0 {
		return nil
	}
	return &Error{Op: op, Path: path, Code: -r}
}

// CheckExists is Check for operations whose "absent" answer is an expected
// branch, returning fs.ErrNotExist-shaped errors the caller can Is().
var ErrNotExist = fs.ErrNotExist

// errnoName gives the kernel errno magnitudes their table names; the table
// is vi.go's ADR 0007 D3 ordering, not POSIX's.
func errnoName(code int64) string {
	switch code {
	case ErrEINVAL:
		return "EINVAL"
	case ErrEBADF:
		return "EBADF"
	case ErrEFAULT:
		return "EFAULT"
	case ErrENOSYS:
		return "ENOSYS"
	case ErrENOSPC:
		return "ENOSPC"
	case ErrENOENT:
		return "ENOENT"
	case ErrEACCES:
		return "EACCES"
	case ErrENAMETOOLONG:
		return "ENAMETOOLONG"
	case ErrENXIO:
		return "ENXIO/EEXIST" // magnitude 9 is ENXIO in the device domains, the file-domain EEXIST row in ADR 0010
	case ErrENOMEM:
		return "ENOMEM"
	case ErrEAGAIN:
		return "EAGAIN"
	case ErrETIMEDOUT:
		return "ETIMEDOUT"
	}
	return fmt.Sprintf("errno %d", code)
}

// IsENOSYS reports whether err is a kernel ENOSYS refusal — the answer an
// adapter must never pass through silently.
func IsENOSYS(err error) bool {
	var e *Error
	return errors.As(err, &e) && e.Code == ErrENOSYS
}
