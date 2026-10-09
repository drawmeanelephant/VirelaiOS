//go:build virelai

package tea

import (
	"virelai/gsport/abi"
	"virelai/gsport/tty"
)

// listenForResize is the Virelai replacement for the SIGWINCH watcher:
// the guest has no signal delivery and no winsize ioctl, so the Program
// polls its own ADR 0009 event queue and delivers tea.WindowSizeMsg in
// CELLS — the TUI contract is cells, never pixels.
//
// Ownership contract (kept from the v2 overlay, signals_virelai.go):
// when this runs, THIS goroutine owns the event queue — a Program-loop
// app must not poll vi.PollEventRaw a second time. gsport/tty and the
// cmd wiring never poll; they only consume what this goroutine records.
//
//   - EvWinResize (kind 10): the kernel pushes the post-clamp frame rect
//     (arg0 w / arg1 h) — the seat's canvas grant, a split relayout and a
//     font zoom all arrive through it. gsport/tty.NoteResize re-reads
//     the active cell rung (the zoom leg) and stores the cells that
//     checkResize/GetSize answers; then the msg reaches the model.
//   - EvWinClose (kind 8): the window's close gesture requests shutdown
//     on the in-band seam (gsport/sig — where ^C/^D also land) and ends
//     the Program with a QuitMsg, so window-close and Ctrl-C/D converge
//     on one teardown.
func (p *Program) listenForResize(done chan struct{}) {
	defer close(done)
	for {
		select {
		case <-p.ctx.Done():
			return
		default:
		}
		ev, _, ok := abi.PollEventRaw()
		if !ok {
			// Non-blocking poll: stay cooperative, one tick per idle.
			abi.Sleep(1)
			continue
		}
		switch ev.Kind {
		case abi.EvWinResize:
			cols, rows := tty.NoteResize(ev.Arg0, ev.Arg1)
			p.Send(WindowSizeMsg{Width: cols, Height: rows})
		case abi.EvWinClose:
			tty.NotifyClose()
			p.Send(QuitMsg{})
			return
		}
	}
}
