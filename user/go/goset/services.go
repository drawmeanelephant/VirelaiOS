package main

import (
	"bytes"
	"errors"
	"strings"

	"virelai/svcgraph"
	"virelai/svcmanifest"
	"virelai/vi"
)

type serviceStore struct {
	read  func(string, int) ([]byte, int64)
	write func(string, []byte) int64
}

type servicesView struct {
	store    serviceStore
	manifest svcmanifest.Manifest
	body     []byte
	reason   string
}

func serviceReason(err error) string {
	if schema, ok := err.(*svcmanifest.Error); ok {
		return string(schema.Code)
	}
	return err.Error()
}

func serviceGraph(m svcmanifest.Manifest) error {
	if err := svcmanifest.Validate(m); err != nil {
		return err
	}
	var nodes []svcgraph.Node
	for _, s := range m.Services {
		if s.Enabled {
			nodes = append(nodes, svcgraph.Node{Name: s.Name, Requires: s.Requires, Wants: s.Wants})
		}
	}
	_, _, err := svcgraph.Resolve(nodes)
	return err
}

// Keep the last valid rows visible on refusal, but make the whole view
// read-only. Never heal a document that init refused.
func (v *servicesView) Refresh() bool {
	body, rc := v.store.read(svcmanifest.Path, svcmanifest.MaxBytes+1)
	if rc < 0 {
		reason := "manifest-read"
		if rc == -vi.ErrENOENT {
			reason = "missing-manifest"
		}
		changed := v.reason != reason
		v.reason = reason
		return changed
	}
	if v.reason == "" && bytes.Equal(v.body, body) {
		return false
	}
	m, err := svcmanifest.Parse(body)
	if err == nil {
		err = serviceGraph(m)
	}
	if err != nil {
		reason := serviceReason(err)
		changed := v.reason != reason || !bytes.Equal(v.body, body)
		v.reason, v.body = reason, append(v.body[:0], body...)
		return changed
	}
	v.manifest, v.body, v.reason = m, append(v.body[:0], body...), ""
	return true
}

func (v *servicesView) Labels() []string {
	var rows []string
	for _, s := range v.manifest.Services {
		line := s.Name + "  " + s.Class + "  enabled=" + serviceState(s.Enabled)
		if s.Class == "boot" {
			line += "  (applies at next boot)"
		}
		rows = append(rows, line)
	}
	return rows
}

func serviceState(enabled bool) string {
	if enabled {
		return "on"
	}
	return "off"
}

func (v *servicesView) Set(name string, enabled bool) error {
	if v.reason != "" {
		return errors.New(v.reason)
	}
	for i := range v.manifest.Services {
		if v.manifest.Services[i].Name == name {
			v.manifest.Services[i].Enabled = enabled
			return nil
		}
	}
	return errors.New("unknown-service")
}

func (v *servicesView) Save() error {
	// Re-read before publishing, so a corrupt or concurrently replaced file
	// cannot be overwritten by an old panel. Refresh reports it in the UI.
	body, rc := v.store.read(svcmanifest.Path, svcmanifest.MaxBytes+1)
	if rc < 0 || !bytes.Equal(body, v.body) {
		v.Refresh()
		if v.reason != "" {
			return errors.New(v.reason)
		}
		return errors.New("config-changed")
	}
	if v.reason != "" {
		return errors.New(v.reason)
	}
	if err := serviceGraph(v.manifest); err != nil {
		return err
	}
	body, err := svcmanifest.Format(v.manifest)
	if err != nil {
		return err
	}
	if rc = v.store.write(svcmanifest.Path, body); rc < 0 {
		return errors.New("manifest-write rc=" + vi.Itoa64(rc))
	}
	v.body = append(v.body[:0], body...)
	return nil
}

func (a *panel) openServices() {
	a.showChords, a.showServices = false, true
	a.sel, a.list.Sel, a.list.ScrollTop = 0, 0, 0
	a.services.Refresh()
	a.serviceStatus()
	vi.ConsoleLine("goset: services n=" + vi.Itoa64(int64(len(a.services.manifest.Services))))
}

func (a *panel) serviceStatus() {
	if a.services.reason != "" {
		a.status = "services refused: " + a.services.reason + " (read-only)"
		vi.ConsoleLine("goset: services refuse " + a.services.reason)
		return
	}
	a.status = "name=on|off + Enter, or Tab then Left/Right, Enter; settings returns"
}

func (a *panel) serviceInput() {
	line := strings.TrimSpace(a.input.Value())
	a.input.Clear()
	if line == "settings" {
		a.showServices = false
		a.sel, a.list.Sel, a.list.ScrollTop = 0, 0, 0
		a.status = "key=value + Enter; services opens Services"
		return
	}
	if line != "" {
		name, value, ok := strings.Cut(line, "=")
		if !ok || value != "on" && value != "off" {
			a.status = "services: name=on or name=off"
			return
		}
		if err := a.services.Set(name, value == "on"); err != nil {
			a.status = "services refused: " + serviceReason(err)
			vi.ConsoleLine("goset: services refuse " + serviceReason(err))
			return
		}
	}
	a.saveServices()
}

func (a *panel) saveServices() {
	if err := a.services.Save(); err != nil {
		a.status = "services refused: " + serviceReason(err)
		vi.ConsoleLine("goset: services refuse " + serviceReason(err))
		return
	}
	a.status = "saved " + svcmanifest.Path
	for _, s := range a.services.manifest.Services {
		vi.ConsoleLine("goset: service saved name=" + s.Name + " enabled=" + serviceState(s.Enabled))
	}
}
