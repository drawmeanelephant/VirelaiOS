//go:build virelai

package tea

import (
	"fmt"
	"os"

	"github.com/charmbracelet/x/term"
)

// The unix shape minus termios: the guest's tty is the kernel's bound
// /dev/tty behind a hosted window, so "is a terminal" is gsport/tty's
// bound-fd answer and MakeRaw is an honest no-op — there is no line
// discipline to enter or restore. ttyInput/ttyOutput set as usual so
// checkResize and the renderer see a real terminal.
func (p *Program) initInput() (err error) {
	// Check if input is a terminal
	if f, ok := p.input.(term.File); ok && term.IsTerminal(f.Fd()) {
		p.ttyInput = f
		p.previousTtyInputState, err = term.MakeRaw(p.ttyInput.Fd())
		if err != nil {
			return fmt.Errorf("error entering raw mode: %w", err)
		}
	}

	if f, ok := p.output.(term.File); ok && term.IsTerminal(f.Fd()) {
		p.ttyOutput = f
	}

	return nil
}

// openInputTTY opens the process's controlling terminal. On virelai this
// is always the bound /dev/tty — the caller binds the window front-end
// before the Program runs (gsport/tty.Open), so this stays a plain open
// and never a host-side fallback.
func openInputTTY() (*os.File, error) {
	f, err := os.OpenFile("/dev/tty", os.O_RDWR, 0)
	if err != nil {
		return nil, fmt.Errorf("could not open a new TTY: %w", err)
	}
	return f, nil
}

// The guest has no SIGTSTP and no process-group signal to suspend on;
// suspension is refused by absence, as on Windows.
const suspendSupported = false

func suspendProcess() {}
