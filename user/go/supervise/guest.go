package supervise

import (
	"errors"

	"virelai/vi"
)

// GuestHooks uses the existing vi ABI, including WriteCrashReceipt unchanged.
// There is no sys_wait, death notification, or dependency/manifest import.
func GuestHooks() Hooks {
	return Hooks{
		Now: vi.Nanos, Exec: vi.Exec, Kill: vi.Kill, Random: vi.Random,
		Receipt: vi.WriteCrashReceipt, Serial: vi.ConsoleLine,
		Table: func() ([]Process, error) {
			var rows [64]vi.ProcRow
			n, rc := vi.Procs(rows[:])
			if rc < 0 {
				return nil, errors.New("supervise: process table unavailable")
			}
			out := make([]Process, n)
			for i := range out {
				out[i] = Process{PID: int64(rows[i].PID),
					Running: rows[i].State == vi.ProcRunning,
					Exited:  rows[i].State == vi.ProcExited, Status: int64(rows[i].ExitStatus)}
			}
			return out, nil
		},
	}
}
