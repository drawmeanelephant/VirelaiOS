package vi

import (
	"bytes"
	"encoding/binary"
	"strings"
	"testing"
	"unsafe"
)

func TestNowUsesSlot66AndPreservesMissingEpoch(t *testing.T) {
	prev := SetSyscallHookForTest(func(num uintptr, a0, a1, a2, a3 uintptr) int64 {
		if num != SlotTime || a0 != 0 || a1 != 0 || a2 != 0 || a3 != 0 {
			t.Fatalf("Now syscall = (%d,%d,%d,%d,%d), want slot 66 with no args", num, a0, a1, a2, a3)
		}
		return 1_789_043_696
	})
	defer SetSyscallHookForTest(prev)
	if got := Now(); got != 1_789_043_696 {
		t.Fatalf("Now() = %d", got)
	}
	SetSyscallHookForTest(func(num uintptr, a0, a1, a2, a3 uintptr) int64 { return -ErrENOSYS })
	if got := Now(); got != -ErrENOSYS {
		t.Fatalf("Now() without firmware epoch = %d, want -ENOSYS", got)
	}
}

func TestTrashPathAndValidation(t *testing.T) {
	if got, want := TrashItemPath("0123456789abcdef"), TrashDir+"/0123456789abcdef.item"; got != want {
		t.Fatalf("TrashItemPath = %q, want %q", got, want)
	}
	for _, id := range []string{"", "../bad", "0123456789ABCDEG", "0123456789abcde"} {
		if got := TrashItemPath(id); got != "" {
			t.Errorf("TrashItemPath(%q) = %q, want empty", id, got)
		}
	}
	calls := 0
	prev := SetSyscallHookForTest(func(num uintptr, a0, a1, a2, a3 uintptr) int64 {
		calls++
		return -ErrENOSYS
	})
	defer SetSyscallHookForTest(prev)
	if _, rc := TrashDelete("/tmp/not-on-share"); rc != -ErrEINVAL {
		t.Fatalf("outside-share delete rc=%d, want -EINVAL", rc)
	}
	if _, _, _, rc := TrashRead("../bad"); rc != -ErrEINVAL {
		t.Fatalf("invalid trash id read rc=%d, want -EINVAL", rc)
	}
	if _, rc := TrashExpire(-1); rc != -ErrEINVAL {
		t.Fatalf("negative expiry time rc=%d, want -EINVAL", rc)
	}
	if calls != 0 {
		t.Fatalf("invalid inputs made %d syscalls before refusing", calls)
	}
}

func TestTrashOperationsFailClosedOnHost(t *testing.T) {
	if _, rc := TrashDelete("/host/FM/NOTE.TXT"); rc != -ErrENOSYS {
		t.Fatalf("host TrashDelete rc=%d, want -ENOSYS", rc)
	}
	if _, _, _, rc := TrashRead("0123456789abcdef"); rc != -ErrENOSYS {
		t.Fatalf("host TrashRead rc=%d, want -ENOSYS", rc)
	}
	if _, _, rc := TrashRestoreLatest(); rc != -ErrENOSYS {
		t.Fatalf("host TrashRestoreLatest rc=%d, want -ENOSYS", rc)
	}
	if _, rc := TrashExpire(0); rc != -ErrENOSYS {
		t.Fatalf("host TrashExpire rc=%d, want -ENOSYS", rc)
	}
}

type trashTestFS struct {
	files   map[string][]byte
	dirs    map[string]bool
	handles map[uint64]string
	cursors map[uint64]int
	next    uint64
	random  uint64
}

func newTrashTestFS() *trashTestFS {
	return &trashTestFS{
		files:   map[string][]byte{"/host/FM/NOTE.TXT": []byte("byte-exact trash payload\n")},
		dirs:    map[string]bool{"/host": true, "/host/FM": true},
		handles: map[uint64]string{},
		cursors: map[uint64]int{},
	}
}

func (f *trashTestFS) path(ptr, n uintptr) string {
	return string(unsafe.Slice((*byte)(unsafe.Pointer(ptr)), int(n)))
}

func (f *trashTestFS) syscall(num uintptr, a0, a1, a2, a3 uintptr) int64 {
	switch num {
	case SlotTime:
		return 1_000
	case SlotGetRandom:
		f.random++
		binary.LittleEndian.PutUint64(unsafe.Slice((*byte)(unsafe.Pointer(a0)), int(a1)), f.random)
		return int64(a1)
	case SlotFileOpen:
		path, flags := f.path(a0, a1), uint32(a2)
		if flags&ModeDir != 0 {
			if f.dirs[path] {
				return ErrFileExists
			}
			f.dirs[path] = true
			return 0
		}
		if flags&ModeWrite != 0 {
			if f.dirs[path] {
				return ErrFileIsDir
			}
			if flags&ModeAppend == 0 {
				f.files[path] = nil
			}
			f.next++
			f.handles[f.next] = path
			f.cursors[f.next] = len(f.files[path])
			return int64(f.next)
		}
		if flags&ModeRead != 0 {
			if _, ok := f.files[path]; !ok {
				return ErrFileNotFound
			}
			f.next++
			f.handles[f.next] = path
			f.cursors[f.next] = 0
			return int64(f.next)
		}
		return -ErrEINVAL
	case SlotFileRead:
		path, ok := f.handles[uint64(a0)]
		if !ok {
			return -ErrEBADF
		}
		body := f.files[path]
		cursor := f.cursors[uint64(a0)]
		n := copy(unsafe.Slice((*byte)(unsafe.Pointer(a1)), int(a2)), body[cursor:])
		f.cursors[uint64(a0)] += n
		return int64(n)
	case SlotFileWrite:
		path, ok := f.handles[uint64(a0)]
		if !ok {
			return -ErrEBADF
		}
		data := unsafe.Slice((*byte)(unsafe.Pointer(a1)), int(a2))
		cursor := f.cursors[uint64(a0)]
		end := cursor + len(data)
		if end > len(f.files[path]) {
			f.files[path] = append(f.files[path], make([]byte, end-len(f.files[path]))...)
		}
		copy(f.files[path][cursor:end], data)
		f.cursors[uint64(a0)] = end
		return int64(len(data))
	case SlotFileClose:
		delete(f.handles, uint64(a0))
		delete(f.cursors, uint64(a0))
		return 0
	case SlotFileSync:
		if _, ok := f.handles[uint64(a0)]; !ok {
			return -ErrEBADF
		}
		return 0
	case SlotFileDelete:
		path := f.path(a0, a1)
		if _, ok := f.files[path]; !ok {
			return ErrFileNotFound
		}
		delete(f.files, path)
		return 0
	case SlotFileRename:
		from, to := f.path(a0, a1), f.path(a2, a3)
		if _, exists := f.files[to]; exists {
			return ErrFileExists
		}
		body, exists := f.files[from]
		if !exists {
			return ErrFileNotFound
		}
		f.files[to] = body
		delete(f.files, from)
		return 0
	case SlotDirList:
		path := f.path(a0, a1)
		if !f.dirs[path] {
			return ErrFileNotFound
		}
		rows := unsafe.Slice((*DirEntry)(unsafe.Pointer(a2)), int(a3))
		prefix, count := path+"/", 0
		for file, body := range f.files {
			rest, ok := strings.CutPrefix(file, prefix)
			if !ok || rest == "" || strings.Contains(rest, "/") || count >= len(rows) {
				continue
			}
			copy(rows[count].Name[:], rest)
			rows[count].Size = uint32(len(body))
			count++
		}
		for dir := range f.dirs {
			rest, ok := strings.CutPrefix(dir, prefix)
			if !ok || rest == "" || strings.Contains(rest, "/") || count >= len(rows) {
				continue
			}
			copy(rows[count].Name[:], rest)
			rows[count].IsDir = 1
			count++
		}
		return int64(count)
	}
	return -ErrENOSYS
}

func TestTrashDeleteRestoreAndExpire(t *testing.T) {
	fs := newTrashTestFS()
	prev := SetSyscallHookForTest(fs.syscall)
	defer SetSyscallHookForTest(prev)

	source := "/host/FM/NOTE.TXT"
	want := []byte("byte-exact trash payload\n")
	id, rc := TrashDelete(source)
	if rc < 0 || id == "" {
		t.Fatalf("TrashDelete = %q, %d", id, rc)
	}
	if _, ok := fs.files[source]; ok {
		t.Fatal("source survived TrashDelete")
	}
	path, body, stamp, rc := TrashRead(id)
	if rc != int64(len(want)) || path != source || stamp != 1_000 || !bytes.Equal(body, want) {
		t.Fatalf("TrashRead = %q, %q, %d, %d", path, body, stamp, rc)
	}
	restored, restoredID, rc := TrashRestoreLatest()
	if rc != 0 || restored != source || restoredID != id || !bytes.Equal(fs.files[source], want) {
		t.Fatalf("TrashRestoreLatest = %q, %q, %d, file=%q", restored, restoredID, rc, fs.files[source])
	}
	if _, _, _, rc := TrashRead(id); rc != ErrFileNotFound {
		t.Fatalf("restored trash receipt rc=%d, want ENOENT", rc)
	}
	recent, rc := ReadFileAll(RecentLogPath, maxRecentBytes)
	if rc < 0 || !strings.Contains(string(recent), "|delete|"+id+"|") ||
		!strings.Contains(string(recent), "|restore|"+id+"|") {
		t.Fatalf("RECENT log rc=%d bytes=%q", rc, recent)
	}

	secondID, rc := TrashDelete(source)
	if rc < 0 || secondID == id {
		t.Fatalf("second TrashDelete = %q, %d (first id %q)", secondID, rc, id)
	}
	expired, rc := TrashExpire(stamp + TrashRetentionSeconds)
	if rc < 0 || expired != 1 {
		t.Fatalf("TrashExpire = %d, %d, want 1, 0", expired, rc)
	}
	if _, _, _, rc := TrashRead(secondID); rc != ErrFileNotFound {
		t.Fatalf("expired trash item rc=%d, want ENOENT", rc)
	}
}

func TestTrashRestoreNeverOverwrites(t *testing.T) {
	fs := newTrashTestFS()
	prev := SetSyscallHookForTest(fs.syscall)
	defer SetSyscallHookForTest(prev)
	source := "/host/FM/NOTE.TXT"
	if _, rc := TrashDelete(source); rc < 0 {
		t.Fatalf("TrashDelete rc=%d", rc)
	}
	fs.files[source] = []byte("new contents")
	if _, _, rc := TrashRestoreLatest(); rc != ErrFileExists {
		t.Fatalf("restore over existing path rc=%d, want EEXIST", rc)
	}
	if got := string(fs.files[source]); got != "new contents" {
		t.Fatalf("existing file overwritten: %q", got)
	}
}

// The slot table is the browser's contract with the kernel (ADR 0007); these
// numbers are pinned so a drift fails the host suite instead of the VM gate.
func TestSlotNumbers(t *testing.T) {
	want := map[uintptr]string{
		1: "write", 2: "yield", 3: "exit", 4: "sleep",
		5: "ipc_send", 6: "ipc_recv", 7: "procs",
		9: "udp_listen", 10: "udp_send", 11: "udp_recv",
		12: "win_open", 13: "win_fill", 14: "win_present", 15: "win_close",
		19: "win_query", 21: "poll_event", 22: "wait_event",
		23: "file_open", 24: "file_read", 25: "file_write", 26: "file_close", 27: "dir_list",
		28: "exec",
		30: "tcp_connect", 31: "tcp_send", 32: "tcp_recv", 33: "tcp_close",
		34: "file_delete", 35: "file_rename", 36: "file_truncate",
		42: "audio_info", 43: "audio_play",
		46: "win_fill_batch", 56: "pipe_read", 57: "pipe_write",
		63: "mmap", 66: "time", 67: "tty_attach",
	}
	got := map[uintptr]string{
		SlotWrite: "write", SlotYield: "yield", SlotExit: "exit", SlotSleep: "sleep",
		SlotIPCSend: "ipc_send", SlotIPCRecv: "ipc_recv", SlotProcs: "procs",
		SlotUDPListen: "udp_listen", SlotUDPSend: "udp_send", SlotUDPRecv: "udp_recv",
		SlotWinOpen: "win_open", SlotWinFill: "win_fill", SlotWinPresent: "win_present",
		SlotWinClose: "win_close", SlotWinQuery: "win_query",
		SlotPollEvent: "poll_event", SlotWaitEvent: "wait_event",
		SlotFileOpen: "file_open", SlotFileRead: "file_read", SlotFileWrite: "file_write",
		SlotFileClose: "file_close", SlotDirList: "dir_list", SlotExec: "exec",
		SlotTCPConnect: "tcp_connect", SlotTCPSend: "tcp_send", SlotTCPRecv: "tcp_recv",
		SlotTCPClose: "tcp_close", SlotFileDelete: "file_delete",
		SlotFileRename: "file_rename", SlotFileTruncate: "file_truncate",
		SlotAudioInfo: "audio_info",
		SlotAudioPlay: "audio_play", SlotWinFillBatch: "win_fill_batch",
		SlotPipeRead: "pipe_read", SlotPipeWrite: "pipe_write",
		SlotMmap: "mmap", SlotTime: "time", SlotTtyAttach: "tty_attach",
	}
	for slot, name := range want {
		if got[slot] != name {
			t.Fatalf("slot %d = %q want %q", slot, got[slot], name)
		}
	}
	if len(got) != len(want) {
		t.Fatalf("slot table drift: %d entries want %d", len(got), len(want))
	}
}

// The event wire format must stay 16 bytes (ADR 0009) — the kernel copies
// exactly that many bytes out through uaccess.
func TestEventWireSize(t *testing.T) {
	if got := unsafe.Sizeof(Event{}); got != 16 {
		t.Fatalf("Event size = %d want 16", got)
	}
	if off := unsafe.Offsetof(Event{}.Seq); off != 4 {
		t.Fatalf("Seq offset = %d want 4", off)
	}
	if off := unsafe.Offsetof(Event{}.Arg1); off != 12 {
		t.Fatalf("Arg1 offset = %d want 12", off)
	}
}

// The batch record is 24 bytes with the kernel's field offsets
// (kernel/src/syscall.zig handle_win_fill_batch).
func TestFillerPacking(t *testing.T) {
	if FillRectSize != 24 || FillBatchMax != 32 {
		t.Fatalf("batch geometry = %d/%d want 24/32", FillRectSize, FillBatchMax)
	}
	var f Filler
	f.Rect(7, 1, 2, 3, 4, 0x11223344)
	if f.Pending() != 1 {
		t.Fatalf("pending = %d want 1", f.Pending())
	}
	b := f.buf[:FillRectSize]
	if b[0] != 7 {
		t.Fatalf("id byte = %d", b[0])
	}
	read := func(off int) uint32 {
		return uint32(b[off]) | uint32(b[off+1])<<8 | uint32(b[off+2])<<16 | uint32(b[off+3])<<24
	}
	if read(4) != 1 || read(8) != 2 || read(12) != 3 || read(16) != 4 || read(20) != 0x11223344 {
		t.Fatalf("packed rect wrong: %v", b)
	}
	f.Reset()
	if f.Pending() != 0 {
		t.Fatal("reset did not clear")
	}
}

func TestFillerAutoFlushBoundary(t *testing.T) {
	var f Filler
	for i := 0; i < FillBatchMax; i++ {
		f.Rect(1, uint32(i), 0, 1, 1, 0xFFFFFF)
	}
	if f.Pending() != FillBatchMax {
		t.Fatalf("pending = %d want %d", f.Pending(), FillBatchMax)
	}
	// One more record overflows the batch: on the host Flush() is a no-op
	// sycall that still resets, so the pending count must drop to 1.
	f.Rect(1, 99, 0, 1, 1, 0xFFFFFF)
	if f.Pending() != 1 {
		t.Fatalf("after overflow pending = %d want 1", f.Pending())
	}
}

func TestZeroSizeRectsDropped(t *testing.T) {
	var f Filler
	f.Rect(1, 0, 0, 0, 5, 0xFFFFFF)
	f.Rect(1, 0, 0, 5, 0, 0xFFFFFF)
	if f.Pending() != 0 {
		t.Fatalf("zero-size rects were queued: %d", f.Pending())
	}
}

func TestOutOfRangeAndCaps(t *testing.T) {
	if MaxFileBytes != 256*1024 {
		t.Fatalf("MaxFileBytes = %d", MaxFileBytes)
	}
	if writeChunk > 256 {
		t.Fatalf("console chunk %d exceeds the kernel cap", writeChunk)
	}
	// Host: every syscall is a no-op, so reads fail cleanly rather than panic.
	if b, rc := ReadFileAll("/host/NOPE.HTML", 1024); b != nil || rc >= 0 {
		t.Fatalf("host read should fail: %v %d", b, rc)
	}
	if FileExists("/host/NOPE.HTML") {
		t.Fatal("host FileExists should be false")
	}
	if FileAppend("/host/NOPE.TXT", []byte("x")) {
		t.Fatal("host FileAppend should fail")
	}
	Console(string(make([]byte, 1000))) // must not panic when chunked
}

func TestItoa64(t *testing.T) {
	for _, tc := range []struct {
		value int64
		want  string
	}{
		{0, "0"},
		{1, "1"},
		{-1, "-1"},
		{9223372036854775807, "9223372036854775807"},
		{-9223372036854775808, "-9223372036854775808"},
	} {
		if got := Itoa64(tc.value); got != tc.want {
			t.Errorf("Itoa64(%d) = %q, want %q", tc.value, got, tc.want)
		}
	}
}

func TestArgsHost(t *testing.T) {
	if Args() != nil {
		t.Fatal("host Args should be nil")
	}
}

func TestDirEntryWireSize(t *testing.T) {
	if got := unsafe.Sizeof(DirEntry{}); got != 40 {
		t.Fatalf("DirEntry size = %d want 40", got)
	}
	if off := unsafe.Offsetof(DirEntry{}.Size); off != 32 {
		t.Fatalf("Size offset = %d want 32", off)
	}
	if off := unsafe.Offsetof(DirEntry{}.IsDir); off != 36 {
		t.Fatalf("IsDir offset = %d want 36", off)
	}
	if MaxDirEntries != 16 {
		t.Fatalf("MaxDirEntries = %d want 16", MaxDirEntries)
	}
}

func TestDirEntryNameString(t *testing.T) {
	var e DirEntry
	copy(e.Name[:], "KNOWN.TXT")
	e.IsDir = 0
	if got := e.NameString(); got != "KNOWN.TXT" {
		t.Fatalf("NameString = %q", got)
	}
	if e.Dir() {
		t.Fatal("file must not report Dir")
	}
	e.IsDir = 1
	if !e.Dir() {
		t.Fatal("directory must report Dir")
	}
}

func TestDirListHostFails(t *testing.T) {
	var buf [MaxDirEntries]DirEntry
	n, rc := DirList("/host", buf[:])
	if n != 0 || rc >= 0 {
		t.Fatalf("host DirList = %d, %d want 0, <0", n, rc)
	}
}

func TestExecRefusals(t *testing.T) {
	if _, err := Exec(""); err != errno(ErrEINVAL) {
		t.Fatalf("empty name: %v", err)
	}
	tooMany := make([]string, 9)
	if _, err := Exec("X.BIN", tooMany...); err != errno(ErrEINVAL) {
		t.Fatalf("argc>8: %v", err)
	}
}

func TestTtyAttachSelectors(t *testing.T) {
	if SlotTtyAttach != 67 {
		t.Fatalf("SlotTtyAttach = %d want 67", SlotTtyAttach)
	}
	if TtyDetach != 0 || TtySerial != 1 || TtyWindow != 2 || TtyNet != 3 {
		t.Fatalf("selectors = %d/%d/%d/%d want 0/1/2/3", TtyDetach, TtySerial, TtyWindow, TtyNet)
	}
	if SlotTtyNetAuth != 71 {
		t.Fatalf("SlotTtyNetAuth = %d want 71", SlotTtyNetAuth)
	}
	if NetSchemeOpen != 0 || NetSchemeHMAC != 1 || NetSchemeEd25519 != 2 {
		t.Fatalf("schemes = %d/%d/%d want 0/1/2", NetSchemeOpen, NetSchemeHMAC, NetSchemeEd25519)
	}
	if NetAuthOpChallenge != 0 || NetAuthOpResponse != 1 || NetAuthOpVerdict != 2 {
		t.Fatalf("ops = %d/%d/%d want 0/1/2", NetAuthOpChallenge, NetAuthOpResponse, NetAuthOpVerdict)
	}
	if NetChallengeLen != 32 || NetAuthLineMax != 160 {
		t.Fatalf("net-auth geometry = %d/%d want 32/160", NetChallengeLen, NetAuthLineMax)
	}
}

func TestTtyAttachHostFails(t *testing.T) {
	if r := TtyAttach(TtyDetach); r != -ErrENOSYS {
		t.Fatalf("host TtyAttach = %d want -ENOSYS", r)
	}
	if r := TtyAttachWindow(1); r != -ErrENOSYS {
		t.Fatalf("host TtyAttachWindow = %d want -ENOSYS", r)
	}
	if r := TtyAttachNet(2323, NetSchemeOpen, 0); r != -ErrENOSYS {
		t.Fatalf("host TtyAttachNet = %d want -ENOSYS", r)
	}
	var buf [32]byte
	if r := TtyNetAuth(NetAuthOpChallenge, buf[:]); r != -ErrENOSYS {
		t.Fatalf("host TtyNetAuth = %d want -ENOSYS", r)
	}
}

// M66a (#1443): the file-domain error rows, the fsync binding, and the
// chunked write helper.

func TestFileErrorRows(t *testing.T) {
	if ErrFileNotFound != -6 || ErrFileIsDir != -1 || ErrFileExists != -9 || ErrFileHandleFull != -5 {
		t.Fatalf("file rows = %d/%d/%d/%d want -6/-1/-9/-5",
			ErrFileNotFound, ErrFileIsDir, ErrFileExists, ErrFileHandleFull)
	}
	if SlotFileSync != 77 {
		t.Fatalf("SlotFileSync = %d want 77", SlotFileSync)
	}
	// The chunk size IS the kernel's sys_file_write stage cap: a larger
	// count is refused with -ENOSPC (handle_file_write).
	if fileWriteChunk != 2048 {
		t.Fatalf("fileWriteChunk = %d want 2048", fileWriteChunk)
	}
}

// FileWriteAll chunks to the kernel's stage cap and advances by the count
// each call CONFIRMS: a 5000-byte body is 2048/2048/904 on the wire.
func TestFileWriteAllChunksByConfirmedCounts(t *testing.T) {
	var calls []int
	prev := SetSyscallHookForTest(func(num uintptr, a0, a1, a2, a3 uintptr) int64 {
		if num != SlotFileWrite {
			t.Fatalf("slot %d, want file_write", num)
		}
		count := int(a2)
		if count > 2048 {
			return -ErrENOSPC
		}
		calls = append(calls, count)
		return int64(count)
	})
	defer SetSyscallHookForTest(prev)

	body := make([]byte, 5000)
	for i := range body {
		body[i] = byte(i)
	}
	n, r := FileWriteAll(3, body)
	if r != 0 || n != len(body) {
		t.Fatalf("FileWriteAll = %d, %d want %d, 0", n, r, len(body))
	}
	if len(calls) != 3 || calls[0] != 2048 || calls[1] != 2048 || calls[2] != 904 {
		t.Fatalf("chunk plan = %v, want 2048/2048/904", calls)
	}
}

// A mid-stream failure reports the CONFIRMED prefix (the caller can resume
// at n without corrupting the stream); a first-chunk failure reports (0, r).
func TestFileWriteAllSurfacesPartialWrites(t *testing.T) {
	calls := 0
	prev := SetSyscallHookForTest(func(num uintptr, a0, a1, a2, a3 uintptr) int64 {
		calls++
		if calls == 2 {
			return -ErrEBADF // the kernel reports a dead handle mid-stream
		}
		return int64(a2)
	})
	defer SetSyscallHookForTest(prev)

	body := make([]byte, 4096)
	if n, r := FileWriteAll(1, body); r != -ErrEBADF || n != 2048 {
		t.Fatalf("mid-stream = %d, %d want 2048, -2", n, r)
	}

	prev2 := SetSyscallHookForTest(func(num uintptr, a0, a1, a2, a3 uintptr) int64 {
		return -ErrENOSPC
	})
	defer SetSyscallHookForTest(prev2)
	if n, r := FileWriteAll(1, body); r != -ErrENOSPC || n != 0 {
		t.Fatalf("first-chunk = %d, %d want 0, -5", n, r)
	}
}

// A zero-count acceptance cannot advance the stream; the helper stops
// instead of spinning (the kernel never reports one for a non-empty chunk,
// so this guards the loop, not the kernel).
func TestFileWriteAllStopsOnAZeroCount(t *testing.T) {
	prev := SetSyscallHookForTest(func(num uintptr, a0, a1, a2, a3 uintptr) int64 {
		return 0
	})
	defer SetSyscallHookForTest(prev)
	if n, r := FileWriteAll(1, []byte("x")); r >= 0 || n != 0 {
		t.Fatalf("zero-count = %d, %d want 0, <0", n, r)
	}
}

func TestFileSyncBinding(t *testing.T) {
	prev := SetSyscallHookForTest(func(num uintptr, a0, a1, a2, a3 uintptr) int64 {
		if num != SlotFileSync {
			t.Fatalf("slot %d, want file_sync", num)
		}
		if a0 != 4 {
			t.Fatalf("fd arg = %d want 4", a0)
		}
		return 0
	})
	defer SetSyscallHookForTest(prev)
	if r := FileSync(4); r != 0 {
		t.Fatalf("FileSync = %d want 0", r)
	}
}

// FileAppend must chunk: a row longer than the kernel's 2048-byte write
// stage lands whole instead of failing with -ENOSPC.
func TestFileAppendWritesLongRows(t *testing.T) {
	prev := SetSyscallHookForTest(func(num uintptr, a0, a1, a2, a3 uintptr) int64 {
		switch num {
		case SlotFileOpen:
			return 1
		case SlotFileWrite:
			if int(a2) > 2048 {
				return -ErrENOSPC
			}
			return int64(a2)
		case SlotFileClose:
			return 0
		}
		t.Fatalf("unexpected slot %d", num)
		return 0
	})
	defer SetSyscallHookForTest(prev)

	if !FileAppend("/host/SELFTEST/OUT/long.txt", make([]byte, 9000)) {
		t.Fatal("FileAppend failed on a 9000-byte row")
	}
}

// M66b (#1444): the rename binding and the crash-safe replace-write.

// hookStr rebuilds a path argument the fake kernel received. The pointer is
// the one strPtr handed the seam (still live inside the call), so this is
// the same contract the guest assembly reads through — test-only.
func hookStr(a0, a1 uintptr) string {
	if a0 == 0 || a1 == 0 {
		return ""
	}
	return string(unsafe.Slice((*byte)(unsafe.Pointer(a0)), a1))
}

// WriteFileSafe's publish order is the contract: open the one-byte temp
// (~, M81e #1765 — a longer suffix cannot publish near max_path_len), write,
// FSYNC, close, delete the live file, rename the temp over it. The fsync
// and the rename are what make a crash leave either the old bytes or no
// file — never a partial one.
func TestWriteFileSafePublishesByRename(t *testing.T) {
	var seq []uintptr
	prev := SetSyscallHookForTest(func(num uintptr, a0, a1, a2, a3 uintptr) int64 {
		seq = append(seq, num)
		switch num {
		case SlotFileOpen:
			if got := hookStr(a0, a1); got != "/host/SET.TXT~" {
				t.Fatalf("open = %q, want the temp path", got)
			}
			if a2 != uintptr(ModeWrite|ModeCreate) {
				t.Fatalf("open flags = %#x", a2)
			}
			return 1
		case SlotFileWrite:
			return int64(a2)
		case SlotFileSync, SlotFileClose:
			return 0
		case SlotFileDelete:
			if got := hookStr(a0, a1); got != "/host/SET.TXT" {
				t.Fatalf("delete = %q, want the live path", got)
			}
			return 0
		case SlotFileRename:
			if got := hookStr(a0, a1); got != "/host/SET.TXT~" {
				t.Fatalf("rename from = %q, want the temp", got)
			}
			if got := hookStr(a2, a3); got != "/host/SET.TXT" {
				t.Fatalf("rename to = %q, want the live path", got)
			}
			return 0
		}
		t.Fatalf("unexpected slot %d", num)
		return 0
	})
	defer SetSyscallHookForTest(prev)

	if rc := WriteFileSafe("/host/SET.TXT", []byte("#v2\nwm=none\n")); rc != 0 {
		t.Fatalf("WriteFileSafe = %d, want 0", rc)
	}
	want := []uintptr{SlotFileOpen, SlotFileWrite, SlotFileSync, SlotFileClose, SlotFileDelete, SlotFileRename}
	if len(seq) != len(want) {
		t.Fatalf("call seq = %v, want %v", seq, want)
	}
	for i, w := range want {
		if seq[i] != w {
			t.Fatalf("call seq = %v, want %v", seq, want)
		}
	}
}

// Every failure path removes the temp and reports the failing code: the
// live file is left exactly as it was (old bytes or absent), never partial.
func TestWriteFileSafeFailureLeavesNoTemp(t *testing.T) {
	for _, tc := range []struct {
		name  string
		fail  uintptr // the slot that fails
		check func(*testing.T, []uintptr)
	}{
		{"write", SlotFileWrite, func(t *testing.T, seq []uintptr) {
			if seq[len(seq)-1] != SlotFileDelete {
				t.Fatalf("last call = %d, want the temp delete", seq[len(seq)-1])
			}
		}},
		{"sync", SlotFileSync, func(t *testing.T, seq []uintptr) {
			if seq[len(seq)-1] != SlotFileDelete {
				t.Fatalf("last call = %d, want the temp delete", seq[len(seq)-1])
			}
		}},
		{"rename", SlotFileRename, func(t *testing.T, seq []uintptr) {
			// delete(live) -> rename fails -> delete(temp): two deletes.
			if seq[len(seq)-1] != SlotFileDelete || seq[len(seq)-2] != SlotFileRename {
				t.Fatalf("tail = %v, want rename then temp delete", seq[len(seq)-2:])
			}
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var seq []uintptr
			prev := SetSyscallHookForTest(func(num uintptr, a0, a1, a2, a3 uintptr) int64 {
				seq = append(seq, num)
				if num == tc.fail {
					return -ErrEACCES
				}
				switch num {
				case SlotFileOpen:
					return 1
				case SlotFileWrite:
					return int64(a2)
				case SlotFileDelete:
					return 0
				case SlotFileSync, SlotFileClose, SlotFileRename:
					return 0
				}
				t.Fatalf("unexpected slot %d", num)
				return 0
			})
			defer SetSyscallHookForTest(prev)

			if rc := WriteFileSafe("/host/SET.TXT", []byte("body")); rc != -ErrEACCES {
				t.Fatalf("WriteFileSafe = %d, want -7", rc)
			}
			tc.check(t, seq)
		})
	}
}

// An absent live file is not a publish failure: the delete reports ENOENT
// and the rename proceeds (first save on a fresh share).
func TestWriteFileSafeFirstSaveHasNothingToDelete(t *testing.T) {
	var seq []uintptr
	prev := SetSyscallHookForTest(func(num uintptr, a0, a1, a2, a3 uintptr) int64 {
		seq = append(seq, num)
		if num == SlotFileDelete {
			return ErrFileNotFound // nothing live yet
		}
		switch num {
		case SlotFileOpen:
			return 1
		case SlotFileWrite:
			return int64(a2)
		case SlotFileSync, SlotFileClose, SlotFileRename:
			return 0
		}
		t.Fatalf("unexpected slot %d", num)
		return 0
	})
	defer SetSyscallHookForTest(prev)

	if rc := WriteFileSafe("/host/SET.TXT", []byte("body")); rc != 0 {
		t.Fatalf("WriteFileSafe = %d, want 0", rc)
	}
	if seq[len(seq)-1] != SlotFileRename {
		t.Fatalf("last call = %d, want the rename", seq[len(seq)-1])
	}
}

func TestFileRenameBinding(t *testing.T) {
	prev := SetSyscallHookForTest(func(num uintptr, a0, a1, a2, a3 uintptr) int64 {
		if num != SlotFileRename {
			t.Fatalf("slot %d, want file_rename", num)
		}
		if got := hookStr(a0, a1); got != "/host/A" {
			t.Fatalf("from = %q", got)
		}
		if got := hookStr(a2, a3); got != "/host/B" {
			t.Fatalf("to = %q", got)
		}
		return 0
	})
	defer SetSyscallHookForTest(prev)
	if r := FileRename("/host/A", "/host/B"); r != 0 {
		t.Fatalf("FileRename = %d, want 0", r)
	}
	if r := FileRename("", "/host/B"); r != -ErrEINVAL {
		t.Fatalf("empty from = %d, want -1", r)
	}
	if r := FileRename("/host/A", ""); r != -ErrEINVAL {
		t.Fatalf("empty to = %d, want -1", r)
	}
}
