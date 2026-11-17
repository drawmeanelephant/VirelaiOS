//go:build !virelai

package main

// M207a (#3817): apps.txt/fleet.txt drift is a test failure, not a
// reader's job. Every `.ELF` row in image/apps.txt is a fleet.txt row;
// the pending set is empty and any entry in it is a finding, so it can
// never hide a missing app. Missing, extra and duplicate fleet rows all
// fail the exact-set check. Adding a fleet row means editing fleet.txt,
// nothing else.
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

// pendingFleet is the retired M207 staging list and must stay empty:
// every apps.txt Go row is now a fleet row. An entry is a test failure
// (see fleetDrift) — it may not substitute for the row a shipping app
// needs.
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
	// The pending set is empty by invariant. Reject any entry rather than
	// letting it stand in for the fleet row a shipping app needs.
	for name, card := range pending {
		why := "pendingFleet must stay empty"
		switch {
		case inFleet[name]:
			why = "it is already a fleet.txt row — drop the stale entry"
		case !isApp[name]:
			why = "it is not an apps.txt row"
		}
		out = append(out, name+" is in pendingFleet ("+card+"): "+why)
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

// wantFleetNames derives the committed fleet's row-name set: every
// apps.txt .ELF row plus the non-apps.txt Go ELFs. The pending list is
// deliberately not an input — a nonempty pending set may not shrink the
// derived membership and hide a missing app. The count falls out of the
// inputs — a row addition never edits a number.
func wantFleetNames(apps []string) map[string]bool {
	want := map[string]bool{}
	for _, n := range apps {
		want[n] = true
	}
	for _, n := range nonAppsFleet {
		want[n] = true
	}
	return want
}

// TestPendingFleetEmpty pins the committed invariant: every apps.txt Go
// ELF holds a fleet row, so the pending list is empty. A nonempty entry
// would let a missing shipping app hide from the exact-set check.
func TestPendingFleetEmpty(t *testing.T) {
	if len(pendingFleet) != 0 {
		t.Fatalf("pendingFleet must stay empty; still pending: %v", pendingFleet)
	}
}

// fleetSetFindings compares the committed row list with the derived set:
// a duplicate row, a missing derived row and an extra row are each named.
func fleetSetFindings(fleet []string, want map[string]bool) []string {
	got := map[string]bool{}
	var out []string
	for _, n := range fleet {
		if got[n] {
			out = append(out, n+" is a duplicate fleet.txt row")
		}
		got[n] = true
	}
	for n := range want {
		if !got[n] {
			out = append(out, n+" is missing from fleet.txt")
		}
	}
	for n := range got {
		if !want[n] {
			out = append(out, n+" is an extra fleet.txt row")
		}
	}
	sort.Strings(out)
	return out
}

// The committed triple: every apps.txt .ELF row is a fleet row, and the
// fleet itself is exactly the derived set.
func TestFleetAppsDrift(t *testing.T) {
	apps := appsELFs(mustRead(t, filepath.Join("..", "..", "..",
		"image", "apps.txt")))
	rows, d := parseFleet(mustRead(t, "fleet.txt"))
	if d.Code != "" {
		t.Fatalf("fleet.txt: %+v", d)
	}
	var fleet []string
	for _, r := range rows {
		fleet = append(fleet, r.Name)
	}
	if probs := fleetDrift(apps, fleet, pendingFleet, nonAppsFleet); len(probs) > 0 {
		t.Fatalf("apps.txt/fleet.txt drift:\n%s", strings.Join(probs, "\n"))
	}
	// Exact-set equality pins the count without typing it: an extra row,
	// a missing one or a duplicate is the same failure.
	want := wantFleetNames(apps)
	if probs := fleetSetFindings(fleet, want); len(probs) > 0 {
		t.Fatalf("fleet.txt exact set (%d rows, want %d derived):\n%s",
			len(fleet), len(want), strings.Join(probs, "\n"))
	}
}

// The checker earns its keep on synthetic input: each failure mode is a
// named finding, and the happy path stays quiet.
func TestFleetDriftFindings(t *testing.T) {
	fleet := []string{"GOCALC.ELF", "GODIFF.ELF"}
	pending := map[string]string{"GOPENDING.ELF": "#0000"}

	// A synthetic apps.txt row in neither the fleet nor the pending list
	// is a drift finding; the nonempty pending entry is a second one.
	probs := fleetDrift([]string{"GOCALC.ELF", "GONEW.ELF", "GOPENDING.ELF"},
		fleet, pending, []string{"GODIFF.ELF"})
	if len(probs) != 2 || !strings.Contains(probs[0], "GONEW.ELF") ||
		!strings.Contains(probs[1], "GOPENDING.ELF") {
		t.Fatalf("row in neither = %v", probs)
	}

	// A nonempty pending entry is itself a finding: it may not stand in
	// for a missing fleet row, even when it names an apps.txt row.
	probs = fleetDrift([]string{"GOCALC.ELF", "GOPENDING.ELF"},
		fleet, pending, []string{"GODIFF.ELF"})
	if len(probs) != 1 || !strings.Contains(probs[0], "GOPENDING.ELF") {
		t.Fatalf("nonempty pending = %v", probs)
	}

	// The fleet-covered path stays quiet with an empty pending set.
	if probs := fleetDrift([]string{"GOCALC.ELF"},
		fleet, map[string]string{}, []string{"GODIFF.ELF"}); len(probs) != 0 {
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

// The exact-set check rejects each non-equality mode, not just a count
// mismatch: a duplicate, a missing derived row and an extra row.
func TestFleetSetFindings(t *testing.T) {
	want := map[string]bool{"A.ELF": true, "B.ELF": true}

	if probs := fleetSetFindings([]string{"A.ELF"}, want); len(probs) != 1 ||
		!strings.Contains(probs[0], "missing") {
		t.Fatalf("missing row = %v", probs)
	}
	if probs := fleetSetFindings([]string{"A.ELF", "B.ELF", "C.ELF"},
		want); len(probs) != 1 || !strings.Contains(probs[0], "extra") {
		t.Fatalf("extra row = %v", probs)
	}
	if probs := fleetSetFindings([]string{"A.ELF", "A.ELF", "B.ELF"},
		want); len(probs) != 1 || !strings.Contains(probs[0], "duplicate") {
		t.Fatalf("duplicate row = %v", probs)
	}
	if probs := fleetSetFindings([]string{"A.ELF", "B.ELF"}, want); len(probs) != 0 {
		t.Fatalf("exact set = %v", probs)
	}
}
