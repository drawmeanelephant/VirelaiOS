//go:build virelai

package platform

import (
	"os"

	"virelai/gsport/sig"
)

// ShutdownSignals returns the platform's shutdown signal set. VirelaiOS
// has NO asynchronous signal delivery — no slot, no kernel mechanism —
// so the honest answer is the explicitly empty set from gsport/sig, not
// a guessed unix default that os/signal would never deliver. Shutdown
// requests reach the runtime in-band instead (window-close events, the
// sys/shutdown IPC route); gsport/sig.Controller is that seam.
func ShutdownSignals() []os.Signal { return sig.ShutdownSignals() }
