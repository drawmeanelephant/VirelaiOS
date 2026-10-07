package main

import (
	"runtime"
	"time"

	"virelai/heap"
	"virelai/vi"
)

const blockBytes = 512 * 1024
const iterations = 8

var retained [][]byte
var checksum byte

//go:noinline
func allocate(leak bool, iteration int) {
	block := make([]byte, blockBytes)
	for i := range block {
		block[i] = byte(i + iteration)
	}
	checksum ^= block[iteration]
	if leak {
		retained = append(retained, block)
	}
	runtime.KeepAlive(block)
}

func main() {
	args := vi.Args()
	if len(args) != 2 || (args[1] != "leak" && args[1] != "clean") {
		vi.ConsoleLine("heapfixture: usage leak|clean")
		vi.Exit(1)
	}
	publisher, err := heap.NewPublisher("HEAPFIX.ELF")
	if err != nil {
		vi.ConsoleLine(err.Error())
		vi.Exit(1)
	}
	// The FIRST guest job proves forced GC + ReadMemStats before the workload.
	if _, err := publisher.Capture(); err != nil {
		vi.ConsoleLine(err.Error())
		vi.Exit(1)
	}
	vi.ConsoleLine("heapfixture: ready mode=" + args[1])
	for i := 0; i < iterations; i++ {
		allocate(args[1] == "leak", i)
		time.Sleep(2 * time.Second)
		if _, err := publisher.Capture(); err != nil {
			vi.ConsoleLine(err.Error())
			vi.Exit(1)
		}
	}
	vi.ConsoleLine("heapfixture: complete mode=" + args[1])
	// Keep the target live while the viewer takes its final snapshot.
	time.Sleep(60 * time.Second)
	runtime.KeepAlive(retained)
}
