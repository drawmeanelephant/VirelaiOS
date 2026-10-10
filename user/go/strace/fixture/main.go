package main

import (
	"encoding/binary"
	"fmt"
	"runtime"
	"sort"
	"unsafe"

	"virelai/strace"
	"virelai/vi"
)

func check(err error) {
	if err != nil {
		vi.ConsoleLine("trace: fixture failed " + err.Error())
		vi.Exit(1)
	}
}

func selfPID() uint64 {
	var rows [16]vi.ProcRow
	n, rc := vi.Procs(rows[:])
	if rc < 0 {
		check(strace.Error{Code: -rc})
	}
	for _, row := range rows[:n] {
		if row.Name() == "TRACEFIX.ELF" && row.State != vi.ProcExited {
			return row.PID
		}
	}
	check(fmt.Errorf("own pid missing"))
	return 0
}

func drain(s *strace.Session) (int, uint64) {
	total := 0
	var dropped uint64
	for {
		records, loss, err := s.Read()
		check(err)
		dropped = loss
		for _, record := range records {
			if record.Number == 70 || record.Number == 71 {
				if record.Flags != vi.TraceRedacted || record.Args != [6]uint64{} ||
					record.Result != 0 || record.Errno != 0 || record.StringMask != 0 ||
					record.FaultMask != 0 || record.TruncatedMask != 0 ||
					record.StringLengths != [6]uint16{} || record.Strings != [6][128]byte{} {
					check(fmt.Errorf("redaction wire leak"))
				}
			}
			vi.ConsoleLine(strace.Render(record))
		}
		total += len(records)
		if len(records) == 0 {
			return total, dropped
		}
	}
}

func main() {
	args := vi.Args()
	if len(args) != 2 {
		check(fmt.Errorf("usage: TRACEFIX self|deny|hold|grab|overhead|capture-overhead|peer"))
	}
	switch args[1] {
	case "peer":
		for i := 0; i < 100; i++ {
			vi.PingPoll()
		}
		vi.ConsoleLine("trace: untraced peer done")
	case "self":
		self()
	case "deny":
		var rows [16]vi.ProcRow
		n, _ := vi.Procs(rows[:])
		for _, row := range rows[:n] {
			if row.Name() != "TRACEFIX.ELF" && row.State != vi.ProcExited {
				_, err := strace.Arm([]uint64{row.PID}, nil)
				if native, ok := err.(strace.Error); ok && native.Code == vi.ErrEACCES {
					vi.ConsoleLine("trace: cross-uid ARM = -EACCES")
					return
				}
				check(fmt.Errorf("cross-uid target %d returned %v", row.PID, err))
			}
		}
		check(fmt.Errorf("no foreign live target"))
	case "hold":
		// M97g (#2086): keep a uid_user session live while a second uid_user
		// process attempts to seize it. The session must stay bound to THIS
		// pid — exit would make the stale token releasable.
		sess, herr := strace.Arm([]uint64{selfPID()}, nil)
		check(herr)
		_ = sess
		vi.ConsoleLine("trace: holding session")
		vi.Sleep(60)
	case "grab":
		// Same uid, different pid, live owner: ARM must refuse EACCES even
		// though the old uid rule would have allowed the takeover.
		_, gerr := strace.Arm([]uint64{selfPID()}, nil)
		if native, ok := gerr.(strace.Error); ok && native.Code == vi.ErrEACCES {
			vi.ConsoleLine("trace: same-uid arm refused")
			return
		}
		check(fmt.Errorf("same-uid arm returned %v", gerr))
	case "overhead":
		overhead()
	case "capture-overhead":
		captureOverhead()
	default:
		check(fmt.Errorf("unknown fixture"))
	}
}

func self() {
	pid := selfPID()
	s, err := strace.Arm([]uint64{pid}, []uint64{60})
	check(err)
	// No reader or runtime call selects slot 60, so exact forced loss is 44.
	for i := 0; i < 300; i++ {
		vi.PingPoll()
	}
	check(s.Disarm())
	total, dropped := drain(s)
	if total != 256 || dropped != 44 {
		check(fmt.Errorf("wrap count=%d dropped=%d", total, dropped))
	}
	vi.ConsoleLine("trace: wrap records=256 dropped=44")
	s, err = strace.Arm([]uint64{pid}, []uint64{23, 70, 71})
	check(err)
	fd, rc := vi.FileOpen("/host/TRACE.IN", vi.ModeRead)
	if rc < 0 {
		check(strace.Error{Code: -rc})
	}
	vi.FileClose(uint32(fd))
	var secrets [1]vi.SecretRecord
	_, _ = vi.SecretList(secrets[:])
	_ = vi.TtyNetAuth(99, nil)
	check(s.Disarm())
	total, dropped = drain(s)
	if total != 3 || dropped != 0 {
		check(fmt.Errorf("decoded/redacted count=%d dropped=%d", total, dropped))
	}
	vi.ConsoleLine("trace: decoded/redacted done dropped=0")
	// A second process is never added to this session. Even its selected
	// calls must produce no records, while our own one call remains visible.
	s, err = strace.Arm([]uint64{pid}, []uint64{60})
	check(err)
	child, err := vi.Exec("TRACEFIX.ELF", "peer")
	check(err)
	_, err = vi.Wait(child)
	check(err)
	vi.PingPoll()
	check(s.Disarm())
	total, dropped = drain(s)
	if total != 1 || dropped != 0 {
		check(fmt.Errorf("untraced peer leaked count=%d dropped=%d", total, dropped))
	}
	vi.ConsoleLine("trace: peer excluded records=1 dropped=0")
}

const overheadPairs = 7

func cheapCalls(calls int) {
	for i := 0; i < calls; i++ {
		vi.PingPoll()
	}
}

func measure(calls int) int64 {
	start := vi.Nanos()
	cheapCalls(calls)
	duration := vi.Nanos() - start
	if duration <= 0 {
		check(fmt.Errorf("non-positive counter duration"))
	}
	return duration
}

func median(values []int64) int64 {
	copyValues := append([]int64(nil), values...)
	sort.Slice(copyValues, func(i, j int) bool { return copyValues[i] < copyValues[j] })
	return copyValues[len(copyValues)/2]
}

// Pair complete identical loops, alternating off then on. All arm/disarm,
// warmup and serial output are outside the measured interval. Report raw
// aggregate nanoseconds too, without claiming individual sub-counter-tick
// timings from an average.
func paired(pid uint64, name string, calls int, slots []uint64) (int64, int64) {
	var off, on, added [overheadPairs]int64
	var session *strace.Session
	for i := range off {
		if session != nil {
			check(session.Disarm())
		}
		cheapCalls(1000)
		off[i] = measure(calls)
		var err error
		session, err = strace.Arm([]uint64{pid}, slots)
		check(err)
		cheapCalls(1000)
		on[i] = measure(calls)
		added[i] = on[i] - off[i]
		vi.ConsoleLine(fmt.Sprintf("trace: pair path=%s index=%d calls=%d off_ns=%d on_ns=%d", name, i+1, calls, off[i], on[i]))
	}
	check(session.Disarm())
	sort.Slice(added[:], func(i, j int) bool { return added[i] < added[j] })
	vi.ConsoleLine(fmt.Sprintf("trace: paired-overhead path=%s pairs=%d calls=%d median_added_ns=%d min_added_ns=%d max_added_ns=%d",
		name, overheadPairs, calls, added[overheadPairs/2], added[0], added[overheadPairs-1]))
	return median(off[:]) / int64(calls), median(on[:]) / int64(calls)
}

func overhead() {
	pid := selfPID()
	baseline, filtered := paired(pid, "filtered", 10000, []uint64{})
	baseline100k, filtered100k := paired(pid, "filtered100k", 100000, []uint64{})
	tracedBaseline, traced := paired(pid, "traced", 10000, []uint64{60})
	vi.ConsoleLine(fmt.Sprintf("trace: overhead calls=10000 pairs=%d untraced=%d filtered=%d traced_untraced=%d traced=%d ns/call freq=%d",
		overheadPairs, baseline, filtered, tracedBaseline, traced, counterFrequency()))
	vi.ConsoleLine(fmt.Sprintf("trace: filtered-overhead calls=100000 pairs=%d untraced=%d filtered=%d ns/call", overheadPairs, baseline100k, filtered100k))
	vi.ConsoleLine("trace-overhead-done")
}

const captureCalls = 10000

func captureBatch(samples []uint64, path uintptr) {
	for i := range samples {
		ticks, result := captureCallTicks(path, vi.TraceStringBytes)
		if ticks == 0 || result != -vi.ErrEINVAL {
			check(fmt.Errorf("capture timing/refusal ticks=%d result=%d", ticks, result))
		}
		samples[i] = ticks
	}
}

func p95Ticks(samples []uint64) uint64 {
	values := append([]uint64(nil), samples...)
	sort.Slice(values, func(i, j int) bool { return values[i] < values[j] })
	return values[(95*len(values)+99)/100-1]
}

func p95AddedTicks(samples []int64) int64 {
	values := append([]int64(nil), samples...)
	sort.Slice(values, func(i, j int) bool { return values[i] < values[j] })
	return values[(95*len(values)+99)/100-1]
}

func totalTicks(samples []uint64) uint64 {
	var total uint64
	for _, ticks := range samples {
		total += ticks
	}
	return total
}

// Binary samples are written only after all timed pairs. Each little-endian
// u64 is one serialized CNTPCT duration, off array then on array for each pair.
func writeCaptureSamples(samples []uint64) {
	buffer := make([]byte, len(samples)*8)
	for i, ticks := range samples {
		binary.LittleEndian.PutUint64(buffer[i*8:], ticks)
	}
	if rc := vi.WriteFileSafe("/host/TRACE-CAPTURE.TICKS", buffer); rc < 0 {
		check(strace.Error{Code: -rc})
	}
}

func captureOverhead() {
	pid := selfPID()
	frequency := counterFrequency()
	if frequency == 0 {
		check(fmt.Errorf("guest counter unavailable"))
	}
	var path [vi.TraceStringBytes]byte
	for i := range path {
		path[i] = 'x'
	}
	copy(path[:], "/host/TRACE-CAPTURE-")
	var pin runtime.Pinner
	pin.Pin(&path[0])
	defer pin.Unpin()
	address := uintptr(unsafe.Pointer(&path[0]))
	// Allocate and touch every sample page before any timing interval.
	samples := make([]uint64, overheadPairs*2*captureCalls)
	for i := range samples {
		samples[i] = 1
	}
	added := make([]int64, overheadPairs*captureCalls)
	var warmup [1000]uint64
	var session *strace.Session
	var sumOff, sumOn uint64
	for pair := 0; pair < overheadPairs; pair++ {
		if session != nil {
			check(session.Disarm())
		}
		off := samples[pair*2*captureCalls : (pair*2+1)*captureCalls]
		on := samples[(pair*2+1)*captureCalls : (pair*2+2)*captureCalls]
		captureBatch(warmup[:], address)
		captureBatch(off, address)
		var err error
		session, err = strace.Arm([]uint64{pid}, []uint64{23})
		check(err)
		captureBatch(warmup[:], address)
		captureBatch(on, address)
		check(session.Disarm())
		status, err := session.Status()
		check(err)
		if status.Count != vi.TraceRingRecords ||
			status.Dropped != uint64(len(warmup)+captureCalls-vi.TraceRingRecords) {
			check(fmt.Errorf("capture count=%d dropped=%d", status.Count, status.Dropped))
		}
		records, _, err := session.Read()
		check(err)
		if len(records) != vi.ObserveMaxReadRecords {
			check(fmt.Errorf("missing measured capture records"))
		}
		for _, record := range records {
			if record.Number != 23 || record.PID != pid || record.Args[0] != uint64(address) ||
				record.Args[1] != vi.TraceStringBytes || record.Args[2] != 0 ||
				record.StringMask != 1 || record.FaultMask != 0 || record.TruncatedMask != 0 ||
				record.StringLengths[0] != vi.TraceStringBytes || record.Strings[0] != path ||
				record.Result != -vi.ErrEINVAL || record.Errno != uint32(vi.ErrEINVAL) {
				check(fmt.Errorf("measured call did not capture all 128 bytes"))
			}
		}
		for i := range off {
			added[pair*captureCalls+i] = int64(on[i]) - int64(off[i])
		}
		offTotal, onTotal := totalTicks(off), totalTicks(on)
		sumOff += offTotal
		sumOn += onTotal
		vi.ConsoleLine(fmt.Sprintf("trace: capture-pair index=%d calls=%d off_ticks=%d on_ticks=%d off_p95_ticks=%d on_p95_ticks=%d added_p95_ticks=%d",
			pair+1, captureCalls, offTotal, onTotal, p95Ticks(off), p95Ticks(on), p95AddedTicks(added[pair*captureCalls:(pair+1)*captureCalls])))
		// Already disarmed; leave the next off half completely untraced.
		session = nil
	}
	vi.ConsoleLine(fmt.Sprintf("trace: capture-overhead pairs=%d calls=%d bytes=128 freq=%d off_ticks=%d on_ticks=%d added_p95_ticks=%d",
		overheadPairs, captureCalls, frequency, sumOff, sumOn, p95AddedTicks(added)))
	writeCaptureSamples(samples)
	vi.ConsoleLine("trace-capture-overhead-done")
	runtime.KeepAlive(path)
}
