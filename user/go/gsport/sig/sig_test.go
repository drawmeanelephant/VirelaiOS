package sig

import (
	"context"
	"testing"
	"time"
)

// TestShutdownSignalsExplicitlyEmpty pins the empty-set answer: the slice
// must be non-nil AND empty. A revert that names os.Interrupt or a
// syscall.SIG* constant fails len()==0; the guard refuses the os/signal
// import such a revert would take.
func TestShutdownSignalsExplicitlyEmpty(t *testing.T) {
	sigs := ShutdownSignals()
	if sigs == nil {
		t.Fatal("ShutdownSignals returned nil — must be the explicit empty slice")
	}
	if len(sigs) != 0 {
		t.Fatalf("ShutdownSignals = %v — virelai has no async signal delivery", sigs)
	}
}

func TestRequestShutdown(t *testing.T) {
	c := New()
	select {
	case <-c.Done():
		t.Fatal("Done closed before RequestShutdown")
	default:
	}
	c.RequestShutdown()
	c.RequestShutdown() // idempotent
	select {
	case <-c.Done():
	case <-time.After(time.Second):
		t.Fatal("Done never closed after RequestShutdown")
	}
}

func TestNotifyContext(t *testing.T) {
	c := New()
	ctx, stop := c.NotifyContext(context.Background())
	defer stop()
	select {
	case <-ctx.Done():
		t.Fatal("ctx done before RequestShutdown")
	default:
	}
	c.RequestShutdown()
	select {
	case <-ctx.Done():
	case <-time.After(time.Second):
		t.Fatal("ctx never done after RequestShutdown")
	}
}

// signalFiresOnlyViaController: there is no other producer — a second
// controller must NOT observe the first's request.
func TestControllersAreIndependent(t *testing.T) {
	a, b := New(), New()
	a.RequestShutdown()
	select {
	case <-b.Done():
		t.Fatal("controller b observed a's request")
	default:
	}
}
