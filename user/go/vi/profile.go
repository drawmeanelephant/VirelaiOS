package vi

import (
	"runtime"
	"unsafe"
)

const (
	ProfileArm         = 0
	ProfileDisarm      = 1
	ProfileRead        = 3
	ProfileStatus      = 4
	ProfileRateHz      = 100
	ProfileFrameDepth  = 16
	ProfileRingRecords = 1024
	ProfileRecordBytes = 176
)

type ProfileConfig struct {
	Version  uint32
	PIDCount uint32
	PIDs     [ObserveMaxPIDs]uint64
}

type SampleRecord struct {
	Version, Flags       uint32
	PID, TID, CNTPCT, PC uint64
	Depth, Reserved      uint32
	Frames               [ProfileFrameDepth]uint64
}

// Profile is the frozen slot-82 gateway, returning -ErrENOSYS in M94a.
func Profile(op, token uint64, buffer []byte) int64 {
	var ptr uintptr
	if len(buffer) != 0 {
		ptr = uintptr(unsafe.Pointer(&buffer[0]))
	}
	result := svc4(SlotProfile, uintptr(op), uintptr(token), ptr, uintptr(len(buffer)))
	runtime.KeepAlive(buffer)
	return result
}
