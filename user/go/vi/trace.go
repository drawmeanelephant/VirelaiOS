package vi

import (
	"runtime"
	"unsafe"
)

const (
	ObserveVersion         = 1
	ObserveMaxPIDs         = 8
	ObserveMaxReadRecords  = 16
	ObserveReadHeaderBytes = 24
	TraceArm               = 0
	TraceDisarm            = 1
	TraceFilter            = 2
	TraceRead              = 3
	TraceStatus            = 4
	TraceArmExec           = 5
	TraceRingRecords       = 256
	TraceStringBytes       = 128
	TraceRecordBytes       = 896
	TraceRedacted          = 1
	TraceNoReturn          = 2
)

type TraceConfig struct {
	Version  uint32
	PIDCount uint32
	PIDs     [ObserveMaxPIDs]uint64
	Slots    [2]uint64
}

type TraceExecRequest struct {
	PathPtr, PathLen, ArgvPtr, Argc uint64
}

type ObserveReadHeader struct {
	Version, RecordBytes, Count, Reserved uint32
	Dropped                               uint64
}

type TraceRecord struct {
	Version, Flags                              uint32
	PID, TID, CNTPCT, Number                    uint64
	Args                                        [6]uint64
	Result                                      int64
	Errno, StringMask, FaultMask, TruncatedMask uint32
	StringLengths                               [6]uint16
	Reserved                                    uint32
	Strings                                     [6][TraceStringBytes]byte
}

// Trace is the frozen slot-81 gateway. M94a's handler returns -ErrENOSYS
// for every operation; this does not simulate or activate observation.
func Trace(op, token uint64, buffer []byte) int64 {
	var ptr uintptr
	if len(buffer) != 0 {
		ptr = uintptr(unsafe.Pointer(&buffer[0]))
	}
	result := svc4(SlotTrace, uintptr(op), uintptr(token), ptr, uintptr(len(buffer)))
	runtime.KeepAlive(buffer)
	return result
}
