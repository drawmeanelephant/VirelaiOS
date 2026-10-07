package main

import (
	"bytes"
	"strings"
	"testing"

	"virelai/svcmanifest"
	"virelai/vi"
)

type fakeServices struct {
	body    []byte
	writes  int
	writeRC int64
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
		write: func(path string, body []byte) int64 {
			if path != svcmanifest.Path {
				t.Fatal(path)
			}
			f.writes++
			if f.writeRC < 0 {
				return f.writeRC
			}
			f.body = append([]byte(nil), body...)
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
		if err := v.Save(); err != nil {
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
	if v.Set("probe", true) == nil || v.Save() == nil || f.writes != 0 {
		t.Fatal("corrupt config was healed")
	}
	f.body = servicesFixture()
	if !v.Refresh() || v.reason != "" {
		t.Fatal("recovery failed")
	}
	v.Set("probe", true)
	f.body = append(f.body, '\n')
	if v.Save() == nil || f.writes != 0 {
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
	if v.Save() == nil || f.writes != 0 {
		t.Fatal("missing required node saved")
	}
	v.manifest.Services[1].Requires = nil
	f.writeRC = -vi.ErrENOSPC
	before := append([]byte(nil), f.body...)
	if v.Save() == nil || !bytes.Equal(f.body, before) {
		t.Fatal("failed publish changed baseline")
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
