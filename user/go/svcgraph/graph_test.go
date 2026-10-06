package svcgraph

import (
	"errors"
	"reflect"
	"strings"
	"testing"
)

func TestRequiredCycles(t *testing.T) {
	tests := []struct {
		name  string
		nodes []Node
		path  []string
	}{
		{
			"three-node",
			[]Node{{Name: "c", Requires: []string{"a"}}, {Name: "a", Requires: []string{"b"}}, {Name: "b", Requires: []string{"c"}}},
			[]string{"a", "b", "c", "a"},
		},
		{
			"self-loop",
			[]Node{{Name: "a", Requires: []string{"a"}}},
			[]string{"a", "a"},
		},
		{
			"two-disjoint",
			[]Node{
				{Name: "y", Requires: []string{"z"}}, {Name: "z", Requires: []string{"y"}},
				{Name: "b", Requires: []string{"a"}}, {Name: "a", Requires: []string{"b"}},
			},
			[]string{"a", "b", "a"},
		},
		{
			"acyclic-ancestor-of-later-cycle",
			[]Node{
				{Name: "0", Requires: []string{"z"}}, {Name: "z", Requires: []string{"y"}}, {Name: "y", Requires: []string{"z"}},
				{Name: "a", Requires: []string{"b"}}, {Name: "b", Requires: []string{"a"}},
			},
			[]string{"a", "b", "a"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p, _, err := Resolve(tt.nodes)
			var cycle *CycleError
			if !errors.As(err, &cycle) {
				t.Fatalf("got plan=%+v err=%v, want *CycleError", p, err)
			}
			if !reflect.DeepEqual(cycle.Path, tt.path) {
				t.Fatalf("path=%v, want %v", cycle.Path, tt.path)
			}
			if want := "cycle: " + strings.Join(tt.path, " -> "); cycle.Error() != want {
				t.Fatalf("error=%q, want %q", cycle.Error(), want)
			}
			if !reflect.DeepEqual(p, Plan{}) {
				t.Fatalf("failed resolution returned a partial plan: %+v", p)
			}
		})
	}
}

func TestMissingRequirement(t *testing.T) {
	p, _, err := Resolve([]Node{{Name: "seat", Requires: []string{"storage"}}})
	var missing *MissingError
	if !errors.As(err, &missing) {
		t.Fatalf("got plan=%+v err=%v, want *MissingError", p, err)
	}
	if missing.From != "seat" || missing.To != "storage" || missing.Error() != "missing requirement: seat -> storage" {
		t.Fatalf("wrong missing dependency: %+v", missing)
	}
	if !reflect.DeepEqual(p, Plan{}) {
		t.Fatalf("failed resolution returned a partial plan: %+v", p)
	}
}
