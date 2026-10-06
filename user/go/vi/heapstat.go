package vi

import (
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

// Memstat is the frozen slot-83 gateway, returning -ErrENOSYS in M94a.
func Memstat(pid uint64, buffer []byte) int64 {
	var ptr uintptr
	if len(buffer) != 0 {
		ptr = uintptr(unsafe.Pointer(&buffer[0]))
	}
	result := svc3(SlotMemstat, uintptr(pid), ptr, uintptr(len(buffer)))
	runtime.KeepAlive(buffer)
	return result
}
