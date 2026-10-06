package vi

import (
	"encoding/binary"
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

// Trace is the frozen slot-81 gateway. Errors are negative native magnitudes.
func Trace(op, token uint64, buffer []byte) int64 {
	var ptr uintptr
	if len(buffer) != 0 {
		ptr = uintptr(unsafe.Pointer(&buffer[0]))
	}
	result := svc4(SlotTrace, uintptr(op), uintptr(token), ptr, uintptr(len(buffer)))
	runtime.KeepAlive(buffer)
	return result
}

func traceConfigBytes(config TraceConfig) []byte {
	buffer := make([]byte, 88)
	binary.LittleEndian.PutUint32(buffer, config.Version)
	binary.LittleEndian.PutUint32(buffer[4:], config.PIDCount)
	for i, pid := range config.PIDs {
		binary.LittleEndian.PutUint64(buffer[8+8*i:], pid)
	}
	for i, mask := range config.Slots {
		binary.LittleEndian.PutUint64(buffer[72+8*i:], mask)
	}
	return buffer
}

// TraceOpen replaces the uid-owned session and returns its positive token.
// A zero PIDCount permits a subsequent atomic TraceExec.
func TraceOpen(config TraceConfig) int64 {
	return Trace(TraceArm, 0, traceConfigBytes(config))
}

// TraceReplaceFilter atomically replaces the entire pid set and slot mask.
func TraceReplaceFilter(token uint64, config TraceConfig) int64 {
	return Trace(TraceFilter, token, traceConfigBytes(config))
}

// TraceExec preserves the caller's principal and arms the child before its
// first syscall. Arguments use slot 28's eight 256-byte packed slots.
func TraceExec(token uint64, path string, args []string) int64 {
	if len(path) == 0 || len(args) > 8 {
		return -ErrEINVAL
	}
	var argv [8 * 256]byte
	for i, arg := range args {
		if len(arg) > 255 {
			return -ErrEINVAL
		}
		for j := range arg {
			if arg[j] == 0 {
				return -ErrEINVAL
			}
		}
		copy(argv[i*256:], arg)
	}
	var request [32]byte
	binary.LittleEndian.PutUint64(request[:], uint64(uintptr(unsafe.Pointer(unsafe.StringData(path)))))
	binary.LittleEndian.PutUint64(request[8:], uint64(len(path)))
	binary.LittleEndian.PutUint64(request[16:], uint64(uintptr(unsafe.Pointer(&argv[0]))))
	binary.LittleEndian.PutUint64(request[24:], uint64(len(args)))
	result := Trace(TraceArmExec, token, request[:])
	runtime.KeepAlive(path)
	runtime.KeepAlive(argv)
	return result
}

func traceDecodeHeader(buffer []byte) ObserveReadHeader {
	return ObserveReadHeader{
		Version:     binary.LittleEndian.Uint32(buffer),
		RecordBytes: binary.LittleEndian.Uint32(buffer[4:]),
		Count:       binary.LittleEndian.Uint32(buffer[8:]),
		Reserved:    binary.LittleEndian.Uint32(buffer[12:]),
		Dropped:     binary.LittleEndian.Uint64(buffer[16:]),
	}
}

// TraceGetStatus returns the unread count and cumulative loss without consuming.
func TraceGetStatus(token uint64) (ObserveReadHeader, int64) {
	var buffer [ObserveReadHeaderBytes]byte
	result := Trace(TraceStatus, token, buffer[:])
	if result < 0 {
		return ObserveReadHeader{}, result
	}
	header := traceDecodeHeader(buffer[:])
	if header.Version != ObserveVersion || header.RecordBytes != TraceRecordBytes || header.Reserved != 0 {
		return ObserveReadHeader{}, -ErrEINVAL
	}
	return header, result
}

// TraceReadBatch consumes at most 16 whole records. A failed syscall consumes
// nothing. The header's Dropped counter is cumulative for the session.
func TraceReadBatch(token uint64) (ObserveReadHeader, []TraceRecord, int64) {
	var buffer [ObserveReadHeaderBytes + ObserveMaxReadRecords*TraceRecordBytes]byte
	result := Trace(TraceRead, token, buffer[:])
	if result < 0 {
		return ObserveReadHeader{}, nil, result
	}
	header := traceDecodeHeader(buffer[:])
	if header.Version != ObserveVersion || header.RecordBytes != TraceRecordBytes ||
		header.Reserved != 0 || header.Count > ObserveMaxReadRecords || int64(header.Count) != result {
		return ObserveReadHeader{}, nil, -ErrEINVAL
	}
	records := make([]TraceRecord, int(header.Count))
	for i := range records {
		b := buffer[ObserveReadHeaderBytes+i*TraceRecordBytes:]
		r := &records[i]
		r.Version = binary.LittleEndian.Uint32(b)
		r.Flags = binary.LittleEndian.Uint32(b[4:])
		r.PID = binary.LittleEndian.Uint64(b[8:])
		r.TID = binary.LittleEndian.Uint64(b[16:])
		r.CNTPCT = binary.LittleEndian.Uint64(b[24:])
		r.Number = binary.LittleEndian.Uint64(b[32:])
		for j := range r.Args {
			r.Args[j] = binary.LittleEndian.Uint64(b[40+j*8:])
		}
		r.Result = int64(binary.LittleEndian.Uint64(b[88:]))
		r.Errno = binary.LittleEndian.Uint32(b[96:])
		r.StringMask = binary.LittleEndian.Uint32(b[100:])
		r.FaultMask = binary.LittleEndian.Uint32(b[104:])
		r.TruncatedMask = binary.LittleEndian.Uint32(b[108:])
		for j := range r.Strings {
			r.StringLengths[j] = binary.LittleEndian.Uint16(b[112+j*2:])
			if r.StringLengths[j] > TraceStringBytes {
				return ObserveReadHeader{}, nil, -ErrEINVAL
			}
			copy(r.Strings[j][:], b[128+j*TraceStringBytes:128+(j+1)*TraceStringBytes])
		}
		r.Reserved = binary.LittleEndian.Uint32(b[124:])
		if r.Version != ObserveVersion || r.Reserved != 0 || r.Flags & ^uint32(TraceRedacted|TraceNoReturn) != 0 {
			return ObserveReadHeader{}, nil, -ErrEINVAL
		}
	}
	return header, records, result
}
