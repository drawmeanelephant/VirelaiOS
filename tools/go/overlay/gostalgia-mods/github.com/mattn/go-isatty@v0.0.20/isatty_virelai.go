//go:build virelai

package isatty

// Virelai has no POSIX tty to probe: the app's terminal is the kernel's
// bound /dev/tty queue, not a host file descriptor. Report "not a
// terminal" the way the js/appengine fallback does; colour still flows
// because the termenv virelai shim pins the ANSI256 profile
// independently of this.
//
// Mirrored from tools/go/overlay/charm/isatty_virelai.go.
func IsTerminal(fd uintptr) bool {
	return false
}

// IsCygwinTerminal reports whether the file descriptor is a cygwin or
// msys2 terminal. Always false on Virelai.
func IsCygwinTerminal(fd uintptr) bool {
	return false
}
