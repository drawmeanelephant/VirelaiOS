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
	plan       svcgraph.Plan
	services   map[string]*bootService
	seat       string
	hooks      supervise.Hooks
	rows       []supervise.Process
	seated     bool
	stopping   bool
	manifest   svcmanifest.Manifest
	seatBinary string
	loginShell bool
	retired    []*bootService
	reloadHold bool
}

func admit(m svcmanifest.Manifest, seatBinary string, loginShell, needSeat bool) (svcgraph.Plan, []svcgraph.Diagnostic, string, error) {
	if err := svcmanifest.Validate(m); err != nil {
		return svcgraph.Plan{}, nil, "", err
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
		return svcgraph.Plan{}, nil, "", err
	}
	seat := ""
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
			if seat != "" || service.Class != "boot" {
				return svcgraph.Plan{}, nil, "", errors.New("seat")
			}
			seat = service.Name
		} else {
			cost, err = taskCost(service.Argv[0])
			if err != nil {
				return svcgraph.Plan{}, nil, "", err
			}
		}
		tasks += cost
	}
	if needSeat && seat == "" {
		return svcgraph.Plan{}, nil, "", errors.New("seat")
	}
	if tasks > 16 {
		return svcgraph.Plan{}, nil, "", errors.New("task-budget")
	}
	return plan, diagnostics, seat, nil
}

func NewBoot(m svcmanifest.Manifest, seatBinary string, loginShell bool, hooks supervise.Hooks) (*Boot, error) {
	plan, diagnostics, seat, err := admit(m, seatBinary, loginShell, true)
	if err != nil {
		return nil, err
	}
	b := &Boot{plan: plan, services: make(map[string]*bootService), hooks: hooks,
		seat: seat, seatBinary: seatBinary, loginShell: loginShell, manifest: cloneManifest(m)}
	for _, service := range m.Services {
		child, err := b.newService(service)
		if err != nil {
			return nil, err
		}
		b.services[service.Name] = child
	}
	for _, diagnostic := range diagnostics {
		hooks.Serial("init: " + diagnostic.Reason + " from=" + diagnostic.From + " to=" + diagnostic.To)
	}
	hooks.Serial("init: manifest ok n=" + strconv.Itoa(len(plan.Order)))
	return b, nil
}

func (b *Boot) newService(service svcmanifest.Service) (*bootService, error) {
	p := service.Restart
	hooks := b.hooks
	hooks.Table = func() ([]supervise.Process, error) { return b.rows, nil }
	child, err := supervise.New([]supervise.Service{{
		Name: service.Name, Binary: service.Argv[0], Args: service.Argv[1:],
		Policy: supervise.Policy{Restart: p.Restart, BackoffBaseS: p.BackoffBaseS,
			BackoffCapS: p.BackoffCapS, MaxRestarts: p.MaxRestarts, Window: p.Window},
	}}, hooks)
	return &bootService{config: cloneService(service), child: child}, err
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
	// Disabled and replaced children still own tasks until a subsequent
	// table poll sees their exit. Drain them before admitting new spawns.
	draining := b.reloadHold
	var retired []*bootService
	for _, service := range b.retired {
		if err := service.child.Tick(); err != nil {
			return err
		}
		if ownsProcess(service) {
			retired = append(retired, service)
			draining = true
		}
	}
	b.retired = retired
	for _, service := range b.services {
		if !service.config.Enabled {
			if err := service.child.Tick(); err != nil {
				return err
			}
		}
		view, _ := service.child.Snapshot(service.config.Name)
		draining = draining || view.State == supervise.Stopping
	}
	for order, name := range b.plan.Order {
		service := b.services[name]
		view, _ := service.child.Snapshot(name)
		allowed := b.prerequisites(name)
		if !b.stopping && !draining && view.State == supervise.Stopped && allowed {
			if err := service.child.Start(name); err != nil {
				return err
			}
			view, _ = service.child.Snapshot(name)
		}
		if (view.State == supervise.Ready || view.State == supervise.Backoff) && (!allowed || draining) {
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
		// A failed first exec is unstartable; a previously live on-failure
		// child may recover before seating without stopping its dependents.
		if !b.seated && !b.stopping && (view.State == supervise.Failed ||
			view.State == supervise.Backoff && (view.Starts == 0 ||
				service.config.Restart.Restart != supervise.OnFailure) ||
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
	for _, service := range b.retired {
		if err := service.child.Stop(service.config.Name); err != nil {
			return err
		}
	}
	return nil
}

func (b *Boot) Stopped() bool {
	for _, service := range b.services {
		if ownsProcess(service) {
			return false
		}
	}
	for _, service := range b.retired {
		if ownsProcess(service) {
			return false
		}
	}
	return true
}

func ownsProcess(service *bootService) bool {
	view, _ := service.child.Snapshot(service.config.Name)
	return view.State == supervise.Running || view.State == supervise.Stopping
}
