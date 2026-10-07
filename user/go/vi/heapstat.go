package vi

import (
	"encoding/binary"
	"fmt"
	"runtime"
	"unsafe"
)

const (
	MemstatRecordBytes = 240
	MemstatMaxRegions  = 16
	MemstatExited      = 1
)

type MemstatRecord struct {
	Version, Flags                                                  uint32
	PID, CNTPCT                                                     uint64
	PeakPages, PeakRegions, StaticPages, TotalPages, RecordFailures uint64
	LivePages, LiveRegions                                          uint64
	TextBytes, ROBytes, DataBytes, StackBytes                       uint64
	RegionSizes                                                     [MemstatMaxRegions]uint64
}

// Memstat is the frozen three-register slot-83 gateway.
func Memstat(pid uint64, buffer []byte) int64 {
	var pin runtime.Pinner
	if len(buffer) != 0 {
		// KeepAlive preserves liveness, not a stack address. svc3 may grow
		// the Go stack after conversion to uintptr, so use pinned backing.
		pin.Pin(&buffer[0])
		defer pin.Unpin()
	}
	return memstatPinned(pid, buffer)
}

func memstatPinned(pid uint64, buffer []byte) int64 {
	var ptr uintptr
	if len(buffer) != 0 {
		ptr = uintptr(unsafe.Pointer(&buffer[0]))
	}
	result := svc3(SlotMemstat, uintptr(pid), ptr, uintptr(len(buffer)))
	runtime.KeepAlive(buffer)
	return result
}

// DecodeMemstat never relies on native padding or host byte order.
func DecodeMemstat(buf []byte) (MemstatRecord, error) {
	var record MemstatRecord
	if len(buf) != MemstatRecordBytes {
		return record, fmt.Errorf("memstat: require %d bytes", MemstatRecordBytes)
	}
	record.Version = binary.LittleEndian.Uint32(buf)
	record.Flags = binary.LittleEndian.Uint32(buf[4:])
	if record.Version != ObserveVersion || record.Flags & ^uint32(MemstatExited) != 0 {
		return MemstatRecord{}, fmt.Errorf("memstat: invalid version or flags")
	}
	fields := []*uint64{&record.PID, &record.CNTPCT, &record.PeakPages, &record.PeakRegions,
		&record.StaticPages, &record.TotalPages, &record.RecordFailures, &record.LivePages,
		&record.LiveRegions, &record.TextBytes, &record.ROBytes, &record.DataBytes, &record.StackBytes}
	for i, field := range fields {
		*field = binary.LittleEndian.Uint64(buf[8+i*8:])
	}
	for i := range record.RegionSizes {
		record.RegionSizes[i] = binary.LittleEndian.Uint64(buf[112+i*8:])
	}
	if record.LivePages > record.PeakPages || record.LiveRegions > record.PeakRegions ||
		record.PeakPages > record.TotalPages || record.LiveRegions > MemstatMaxRegions {
		return MemstatRecord{}, fmt.Errorf("memstat: inconsistent counters")
	}
	for i, size := range record.RegionSizes {
		if (i < int(record.LiveRegions)) != (size != 0) {
			return MemstatRecord{}, fmt.Errorf("memstat: inconsistent region sizes")
		}
	}
	if record.Flags&MemstatExited != 0 && (record.LivePages != 0 || record.LiveRegions != 0 ||
		record.TextBytes != 0 || record.ROBytes != 0 || record.DataBytes != 0 || record.StackBytes != 0) {
		return MemstatRecord{}, fmt.Errorf("memstat: exited record has live state")
	}
	return record, nil
}

func HeapStat(pid uint64) (MemstatRecord, error) {
	record, _, err := HeapStatTimed(pid)
	return record, err
}

// HeapStatTimed measures the gateway only. Buffer preparation and Go decoding
// are outside the kernel snapshot budget, as is an opt-in publisher's GC.
func HeapStatTimed(pid uint64) (MemstatRecord, int64, error) {
	var buffer [MemstatRecordBytes]byte
	var pin runtime.Pinner
	pin.Pin(&buffer[0])
	defer pin.Unpin()
	start := Nanos()
	rc := memstatPinned(pid, buffer[:])
	elapsed := Nanos() - start
	if rc != MemstatRecordBytes {
		return MemstatRecord{}, elapsed, fmt.Errorf("memstat: native result %d", rc)
	}
	record, err := DecodeMemstat(buffer[:])
	if err == nil && record.PID != pid {
		err = fmt.Errorf("memstat: pid mismatch")
	}
	return record, elapsed, err
}
