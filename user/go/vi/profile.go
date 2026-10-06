package vi

import (
	"encoding/binary"
	"fmt"
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

// Profile is the frozen four-register slot-82 gateway.
func Profile(op, token uint64, buffer []byte) int64 {
	var ptr uintptr
	if len(buffer) != 0 {
		ptr = uintptr(unsafe.Pointer(&buffer[0]))
	}
	result := svc4(SlotProfile, uintptr(op), uintptr(token), ptr, uintptr(len(buffer)))
	runtime.KeepAlive(buffer)
	return result
}

// ProfileStart replaces the same-uid session only after the kernel validates
// the complete target set. A failed ARM leaves the prior session untouched.
func ProfileStart(pids []uint64) (uint64, error) {
	if len(pids) == 0 || len(pids) > ObserveMaxPIDs {
		return 0, fmt.Errorf("profile: require 1..%d pids", ObserveMaxPIDs)
	}
	var buf [72]byte
	binary.LittleEndian.PutUint32(buf[:4], ObserveVersion)
	binary.LittleEndian.PutUint32(buf[4:8], uint32(len(pids)))
	for i, pid := range pids {
		binary.LittleEndian.PutUint64(buf[8+i*8:], pid)
	}
	result := Profile(ProfileArm, 0, buf[:])
	if result <= 0 {
		return 0, fmt.Errorf("profile ARM: native result %d", result)
	}
	return uint64(result), nil
}

func ProfileStop(token uint64) error {
	result := Profile(ProfileDisarm, token, nil)
	if result != 0 {
		return fmt.Errorf("profile DISARM: native result %d", result)
	}
	return nil
}

// DecodeSamples validates the versioned header and every fixed wire record.
// No native struct padding, pointers or uintptr cross this boundary.
func DecodeSamples(buf []byte, result int64) ([]SampleRecord, uint64, error) {
	if result < 0 || result > 16 || len(buf) < 24 {
		return nil, 0, fmt.Errorf("profile READ: invalid result %d", result)
	}
	u32 := binary.LittleEndian.Uint32
	u64 := binary.LittleEndian.Uint64
	if u32(buf) != ObserveVersion || u32(buf[4:]) != ProfileRecordBytes ||
		u32(buf[8:]) != uint32(result) || u32(buf[12:]) != 0 ||
		len(buf) < 24+int(result)*ProfileRecordBytes {
		return nil, 0, fmt.Errorf("profile: invalid read header")
	}
	records := make([]SampleRecord, int(result))
	for i := range records {
		b := buf[24+i*ProfileRecordBytes : 24+(i+1)*ProfileRecordBytes]
		r := &records[i]
		r.Version, r.Flags = u32(b), u32(b[4:])
		r.PID, r.TID, r.CNTPCT, r.PC = u64(b[8:]), u64(b[16:]), u64(b[24:]), u64(b[32:])
		r.Depth, r.Reserved = u32(b[40:]), u32(b[44:])
		if r.Version != ObserveVersion || r.Flags != 0 || r.Depth > ProfileFrameDepth || r.Reserved != 0 {
			return nil, 0, fmt.Errorf("profile: invalid sample %d", i)
		}
		for f := range r.Frames {
			r.Frames[f] = u64(b[48+f*8:])
			if f >= int(r.Depth) && r.Frames[f] != 0 {
				return nil, 0, fmt.Errorf("profile: nonzero unused frame")
			}
		}
	}
	return records, u64(buf[16:]), nil
}

func ProfileSamples(token uint64) ([]SampleRecord, uint64, error) {
	var buf [24 + 16*ProfileRecordBytes]byte
	result := Profile(ProfileRead, token, buf[:])
	return DecodeSamples(buf[:], result)
}
