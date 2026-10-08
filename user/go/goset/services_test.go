package main

import (
	"bytes"
	"strings"
	"testing"

	"virelai/svcmanifest"
	"virelai/vi"
)

type fakeServices struct {
	body      []byte
	staged    []byte
	writes    int
	writeRC   int64
	publishRC int64
	onPublish func()
}

func (f *fakeServices) view(t *testing.T) servicesView {
	t.Helper()
	v := servicesView{store: serviceStore{
		read: func(path string, max int) ([]byte, int64) {
			if path != svcmanifest.Path || max != svcmanifest.MaxBytes+1 {
				t.Fatal(path, max)
			}
			return append([]byte(nil), f.body...), int64(len(f.body))
		},
		stage: func(path string, body []byte) int64 {
			if path != svcmanifest.Path {
				t.Fatal(path)
			}
			f.writes++
			if f.writeRC < 0 {
				return f.writeRC
			}
			f.staged = append([]byte(nil), body...)
			return 0
		},
		publish: func(path string) int64 {
			if path != svcmanifest.Path {
				t.Fatal(path)
			}
			if f.publishRC < 0 {
				return f.publishRC
			}
			f.body = append([]byte(nil), f.staged...)
			f.staged = nil
			if f.onPublish != nil {
				f.onPublish()
			}
			return 0
		},
	}}
	v.Refresh()
	return v
}

func servicesFixture() []byte {
	return []byte(`{"version":1,"services":[
{"name":"seat","argv":["GOTABWM.ELF"],"restart":{"restart":"never"},"class":"boot"},
{"name":"probe","argv":["INITDEP.BIN","a b"],"restart":{"restart":"never"},"class":"on-demand","enabled":false}]}`)
}

func TestServicesRowsPersistBothClassesThroughSafeStore(t *testing.T) {
	f := &fakeServices{body: servicesFixture()}
	v := f.view(t)
	labels := v.Labels()
	if len(labels) != 2 || !strings.Contains(labels[0], "boot  enabled=on  (applies at next boot)") ||
		!strings.Contains(labels[1], "on-demand  enabled=off") {
		t.Fatal(labels)
	}
	for _, name := range []string{"probe", "seat"} {
		if err := v.Set(name, name == "probe"); err != nil {
			t.Fatal(err)
		}
		if err := v.Save(nil); err != nil {
			t.Fatal(err)
		}
	}
	m, err := svcmanifest.Parse(f.body)
	if err != nil || m.Services[0].Enabled || !m.Services[1].Enabled ||
		m.Services[1].Argv[1] != "a b" || f.writes != 2 {
		t.Fatal(m, err, f.writes)
	}
	if v.Refresh() {
		t.Fatal("unchanged content repainted")
	}
}

func TestServicesCorruptionAndConflictsNeverPublish(t *testing.T) {
	f := &fakeServices{body: servicesFixture()}
	v := f.view(t)
	f.body = []byte(`{"version":1,"services":null}`)
	if !v.Refresh() || v.reason != "null-field" || len(v.Labels()) != 2 {
		t.Fatal(v.reason, v.Labels())
	}
	if v.Set("probe", true) == nil || v.Save(nil) == nil || f.writes != 0 {
		t.Fatal("corrupt config was healed")
	}
	f.body = servicesFixture()
	if !v.Refresh() || v.reason != "" {
		t.Fatal("recovery failed")
	}
	v.Set("probe", true)
	f.body = append(f.body, '\n')
	if v.Save(nil) == nil || f.writes != 0 {
		t.Fatal("concurrent config overwritten")
	}
	if v.manifest.Services[1].Enabled {
		t.Fatal("stale pending edit survived refresh")
	}
}

func TestServicesGraphFailureAndWriteError(t *testing.T) {
	f := &fakeServices{body: servicesFixture()}
	v := f.view(t)
	v.manifest.Services[1].Requires = []string{"missing"}
	v.Set("probe", true)
	if v.Save(nil) == nil || f.writes != 0 {
		t.Fatal("missing required node saved")
	}
	v.manifest.Services[1].Requires = nil
	f.writeRC = -vi.ErrENOSPC
	before := append([]byte(nil), f.body...)
	if v.Save(nil) == nil || !bytes.Equal(f.body, before) {
		t.Fatal("failed publish changed baseline")
	}
	f.writeRC = 0
	f.publishRC = -vi.ErrENOSPC
	if err := v.Save(nil); err == nil || !strings.Contains(err.Error(), "manifest-publish") ||
		!bytes.Equal(f.body, before) {
		t.Fatal("failed rename changed baseline or went unreported", err)
	}
}

// The toggle receipt contract: every `goset: service saved` line must land
// before the publish that makes the manifest observable, because the
// `svc: start` a poller answers it with is a consequence of that publish.
// The injected publish simulates init's ~1ms reloader reacting instantly; a
// receipt that waited for the rename to return would land after the start it
// caused — the live-supervise toggle race (svc: start ahead of the saved
// receipt, observed on main f6594086).
func TestServicesReceiptsPrecedePublish(t *testing.T) {
	f := &fakeServices{body: servicesFixture()}
	var events []string
	old := consoleLine
	consoleLine = func(s string) { events = append(events, s) }
	defer func() { consoleLine = old }()
	a := newPanel(nil)
	a.services = f.view(t)
	f.onPublish = func() {
		events = append(events, "svc: start name=probe")
	}
	a.input.SetValue("probe=on")
	a.serviceInput()
	if f.writes != 1 {
		t.Fatal("no publish staged", f.writes, events)
	}
	start, lastReceipt := -1, -1
	for i, e := range events {
		if e == "svc: start name=probe" {
			if start >= 0 {
				t.Fatal("probe started twice", events)
			}
			start = i
		}
		if strings.HasPrefix(e, "goset: service saved name=") {
			lastReceipt = i
		}
	}
	if start < 0 {
		t.Fatal("publish never reached the starter", events)
	}
	if lastReceipt < 0 {
		t.Fatal("no save receipts", events)
	}
	if lastReceipt > start {
		t.Fatalf("receipt landed after the svc: start it caused: %v", events)
	}
}

func TestPanelServicesTypedAndKeyboardPaths(t *testing.T) {
	f := &fakeServices{body: servicesFixture()}
	a := newPanel(nil)
	a.services = f.view(t)
	before := a.summary()
	a.input.SetValue("services")
	a.key(vi.Event{Kind: vi.EvKeyDown, Arg1: '\n'})
	if !a.showServices || !strings.HasPrefix(a.headLabel(), "Services") || f.writes != 0 {
		t.Fatal("opening view wrote a file")
	}
	a.input.SetValue("probe=on")
	a.key(vi.Event{Kind: vi.EvKeyDown, Arg1: '\n'})
	if f.writes != 1 || !a.services.manifest.Services[1].Enabled {
		t.Fatal("typed toggle did not persist")
	}
	a.sel = 1
	a.focus.Focus(0)
	a.cycle(-1)
	a.key(vi.Event{Kind: vi.EvKeyDown, Arg1: '\n'})
	if f.writes != 2 || a.services.manifest.Services[1].Enabled {
		t.Fatal("row toggle did not persist")
	}
	a.input.SetValue("settings")
	a.key(vi.Event{Kind: vi.EvKeyDown, Arg1: '\n'})
	if a.showServices || a.summary() != before || f.writes != 2 {
		t.Fatal("services touched SETTINGS.TXT rows")
	}
}
