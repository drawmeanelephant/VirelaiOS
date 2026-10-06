//go:build !virelai

package main

import (
	"unsafe"

	"virelai/vi"
)

func counterFrequency() uint64 { return 0 }

// Host tests exercise the call shape only; zero frequency forbids treating
// the sentinel tick below as guest counter evidence.
func captureCallTicks(path uintptr, length uint64) (uint64, int64) {
	_, result := vi.FileOpen(unsafe.String((*byte)(unsafe.Pointer(path)), int(length)), 0)
	return 1, result
}
