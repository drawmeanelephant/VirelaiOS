//go:build virelai

package termenv

import "io"

// Virelai has no termios, no ioctl winsize, and no queryable terminal
// attributes. These are the same no-op fallbacks termenv ships for
// js/plan9/aix (termenv_other.go), retagged for the guest: the bound
// window grid always speaks ANSI, so the profile is unconditionally
// ANSI256 and lipgloss keeps its colours without probing anything.
//
// Mirrored from tools/go/overlay/charm/termenv_virelai.go (the v2 shim);
// muesli/termenv is the v1 API — same answer, older module.
func (o Output) ColorProfile() Profile {
	return ANSI256
}

func (o Output) foregroundColor() Color {
	// default gray
	return ANSIColor(7)
}

func (o Output) backgroundColor() Color {
	// default black
	return ANSIColor(0)
}

// EnableVirtualTerminalProcessing enables virtual terminal processing on
// Windows for w and returns a function that restores w to its previous
// state. On non-Windows platforms, or if w does not refer to a terminal,
// then it returns a non-nil no-op function and no error.
func EnableVirtualTerminalProcessing(w io.Writer) (func() error, error) {
	return func() error { return nil }, nil
}
