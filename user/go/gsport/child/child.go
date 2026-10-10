// Package child is the gsport child-process model: Gostalgia's "child"
// kind as a real EL0 process — spawn through gsport/proc (slot 28),
// liveness from the sys_procs registry (slot 7), kill by arming slot 29.
//
// Host assumption it replaces: os/exec's fork/exec/wait/kill and the
// POSIX signal model underneath it. None exists on VirelaiOS: the kernel
// owns process lifetime, exit status is a registry row (0 clean, 137
// armed kill, 139 unhandled fault), and "wait" is polling.
//
// Restart, backoff, give-up and crash receipts are NOT implemented here:
// they belong to virelai/supervise (M92c, ADR 0042). This package maps one
// Gostalgia child spec onto one single-service supervisor per label, so a
// restarting spec's retry history — and the "failed" give-up — survives
// across Manager.Start calls. A supervisor stop is never a failure.
//
// Bounded capture: a child's output is its M82e per-app log ring
// (/host/APPLOG/<label>.LOG, 32 lines of 256 B), read through gsport/fsys.
// That covers COOPERATING children that write the ring — the guest has no
// mechanism that captures an arbitrary ELF's stdout.
//
// Slots: none directly — all kernel traffic rides gsport/proc and
// supervise's hooks (28 exec, 7 procs, 29 kill, 4 sleep pacing).
//
// Refuses by name: labels outside the receipt grammar ([A-Za-z0-9._-]{1,28},
// the vi.appFileName rule — receipts and log rings are share paths), empty
// argv, a spec change on a live name, a second start while running, and a
// start after the supervisor has given up (the supervise "failed service"
// refusal, surfaced unchanged).
package child

import (
	"errors"
	"fmt"
	"io/fs"
	"slices"
	"strings"
	"sync"

	"virelai/gsport/abi"
	"virelai/gsport/fsys"
	"virelai/gsport/proc"
	"virelai/supervise"
)

// The M82e per-app log ring geometry (vi/log.go, owned by M94f): label
// grammar, directory, per-line and ring bounds. Duplicated here because
// the gsport guard keeps virelai/vi out of the adapter tree — the values
// are a contract, not a preference.
const (
	appLogDir      = "/host/APPLOG"
	labelMax       = 28
	AppLogMaxLines = 32
	AppLogMaxLine  = 256
	appLogMaxBytes = AppLogMaxLines * (AppLogMaxLine + 1)
)

// ErrFailed is the named refusal a spec gets once its supervisor has given
// up (restart budget spent inside the window). Callers must not retry in a
// loop: give-up is terminal until the spec name leaves the table.
var ErrFailed = errors.New("child: service failed")

// Spec is one spawnable child: the label that owns its receipts and log
// ring, the kernel argv (Args[0] is the share ELF name), and the restart
// policy. The zero Policy is never — Gostalgia's default.
type Spec struct {
	Name   string
	Args   []string
	Policy supervise.Policy
}

// Hooks is the injectable seam. Guest wiring is GuestHooks(); host tests
// inject fakes. ReadLog reads one label's bounded APPLOG ring (absent ring
// is nil, nil — not an error).
type Hooks struct {
	Sup     supervise.Hooks
	ReadLog func(label string) ([]byte, error)
}

// GuestHooks wires the real guest: supervise's own vi hooks for the
// process table, clock, entropy, receipts and serial markers; gsport/proc
// for spawn and kill so argv bounds and kill arming stay in the adapter;
// gsport/fsys for the log-ring read.
func GuestHooks() Hooks {
	h := supervise.GuestHooks()
	h.Exec = proc.Spawn
	h.Kill = func(pid uint64) int64 {
		if err := proc.Kill(int64(pid)); err != nil {
			var ae *abi.Error
			if errors.As(err, &ae) {
				return -ae.Code
			}
			return -int64(abi.ErrEINVAL)
		}
		return 0
	}
	return Hooks{
		Sup:     h,
		ReadLog: readRing,
	}
}

// Snapshot is one child's observable state, lifted from supervise.
type Snapshot = supervise.Snapshot

// Manager tracks child supervisors by label. Not safe to share a Manager
// across unsynchronized callers beyond the documented methods — every
// entry point takes the mutex.
type Manager struct {
	mu    sync.Mutex
	hooks Hooks
	sups  map[string]*supervise.Supervisor
	specs map[string]Spec
}

// New validates the hooks and returns an empty manager.
func New(h Hooks) (*Manager, error) {
	if h.ReadLog == nil || h.Sup.Now == nil || h.Sup.Exec == nil ||
		h.Sup.Table == nil || h.Sup.Kill == nil || h.Sup.Random == nil ||
		h.Sup.Receipt == nil || h.Sup.Serial == nil {
		return nil, errors.New("child: missing hook")
	}
	return &Manager{hooks: h, sups: map[string]*supervise.Supervisor{}, specs: map[string]Spec{}}, nil
}

// LabelValid applies the receipt-label grammar: a label becomes share path
// components under APPLOG/ and CRASH/, so it obeys vi.appFileName's rule.
func LabelValid(label string) bool {
	if len(label) == 0 || len(label) > labelMax || label == "." || label == ".." {
		return false
	}
	for i := 0; i < len(label); i++ {
		c := label[i]
		if !(c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' ||
			c >= '0' && c <= '9' || c == '.' || c == '_' || c == '-') {
			return false
		}
	}
	return true
}

func appLogPath(label string) string {
	if !LabelValid(label) {
		return ""
	}
	return appLogDir + "/" + label + ".LOG"
}

func readRing(label string) ([]byte, error) {
	path := appLogPath(label)
	if path == "" {
		return nil, fmt.Errorf("child: invalid label %q", label)
	}
	body, err := fsys.ReadFile(path, appLogMaxBytes)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil, nil
		}
		return nil, err
	}
	return body, nil
}

// Start admits spec under its label. A first start or a start after a
// terminal state spawns on the next Tick; a live same-name start and a
// changed spec on a known label are refused; a label whose supervisor has
// reached failed refuses with ErrFailed.
func (m *Manager) Start(spec Spec) error {
	if !LabelValid(spec.Name) {
		return fmt.Errorf("child: invalid label %q", spec.Name)
	}
	if len(spec.Args) == 0 || spec.Args[0] == "" {
		return fmt.Errorf("child %s: empty argv", spec.Name)
	}
	// Gostalgia's no-restart default maps to supervise's never policy.
	if spec.Policy.Restart == "" {
		spec.Policy.Restart = supervise.Never
	}

	m.mu.Lock()
	defer m.mu.Unlock()
	sup := m.sups[spec.Name]
	if sup == nil {
		s, err := supervise.New([]supervise.Service{{
			Name: spec.Name, Binary: spec.Args[0],
			Args: append([]string(nil), spec.Args[1:]...), Policy: spec.Policy,
		}}, m.hooks.Sup)
		if err != nil {
			return fmt.Errorf("child %s: %w", spec.Name, err)
		}
		sup = s
		m.sups[spec.Name] = s
		m.specs[spec.Name] = spec
	} else {
		prev := m.specs[spec.Name]
		if prev.Policy != spec.Policy || !slices.Equal(prev.Args, spec.Args) {
			return fmt.Errorf("child %s: spec changed on a live label", spec.Name)
		}
		snap, _ := sup.Snapshot(spec.Name)
		switch snap.State {
		case supervise.Failed:
			return ErrFailed
		case supervise.Ready, supervise.Running, supervise.Backoff, supervise.Stopping:
			return fmt.Errorf("child %s: already live (%s)", spec.Name, snap.State)
		}
	}
	if err := sup.Start(spec.Name); err != nil {
		if strings.Contains(err.Error(), "failed service") {
			return ErrFailed
		}
		return fmt.Errorf("child %s: %w", spec.Name, err)
	}
	return nil
}

// Stop asks the supervisor to stop the child: an armed kill when running
// (the eventual 137 is a supervisor stop, never a failure), a state fold
// to stopped when it never spawned.
func (m *Manager) Stop(name string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	sup := m.sups[name]
	if sup == nil {
		return fmt.Errorf("child %s: unknown", name)
	}
	return sup.Stop(name)
}

// Tick advances every known supervisor once. Callers pace it; the guest
// caller is the watching goroutine's sleep loop.
func (m *Manager) Tick() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	var first error
	for _, sup := range m.sups {
		if err := sup.Tick(); err != nil && first == nil {
			first = err
		}
	}
	return first
}

// Snapshot reads one child's state.
func (m *Manager) Snapshot(name string) (Snapshot, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	sup := m.sups[name]
	if sup == nil {
		return Snapshot{}, false
	}
	return sup.Snapshot(name)
}

// Known reports whether a label has ever been admitted.
func (m *Manager) Known(name string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	_, ok := m.sups[name]
	return ok
}

// Table is the raw registry read through the injected hook — for the
// caller's own occupancy accounting (baseline vs final live counts).
func (m *Manager) Table() ([]supervise.Process, error) {
	return m.hooks.Sup.Table()
}

// LogTail returns the last n lines of a label's APPLOG ring (n clamped to
// the ring's 32-line bound) and whether the answer is a truncated window
// onto a full ring — honest truncation, never invented completeness.
func (m *Manager) LogTail(label string, n int) ([]string, bool, error) {
	body, err := m.hooks.ReadLog(label)
	if err != nil {
		return nil, false, err
	}
	text := strings.TrimSuffix(string(body), "\n")
	if text == "" {
		return nil, false, nil
	}
	lines := strings.Split(text, "\n")
	truncated := len(lines) >= AppLogMaxLines
	if n > AppLogMaxLines {
		n = AppLogMaxLines
	}
	if len(lines) > n {
		lines = lines[len(lines)-n:]
	}
	return lines, truncated, nil
}

// AppendLog writes one line to a label's APPLOG ring under the M82e
// bounds (256 B per line, newest 32 lines kept). It is the cooperating
// child's side of the capture contract — a childfix-style ELF writes its
// own ring; nothing captures an arbitrary ELF's stdout. The label must
// bind to the child's own image name (#2085) — a foreign label fails.
func AppendLog(label, message string) error {
	path := appLogPath(label)
	if path == "" {
		return fmt.Errorf("child: invalid label %q", label)
	}
	if err := fsys.Mkdir(appLogDir); err != nil {
		return err
	}
	line := strings.NewReplacer("\r", " ", "\n", " ").Replace(message)
	if len(line) > AppLogMaxLine {
		line = line[:AppLogMaxLine]
	}
	old, err := fsys.ReadFile(path, appLogMaxBytes+1)
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	rows := strings.Split(strings.TrimSuffix(string(old), "\n"), "\n")
	if len(rows) == 1 && rows[0] == "" {
		rows = nil
	}
	rows = append(rows, line)
	if len(rows) > AppLogMaxLines {
		rows = rows[len(rows)-AppLogMaxLines:]
	}
	f, err := fsys.Create(path)
	if err != nil {
		return err
	}
	if err := f.Truncate(0); err != nil {
		f.Close()
		return err
	}
	if _, err := f.Write([]byte(strings.Join(rows, "\n") + "\n")); err != nil {
		f.Close()
		return err
	}
	if err := f.Sync(); err != nil {
		f.Close()
		return err
	}
	return f.Close()
}
