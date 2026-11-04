//go:build linux && !virelai

package main

import "syscall"

// statCtime reads the host's change-time field — Ctim on Linux.
func statCtime(sb *syscall.Stat_t) (int64, bool) {
	return sb.Ctim.Sec*1e9 + sb.Ctim.Nsec, true
}
