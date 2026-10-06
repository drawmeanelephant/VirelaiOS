// Package supervise owns a flat set of child processes, not dependency ordering.
// Tick is non-blocking apart from the injected syscalls. Its owner calls it once
// per scheduler tick and serializes Start, Stop, Tick and Snapshot.
package supervise

import (
	"errors"
	"strconv"
	"strings"

	"virelai/vi"
)

const (
	Never     = "never"
	Always    = "always"
	OnFailure = "on-failure"
	second    = int64(1e9)

	// StatusUnknown is a failed spawn or a lost process-table row, not an
	// invented clean exit. StatusKilled is the kernel's external-kill status.
	StatusUnknown = int64(-1)
	StatusKilled  = int64(137)
)

// Policy is ADR 0042 §3's input contract, independent of svcmanifest.
type Policy struct {
	Restart      string
	BackoffBaseS uint64
	BackoffCapS  uint64
	MaxRestarts  uint32
	Window       uint64 // monotonic whole seconds
}

func (p Policy) Validate() error {
	if p.Restart == Never {
		if p.BackoffBaseS == 0 && p.BackoffCapS == 0 && p.MaxRestarts == 0 && p.Window == 0 {
			return nil
		}
	} else if p.Restart == Always || p.Restart == OnFailure {
		if p.BackoffBaseS >= 1 && p.BackoffBaseS <= p.BackoffCapS &&
			p.BackoffCapS <= 3600 && p.MaxRestarts >= 1 && p.MaxRestarts <= 32 &&
			p.Window >= p.BackoffCapS && p.Window <= 86400 {
			return nil
		}
	}
	return errors.New("invalid restart policy")
}

type State string

const (
	Stopped  State = "stopped"
	Ready    State = "ready"
	Running  State = "running"
	Backoff  State = "backoff"
	Stopping State = "stopping"
	Exited   State = "exited"
	Failed   State = "failed"
)

type Service struct {
	Name   string // receipt label, not necessarily the ELF basename
	Binary string
	Args   []string
	Policy Policy
}

type Process struct {
	PID     int64
	Running bool
	Exited  bool
	Status  int64
}

// Hooks keep policy tests on the host deterministic. Table must return the
// complete registry, including exited rows. Receipt publishes the latest M82e
// receipt; a failed publication fails closed rather than silently restarting.
type Hooks struct {
	Now     func() int64 // monotonic nanoseconds
	Exec    func(string, ...string) (int64, error)
	Table   func() ([]Process, error)
	Kill    func(uint64) int64
	Random  func([]byte) (int, error)
	Receipt func(string, string) int64
	Serial  func(string)
}

type Snapshot struct {
	Name     string
	State    State
	PID      int64
	Status   int64
	Starts   uint64
	Restart  uint32 // last admitted retry, initial launch is zero
	DelayS   uint64
	NextNS   int64
	Observed bool // pid observed live on a subsequent process-table poll
}

type child struct {
	service  Service
	view     Snapshot
	startNS  int64
	streak   uint32
	attempts []int64 // at most MaxRestarts; timestamps of actual retry execs
}

type Supervisor struct {
	hooks    Hooks
	children []child
}

func New(services []Service, hooks Hooks) (*Supervisor, error) {
	if hooks.Now == nil || hooks.Exec == nil || hooks.Table == nil ||
		hooks.Kill == nil || hooks.Random == nil || hooks.Receipt == nil || hooks.Serial == nil {
		return nil, errors.New("supervise: missing hook")
	}
	s := &Supervisor{hooks: hooks}
	for _, service := range services {
		if vi.CrashReceiptPath(service.Name) == "" || service.Binary == "" || strings.IndexByte(service.Binary, 0) >= 0 ||
			len(service.Binary) > 255 || len(service.Args) > vi.ExecMaxArgs {
			return nil, errors.New("supervise: invalid service " + service.Name)
		}
		for _, arg := range service.Args {
			if len(arg) > vi.ExecArgMax || strings.IndexByte(arg, 0) >= 0 {
				return nil, errors.New("supervise: invalid argv for " + service.Name)
			}
		}
		if err := service.Policy.Validate(); err != nil {
			return nil, errors.New("supervise: " + service.Name + ": " + err.Error())
		}
		if s.find(service.Name) != nil {
			return nil, errors.New("supervise: duplicate service " + service.Name)
		}
		service.Args = append([]string(nil), service.Args...)
		s.children = append(s.children, child{service: service, view: Snapshot{
			Name: service.Name, State: Stopped,
		}})
	}
	return s, nil
}

func (s *Supervisor) find(name string) *child {
	for i := range s.children {
		if s.children[i].service.Name == name {
			return &s.children[i]
		}
	}
	return nil
}

func (s *Supervisor) Snapshot(name string) (Snapshot, bool) {
	c := s.find(name)
	if c == nil {
		return Snapshot{}, false
	}
	return c.view, true
}

// Start requests an initial launch. Failed services are deliberately terminal,
// and unchanged services retain retry history across a stop/start (live reload).
func (s *Supervisor) Start(name string) error {
	c := s.find(name)
	if c == nil {
		return errors.New("supervise: unknown service " + name)
	}
	if c.view.State == Failed {
		return errors.New("supervise: failed service " + name)
	}
	if c.view.State == Stopped || c.view.State == Exited {
		c.view.State = Ready
		c.view.DelayS, c.view.NextNS = 0, 0
	}
	return nil
}

// Stop only suppresses failures/restarts after a successful kill arm. It never
// claims a still-running pid is gone; Tick observes its eventual exit.
func (s *Supervisor) Stop(name string) error {
	c := s.find(name)
	if c == nil {
		return errors.New("supervise: unknown service " + name)
	}
	switch c.view.State {
	case Running:
		if rc := s.hooks.Kill(uint64(c.view.PID)); rc < 0 {
			return errors.New("supervise: stop " + name + " rc=" + strconv.FormatInt(rc, 10))
		}
		c.view.State = Stopping
	case Ready, Backoff, Exited:
		c.view.State = Stopped
		c.view.DelayS, c.view.NextNS = 0, 0
	}
	return nil
}

// Tick takes one shared registry snapshot. A transient table error leaves all
// state intact. A missing pid is an unknown failure, never assumed status zero.
func (s *Supervisor) Tick() error {
	rows, err := s.hooks.Table()
	if err != nil {
		return err
	}
	for i := range s.children {
		c := &s.children[i]
		now := s.hooks.Now()
		switch c.view.State {
		case Ready:
			s.spawn(c, now, false)
		case Backoff:
			if now >= c.view.NextNS {
				s.spawn(c, now, true)
			}
		case Running, Stopping:
			var row *Process
			for j := range rows {
				if rows[j].PID == c.view.PID {
					row = &rows[j]
					break
				}
			}
			if row != nil && !row.Exited {
				c.view.Observed = row.Running
				if row.Running && c.service.Policy.Window > 0 &&
					now-c.startNS >= int64(c.service.Policy.Window)*second {
					c.streak = 0
				}
				continue
			}
			status := StatusUnknown
			if row != nil {
				status = row.Status
			}
			stopping := c.view.State == Stopping
			c.view.PID, c.view.Observed = 0, false
			if stopping {
				c.view.State, c.view.Status = Stopped, status
				s.marker(c, "exit", " status="+strconv.FormatInt(status, 10))
				continue
			}
			s.finish(c, status, now)
		}
	}
	return nil
}

func (s *Supervisor) prune(c *child, now int64) {
	window := int64(c.service.Policy.Window) * second
	n := 0
	for _, at := range c.attempts {
		if now-at < window {
			c.attempts[n] = at
			n++
		}
	}
	c.attempts = c.attempts[:n]
}

func (s *Supervisor) spawn(c *child, now int64, retry bool) {
	if retry {
		s.prune(c, now)
		if len(c.attempts) >= int(c.service.Policy.MaxRestarts) {
			s.fail(c, "restart-limit")
			return
		}
		c.attempts = append(c.attempts, now)
		c.streak++
	}
	pid, err := s.hooks.Exec(c.service.Binary, c.service.Args...)
	if err != nil || pid <= 0 {
		s.finish(c, StatusUnknown, s.hooks.Now())
		return
	}
	c.startNS = s.hooks.Now()
	c.view.PID, c.view.State, c.view.Observed = pid, Running, false
	c.view.Starts++
	c.view.NextNS, c.view.DelayS = 0, 0
	s.marker(c, "start", " pid="+strconv.FormatInt(pid, 10))
}

func (s *Supervisor) finish(c *child, status, now int64) {
	c.view.Status = status
	s.marker(c, "exit", " status="+strconv.FormatInt(status, 10))
	failure := status != 0
	p := c.service.Policy
	retry := p.Restart == Always || (p.Restart == OnFailure && failure)
	c.view.DelayS, c.view.NextNS = 0, 0
	reason := ""
	if retry {
		s.prune(c, now)
		if len(c.attempts) >= int(p.MaxRestarts) {
			reason = "restart-limit"
		} else {
			delay, err := Delay(p, c.streak, s.hooks.Random)
			if err != nil {
				reason = "entropy"
			} else {
				c.view.Restart++
				c.view.DelayS = delay
			}
		}
	}
	if failure {
		if rc := s.hooks.Receipt(c.service.Name, Outcome(c.view.Restart, p.MaxRestarts, status, c.view.DelayS)); rc < 0 {
			s.fail(c, "receipt")
			return
		}
	}
	if reason != "" {
		s.fail(c, reason)
	} else if retry {
		c.view.State = Backoff
		// Publication is part of failure handling. Time the delay from the
		// completed receipt, not from before potentially blocking file I/O.
		s.marker(c, "backoff", " k="+strconv.FormatUint(uint64(c.view.Restart), 10)+
			" delay_s="+strconv.FormatUint(c.view.DelayS, 10))
		c.view.NextNS = s.hooks.Now() + int64(c.view.DelayS)*second
	} else {
		c.view.State = Exited
	}
}

func (s *Supervisor) fail(c *child, reason string) {
	c.view.State = Failed
	c.view.DelayS, c.view.NextNS = 0, 0
	s.marker(c, "failed", " reason="+reason)
}

func (s *Supervisor) marker(c *child, event, fields string) {
	s.hooks.Serial("svc: " + event + " name=" + c.service.Name + fields +
		" t_ns=" + strconv.FormatInt(s.hooks.Now(), 10))
}

func Outcome(restart, max uint32, status int64, delay uint64) string {
	return "restart=" + strconv.FormatUint(uint64(restart), 10) +
		"/" + strconv.FormatUint(uint64(max), 10) +
		" status=" + strconv.FormatInt(status, 10) +
		" backoff_s=" + strconv.FormatUint(delay, 10)
}

// Delay's exponent is zero for the first restart. Saturating before doubling
// avoids overflow. Rejection sampling avoids modulo bias; a broken entropy
// source is bounded to eight draws and fails closed, never spins in Tick.
func Delay(p Policy, exponent uint32, random func([]byte) (int, error)) (uint64, error) {
	if err := p.Validate(); err != nil || p.Restart == Never || random == nil {
		return 0, errors.New("supervise: invalid backoff")
	}
	delay := p.BackoffBaseS
	for k := uint32(0); k < exponent && delay < p.BackoffCapS; k++ {
		if delay > p.BackoffCapS/2 {
			delay = p.BackoffCapS
		} else {
			delay *= 2
		}
	}
	bound := p.BackoffBaseS
	threshold := -bound % bound
	for tries := 0; tries < 8; tries++ {
		var b [8]byte
		n, err := random(b[:])
		if err != nil || n != len(b) {
			return 0, errors.New("supervise: entropy unavailable")
		}
		var value uint64
		for i := 0; i < len(b); i++ {
			value |= uint64(b[i]) << (8 * i)
		}
		if value >= threshold {
			return delay + value%bound, nil
		}
	}
	return 0, errors.New("supervise: entropy rejection limit")
}
