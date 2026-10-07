package heap

import (
	"fmt"
	"strconv"
	"time"

	"virelai/vi"
)

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
	row, err := Resolve(target)
	if err != nil {
		return err
	}
	if polls < 1 || polls > MaxRows {
		return fmt.Errorf("heap: require 1..%d polls", MaxRows)
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
			body, rc := vi.ReadFileAll(path, MaxBytes+1)
			if rc >= 0 {
				samples, parseErr := ParseSeries(body)
				if parseErr != nil {
					return parseErr
				}
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
			} else if rc != vi.ErrFileNotFound {
				return fmt.Errorf("heap: read rc=%d", rc)
			}
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
	vi.ConsoleLine(fmt.Sprintf("heap: done app=%s samples=%d", app, len(series)))
	return nil
}

func Run(args []string) error {
	if len(args) != 3 && len(args) != 5 || len(args) < 3 || args[1] != "-p" {
		return fmt.Errorf("usage: HEAP.ELF -p <pid|name> [--polls 1..32]")
	}
	polls := 16
	if len(args) == 5 {
		var err error
		polls, err = strconv.Atoi(args[4])
		if args[3] != "--polls" || err != nil {
			return fmt.Errorf("heap: invalid poll count")
		}
	}
	return View(args[2], polls)
}
