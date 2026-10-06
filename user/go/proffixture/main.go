// The profiler-only fixed-work fixture uses the same integer work in every
// run. Timing excludes ARM, reads, sleeps and serial output. GOMAXPROCS=1
// keeps the worker count unchanged between off and on measurements.
package main

import (
	"fmt"
	"runtime"
	"sort"

	"virelai/vi"
)

var sink uint64

// Keep the existing host ELF/argv inspector's tail-page guard satisfied.
var argvEnvpGuard [2048]byte

//go:noinline
func fixedWork(seed uint64) uint64 {
	for i := uint64(0); i < 80_000_000; i++ {
		seed ^= seed << 13
		seed ^= seed >> 7
		seed ^= seed << 17
	}
	return seed
}

func selfPID() (uint64, error) {
	var rows [16]vi.ProcRow
	n, raw := vi.Procs(rows[:])
	if raw < 0 {
		return 0, fmt.Errorf("process snapshot %d", raw)
	}
	for _, row := range rows[:n] {
		if row.Name() == "PROFFIX.ELF" && row.State == vi.ProcRunning {
			return row.PID, nil
		}
	}
	return 0, fmt.Errorf("no fixture pid")
}

func median(values []int64) int64 {
	copy_ := append([]int64(nil), values...)
	sort.Slice(copy_, func(i, j int) bool { return copy_[i] < copy_[j] })
	return copy_[len(copy_)/2]
}

func run() error {
	runtime.GOMAXPROCS(1)
	pid, err := selfPID()
	if err != nil {
		return err
	}
	if args := vi.Args(); len(args) == 2 && args[1] == "--timer" {
		token, err := vi.ProfileStart([]uint64{pid})
		if err != nil {
			return err
		}
		start := vi.Nanos()
		vi.Sleep(12)
		if err := vi.ProfileStop(token); err != nil {
			return err
		}
		if vi.Nanos()-start < 10_000_000_000 {
			return fmt.Errorf("short timer probe")
		}
		fmt.Println("proffixture: timer done")
		fmt.Println("proffixture: done")
		return nil
	}
	if args := vi.Args(); len(args) == 2 && args[1] == "--samples" {
		token, err := vi.ProfileStart([]uint64{pid})
		if err != nil {
			return err
		}
		vi.Sleep(1)
		start := vi.Nanos()
		for i := 0; i < 3; i++ {
			sink = fixedWork(1)
		}
		elapsed := vi.Nanos() - start
		if err := vi.ProfileStop(token); err != nil {
			return err
		}
		var count, dropped uint64
		for batch := 0; batch < vi.ProfileRingRecords/16; batch++ {
			samples, loss, err := vi.ProfileSamples(token)
			if err != nil {
				return err
			}
			count += uint64(len(samples))
			dropped = loss
			if len(samples) < 16 {
				break
			}
		}
		fmt.Printf("prof: sample_probe samples=%d dropped=%d work_ns=%d\n", count, dropped, elapsed)
		fmt.Println("proffixture: done")
		return nil
	}
	// Populate runtime and code pages before either measurement arm.
	sink = fixedWork(1)
	argvEnvpGuard[0] = byte(sink)
	runtime.KeepAlive(&argvEnvpGuard)
	var off, on [5]int64
	var sampleCount uint64
	var dropped uint64
	measure := func(values *[5]int64, token uint64, label string) error {
		for i := range values {
			vi.Sleep(1)
			start := vi.Nanos()
			value := fixedWork(1)
			elapsed := vi.Nanos() - start
			if value != sink || elapsed <= 0 {
				return fmt.Errorf("invalid fixed-work result or clock")
			}
			values[i] = elapsed
			if token != 0 {
				for batch := 0; batch < vi.ProfileRingRecords/16; batch++ {
					samples, loss, err := vi.ProfileSamples(token)
					if err != nil {
						return err
					}
					sampleCount += uint64(len(samples))
					dropped = loss
					if len(samples) < 16 {
						break
					}
				}
			}
			fmt.Printf("prof: work mode=%s run=%d ns=%d checksum=%x\n", label, i+1, elapsed, value)
		}
		return nil
	}
	if err := measure(&off, 0, "off"); err != nil {
		return err
	}
	token, err := vi.ProfileStart([]uint64{pid})
	if err != nil {
		return err
	}
	defer vi.ProfileStop(token)
	if err := measure(&on, token, "on"); err != nil {
		return err
	}
	// A full counter-timed delivery window on BOTH cores. Sleeping here
	// explicitly distinguishes hardware IRQ delivery from fixture polling.
	start := vi.Nanos()
	vi.Sleep(12)
	if vi.Nanos()-start < 10_000_000_000 {
		return fmt.Errorf("short delivery window")
	}
	if err := vi.ProfileStop(token); err != nil {
		return err
	}
	offMedian, onMedian := median(off[:]), median(on[:])
	fmt.Printf("prof: samples=%d dropped=%d off_median_ns=%d on_median_ns=%d overhead_pct=%.6f\n",
		sampleCount, dropped, offMedian, onMedian,
		100*float64(onMedian-offMedian)/float64(offMedian))
	fmt.Println("proffixture: done")
	return nil
}

func main() {
	if err := run(); err != nil {
		fmt.Println("proffixture: FAIL", err)
		fmt.Println("proffixture: done")
		vi.Exit(1)
	}
}
