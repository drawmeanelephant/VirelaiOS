// Quiet-host memstat timing: no allocation workload or periodic publisher.
package main

import (
	"fmt"
	"runtime"
	"time"

	"virelai/heap"
	"virelai/vi"
)

func main() {
	row, err := heap.Resolve("HEAPBUDG.ELF")
	if err != nil {
		vi.ConsoleLine(err.Error())
		vi.Exit(1)
	}
	runtime.GC()
	for i := 0; i < 100; i++ {
		record, elapsed, err := vi.HeapStatTimed(row.PID)
		if err != nil {
			vi.ConsoleLine(err.Error())
			vi.Exit(1)
		}
		vi.ConsoleLine(fmt.Sprintf("heap: budget seq=%d ns=%d cntpct=%d pages=%d peak=%d",
			i+1, elapsed, record.CNTPCT, record.LivePages, record.PeakPages))
		if i != 99 {
			time.Sleep(time.Second)
		}
	}
	vi.ConsoleLine("heap: budget done polls=100")
}
