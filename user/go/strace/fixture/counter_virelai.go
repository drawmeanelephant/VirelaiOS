//go:build virelai && arm64

package main

func counterFrequency() uint64

// The assembler brackets only the SVC and its return synchronization.
func captureCallTicks(path uintptr, length uint64) (ticks uint64, result int64)
