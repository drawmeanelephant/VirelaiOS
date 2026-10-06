package main

import (
	"errors"
	"strconv"
	"strings"

	"virelai/supervise"
	"virelai/svcgraph"
	"virelai/svcmanifest"
)

// A seat always reserves its first-boot shell. A .BIN suffix is not proof
// of a single-task program: only these two syscall-only fixtures qualify.
func taskCost(binary string) (int, error) {
	switch binary {
	case "INITPRE.BIN", "INITDEP.BIN":
		return 1, nil
	}
	if strings.HasSuffix(binary, ".ELF") {
		return 4, nil
	}
	return 0, errors.New("task-budget")
}

type bootService struct {
	config svcmanifest.Service
	child  *supervise.Supervisor
	ready  uint64
}

type Boot struct {
	plan     svcgraph.Plan
	services map[string]*bootService
	seat     string
	hooks    supervise.Hooks
	rows     []supervise.Process
	seated   bool
	stopping bool
}

func NewBoot(m svcmanifest.Manifest, seatBinary string, loginShell bool, hooks supervise.Hooks) (*Boot, error) {
	if err := svcmanifest.Validate(m); err != nil {
		return nil, err
	}
	var nodes []svcgraph.Node
	for _, service := range m.Services {
		if service.Enabled {
			nodes = append(nodes, svcgraph.Node{Name: service.Name,
				Requires: service.Requires, Wants: service.Wants})
		}
	}
	plan, diagnostics, err := svcgraph.Resolve(nodes)
	if err != nil {
		return nil, err
	}
	b := &Boot{plan: plan, services: make(map[string]*bootService), hooks: hooks}
	tasks := 3 + 3 + 4 // kernel, init, seat-owned GOSH
	if loginShell {
		tasks += 4
	}
	for _, service := range m.Services {
		if !service.Enabled {
			continue
		}
		cost := 4 // conservative even for the retained native fallback seat
		if service.Argv[0] == seatBinary {
			if b.seat != "" || service.Class != "boot" {
				return nil, errors.New("seat")
			}
			b.seat = service.Name
		} else {
			cost, err = taskCost(service.Argv[0])
			if err != nil {
				return nil, err
			}
		}
		tasks += cost
	}
	if b.seat == "" {
		return nil, errors.New("seat")
	}
	if tasks > 16 {
		return nil, errors.New("task-budget")
	}
	for _, service := range m.Services {
		if !service.Enabled {
			continue
		}
		p := service.Restart
		childHooks := hooks
		childHooks.Table = func() ([]supervise.Process, error) { return b.rows, nil }
		child, err := supervise.New([]supervise.Service{{
			Name: service.Name, Binary: service.Argv[0], Args: service.Argv[1:],
			Policy: supervise.Policy{Restart: p.Restart, BackoffBaseS: p.BackoffBaseS,
				BackoffCapS: p.BackoffCapS, MaxRestarts: p.MaxRestarts, Window: p.Window},
		}}, childHooks)
		if err != nil {
			return nil, err
		}
		b.services[service.Name] = &bootService{config: service, child: child}
	}
	for _, diagnostic := range diagnostics {
		hooks.Serial("init: " + diagnostic.Reason + " from=" + diagnostic.From + " to=" + diagnostic.To)
	}
	hooks.Serial("init: manifest ok n=" + strconv.Itoa(len(nodes)))
	return b, nil
}

func (b *Boot) satisfied(name string) bool {
	service := b.services[name]
	view, _ := service.child.Snapshot(name)
	return view.State == supervise.Running && view.Observed ||
		service.config.Restart.Restart == supervise.Never &&
			view.State == supervise.Exited && view.Status == 0
}

func (b *Boot) prerequisites(name string) bool {
	for _, prerequisite := range b.plan.Prerequisites[name] {
		if !b.satisfied(prerequisite) {
			return false
		}
	}
	return true
}

// Each child owns its unchanged restart counters. Hold Ready/Backoff ticks
// behind prerequisites, but always observe Running/Stopping children.
func (b *Boot) Tick() error {
	rows, err := b.hooks.Table()
	if err != nil {
		return err
	}
	b.rows = rows
	for order, name := range b.plan.Order {
		service := b.services[name]
		view, _ := service.child.Snapshot(name)
		allowed := b.prerequisites(name)
		if !b.stopping && view.State == supervise.Stopped && allowed {
			if err := service.child.Start(name); err != nil {
				return err
			}
			view, _ = service.child.Snapshot(name)
		}
		if (view.State == supervise.Ready || view.State == supervise.Backoff) && !allowed {
			continue
		}
		// Supply one consistent table to all children. A newly exec'd pid
		// cannot be observed ready until the next outer poll.
		before := view.Starts
		if err := service.child.Tick(); err != nil {
			return err
		}
		view, _ = service.child.Snapshot(name)
		if view.Starts > before {
			b.hooks.Serial("init: start name=" + name + " order=" + strconv.Itoa(order+1))
		}
		if b.satisfied(name) && service.ready != view.Starts {
			service.ready = view.Starts
			b.hooks.Serial("init: ready name=" + name)
		}
		if !b.seated && !b.stopping && (view.State == supervise.Failed ||
			view.State == supervise.Backoff ||
			view.State == supervise.Exited && !b.satisfied(name)) {
			return errors.New("unstartable")
		}
	}
	return nil
}

// Registration is acknowledged by the kernel, not inferred from exec or
// from this process's running-row readiness contract.
func (b *Boot) SeatReady() bool { return b.satisfied(b.seat) }

func (b *Boot) Seated() {
	if !b.seated && b.SeatReady() {
		b.seated = true
		b.hooks.Serial("init: seated")
	}
}

func (b *Boot) Stop() error {
	b.stopping = true
	for i := len(b.plan.Order) - 1; i >= 0; i-- {
		name := b.plan.Order[i]
		if err := b.services[name].child.Stop(name); err != nil {
			return err
		}
	}
	return nil
}

func (b *Boot) Stopped() bool {
	for _, name := range b.plan.Order {
		view, _ := b.services[name].child.Snapshot(name)
		if view.State == supervise.Running || view.State == supervise.Stopping {
			return false
		}
	}
	return true
}
