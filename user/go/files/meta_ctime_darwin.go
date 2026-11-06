//go:build darwin && !virelai

package main

import "syscall"

// statCtime reads the host's change-time field — Ctimespec on Darwin.
func statCtime(sb *syscall.Stat_t) (int64, bool) {
	return sb.Ctimespec.Sec*1e9 + sb.Ctimespec.Nsec, true
}
