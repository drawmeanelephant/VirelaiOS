//go:build !virelai

package main

// M207a (#3817): apps.txt/fleet.txt drift is a test failure, not a
// reader's job. Every `.ELF` row in image/apps.txt is either a fleet.txt
// row or a pending name whose entry says which M207 card adds it; a row
// in neither — or a pending name already in the fleet — fails here.
// Adding a fleet row means editing fleet.txt and deleting its pending
// entry, nothing else.
//
// Host-only: the drift set is derived from image/apps.txt, a repo path
// the staged share does not carry (the in-guest gobuild test run compiles
// the untagged files only).

import (
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

// pendingFleet maps each apps.txt .ELF row that is not yet a fleet row to
// the M207 card that adds it. M207d (#3820) emptied the list: every
// apps.txt Go row is now a fleet row, and this map stays empty — a new
// pending entry is only lawful while its card is open, and the drift
// findings above flag a stale one.
var pendingFleet = map[string]string{}

// nonAppsFleet pins the Go ELFs that hold a fleet row without being
// apps.txt rows — GODIFF is a named tool (M201b), the other four are the
// toolchain itself. A row removed here has no drift to catch it, so the
// list is explicit.
var nonAppsFleet = []string{
	"GODIFF.ELF", "FLINT.ELF", "STORE.ELF", "AGENTD.ELF", "GOBUILD.ELF",
}

// appsELFs reads the NAME field of each image/apps.txt row and keeps the
// `.ELF` names (ZC.BIN and TABWM.BIN are Zig, out of scope).
func appsELFs(text string) []string {
	var out []string
	for _, line := range strings.Split(text, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		name, _, _ := strings.Cut(line, "|")
		name = strings.TrimSpace(name)
		if strings.HasSuffix(name, ".ELF") {
			out = append(out, name)
		}
	}
	return out
}

// fleetDrift returns the drift findings for one (apps.txt rows, fleet
// rows, pending, non-row ELFs) quad, sorted for a stable report. Pure —
// the committed-file test and the synthetic cases share it.
func fleetDrift(apps, fleet []string, pending map[string]string, nonRow []string) []string {
	inFleet := map[string]bool{}
	for _, n := range fleet {
		inFleet[n] = true
	}
	isApp := map[string]bool{}
	for _, n := range apps {
		isApp[n] = true
	}
	var out []string
	for _, name := range apps {
		if inFleet[name] || pending[name] != "" {
			continue
		}
		out = append(out, name+" is an apps.txt row in neither fleet.txt "+
			"nor the pending list")
	}
	for name, card := range pending {
		if inFleet[name] {
			out = append(out, name+" is already a fleet.txt row — drop its "+
				"pending entry ("+card+")")
		}
		if !isApp[name] {
			out = append(out, name+" is pending "+card+" but not an "+
				"apps.txt row")
		}
	}
	for _, name := range nonRow {
		if !inFleet[name] {
			out = append(out, name+" is a non-apps.txt Go ELF missing from "+
				"fleet.txt")
		}
	}
	sort.Strings(out)
	return out
}

// wantFleetNames derives the committed fleet's row-name set: the apps.txt
// .ELF rows that are not pending, plus the non-apps.txt Go ELFs. The
// count falls out of the inputs — a row addition never edits a number.
func wantFleetNames(apps []string) map[string]bool {
	want := map[string]bool{}
	for _, n := range apps {
		if pendingFleet[n] == "" {
			want[n] = true
		}
	}
	for _, n := range nonAppsFleet {
		want[n] = true
	}
	return want
}

// The committed triple: every apps.txt .ELF row is a fleet row or a
// pending name, and the fleet itself is exactly the derived set.
func TestFleetAppsDrift(t *testing.T) {
	apps := appsELFs(mustRead(t, filepath.Join("..", "..", "..",
		"image", "apps.txt")))
	rows, d := parseFleet(mustRead(t, "fleet.txt"))
	if d.Code != "" {
		t.Fatalf("fleet.txt: %+v", d)
	}
	var fleet []string
	got := map[string]bool{}
	for _, r := range rows {
		if got[r.Name] {
			t.Fatalf("fleet.txt repeats %s", r.Name)
		}
		got[r.Name] = true
		fleet = append(fleet, r.Name)
	}
	if probs := fleetDrift(apps, fleet, pendingFleet, nonAppsFleet); len(probs) > 0 {
		t.Fatalf("apps.txt/fleet.txt drift:\n%s", strings.Join(probs, "\n"))
	}
	// Exact-set equality pins the count without typing it: an extra row
	// or a missing one is the same failure.
	want := wantFleetNames(apps)
	if len(fleet) != len(want) {
		t.Fatalf("fleet.txt rows = %d, want %d derived (apps.txt minus "+
			"pending, plus non-row ELFs)", len(fleet), len(want))
	}
	for n := range want {
		if !got[n] {
			t.Fatalf("fleet.txt missing derived row %s", n)
		}
	}
	for n := range got {
		if !want[n] {
			t.Fatalf("fleet.txt row %s is neither an apps.txt row nor a "+
				"named non-row ELF", n)
		}
	}
}

// The checker earns its keep on synthetic input: each failure mode is a
// named finding, and the happy path stays quiet.
func TestFleetDriftFindings(t *testing.T) {
	fleet := []string{"GOCALC.ELF", "GODIFF.ELF"}
	pending := map[string]string{"GOPENDING.ELF": "#0000"}

	// A synthetic apps.txt row in neither the fleet nor the pending list
	// is a drift finding.
	probs := fleetDrift([]string{"GOCALC.ELF", "GONEW.ELF", "GOPENDING.ELF"},
		fleet, pending, []string{"GODIFF.ELF"})
	if len(probs) != 1 || !strings.Contains(probs[0], "GONEW.ELF") {
		t.Fatalf("row in neither = %v", probs)
	}

	// Pending covers the row; the fleet-covered row stays quiet.
	if probs := fleetDrift([]string{"GOCALC.ELF", "GOPENDING.ELF"},
		fleet, pending, []string{"GODIFF.ELF"}); len(probs) != 0 {
		t.Fatalf("covered rows = %v", probs)
	}

	// A pending name already in the fleet is a finding — the entry is
	// stale, not harmless.
	probs = fleetDrift([]string{"GOCALC.ELF"}, fleet,
		map[string]string{"GOCALC.ELF": "#0000"}, []string{"GODIFF.ELF"})
	if len(probs) != 1 || !strings.Contains(probs[0], "GOCALC.ELF") {
		t.Fatalf("pending-already-in-fleet = %v", probs)
	}

	// A missing non-apps.txt ELF is a finding.
	probs = fleetDrift(nil, fleet, nil, []string{"GODIFF.ELF", "GOBUILD.ELF"})
	if len(probs) != 1 || !strings.Contains(probs[0], "GOBUILD.ELF") {
		t.Fatalf("missing non-row ELF = %v", probs)
	}
}
