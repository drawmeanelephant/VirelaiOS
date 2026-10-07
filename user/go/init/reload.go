package main

import (
	"bytes"
	"errors"

	"virelai/svcmanifest"
	"virelai/vi"
)

func cloneService(s svcmanifest.Service) svcmanifest.Service {
	s.Argv = append([]string(nil), s.Argv...)
	s.Requires = append([]string(nil), s.Requires...)
	s.Wants = append([]string(nil), s.Wants...)
	s.Caps = append([]string(nil), s.Caps...)
	return s
}

func cloneManifest(m svcmanifest.Manifest) svcmanifest.Manifest {
	out := svcmanifest.Manifest{Version: m.Version}
	for _, s := range m.Services {
		out.Services = append(out.Services, cloneService(s))
	}
	return out
}

func sameStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func sameIdentity(a, b svcmanifest.Service) bool {
	return a.Name == b.Name && a.Class == b.Class && a.Restart == b.Restart &&
		sameStrings(a.Argv, b.Argv) && sameStrings(a.Caps, b.Caps)
}

func sameService(a, b svcmanifest.Service) bool {
	return sameIdentity(a, b) && a.Enabled == b.Enabled &&
		sameStrings(a.Requires, b.Requires) && sameStrings(a.Wants, b.Wants)
}

type retryReload struct{ error }

// Reload validates both the persisted graph and the boot-frozen live graph
// before touching a supervisor. The owner serializes this with Tick.
func (b *Boot) Reload(m svcmanifest.Manifest) error {
	if _, _, _, err := admit(m, b.seatBinary, b.loginShell, false); err != nil {
		return err
	}
	effective := svcmanifest.Manifest{Version: m.Version}
	nextBoot := make(map[string]bool)
	seen := make(map[string]bool)
	for _, s := range m.Services {
		old := b.services[s.Name]
		seen[s.Name] = true
		if s.Class == "boot" || old != nil && old.config.Class == "boot" {
			if old == nil || !sameService(old.config, s) {
				nextBoot[s.Name] = true
			}
			if old != nil {
				effective.Services = append(effective.Services, cloneService(old.config))
			}
		} else {
			effective.Services = append(effective.Services, cloneService(s))
		}
	}
	for _, s := range b.manifest.Services {
		if !seen[s.Name] && s.Class == "boot" {
			effective.Services = append(effective.Services, cloneService(s))
			nextBoot[s.Name] = true
		}
	}
	plan, diagnostics, _, err := admit(effective, b.seatBinary, b.loginShell, true)
	if err != nil {
		return err
	}
	replacement := make(map[string]*bootService)
	for _, s := range effective.Services {
		old := b.services[s.Name]
		if old != nil && sameIdentity(old.config, s) {
			replacement[s.Name] = old
		} else {
			child, err := b.newService(s)
			if err != nil {
				return err
			}
			replacement[s.Name] = child
		}
	}
	enabled := make(map[string]bool)
	for _, s := range effective.Services {
		enabled[s.Name] = s.Enabled
	}
	// Stop dependents first, suppressing policy before observing kill exits.
	// Failed kill arms are retryable; never cache that content as applied.
	for i := len(b.plan.Order) - 1; i >= 0; i-- {
		name := b.plan.Order[i]
		old := b.services[name]
		if replacement[name] != old || !enabled[name] {
			if err := old.child.Stop(name); err != nil {
				// Some earlier stop arms may already have succeeded. Keep
				// observing exits, but do not restart those children while
				// retrying the remaining arms against the old live plan.
				b.reloadHold = true
				return &retryReload{err}
			}
			b.hooks.Serial("init: stop name=" + name + " reason=config")
		}
	}
	for name, old := range b.services {
		if replacement[name] != old && ownsProcess(old) {
			b.retired = append(b.retired, old)
		}
	}
	for _, s := range effective.Services {
		replacement[s.Name].config = cloneService(s)
	}
	b.plan, b.services, b.manifest = plan, replacement, effective
	b.reloadHold = false
	for _, s := range m.Services {
		if nextBoot[s.Name] {
			b.hooks.Serial("init: next boot name=" + s.Name)
			delete(nextBoot, s.Name)
		}
	}
	for _, s := range effective.Services {
		if nextBoot[s.Name] {
			b.hooks.Serial("init: next boot name=" + s.Name)
		}
	}
	for _, d := range diagnostics {
		b.hooks.Serial("init: " + d.Reason + " from=" + d.From + " to=" + d.To)
	}
	b.hooks.Serial("init: reload applied")
	return nil
}

func contentHash(body []byte) uint64 {
	hash := uint64(14695981039346656037)
	for _, c := range body {
		hash = (hash ^ uint64(c)) * 1099511628211
	}
	return hash
}

type reloader struct {
	read      func(string, int) ([]byte, int64)
	body      []byte
	hash      uint64
	readError int64
}

func newReloader(body []byte, read func(string, int) ([]byte, int64)) *reloader {
	return &reloader{read: read, body: append([]byte(nil), body...), hash: contentHash(body)}
}

func (r *reloader) Poll(b *Boot) {
	body, rc := r.read(svcmanifest.Path, svcmanifest.MaxBytes+1)
	if rc < 0 {
		if rc != r.readError {
			reason := "manifest-read"
			if rc == -vi.ErrENOENT {
				reason = "missing-manifest"
			}
			b.hooks.Serial("init: reload refuse " + reason)
		}
		r.readError = rc
		return
	}
	r.readError = 0
	hash := contentHash(body)
	if !b.reloadHold && hash == r.hash && bytes.Equal(body, r.body) {
		return
	}
	m, err := svcmanifest.Parse(body)
	if err == nil {
		err = b.Reload(m)
	}
	if err != nil {
		b.hooks.Serial("init: reload refuse " + refusal(err))
		var retry *retryReload
		if errors.As(err, &retry) {
			return
		}
	}
	r.body, r.hash = append(r.body[:0], body...), hash
}
