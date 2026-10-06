package strace

import (
	"encoding/binary"
	"strings"
	"testing"
	"unsafe"

	"virelai/vi"
)

func TestRenderKindsAndNativeResults(t *testing.T) {
	r := vi.TraceRecord{Version: 1, PID: 2, Number: 23, Args: [6]uint64{0x123, 14, 1}}
	r.StringMask = 1
	r.StringLengths[0] = uint16(copy(r.Strings[0][:], "/host/TRACE.IN"))
	if got := Render(r); got != `[strace 2] sys_file_open(path="/host/TRACE.IN", len=14, flags=READ) = 0` {
		t.Fatal(got)
	}
	r.Number, r.Args, r.Result = 24, [6]uint64{3, 0x123, 12}, 12
	if got := Render(r); got != `[strace 2] sys_file_read(fd=3, buf=0x123, len=12) = 12` {
		t.Fatal(got)
	}
	r.Number, r.Args, r.Result, r.Errno = 29, [6]uint64{123}, -7, 7
	if got := Render(r); got != `[strace 2] sys_kill(arg0=123) = -EACCES` {
		t.Fatal(got)
	}
	r.Number, r.Args, r.Result, r.Errno = 0, [6]uint64{^uint64(0)}, -1, 1
	if got := Render(r); got != `[strace 2] sys_ping(arg0=-1) = -EINVAL` {
		t.Fatal(got)
	}
	r.Number, r.Flags = 3, vi.TraceNoReturn
	if !strings.HasSuffix(Render(r), " = <no-return>") {
		t.Fatal(Render(r))
	}
}

func TestRenderStringsRedactionAndAllMetadata(t *testing.T) {
	r := vi.TraceRecord{Version: 1, Number: 23, StringMask: 1, TruncatedMask: 1}
	r.StringLengths[0] = uint16(copy(r.Strings[0][:], "a\n\"\\\x00\xff"))
	if got := Render(r); !strings.Contains(got, `path="a\n\"\\\x00\xff"…`) {
		t.Fatal(got)
	}
	r.FaultMask = 1
	if !strings.Contains(Render(r), "path=<fault>") {
		t.Fatal(Render(r))
	}
	for _, slot := range []uint64{70, 71} {
		r.Number, r.Flags = slot, 0
		got := Render(r)
		if !strings.HasSuffix(got, "(<redacted>)") || strings.Contains(got, "path=") {
			t.Fatal(got)
		}
	}
	for _, slot := range vi.SyscallSlots {
		r = vi.TraceRecord{Version: 1, Number: uint64(slot.Number)}
		if got := Render(r); !strings.Contains(got, slot.Name+"(") {
			t.Fatal(got)
		}
	}
	for _, variant := range vi.SyscallVariants {
		r = vi.TraceRecord{Version: 1, Number: uint64(variant.Number)}
		r.Args[variant.Selector] = variant.Op
		if got := Render(r); !strings.Contains(got, vi.SyscallSlots[variant.Number].Name+"(") {
			t.Fatal(got)
		}
	}
}

func TestArmExecWireOrderingAndFailureDisarm(t *testing.T) {
	var ops []uintptr
	previous := vi.SetSyscallHookForTest(func(slot, op, token, ptr, size uintptr) int64 {
		if slot != vi.SlotTrace {
			t.Fatal(slot)
		}
		ops = append(ops, op)
		b := unsafe.Slice((*byte)(unsafe.Pointer(ptr)), size)
		switch op {
		case vi.TraceArm:
			if size != 88 || binary.LittleEndian.Uint32(b) != 1 ||
				binary.LittleEndian.Uint32(b[4:]) != 0 || binary.LittleEndian.Uint64(b[72:]) != 1<<23 {
				t.Fatal(b)
			}
			return 5
		case vi.TraceArmExec:
			if token != 5 || size != 32 || binary.LittleEndian.Uint64(b[8:]) != 8 ||
				binary.LittleEndian.Uint64(b[24:]) != 2 {
				t.Fatal(b)
			}
			path := unsafe.Slice((*byte)(unsafe.Pointer(uintptr(binary.LittleEndian.Uint64(b)))), 8)
			argv := unsafe.Slice((*byte)(unsafe.Pointer(uintptr(binary.LittleEndian.Uint64(b[16:])))), 512)
			if string(path) != "GOSH.ELF" || string(argv[:2]) != "-c" || string(argv[256:263]) != "echo hi" {
				t.Fatalf("%q %q", path, argv)
			}
			return -vi.ErrENOENT
		case vi.TraceDisarm:
			return 0
		}
		t.Fatal(op)
		return -vi.ErrEINVAL
	})
	defer vi.SetSyscallHookForTest(previous)
	_, _, err := ArmExec("GOSH.ELF", []string{"-c", "echo hi"}, []uint64{23})
	if err == nil || err.Error() != "ENOENT" || len(ops) != 3 ||
		ops[0] != vi.TraceArm || ops[1] != vi.TraceArmExec || ops[2] != vi.TraceDisarm {
		t.Fatalf("ops=%v err=%v", ops, err)
	}
}

func TestReadBatchWireAndBounds(t *testing.T) {
	previous := vi.SetSyscallHookForTest(func(slot, op, token, ptr, size uintptr) int64 {
		b := unsafe.Slice((*byte)(unsafe.Pointer(ptr)), size)
		binary.LittleEndian.PutUint32(b, 1)
		binary.LittleEndian.PutUint32(b[4:], vi.TraceRecordBytes)
		binary.LittleEndian.PutUint32(b[8:], 1)
		binary.LittleEndian.PutUint64(b[16:], 44)
		r := b[24:]
		binary.LittleEndian.PutUint32(r, 1)
		binary.LittleEndian.PutUint64(r[8:], 2)
		binary.LittleEndian.PutUint64(r[16:], 3)
		binary.LittleEndian.PutUint64(r[24:], 4)
		binary.LittleEndian.PutUint64(r[32:], 23)
		binary.LittleEndian.PutUint64(r[80:], 123) // Sixth argument.
		binary.LittleEndian.PutUint64(r[88:], 3)
		binary.LittleEndian.PutUint32(r[100:], 1)
		binary.LittleEndian.PutUint16(r[112:], 4)
		copy(r[128:], "test")
		return 1
	})
	defer vi.SetSyscallHookForTest(previous)
	s := &Session{Token: 5}
	records, dropped, err := s.Read()
	if err != nil || dropped != 44 || len(records) != 1 ||
		records[0].Args[5] != 123 || records[0].Result != 3 ||
		string(records[0].Strings[0][:4]) != "test" {
		t.Fatalf("records=%v dropped=%d err=%v", records, dropped, err)
	}
	if _, err := config(nil, []uint64{128}); err == nil {
		t.Fatal("out-of-range slot accepted")
	}
	if _, err := config(make([]uint64, 9), nil); err == nil {
		t.Fatal("oversized pid set accepted")
	}
}
