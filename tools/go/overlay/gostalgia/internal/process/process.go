// pinned-sha256: 91e35fe146a7af873d68c92b4c4d84fce54d69d12616a81ae4d2c2e302717644
//
// Overlay replacement for the pinned internal/process/process.go (M95e).
// The pin's "child" kind is a host process (os/exec fork/exec/wait/kill);
// none of that exists on VirelaiOS. This replacement keeps the pinned API
// and the inproc kind verbatim, and maps child onto the gsport adapter:
//
//	child spec -> gsport/child (EL0 spawn via slot 28, liveness polled
//	              from the slot-7 registry, kill armed via slot 29)
//	restart/backoff/give-up/receipts -> virelai/supervise (M92c, ADR 0042)
//
// Status honesty: a child process's registry status is the truth — 0 is a
// clean exit (StateStopped), any nonzero status is StateFailed including
// the kernel's reserved 137 (external kill) and 139 (unhandled fault).
// A supervisor-initiated Stop is never a failure: supervise folds it to
// Stopped before the watcher here reads a terminal state.
//
// Capture: an EL0 child has no stdout pipe. Bounded capture is the M82e
// per-app log ring under /host/APPLOG/<label>.LOG (32 x 256 B), readable
// through gsport/child.Manager.LogTail — cooperating children only.
//
// Nothing in this package pretends a goroutine is an OS process.
package process

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sort"
	"sync"
	"time"

	"gostalgia/internal/events"
	"gostalgia/internal/ipc"
	"gostalgia/internal/security"

	"virelai/gsport/child"
	"virelai/supervise"
)

type Kind string

const (
	KindInProc Kind = "inproc"
	KindChild  Kind = "child"
)

type State string

const (
	StateStarting State = "starting"
	StateRunning  State = "running"
	StateStopping State = "stopping"
	StateStopped  State = "stopped"
	StateFailed   State = "failed"
)

// Spec describes a process to start.
type Spec struct {
	Name      string   `json:"name"`              // logical name (app id, child label)
	Kind      Kind     `json:"kind"`              // inproc or child
	SessionID string   `json:"session,omitempty"` // owning session
	User      string   `json:"user,omitempty"`    // owning user
	Caps      []string `json:"caps,omitempty"`    // granted capabilities
	Args      []string `json:"args,omitempty"`    // child only: program and arguments
	Dir       string   `json:"dir,omitempty"`     // child only: refused on virelai (no cwd)
	Env       []string `json:"env,omitempty"`     // child only: refused on virelai (no environ)

	// Restart is the ADR 0042 supervision contract for child processes.
	// The zero value is "never" — the pin's no-restart behaviour.
	Restart supervise.Policy `json:"restart,omitempty"`
}

// Info is a snapshot of a process's state.
type Info struct {
	ID        int32     `json:"id"`
	Name      string    `json:"name"`
	Kind      Kind      `json:"kind"`
	State     State     `json:"state"`
	SessionID string    `json:"session,omitempty"`
	User      string    `json:"user,omitempty"`
	Caps      []string  `json:"caps,omitempty"` // granted capabilities, from the spec
	StartedAt time.Time `json:"started_at,omitempty"`
	ExitedAt  time.Time `json:"exited_at,omitempty"`
	Err       string    `json:"error,omitempty"`
	ExitCode  int       `json:"exit_code,omitempty"`
}

// Event is published on every state transition.
type Event struct {
	ID    int32  `json:"id"`
	Name  string `json:"name"`
	Kind  Kind   `json:"kind"`
	State State  `json:"state"`
	Err   string `json:"error,omitempty"`
}

func (Event) Type() string { return "proc.state" }

// Process is one environment process.
type Process struct {
	mu     sync.Mutex
	info   Info
	ctx    context.Context
	cancel context.CancelFunc
	done   chan struct{}
	label  string // child only: gsport/child supervisor key
}

// ID returns the process ID.
func (p *Process) ID() int32 { return p.info.ID }

// Context returns the process context: canceled when the process is
// stopped. In-proc applications must honor it.
func (p *Process) Context() context.Context { return p.ctx }

// Done is closed when the process has exited.
func (p *Process) Done() <-chan struct{} { return p.done }

// Info returns a state snapshot.
func (p *Process) Info() Info {
	p.mu.Lock()
	defer p.mu.Unlock()
	info := p.info
	info.Caps = append([]string(nil), info.Caps...)
	return info
}

// Caps returns the capabilities granted to this process (from its spec).
func (p *Process) Caps() []string {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.info.Caps
}

func (p *Process) setState(state State, err error) {
	p.mu.Lock()
	p.info.State = state
	if err != nil {
		p.info.Err = err.Error()
	}
	p.mu.Unlock()
}

// Manager tracks all environment processes.
type Manager struct {
	mu       sync.Mutex
	next     int32
	procs    map[int32]*Process
	bus      *events.Bus
	log      *slog.Logger
	children *child.Manager
	childErr error
}

func NewManager(bus *events.Bus, log *slog.Logger) *Manager {
	children, err := child.New(child.GuestHooks())
	return &Manager{
		next:     1,
		procs:    map[int32]*Process{},
		bus:      bus,
		log:      log,
		children: children,
		childErr: err,
	}
}

// Children exposes the EL0 child adapter for callers that need the raw
// supervise snapshot (restart streaks, observed kernel pid) or the
// bounded APPLOG capture. Nil only when hook wiring failed at NewManager.
func (m *Manager) Children() *child.Manager { return m.children }

func (m *Manager) alloc() int32 {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.next++
	return m.next - 1
}

func (m *Manager) publish(p *Process) {
	info := p.Info()
	m.bus.Publish("process", Event{
		ID: info.ID, Name: info.Name, Kind: info.Kind, State: info.State, Err: info.Err,
	})
}

func (m *Manager) add(p *Process) {
	m.mu.Lock()
	m.procs[p.info.ID] = p
	m.mu.Unlock()
}

// StartInProc starts run as an environment process of kind "inproc". The
// process context is canceled by Stop; run must honor it.
func (m *Manager) StartInProc(ctx context.Context, spec Spec, run func(p *Process) error) (*Process, error) {
	if run == nil {
		return nil, errors.New("process: run function is required")
	}
	spec.Kind = KindInProc
	id := m.alloc()
	procCtx, cancel := context.WithCancel(ipc.WithCapabilities(ctx, security.NewCapabilities(spec.Caps...)))
	p := &Process{done: make(chan struct{}), ctx: procCtx, cancel: cancel}
	p.info = Info{
		ID:        id,
		Name:      spec.Name,
		Kind:      spec.Kind,
		State:     StateStarting,
		SessionID: spec.SessionID,
		User:      spec.User,
		StartedAt: time.Now(),
		Caps:      append([]string(nil), spec.Caps...),
	}
	m.add(p)
	m.publish(p) // starting
	p.setState(StateRunning, nil)
	m.publish(p)
	m.log.Info("process started", "pid", id, "name", spec.Name, "kind", spec.Kind)

	go func() {
		err := run(p)

		stopping := p.isStopping() // read before taking the lock
		p.mu.Lock()
		p.info.ExitedAt = time.Now()
		p.info.Err = ""
		state := StateStopped
		var finalErr error
		if err != nil && !stopping {
			state = StateFailed
			p.info.Err = err.Error()
			finalErr = err
		}
		p.info.State = state
		p.mu.Unlock()
		cancel() // release context resources
		m.publish(p)
		close(p.done)
		if finalErr != nil {
			m.log.Error("process failed", "pid", id, "name", spec.Name, "err", finalErr)
		} else {
			m.log.Info("process stopped", "pid", id, "name", spec.Name)
		}
	}()
	return p, nil
}

// StartChild admits spec.Args as an EL0 child process under the M92c
// supervisor. There is no fork/exec/wait: the kernel loads spec.Args[0]
// from the host share into a fresh process slot, the watcher goroutine
// polls the registry through the supervisor's Tick, and Stop arms a kill
// rather than sending a signal. spec.Restart is the only restart policy;
// its zero value is the pin's no-restart behaviour. Dir and Env name host
// concepts (cwd, environ) that EL0 children do not have — non-empty values
// are refused rather than silently dropped.
func (m *Manager) StartChild(ctx context.Context, spec Spec) (*Process, error) {
	if len(spec.Args) == 0 {
		return nil, errors.New("process: child spec requires args")
	}
	if spec.Dir != "" || spec.Env != nil {
		return nil, errors.New("process: child Dir/Env are host concepts virelai does not have")
	}
	if m.childErr != nil {
		return nil, fmt.Errorf("process: child adapter unavailable: %w", m.childErr)
	}
	spec.Kind = KindChild
	id := m.alloc()
	procCtx, cancel := context.WithCancel(ipc.WithCapabilities(ctx, security.NewCapabilities(spec.Caps...)))
	p := &Process{done: make(chan struct{}), ctx: procCtx, cancel: cancel, label: spec.Name}
	p.info = Info{
		ID:        id,
		Name:      spec.Name,
		Kind:      spec.Kind,
		State:     StateStarting,
		SessionID: spec.SessionID,
		User:      spec.User,
		StartedAt: time.Now(),
		Caps:      append([]string(nil), spec.Caps...),
	}

	if err := m.children.Start(child.Spec{
		Name: spec.Name, Args: spec.Args, Policy: spec.Restart,
	}); err != nil {
		cancel()
		return nil, fmt.Errorf("process: start %s: %w", spec.Name, err)
	}
	m.add(p)
	m.publish(p) // starting
	m.log.Info("child process admitted", "pid", id, "name", spec.Name)

	go m.watchChild(p)
	return p, nil
}

// watchChild is the child's liveness pump: it drives the shared supervisor
// Tick (spawn admission, registry polls, backoff, give-up, receipts) and
// folds the supervise snapshot into the pinned Info/State surface. The pin
// has no death notification either (cmd.Wait is a poll); the cadence here
// is a bounded 50 ms between ticks.
func (m *Manager) watchChild(p *Process) {
	defer func() {
		p.cancel()
		close(p.done)
	}()
	for {
		if err := m.children.Tick(); err != nil {
			m.log.Warn("child tick failed", "name", p.label, "err", err)
		}
		snap, ok := m.children.Snapshot(p.label)
		if !ok {
			p.setState(StateFailed, errors.New("child: supervisor lost the label"))
			m.publish(p)
			return
		}
		var publish bool
		p.mu.Lock()
		switch snap.State {
		case supervise.Running:
			if p.info.State != StateRunning {
				p.info.State = StateRunning
				p.info.Err = ""
				publish = true
			}
		case supervise.Backoff, supervise.Ready:
			// Between incarnations: not running, not terminal. The pinned
			// State set has no "restarting"; starting is the honest read.
			if p.info.State != StateStarting {
				p.info.State = StateStarting
				publish = true
			}
		case supervise.Stopping:
			if p.info.State != StateStopping {
				p.info.State = StateStopping
				publish = true
			}
		case supervise.Stopped:
			// Supervisor-initiated stop — never a failure, even when the
			// armed kill recorded status 137.
			p.info.ExitedAt = time.Now()
			p.info.ExitCode = int(snap.Status)
			p.info.State = StateStopped
			p.info.Err = ""
			p.mu.Unlock()
			m.publish(p)
			m.log.Info("child process stopped", "pid", p.info.ID, "name", p.info.Name)
			return
		case supervise.Exited:
			p.info.ExitedAt = time.Now()
			p.info.ExitCode = int(snap.Status)
			if snap.Status == 0 {
				p.info.State = StateStopped
				p.info.Err = ""
			} else {
				// Nonzero is a failure, including the kernel's reserved
				// 137 (external kill) and 139 (unhandled fault) — the
				// registry status is never remapped to clean.
				p.info.State = StateFailed
				p.info.Err = fmt.Sprintf("exit status %d", snap.Status)
			}
			p.mu.Unlock()
			m.publish(p)
			m.log.Info("child process exited", "pid", p.info.ID, "name", p.info.Name,
				"exit_code", p.info.ExitCode, "state", p.info.State)
			return
		case supervise.Failed:
			// The supervisor's restart budget is spent; Start refuses
			// this label until the process row is reaped.
			p.info.ExitedAt = time.Now()
			p.info.ExitCode = int(snap.Status)
			p.info.State = StateFailed
			p.info.Err = fmt.Sprintf("supervisor gave up after %d restarts (status %d)", snap.Restart, snap.Status)
			p.mu.Unlock()
			m.publish(p)
			m.log.Error("child process failed", "pid", p.info.ID, "name", p.info.Name, "err", p.info.Err)
			return
		}
		p.mu.Unlock()
		if publish {
			m.publish(p)
		}
		time.Sleep(50 * time.Millisecond)
	}
}

func (p *Process) isStopping() bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.info.State == StateStopping
}

// Stop asks a process to stop and waits up to timeout for it to exit.
// For a child the stop is the supervisor's armed kill (slot 29); the
// eventual registry status — usually 137 — is a supervisor stop, folded
// to stopped, never a failure and never a receipt.
func (m *Manager) Stop(id int32, timeout time.Duration) error {
	m.mu.Lock()
	p, ok := m.procs[id]
	m.mu.Unlock()
	if !ok {
		return fmt.Errorf("process: no such process %d", id)
	}
	switch p.Info().State {
	case StateStopped, StateFailed, StateStopping:
		select {
		case <-p.done:
			return nil
		case <-time.After(timeout):
			return fmt.Errorf("process: %d is stopping but has not exited within %s", id, timeout)
		}
	}
	p.setState(StateStopping, nil)
	m.publish(p)
	if p.info.Kind == KindChild {
		// The context does not reach the kernel child; the supervisor's
		// armed kill does. The watcher owns the eventual cancel+done.
		if err := m.children.Stop(p.label); err != nil {
			m.log.Warn("child stop failed", "pid", id, "name", p.label, "err", err)
		}
	} else {
		p.cancel()
	}
	select {
	case <-p.done:
		return nil
	case <-time.After(timeout):
		return fmt.Errorf("process: %d (%s) did not stop within %s", id, timeout, p.Info().Name)
	}
}

// StopAll stops every live process, best effort. Used at shutdown.
func (m *Manager) StopAll(timeout time.Duration) {
	for _, info := range m.List() {
		if info.State != StateRunning && info.State != StateStarting {
			continue
		}
		if err := m.Stop(info.ID, timeout); err != nil {
			m.log.Warn("process shutdown problem", "pid", info.ID, "err", err)
		}
	}
}

// Get returns a process by ID.
func (m *Manager) Get(id int32) (*Process, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	p, ok := m.procs[id]
	return p, ok
}

// List returns snapshots of all known processes, sorted by ID. Exited
// processes remain listed until reaping (future milestone).
func (m *Manager) List() []Info {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]Info, 0, len(m.procs))
	for _, p := range m.procs {
		out = append(out, p.Info())
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}

// Count returns the number of live (running or starting) processes.
func (m *Manager) Count() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	n := 0
	for _, p := range m.procs {
		switch p.Info().State {
		case StateRunning, StateStarting:
			n++
		}
	}
	return n
}
