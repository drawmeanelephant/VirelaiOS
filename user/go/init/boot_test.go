package main

import (
	"errors"
	"reflect"
	"strings"
	"testing"

	"virelai/supervise"
	"virelai/svcmanifest"
)

type fakeBoot struct {
	now      int64
	rows     []supervise.Process
	launches []string
	kills    []uint64
	markers  []string
	failExec bool
}

func (f *fakeBoot) hooks() supervise.Hooks {
	return supervise.Hooks{
		Now:   func() int64 { return f.now },
		Table: func() ([]supervise.Process, error) { return f.rows, nil },
		Exec: func(binary string, args ...string) (int64, error) {
			f.launches = append(f.launches, binary)
			if f.failExec {
				return -1, errors.New("exec")
			}
			pid := int64(len(f.rows) + 1)
			f.rows = append(f.rows, supervise.Process{PID: pid, Running: true})
			return pid, nil
		},
		Kill: func(pid uint64) int64 {
			f.kills = append(f.kills, pid)
			for i := range f.rows {
				if uint64(f.rows[i].PID) == pid {
					f.rows[i].Running, f.rows[i].Exited = false, true
					f.rows[i].Status = 137
				}
			}
			return 0
		},
		Random:  func(buf []byte) (int, error) { clear(buf); return len(buf), nil },
		Receipt: func(string, string) int64 { return 0 },
		Serial:  func(line string) { f.markers = append(f.markers, line) },
	}
}

func service(name, binary string, requires ...string) svcmanifest.Service {
	return svcmanifest.Service{Name: name, Argv: []string{binary},
		Requires: requires, Restart: svcmanifest.Policy{Restart: "never"}, Class: "boot", Enabled: true}
}

func manifest() svcmanifest.Manifest {
	return svcmanifest.Manifest{Version: 1, Services: []svcmanifest.Service{
		service("seat", "GOTABWM.ELF", "a-dep"),
		service("a-dep", "INITDEP.BIN", "z-pre"),
		service("z-pre", "INITPRE.BIN"),
	}}
}

func tick(t *testing.T, b *Boot, f *fakeBoot) {
	t.Helper()
	f.now += 1e9
	if err := b.Tick(); err != nil {
		t.Fatal(err)
	}
}

func TestPollReadinessAndDeterministicOrder(t *testing.T) {
	f := &fakeBoot{}
	b, err := NewBoot(manifest(), "GOTABWM.ELF", false, f.hooks())
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 3; i++ {
		tick(t, b, f)
		if len(f.launches) != i+1 {
			t.Fatalf("poll %d launches %v", i, f.launches)
		}
		if b.SeatReady() {
			t.Fatal("seat ready before subsequent poll")
		}
	}
	tick(t, b, f)
	if !b.SeatReady() || b.seated {
		t.Fatal("readiness does not establish registration")
	}
	b.Seated()
	b.Seated()
	if !reflect.DeepEqual(f.launches, []string{"INITPRE.BIN", "INITDEP.BIN", "GOTABWM.ELF"}) {
		t.Fatal(f.launches)
	}
	want := []string{"init: manifest ok n=3", "init: start name=z-pre order=1",
		"init: ready name=z-pre", "init: start name=a-dep order=2",
		"init: ready name=a-dep", "init: start name=seat order=3", "init: ready name=seat", "init: seated"}
	var markers []string
	for _, line := range f.markers {
		if strings.HasPrefix(line, "init:") {
			markers = append(markers, line)
		}
	}
	if !reflect.DeepEqual(markers, want) {
		t.Fatalf("markers %v", markers)
	}
}

func TestCompletedNeverOneShotSatisfiesDependency(t *testing.T) {
	f := &fakeBoot{}
	b, _ := NewBoot(manifest(), "GOTABWM.ELF", false, f.hooks())
	tick(t, b, f)
	f.rows[0].Running, f.rows[0].Exited = false, true
	tick(t, b, f)
	if len(f.launches) != 2 {
		t.Fatal(f.launches)
	}
}

func TestAdmissionRefusesBeforeAnyExec(t *testing.T) {
	for _, tc := range []struct {
		name  string
		edit  func(*svcmanifest.Manifest)
		login bool
	}{
		{"extra-Go", func(m *svcmanifest.Manifest) { m.Services[1].Argv[0] = "DAEMON.ELF" }, false},
		{"unknown-native", func(m *svcmanifest.Manifest) { m.Services[1].Argv[0] = "DAEMON.BIN" }, false},
		{"duplicate-shell", func(m *svcmanifest.Manifest) { m.Services[1].Argv[0] = "GOSH.ELF" }, false},
		{"serial-login", func(*svcmanifest.Manifest) {}, true},
		{"missing-seat", func(m *svcmanifest.Manifest) { m.Services[0].Enabled = false }, false},
		{"cycle", func(m *svcmanifest.Manifest) { m.Services[2].Requires = []string{"seat"} }, false},
		{"disabled-required", func(m *svcmanifest.Manifest) { m.Services[2].Enabled = false }, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m, f := manifest(), &fakeBoot{}
			tc.edit(&m)
			if _, err := NewBoot(m, "GOTABWM.ELF", tc.login, f.hooks()); err == nil {
				t.Fatal("accepted")
			}
			if len(f.launches) != 0 {
				t.Fatal("partial activation")
			}
		})
	}
}

func TestInitialFailureAndReverseCleanup(t *testing.T) {
	f := &fakeBoot{}
	b, _ := NewBoot(manifest(), "GOTABWM.ELF", false, f.hooks())
	for i := 0; i < 2; i++ {
		tick(t, b, f)
	}
	f.failExec = true
	if err := b.Tick(); err == nil || err.Error() != "unstartable" {
		t.Fatal(err)
	}
	if err := b.Stop(); err != nil {
		t.Fatal(err)
	}
	tick(t, b, f)
	if !b.Stopped() || !reflect.DeepEqual(f.kills, []uint64{2, 1}) {
		t.Fatal(f.kills)
	}
}

func TestRetryWaitsForPrerequisiteWithoutStoppingDependent(t *testing.T) {
	m := manifest()
	for i := range m.Services {
		m.Services[i].Restart = svcmanifest.Policy{Restart: "on-failure", BackoffBaseS: 1, BackoffCapS: 1, MaxRestarts: 3, Window: 10}
	}
	f := &fakeBoot{}
	b, _ := NewBoot(m, "GOTABWM.ELF", false, f.hooks())
	for i := 0; i < 4; i++ {
		tick(t, b, f)
	}
	b.Seated()
	f.rows[0].Running, f.rows[0].Exited, f.rows[0].Status = false, true, 137
	tick(t, b, f)
	if len(f.kills) != 0 {
		t.Fatal("prerequisite death stopped a dependent")
	}
	f.rows[1].Running, f.rows[1].Exited, f.rows[1].Status = false, true, 137
	tick(t, b, f) // prerequisite retries; dependent schedules backoff
	before := len(f.launches)
	f.rows[len(f.rows)-1].Running = false // prerequisite has not been observed running
	tick(t, b, f)
	if len(f.launches) != before {
		t.Fatal("dependent restarted before prerequisite readiness")
	}
	f.rows[len(f.rows)-1].Running = true
	tick(t, b, f)
	view, _ := b.services["a-dep"].child.Snapshot("a-dep")
	if view.Starts != 2 || view.Restart != 1 {
		t.Fatal(view)
	}
}
