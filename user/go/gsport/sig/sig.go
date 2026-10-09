// Package sig is the gsport signal adapter: an explicit answer for a guest
// with no signals at all, plus the shutdown-request seam M95c wires to
// window-close and Ctrl-C input bytes.
//
// Host assumption it replaces: Gostalgia calls
// signal.NotifyContext(ctx, platform.ShutdownSignals()...) where unix
// answers SIGINT/SIGTERM and windows answers os.Interrupt. VirelaiOS has
// NO asynchronous signal delivery — no slot, no kernel mechanism — so the
// platform overlay must return an explicitly EMPTY set, and the only
// shutdown request a guest can observe is one another part of the runtime
// delivers in-band (a window-close event, a ^C byte), which the
// Controller here funnels to the same Done/NotifyContext contract the
// standard API uses.
//
// Slots: none.
//
// Refuses by name: nothing — the refusal IS the empty signal set; the
// overlay and tests pin that it is empty rather than "whatever the OS
// guessed".
package sig

import (
	"context"
	"os"
	"sync"
)

// ShutdownSignals is the platform-overlay answer: the Virelai signal set
// is EMPTY. Returning a non-nil zero-length slice is deliberate — callers
// must not read nil as "unset" and fall back to a POSIX default.
//
// os/signal.NotifyContext with zero signals never fires its own ctx, so
// an empty set composed through it is inert — but the overlay returns this
// slice so a future caller that DOES name signals fails review, not just
// the guest.
func ShutdownSignals() []os.Signal { return []os.Signal{} }

// Controller is the in-band shutdown request channel. One per runtime;
// the producer (M95c's input path, or the sys/shutdown service) calls
// RequestShutdown, and consumers wait on Done or compose a context through
// NotifyContext — the same shape signal.NotifyContext presents, so the
// host habit transfers without pretending a signal arrived.
type Controller struct {
	done chan struct{}
	once sync.Once
}

func New() *Controller { return &Controller{done: make(chan struct{})} }

// RequestShutdown asks the runtime to stop. Idempotent.
func (c *Controller) RequestShutdown() {
	c.once.Do(func() { close(c.done) })
}

// Done reports the request. The zero value is nil — a caller that never
// sees Done never blocks on it; compose through NotifyContext instead.
func (c *Controller) Done() <-chan struct{} { return c.done }

// NotifyContext returns a ctx done when the shutdown lands or parent
// ends, and a stop that releases it — signal.NotifyContext's contract
// exactly, minus the signals.
func (c *Controller) NotifyContext(parent context.Context) (context.Context, context.CancelFunc) {
	ctx, cancel := context.WithCancel(parent)
	go func() {
		select {
		case <-c.done:
			cancel()
		case <-ctx.Done():
		}
	}()
	return ctx, cancel
}
