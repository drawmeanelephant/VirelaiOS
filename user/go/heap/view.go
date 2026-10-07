package heap

import (
	"fmt"
	"strconv"
	"time"

	"virelai/vi"
)

const MaxPolls = 120

func Resolve(target string) (vi.ProcRow, error) {
	pid, numberErr := strconv.ParseUint(target, 10, 64)
	var rows [16]vi.ProcRow
	// One immediate re-read tolerates the existing suspect sys_procs scan.
	for attempt := 0; attempt < 2; attempt++ {
		n, rc := vi.Procs(rows[:])
		if rc < 0 {
			return vi.ProcRow{}, fmt.Errorf("heap: procs rc=%d", rc)
		}
		var found vi.ProcRow
		matches := 0
		for _, row := range rows[:n] {
			if (numberErr == nil && row.PID == pid) ||
				(numberErr != nil && row.Name() == target && row.State == vi.ProcRunning) {
				found = row
				matches++
			}
		}
		if matches == 1 {
			return found, nil
		}
		if matches > 1 {
			return vi.ProcRow{}, fmt.Errorf("heap: ambiguous target %s", target)
		}
	}
	return vi.ProcRow{}, fmt.Errorf("heap: target not found %s", target)
}

// View is shared with the combined OBSERVE tool. The independent share
// channel is opt-in diagnostics, not an authorization or isolation boundary.
// Only new post-GC samples are joined; repeated reads do not extend a trend.
func View(target string, polls int) error {
	return ViewUntil(target, polls, 0)
}

// ViewUntil observes until the requested number of joined post-GC samples,
// or refuses at the caller's finite poll deadline.
func ViewUntil(target string, polls, minimum int) error {
	row, err := Resolve(target)
	if err != nil {
		return err
	}
	if polls < 1 || polls > MaxPolls || minimum < 0 || minimum > MaxRows {
		return fmt.Errorf("heap: require 1..%d polls and 0..%d samples", MaxPolls, MaxRows)
	}
	app := row.Name()
	path := Path(app)
	var series []Joined
	var last Sample
	flagged := false
	for poll := 0; poll < polls; poll++ {
		kernel, elapsed, err := vi.HeapStatTimed(row.PID)
		if err != nil {
			return err
		}
		vi.ConsoleLine(fmt.Sprintf("heap: kernel app=%s pid=%d pages=%d peak_pages=%d regions=%d static_pages=%d total_pages=%d record_failures=%d cntpct=%d snapshot_ns=%d",
			app, kernel.PID, kernel.LivePages, kernel.PeakPages, kernel.LiveRegions,
			kernel.StaticPages, kernel.TotalPages, kernel.RecordFailures, kernel.CNTPCT, elapsed))
		vi.ConsoleLine(fmt.Sprintf("heap: image text=%d ro=%d data=%d stack=%d regions=%v",
			kernel.TextBytes, kernel.ROBytes, kernel.DataBytes, kernel.StackBytes, kernel.RegionSizes))
		if path != "" && kernel.Flags&vi.MemstatExited == 0 {
			samples, readErr := readSeries(path, vi.ReadFileAll, time.Sleep)
			if readErr != nil {
				return readErr
			}
			if len(samples) != 0 {
				sample := samples[len(samples)-1]
				if sample.PID == row.PID && (sample.Session != last.Session || sample.Seq != last.Seq) {
					if len(series) != 0 && !consecutive(last, sample) {
						series = nil
						flagged = false
					}
					last = sample
					series = append(series, Joined{Sample: sample, Kernel: kernel})
					vi.ConsoleLine(fmt.Sprintf("heap: sample app=%s pid=%d seq=%d live=%d objects=%d mallocs=%d frees=%d num_gc=%d pages=%d peak_pages=%d",
						app, sample.PID, sample.Seq, sample.LiveBytes, sample.Objects,
						sample.Mallocs, sample.Frees, sample.NumGC, kernel.LivePages, kernel.PeakPages))
					if !flagged && Suspected(series) {
						flagged = true
						vi.ConsoleLine("heap: leak suspected app=" + app)
					}
				}
			}
		}
		if minimum != 0 && len(series) >= minimum {
			break
		}
		if kernel.Flags&vi.MemstatExited != 0 {
			break
		}
		if poll+1 < polls {
			// Runtime timers use a monotonic deadline, not one potentially
			// fractional kernel tick. Park the G without occupying a P.
			time.Sleep(time.Second)
		}
	}
	if len(series) < minimum {
		return fmt.Errorf("heap: sample deadline app=%s got=%d need=%d", app, len(series), minimum)
	}
	vi.ConsoleLine(fmt.Sprintf("heap: done app=%s samples=%d", app, len(series)))
	return nil
}

// Safe publication has a delete/rename gap. The stateless file ABI may return
// successful EOF during it, even after a successful open. Reopen at most three
// times; never accept partial rows and never turn persistent corruption green.
func readSeries(path string, read func(string, int) ([]byte, int64), sleep func(time.Duration)) ([]Sample, error) {
	var last error
	missing := false
	for attempt := 0; attempt < 3; attempt++ {
		body, rc := read(path, MaxBytes+1)
		missing = rc == vi.ErrFileNotFound
		if rc < 0 && !missing {
			return nil, fmt.Errorf("heap: read rc=%d", rc)
		}
		if !missing {
			samples, err := ParseSeries(body)
			if err == nil {
				return samples, nil
			}
			last = err
		}
		if attempt != 2 {
			vi.ConsoleLine("heap: publication view retry")
			sleep(time.Second)
		}
	}
	if missing {
		return nil, nil
	}
	return nil, last
}

func Run(args []string) error {
	if len(args) < 3 || len(args)%2 != 1 || len(args) > 7 || args[1] != "-p" {
		return fmt.Errorf("usage: HEAP.ELF -p <pid|name> [--polls 1..120] [--samples 1..32]")
	}
	polls, minimum := 16, 0
	seen := map[string]bool{}
	for i := 3; i < len(args); i += 2 {
		value, err := strconv.Atoi(args[i+1])
		if err != nil || seen[args[i]] {
			return fmt.Errorf("heap: invalid option")
		}
		seen[args[i]] = true
		switch args[i] {
		case "--polls":
			polls = value
		case "--samples":
			if value < 1 {
				return fmt.Errorf("heap: invalid sample count")
			}
			minimum = value
		default:
			return fmt.Errorf("heap: invalid option")
		}
	}
	return ViewUntil(args[2], polls, minimum)
}
