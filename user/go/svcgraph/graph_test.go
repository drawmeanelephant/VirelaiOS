package svcgraph

import (
	"errors"
	"fmt"
	"reflect"
	"slices"
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
		{
			"backtrack-past-a-nested-cycle",
			[]Node{
				{Name: "a", Requires: []string{"b"}}, {Name: "b", Requires: []string{"d", "c"}},
				{Name: "c", Requires: []string{"b"}}, {Name: "d", Requires: []string{"a"}},
			},
			[]string{"a", "b", "d", "a"},
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

func TestResolvePlan(t *testing.T) {
	tests := []struct {
		name        string
		nodes       []Node
		order       []string
		requires    map[string][]string
		diagnostics []Diagnostic
	}{
		{"empty", nil, nil, map[string][]string{}, nil},
		{"chain",
			[]Node{{Name: "c", Requires: []string{"b"}}, {Name: "b", Requires: []string{"a"}}, {Name: "a"}},
			[]string{"a", "b", "c"}, map[string][]string{"a": nil, "b": {"a"}, "c": {"b"}}, nil},
		{"diamond",
			[]Node{{Name: "d", Requires: []string{"c", "b"}}, {Name: "c", Requires: []string{"a"}}, {Name: "b", Requires: []string{"a"}}, {Name: "a"}},
			[]string{"a", "b", "c", "d"}, map[string][]string{"a": nil, "b": {"a"}, "c": {"a"}, "d": {"b", "c"}}, nil},
		{"independent",
			[]Node{{Name: "z"}, {Name: "b"}, {Name: "a"}},
			[]string{"a", "b", "z"}, map[string][]string{"a": nil, "b": nil, "z": nil}, nil},
		{"newly-ready-name-wins",
			[]Node{{Name: "z"}, {Name: "b", Requires: []string{"a"}}, {Name: "a"}},
			[]string{"a", "b", "z"}, map[string][]string{"a": nil, "b": {"a"}, "z": nil}, nil},
		{"missing-want",
			[]Node{{Name: "seat", Wants: []string{"optional"}}},
			[]string{"seat"}, map[string][]string{"seat": nil}, []Diagnostic{{"seat", "optional", "missing-want"}}},
		{"acyclic-want-changes-order",
			[]Node{{Name: "a", Wants: []string{"b"}}, {Name: "b"}},
			[]string{"b", "a"}, map[string][]string{"a": {"b"}, "b": nil}, nil},
		{"wanted-self-loop",
			[]Node{{Name: "a", Wants: []string{"a"}}},
			[]string{"a"}, map[string][]string{"a": nil}, []Diagnostic{{"a", "a", "cyclic-want"}}},
		{"mutual-wants-lexical-winner",
			[]Node{{Name: "b", Wants: []string{"a"}}, {Name: "a", Wants: []string{"b"}}},
			[]string{"b", "a"}, map[string][]string{"a": {"b"}, "b": nil}, []Diagnostic{{"b", "a", "cyclic-want"}}},
		{"three-wanted-edges",
			[]Node{{Name: "c", Wants: []string{"a"}}, {Name: "a", Wants: []string{"b"}}, {Name: "b", Wants: []string{"c"}}},
			[]string{"c", "b", "a"}, map[string][]string{"a": {"b"}, "b": {"c"}, "c": nil}, []Diagnostic{{"c", "a", "cyclic-want"}}},
		{"want-admission-sorts-from-before-to",
			[]Node{{Name: "z", Wants: []string{"b"}}, {Name: "a", Wants: []string{"z"}}, {Name: "b", Wants: []string{"a"}}},
			[]string{"z", "a", "b"}, map[string][]string{"a": {"z"}, "b": {"a"}, "z": nil}, []Diagnostic{{"z", "b", "cyclic-want"}}},
		{"required-edges-win",
			[]Node{{Name: "b", Requires: []string{"a"}}, {Name: "a", Wants: []string{"b"}}},
			[]string{"a", "b"}, map[string][]string{"a": nil, "b": {"a"}}, []Diagnostic{{"a", "b", "cyclic-want"}}},
		{"transitive-required-cycle-closer",
			[]Node{{Name: "a", Requires: []string{"b"}}, {Name: "b", Requires: []string{"c"}}, {Name: "c", Wants: []string{"a"}}},
			[]string{"c", "b", "a"}, map[string][]string{"a": {"b"}, "b": {"c"}, "c": nil}, []Diagnostic{{"c", "a", "cyclic-want"}}},
		{"redundant-transitive-want-retained",
			[]Node{{Name: "a", Requires: []string{"b"}, Wants: []string{"c"}}, {Name: "b", Requires: []string{"c"}}, {Name: "c"}},
			[]string{"c", "b", "a"}, map[string][]string{"a": {"b", "c"}, "b": {"c"}, "c": nil}, nil},
		{"diagnostics-in-edge-order",
			[]Node{{Name: "b", Wants: []string{"z", "a"}}, {Name: "a", Wants: []string{"z", "b"}}},
			[]string{"b", "a"}, map[string][]string{"a": {"b"}, "b": nil},
			[]Diagnostic{{"a", "z", "missing-want"}, {"b", "a", "cyclic-want"}, {"b", "z", "missing-want"}}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			want := Plan{Order: tt.order, Prerequisites: tt.requires}
			// Exercise every node permutation and reverse each dependency list.
			permutations(tt.nodes, func(nodes []Node) {
				for _, reverse := range []bool{false, true} {
					input := cloneNodes(nodes)
					if reverse {
						for i := range input {
							slices.Reverse(input[i].Requires)
							slices.Reverse(input[i].Wants)
						}
					}
					before := cloneNodes(input)
					p, diagnostics, err := Resolve(input)
					if err != nil || !reflect.DeepEqual(p, want) || !reflect.DeepEqual(diagnostics, tt.diagnostics) {
						t.Fatalf("input=%+v: plan=%+v diagnostics=%v err=%v; want %+v %v",
							input, p, diagnostics, err, want, tt.diagnostics)
					}
					if !reflect.DeepEqual(input, before) {
						t.Fatalf("Resolve mutated the input: before=%+v after=%+v", before, input)
					}
				}
			})
		})
	}
}

func TestDuplicateNames(t *testing.T) {
	_, _, err := Resolve([]Node{{Name: "seat"}, {Name: "seat"}})
	var duplicate *DuplicateNameError
	if !errors.As(err, &duplicate) || duplicate.Name != "seat" || err.Error() != "duplicate name: seat" {
		t.Fatalf("got %v, want duplicate name: seat", err)
	}
}

func TestDuplicateReferences(t *testing.T) {
	tests := []Node{
		{Name: "seat", Requires: []string{"missing", "missing"}},
		{Name: "seat", Wants: []string{"missing", "missing"}},
		{Name: "seat", Requires: []string{"missing"}, Wants: []string{"missing"}},
	}
	for _, node := range tests {
		_, _, err := Resolve([]Node{node})
		var duplicate *DuplicateReferenceError
		if !errors.As(err, &duplicate) || duplicate.From != "seat" || duplicate.To != "missing" ||
			err.Error() != "duplicate reference: seat -> missing" {
			t.Fatalf("%+v: got %v, want duplicate reference", node, err)
		}
	}
}

func TestNamedCaps(t *testing.T) {
	if MaxNodes != 16 || MaxEdges != 64 {
		t.Fatalf("M92a correction requires caps 16/64, got %d/%d", MaxNodes, MaxEdges)
	}
	nodes := make([]Node, MaxNodes)
	for i := range nodes {
		nodes[i].Name = fmt.Sprintf("n%02d", i)
	}
	p, _, err := Resolve(nodes)
	if err != nil || len(p.Order) != MaxNodes {
		t.Fatalf("node cap refused: %+v %v", p, err)
	}
	expectLimit(t, append(cloneNodes(nodes), Node{Name: "extra"}), "MaxNodes")

	// Eight dependents, each with eight distinct required roots: 64 edges.
	for i := 8; i < MaxNodes; i++ {
		for j := 0; j < 8; j++ {
			nodes[i].Requires = append(nodes[i].Requires, nodes[j].Name)
		}
	}
	p, _, err = Resolve(nodes)
	if err != nil || len(p.Prerequisites["n15"]) != 8 {
		t.Fatalf("edge cap refused: %+v %v", p, err)
	}
	over := cloneNodes(nodes)
	over[15].Wants = []string{"n08"}
	expectLimit(t, over, "MaxEdges")

	// Count missing and cycle-closing wants before dropping any of them.
	wants := []Node{{Name: "seat"}}
	for i := 0; i < MaxEdges; i++ {
		wants[0].Wants = append(wants[0].Wants, fmt.Sprintf("missing%02d", i))
	}
	_, diagnostics, err := Resolve(wants)
	if err != nil || len(diagnostics) != MaxEdges {
		t.Fatalf("missing wants at cap: diagnostics=%d err=%v", len(diagnostics), err)
	}
	wants[0].Wants = append(wants[0].Wants, "another")
	expectLimit(t, wants, "MaxEdges")

	// Duplicate references also count before duplicate validation.
	over = cloneNodes(nodes)
	over[15].Wants = []string{"n00"}
	expectLimit(t, over, "MaxEdges")
	duplicateAtCap := []Node{{Name: "seat", Wants: make([]string, MaxEdges)}}
	_, _, err = Resolve(duplicateAtCap)
	var duplicate *DuplicateReferenceError
	if !errors.As(err, &duplicate) {
		t.Fatalf("duplicate references at cap: got %v", err)
	}

	cyclicWants := cloneNodes(nodes[:9])
	for i := range cyclicWants {
		cyclicWants[i].Requires = nil
		for j := 0; j < 8; j++ {
			if i != j {
				cyclicWants[i].Wants = append(cyclicWants[i].Wants, cyclicWants[j].Name)
			}
		}
	}
	_, diagnostics, err = Resolve(cyclicWants)
	if err != nil || len(diagnostics) == 0 {
		t.Fatalf("cycle-closing wants at cap: diagnostics=%v err=%v", diagnostics, err)
	}
	cyclicWants[0].Wants = append(cyclicWants[0].Wants, "n08")
	expectLimit(t, cyclicWants, "MaxEdges")
}

func TestErrorsArePermutationInvariant(t *testing.T) {
	tests := [][]Node{
		{{Name: "z", Requires: []string{"missing"}}, {Name: "a", Requires: []string{"z", "b"}}},
		{{Name: "a", Requires: []string{"c", "b"}}, {Name: "b", Requires: []string{"a"}}, {Name: "c", Requires: []string{"a"}}},
		{{Name: "z"}, {Name: "a"}, {Name: "z"}, {Name: "a"}},
		{{Name: "z", Wants: []string{"z", "z"}}, {Name: "a", Requires: []string{"c", "c", "b", "b"}}},
	}
	for _, nodes := range tests {
		_, _, original := Resolve(nodes)
		if original == nil {
			t.Fatal("fixture did not refuse")
		}
		permutations(nodes, func(input []Node) {
			input = cloneNodes(input)
			for i := range input {
				slices.Reverse(input[i].Requires)
				slices.Reverse(input[i].Wants)
			}
			_, _, err := Resolve(input)
			if err == nil || err.Error() != original.Error() {
				t.Fatalf("%+v: got %v, want %v", input, err, original)
			}
		})
	}
}

func TestPlanDoesNotAliasInputs(t *testing.T) {
	nodes := []Node{{Name: "a", Requires: []string{"b"}}, {Name: "b"}}
	p, _, err := Resolve(nodes)
	if err != nil {
		t.Fatal(err)
	}
	p.Prerequisites["a"][0] = "changed"
	p.Order[0] = "changed"
	if nodes[0].Requires[0] != "b" || nodes[1].Name != "b" {
		t.Fatalf("plan aliases input: %+v", nodes)
	}
}

func expectLimit(t *testing.T, nodes []Node, name string) {
	t.Helper()
	p, diagnostics, err := Resolve(nodes)
	var limit *LimitError
	if !errors.As(err, &limit) || limit.Limit != name || err.Error() != "limit exceeded: "+name {
		t.Fatalf("got %v, want %s refusal", err, name)
	}
	if !reflect.DeepEqual(p, Plan{}) || len(diagnostics) != 0 {
		t.Fatalf("cap refusal returned partial results: %+v %v", p, diagnostics)
	}
}

func cloneNodes(nodes []Node) []Node {
	out := slices.Clone(nodes)
	for i := range out {
		out[i].Requires = slices.Clone(nodes[i].Requires)
		out[i].Wants = slices.Clone(nodes[i].Wants)
	}
	return out
}

func permutations(nodes []Node, visit func([]Node)) {
	work := slices.Clone(nodes)
	var permute func(int)
	permute = func(i int) {
		if i == len(work) {
			visit(work)
			return
		}
		for j := i; j < len(work); j++ {
			work[i], work[j] = work[j], work[i]
			permute(i + 1)
			work[i], work[j] = work[j], work[i]
		}
	}
	permute(0)
}
