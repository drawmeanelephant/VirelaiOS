package main

import (
	"bytes"
	"reflect"
	"strings"
	"testing"

	"virelai/supervise"
	"virelai/svcmanifest"
	"virelai/vi"
)

func reloadManifest() svcmanifest.Manifest {
	m := manifest()
	m.Services[0].Requires = nil
	m.Services[1].Requires = nil
	m.Services[1].Class, m.Services[1].Enabled = "on-demand", false
	return m
}

func document(t *testing.T, m svcmanifest.Manifest) []byte {
	t.Helper()
	body, err := svcmanifest.Format(m)
	if err != nil {
		t.Fatal(err)
	}
	return body
}

func liveBoot(t *testing.T, m svcmanifest.Manifest, hooks supervise.Hooks, f *fakeBoot) *Boot {
	t.Helper()
	b, err := NewBoot(m, "GOTABWM.ELF", false, hooks)
	if err != nil {
		t.Fatal(err)
	}
	tick(t, b, f)
	tick(t, b, f)
	b.Seated()
	return b
}

func TestContentPollSameLengthToggleAndWholeRefusal(t *testing.T) {
	m, f := reloadManifest(), &fakeBoot{}
	b := liveBoot(t, m, f.hooks(), f)
	body := document(t, m)
	reads := 0
	r := newReloader(body, func(path string, max int) ([]byte, int64) {
		reads++
		if path != svcmanifest.Path || max != svcmanifest.MaxBytes+1 {
			t.Fatal(path, max)
		}
		return body, int64(len(body))
	})
	oldLen := len(body)
	body = bytes.Replace(body, []byte(`"enabled": false`), []byte(`"enabled": true `), 1)
	if len(body) != oldLen {
		t.Fatal("fixture must have identical byte length")
	}
	r.Poll(b)
	tick(t, b, f)
	if len(f.launches) != 3 || f.launches[2] != "INITDEP.BIN" {
		t.Fatalf("same-length toggle ignored: %v", f.launches)
	}
	unchanged := b.services["a-dep"]
	for _, corrupt := range [][]byte{[]byte("{"), []byte(`{"version":1,"services":null}`),
		bytes.Repeat([]byte(" "), svcmanifest.MaxBytes+1)} {
		body = corrupt
		r.Poll(b)
		r.Poll(b)
		tick(t, b, f)
		if b.services["a-dep"] != unchanged || len(f.kills) != 0 || len(f.launches) != 3 {
			t.Fatal("refusal changed running set")
		}
	}
	if reads != 7 {
		t.Fatal(reads)
	}
	if strings.Count(strings.Join(f.markers, "\n"), "init: reload refuse") != 3 {
		t.Fatal("unchanged corrupt contents must be named once")
	}
}

func TestDisableSuppressesReceiptAndReenableKeepsCounters(t *testing.T) {
	m, f := reloadManifest(), &fakeBoot{}
	m.Services[1].Enabled = true
	m.Services[1].Restart = svcmanifest.Policy{Restart: "always", BackoffBaseS: 1,
		BackoffCapS: 1, MaxRestarts: 3, Window: 10}
	hooks := f.hooks()
	receipts := 0
	hooks.Receipt = func(string, string) int64 { receipts++; return 0 }
	b := liveBoot(t, m, hooks, f)
	child := b.services["a-dep"].child
	view, _ := child.Snapshot("a-dep")
	for i := range f.rows {
		if f.rows[i].PID == view.PID {
			f.rows[i].Running, f.rows[i].Exited, f.rows[i].Status = false, true, 137
		}
	}
	tick(t, b, f)
	tick(t, b, f)
	if receipts != 1 {
		t.Fatal(receipts)
	}
	m.Services[1].Enabled = false
	if err := b.Reload(m); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 4; i++ {
		tick(t, b, f)
	}
	view, _ = child.Snapshot("a-dep")
	if view.State != supervise.Stopped || receipts != 1 || view.Restart != 1 {
		t.Fatal(view, receipts)
	}
	m.Services[1].Enabled = true
	if err := b.Reload(m); err != nil {
		t.Fatal(err)
	}
	tick(t, b, f)
	view, _ = child.Snapshot("a-dep")
	if b.services["a-dep"].child != child || view.Restart != 1 || view.Starts != 3 {
		t.Fatal("stop/start reset supervisor", view)
	}
}

func TestDeferredBootChangeAndInvalidProjectedGraph(t *testing.T) {
	m, f := reloadManifest(), &fakeBoot{}
	b := liveBoot(t, m, f.hooks(), f)
	boot := b.services["z-pre"]
	m.Services[2].Enabled = false
	if err := b.Reload(m); err != nil {
		t.Fatal(err)
	}
	tick(t, b, f)
	if b.services["z-pre"] != boot || !boot.config.Enabled || len(f.kills) != 0 {
		t.Fatal("boot change applied live")
	}
	if !strings.Contains(strings.Join(f.markers, "\n"), "init: next boot name=z-pre") {
		t.Fatal(f.markers)
	}
	// A class change is deferred as well.
	m = manifest()
	f = &fakeBoot{}
	b = liveBoot(t, m, f.hooks(), f)
	tick(t, b, f)
	tick(t, b, f)
	b.Seated()
	m.Services[0].Requires = nil
	m.Services[1].Enabled, m.Services[1].Class = false, "on-demand"
	// A class change is also boot-frozen, leaving the prerequisite enabled.
	if err := b.Reload(m); err != nil {
		t.Fatal(err)
	}
	if b.services["a-dep"].config.Class != "boot" || !b.services["a-dep"].config.Enabled {
		t.Fatal("class change was not deferred")
	}
	// The requested graph resolves, but changing the seat's dependency is
	// deferred, so disabling its live prerequisite must refuse whole.
	m = manifest()
	m.Services[1].Class = "on-demand"
	f = &fakeBoot{}
	b = liveBoot(t, m, f.hooks(), f)
	for i := 0; i < 2; i++ {
		tick(t, b, f)
	}
	b.Seated()
	m.Services[0].Requires = nil
	m.Services[1].Enabled = false
	if err := b.Reload(m); err == nil || len(f.kills) != 0 || !b.services["a-dep"].config.Enabled {
		t.Fatal("invalid projected graph changed live set", err, f.kills)
	}
}

func TestReverseDisableAndDelayedReplacementAdmission(t *testing.T) {
	m, f := manifest(), &fakeBoot{}
	m.Services[0].Requires = nil
	m.Services[1].Class, m.Services[2].Class = "on-demand", "on-demand"
	hooks := f.hooks()
	hooks.Kill = func(pid uint64) int64 { f.kills = append(f.kills, pid); return 0 }
	b := liveBoot(t, m, hooks, f)
	tick(t, b, f)
	tick(t, b, f)
	b.Seated()
	m.Services[1].Enabled, m.Services[2].Enabled = false, false
	if err := b.Reload(m); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(f.kills, []uint64{3, 2}) {
		// Lexical order starts the seat and z-pre first, then a-dep.
		t.Fatal("reverse dependency stop", f.kills)
	}
	old := b.services["a-dep"].child
	m.Services[1].Enabled, m.Services[2].Enabled = true, true
	m.Services[1].Argv = []string{"INITDEP.BIN", "replacement"}
	if err := b.Reload(m); err != nil {
		t.Fatal(err)
	}
	before := len(f.launches)
	tick(t, b, f)
	if len(f.launches) != before {
		t.Fatal("replacement spawned while old tasks still live")
	}
	for i := range f.rows {
		if f.rows[i].PID == 2 || f.rows[i].PID == 3 {
			f.rows[i].Running, f.rows[i].Exited, f.rows[i].Status = false, true, 137
		}
	}
	for i := 0; i < 3; i++ {
		tick(t, b, f)
	}
	if b.services["a-dep"].child == old || len(f.launches) != before+2 || len(b.retired) != 0 {
		t.Fatal("replacement failed to drain/start", f.launches, len(b.retired))
	}
}

func TestReloadValidationReadGapAndKillRetry(t *testing.T) {
	m, f := reloadManifest(), &fakeBoot{}
	m.Services[1].Enabled = true
	hooks := f.hooks()
	kill := hooks.Kill
	failKill := true
	hooks.Kill = func(pid uint64) int64 {
		if failKill {
			return -vi.ErrEAGAIN
		}
		return kill(pid)
	}
	b := liveBoot(t, m, hooks, f)
	body, rc := document(t, m), int64(-vi.ErrENOENT)
	r := newReloader(body, func(string, int) ([]byte, int64) { return body, rc })
	r.Poll(b)
	r.Poll(b)
	m.Services[1].Enabled = false
	body, rc = document(t, m), 0
	r.Poll(b)
	if !b.services["a-dep"].config.Enabled {
		t.Fatal("failed kill committed replacement")
	}
	failKill = false
	r.Poll(b)
	tick(t, b, f)
	if b.services["a-dep"].config.Enabled || len(f.kills) != 1 {
		t.Fatal("identical content not retried after failed stop")
	}
	for _, edit := range []func(*svcmanifest.Manifest){
		func(m *svcmanifest.Manifest) { m.Services[0].Requires = []string{"absent"} },
		func(m *svcmanifest.Manifest) { m.Services[0].Requires = []string{"seat"} },
		func(m *svcmanifest.Manifest) { m.Services[1].Enabled = true; m.Services[1].Argv[0] = "OTHER.ELF" },
	} {
		bad := cloneManifest(m)
		edit(&bad)
		original := b.services["seat"]
		if err := b.Reload(bad); err == nil {
			t.Fatal("invalid replacement accepted")
		}
		if b.services["seat"] != original || len(f.kills) != 1 {
			t.Fatal("invalid replacement had effects")
		}
	}
}

func TestPartialStopRetryDoesNotRespawnStoppedDependent(t *testing.T) {
	m, f := manifest(), &fakeBoot{}
	m.Services[0].Requires = nil
	m.Services[1].Class, m.Services[2].Class = "on-demand", "on-demand"
	hooks := f.hooks()
	kill := hooks.Kill
	fail := true
	hooks.Kill = func(pid uint64) int64 {
		if pid == 2 && fail {
			return -vi.ErrEAGAIN
		}
		return kill(pid)
	}
	b := liveBoot(t, m, hooks, f)
	tick(t, b, f)
	tick(t, b, f)
	b.Seated()
	m.Services[1].Enabled, m.Services[2].Enabled = false, false
	before := len(f.launches)
	for i := 0; i < 3; i++ {
		if err := b.Reload(m); err == nil {
			t.Fatal("kill failure not returned")
		}
		tick(t, b, f)
		if len(f.launches) != before {
			t.Fatal("partially stopped dependent respawned during kill retry", f.launches)
		}
	}
	fail = false
	if err := b.Reload(m); err != nil {
		t.Fatal(err)
	}
	tick(t, b, f)
	if len(f.launches) != before || b.services["a-dep"].config.Enabled || b.services["z-pre"].config.Enabled {
		t.Fatal("stop retry failed to finish")
	}
}

func TestCancelledPartialStopRetriesEvenCachedOriginalContent(t *testing.T) {
	m, f := manifest(), &fakeBoot{}
	m.Services[0].Requires = nil
	m.Services[1].Class, m.Services[2].Class = "on-demand", "on-demand"
	hooks := f.hooks()
	kill := hooks.Kill
	hooks.Kill = func(pid uint64) int64 {
		if pid == 2 {
			return -vi.ErrEAGAIN
		}
		return kill(pid)
	}
	b := liveBoot(t, m, hooks, f)
	tick(t, b, f)
	tick(t, b, f)
	b.Seated()
	original := document(t, m)
	body := original
	r := newReloader(original, func(string, int) ([]byte, int64) { return body, 0 })
	m.Services[1].Enabled, m.Services[2].Enabled = false, false
	body = document(t, m)
	r.Poll(b)
	tick(t, b, f)
	if !b.reloadHold {
		t.Fatal("partial stop did not hold spawns")
	}
	body = original
	r.Poll(b)
	tick(t, b, f)
	if b.reloadHold || len(f.launches) != 4 || !b.services["a-dep"].config.Enabled {
		t.Fatal("original content was incorrectly cached after cancellation", f.launches)
	}
}
