package vi

// M216e (#3944): the Arc4 #242 unsaved flag (kernel slot 53,
// `sys_win_set_unsaved(id, flag)` — kernel/src/syscall.zig) had no Go
// wrapper. The kernel stores the bit per window and mirrors it to the
// seat in kind-20 `flags` bit 12, which is what the rail's dirty dot and
// the seat's close-time Reopen toast read.
//
// WinSetUnsaved marks window id's document dirty (on) or clean. The
// kernel refuses (negative rc) an id the caller does not own and a flag
// outside {0,1}; the seat's own windows simply never call it.
func WinSetUnsaved(id int, on bool) int64 {
	flag := uintptr(0)
	if on {
		flag = 1
	}
	return syscall3(SlotWinSetUnsaved, uintptr(id), flag, 0)
}
