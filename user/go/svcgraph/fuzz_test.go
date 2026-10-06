package svcgraph

import (
	"fmt"
	"reflect"
	"slices"
	"sort"
	"testing"
)

func FuzzResolve(f *testing.F) {
	for _, seed := range [][]byte{
		{}, {0}, {1, 0}, {17, 0},
		{1, 0, 0, 0, 0},                            // Required self-loop.
		{1, 0, 0, 1, 0},                            // Missing requirement.
		{1, 0, 0, 1, 1},                            // Missing want.
		{2, 0, 0, 1, 1, 1, 0, 1},                   // Mutual wants.
		{3, 0, 0, 1, 0, 1, 2, 0, 2, 0, 0},          // Required cycle.
		{4, 0, 1, 0, 0, 2, 0, 0, 3, 1, 0, 3, 2, 0}, // Diamond.
		{2, 1},          // Duplicate names.
		{2, 2, 0, 1, 2}, // Duplicate across requires/wants.
	} {
		f.Add(seed)
	}
	// Exercise the edge cap, including duplicate and missing references.
	atCap := []byte{1, 0}
	for i := 0; i < MaxEdges; i++ {
		atCap = append(atCap, 0, 1, 1)
	}
	f.Add(atCap)
	f.Add(append(slices.Clone(atCap), 0, 2, 1))
	f.Fuzz(func(t *testing.T, data []byte) {
		nodes := decodeGraph(data)
		before := cloneNodes(nodes)
		p, diagnostics, err := Resolve(nodes)
		if !reflect.DeepEqual(nodes, before) {
			t.Fatal("input changed")
		}

		// Node and reference permutations must preserve every observable result.
		permuted := cloneNodes(nodes)
		slices.Reverse(permuted)
		for i := range permuted {
			slices.Reverse(permuted[i].Requires)
			slices.Reverse(permuted[i].Wants)
		}
		p2, d2, e2 := Resolve(permuted)
		if !reflect.DeepEqual(p, p2) || !reflect.DeepEqual(diagnostics, d2) ||
			!reflect.DeepEqual(err, e2) {
			t.Fatalf("permutation changed result: %+v %v %v versus %+v %v %v", p, diagnostics, err, p2, d2, e2)
		}
		if err != nil {
			if !reflect.DeepEqual(p, Plan{}) || len(diagnostics) != 0 {
				t.Fatal("partial results on error")
			}
			checkRefusal(t, nodes, err)
			return
		}
		checkPlan(t, nodes, p, diagnostics)
	})
}

// Byte 0 chooses 0..17 nodes; byte 1 enables duplicate names/references.
// Subsequent triples encode (from, to, required/wanted). Two extra target
// names exercise missing edges. Decode at most 65 triples to bound the oracle.
func decodeGraph(data []byte) []Node {
	if len(data) == 0 {
		return nil
	}
	nodes := make([]Node, int(data[0])%(MaxNodes+2))
	flags := byte(0)
	if len(data) > 1 {
		flags = data[1]
	}
	for i := range nodes {
		nodes[i].Name = fmt.Sprintf("n%02d", i)
	}
	if len(nodes) > 1 && flags&1 != 0 {
		nodes[len(nodes)-1].Name = nodes[0].Name
	}
	for i, count := 2, 0; i+2 < len(data) && count <= MaxEdges && len(nodes) > 0; i, count = i+3, count+1 {
		from := int(data[i]) % len(nodes)
		to := fmt.Sprintf("n%02d", int(data[i+1])%(len(nodes)+2))
		if data[i+2]&1 == 0 {
			nodes[from].Requires = append(nodes[from].Requires, to)
		} else {
			nodes[from].Wants = append(nodes[from].Wants, to)
		}
		if flags&2 != 0 && data[i+2]&2 != 0 {
			// Repeat the same reference, possibly across dependency kinds.
			nodes[from].Wants = append(nodes[from].Wants, to)
		}
	}
	return nodes
}

func checkRefusal(t *testing.T, nodes []Node, err error) {
	t.Helper()
	byName := make(map[string]Node)
	edges := 0
	for _, node := range nodes {
		byName[node.Name] = node
		edges += len(node.Requires) + len(node.Wants)
	}
	switch e := err.(type) {
	case *LimitError:
		if (e.Limit == "MaxNodes" && len(nodes) > MaxNodes) || (e.Limit == "MaxEdges" && edges > MaxEdges) {
			return
		}
	case *DuplicateNameError:
		count := 0
		for _, node := range nodes {
			if node.Name == e.Name {
				count++
			}
		}
		if count > 1 {
			return
		}
	case *DuplicateReferenceError:
		node, exists := byName[e.From]
		count := 0
		for _, refs := range [][]string{node.Requires, node.Wants} {
			for _, to := range refs {
				if to == e.To {
					count++
				}
			}
		}
		if exists && count > 1 {
			return
		}
	case *MissingError:
		node, exists := byName[e.From]
		_, targetExists := byName[e.To]
		if exists && !targetExists && slices.Contains(node.Requires, e.To) {
			return
		}
	case *CycleError:
		if len(e.Path) < 2 || len(e.Path) > len(nodes)+1 || e.Path[0] != e.Path[len(e.Path)-1] {
			t.Fatalf("invalid closed cycle: %v", e.Path)
		}
		seen := make(map[string]bool)
		required := make(map[string][]string)
		for _, node := range nodes {
			required[node.Name] = node.Requires
		}
		for i, from := range e.Path[:len(e.Path)-1] {
			if seen[from] || from < e.Path[0] || !slices.Contains(required[from], e.Path[i+1]) {
				t.Fatalf("cycle is not a canonical real required cycle: %v", e.Path)
			}
			seen[from] = true
		}
		for _, node := range nodes {
			if node.Name >= e.Path[0] {
				continue
			}
			for _, to := range node.Requires {
				if reaches(required, to, node.Name) {
					t.Fatalf("reported %v before lexically earlier cycle member %q", e.Path, node.Name)
				}
			}
		}
		return
	}
	t.Fatalf("refusal has no input witness: %+v on %+v", err, nodes)
}

// Independent oracle: maps and DFS, not the production bitset closure.
func checkPlan(t *testing.T, nodes []Node, p Plan, diagnostics []Diagnostic) {
	t.Helper()
	if len(nodes) > MaxNodes {
		t.Fatal("node cap accepted")
	}
	ordered := cloneNodes(nodes)
	sort.Slice(ordered, func(i, j int) bool { return ordered[i].Name < ordered[j].Name })
	graph := make(map[string][]string)
	byName := make(map[string]Node)
	edgeCount := 0
	for _, node := range ordered {
		if _, exists := byName[node.Name]; exists {
			t.Fatal("duplicate name accepted")
		}
		byName[node.Name] = node
		graph[node.Name] = slices.Clone(node.Requires)
		seen := make(map[string]bool)
		for _, refs := range [][]string{node.Requires, node.Wants} {
			for _, to := range refs {
				if seen[to] {
					t.Fatal("duplicate reference accepted")
				}
				seen[to] = true
				edgeCount++
			}
		}
	}
	if edgeCount > MaxEdges {
		t.Fatal("edge cap accepted")
	}
	for _, node := range ordered {
		for _, to := range node.Requires {
			if _, exists := byName[to]; !exists {
				t.Fatal("missing requirement accepted")
			}
			if reaches(graph, to, node.Name) {
				t.Fatal("required cycle accepted")
			}
		}
	}
	var wantDiagnostics []Diagnostic
	for _, node := range ordered {
		wants := slices.Clone(node.Wants)
		sort.Strings(wants)
		for _, to := range wants {
			reason := ""
			if _, exists := byName[to]; !exists {
				reason = "missing-want"
			} else if reaches(graph, to, node.Name) {
				reason = "cyclic-want"
			}
			if reason != "" {
				wantDiagnostics = append(wantDiagnostics, Diagnostic{node.Name, to, reason})
			} else {
				graph[node.Name] = append(graph[node.Name], to)
			}
		}
		sort.Strings(graph[node.Name])
	}
	if !reflect.DeepEqual(p.Prerequisites, graph) || !reflect.DeepEqual(diagnostics, wantDiagnostics) {
		t.Fatalf("retained edges/diagnostics differ: %+v %v; want %+v %v", p, diagnostics, graph, wantDiagnostics)
	}

	done := make(map[string]bool)
	var wantOrder []string
	for len(done) < len(ordered) {
		found := false
		for _, node := range ordered {
			if done[node.Name] {
				continue
			}
			ready := true
			for _, to := range graph[node.Name] {
				ready = ready && done[to]
			}
			if ready {
				done[node.Name] = true
				wantOrder = append(wantOrder, node.Name)
				found = true
				break
			}
		}
		if !found {
			t.Fatal("retained graph is cyclic")
		}
	}
	if !reflect.DeepEqual(p.Order, wantOrder) {
		t.Fatalf("not the lexical topological order: %v, want %v", p.Order, wantOrder)
	}
}

func reaches(graph map[string][]string, from, target string) bool {
	seen := make(map[string]bool)
	var visit func(string) bool
	visit = func(name string) bool {
		if name == target {
			return true
		}
		if seen[name] {
			return false
		}
		seen[name] = true
		for _, to := range graph[name] {
			if visit(to) {
				return true
			}
		}
		return false
	}
	return visit(from)
}
