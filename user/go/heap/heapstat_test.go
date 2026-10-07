package heap

import (
	"encoding/binary"
	"runtime"
	"testing"
	"unsafe"

	"virelai/vi"
)

// The host syscall hook mirrors a live gateway's uintptr buffer arguments.
// Conversion is confined to this test seam; the caller keeps its buffer alive.
//
//go:nocheckptr
func hookBuffer(pointer, size uintptr) []byte {
	return unsafe.Slice((*byte)(unsafe.Pointer(pointer)), size)
}

// Force a stack copy after the gateway has turned the buffer into uintptr.
//
//go:noinline
func growGatewayStack(depth int) byte {
	var scratch [4096]byte
	for i := range scratch {
		scratch[i] = byte(i + depth)
	}
	var result byte
	if depth > 0 {
		result = growGatewayStack(depth - 1)
	}
	for _, value := range scratch {
		result ^= value
	}
	runtime.KeepAlive(&scratch)
	return result
}

func TestMemstatBufferSurvivesGatewayStackGrowth(t *testing.T) {
	var wire [vi.MemstatRecordBytes]byte
	binary.LittleEndian.PutUint32(wire[:], 1)
	binary.LittleEndian.PutUint64(wire[8:], 7)
	prior := vi.SetSyscallHookForTest(func(_, _, pointer, size, _ uintptr) int64 {
		_ = growGatewayStack(64)
		copy(hookBuffer(pointer, size), wire[:])
		return vi.MemstatRecordBytes
	})
	defer vi.SetSyscallHookForTest(prior)
	result := make(chan error, 1)
	go func() {
		_, err := vi.HeapStat(7)
		result <- err
	}()
	if err := <-result; err != nil {
		t.Fatal(err)
	}
}

func TestMemstatFrozenDecodeAndNativeErrors(t *testing.T) {
	var wire [vi.MemstatRecordBytes]byte
	binary.LittleEndian.PutUint32(wire[:], 1)
	for offset, value := range map[int]uint64{
		8: 7, 16: 12345, 24: 5000, 32: 3, 40: 90, 48: 6000, 56: 2,
		64: 4500, 72: 2, 80: 8192, 88: 12288, 96: 16384, 104: 65536,
		112: 4096, 120: 8192,
	} {
		binary.LittleEndian.PutUint64(wire[offset:], value)
	}
	record, err := vi.DecodeMemstat(wire[:])
	if err != nil || record.PID != 7 || record.CNTPCT != 12345 ||
		record.LivePages != 4500 || record.PeakPages != 5000 || record.StackBytes != 65536 ||
		record.RegionSizes[1] != 8192 || record.RegionSizes[2] != 0 {
		t.Fatalf("record=%+v err=%v", record, err)
	}
	// The inline 4096 ownership capacity is NOT a wire-record peak ceiling.
	prior := vi.SetSyscallHookForTest(func(slot, pid, pointer, size, unused uintptr) int64 {
		if slot != vi.SlotMemstat || pid != 7 || size != vi.MemstatRecordBytes || unused != 0 {
			t.Fatalf("gateway args: %d %d %d %d", slot, pid, size, unused)
		}
		copy(hookBuffer(pointer, size), wire[:])
		return vi.MemstatRecordBytes
	})
	defer vi.SetSyscallHookForTest(prior)
	if got, err := vi.HeapStat(7); err != nil || got != record {
		t.Fatalf("HeapStat=%+v err=%v", got, err)
	}
	if got, elapsed, err := vi.HeapStatTimed(7); err != nil || got != record || elapsed < 0 {
		t.Fatalf("HeapStatTimed=%+v elapsed=%d err=%v", got, elapsed, err)
	}
	for _, result := range []int64{-vi.ErrEACCES, -vi.ErrEFAULT, -vi.ErrEINVAL, 239, 241} {
		vi.SetSyscallHookForTest(func(uintptr, uintptr, uintptr, uintptr, uintptr) int64 { return result })
		if _, err := vi.HeapStat(7); err == nil {
			t.Fatalf("accepted native result %d", result)
		}
	}
	for _, mutate := range []func([]byte){
		func(buf []byte) { buf[0] = 2 },
		func(buf []byte) { buf[4] = 2 },
		func(buf []byte) { binary.LittleEndian.PutUint64(buf[72:], 17) },
		func(buf []byte) { binary.LittleEndian.PutUint64(buf[64:], 5001) },
		func(buf []byte) { binary.LittleEndian.PutUint64(buf[128:], 1) },
		func(buf []byte) { buf[4] = vi.MemstatExited },
	} {
		bad := wire
		mutate(bad[:])
		if _, err := vi.DecodeMemstat(bad[:]); err == nil {
			t.Fatal("accepted malformed memstat")
		}
	}
	if _, err := vi.DecodeMemstat(wire[:239]); err == nil {
		t.Fatal("accepted partial memstat")
	}
}
