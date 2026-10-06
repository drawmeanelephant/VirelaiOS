package main

import (
	"fmt"
	"sort"

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
		check(fmt.Errorf("usage: TRACEFIX self|deny|overhead|peer"))
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
	case "overhead":
		overhead()
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

func measure(calls int) int64 {
	start := vi.Nanos()
	for i := 0; i < calls; i++ {
		vi.PingPoll()
	}
	duration := vi.Nanos() - start
	if duration <= 0 {
		check(fmt.Errorf("non-positive counter duration"))
	}
	return duration / int64(calls)
}

func median(calls int) int64 {
	var runs [5]int64
	for i := range runs {
		runs[i] = measure(calls)
	}
	sort.Slice(runs[:], func(i, j int) bool { return runs[i] < runs[j] })
	return runs[2]
}

func overhead() {
	pid := selfPID()
	baseline := median(10000)
	s, err := strace.Arm([]uint64{pid}, []uint64{})
	check(err)
	filtered := median(10000)
	// ADR 0043 also asks for >=100,000 filtered-out calls against a baseline.
	check(s.Disarm())
	baseline100k := median(100000)
	check(s.Filter([]uint64{pid}, []uint64{}))
	// FILTER preserves disarmed state; opening is the explicit activation.
	s, err = strace.Arm([]uint64{pid}, []uint64{})
	check(err)
	filtered100k := median(100000)
	check(s.Disarm())
	s, err = strace.Arm([]uint64{pid}, []uint64{60})
	check(err)
	traced := median(10000)
	check(s.Disarm())
	vi.ConsoleLine(fmt.Sprintf("trace: overhead calls=10000 runs=5 untraced=%d filtered=%d traced=%d ns/call freq=%d", baseline, filtered, traced, counterFrequency()))
	vi.ConsoleLine(fmt.Sprintf("trace: filtered-overhead calls=100000 runs=5 untraced=%d filtered=%d ns/call", baseline100k, filtered100k))
	vi.ConsoleLine("trace-overhead-done")
}
