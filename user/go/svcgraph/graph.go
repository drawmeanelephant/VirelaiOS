// Package svcgraph resolves bounded service dependencies without guest APIs.
package svcgraph

import (
	"math/bits"
	"sort"
	"strings"
)

// Graph bounds, distinct from manifest and runtime admission limits:
// https://github.com/drawmeanelephant/VirelaiOS/issues/1986#issuecomment-6008103593
const (
	MaxNodes = 16
	MaxEdges = 64 // All requires + wants references, before optional drops.
)

// Changing MaxNodes beyond the bitset width must not silently truncate edges.
const _ uint16 = 1 << (MaxNodes - 1)

type Node struct {
	Name     string
	Requires []string
	Wants    []string
}

type Plan struct {
	Order         []string
	Prerequisites map[string][]string // Retained edges, sorted by name.
}

type Diagnostic struct {
	From, To string
	Reason   string // "missing-want" or "cyclic-want"
}

type CycleError struct {
	Path []string
}

func (e *CycleError) Error() string {
	return "cycle: " + strings.Join(e.Path, " -> ")
}

type MissingError struct {
	From, To string
}

func (e *MissingError) Error() string {
	return "missing requirement: " + e.From + " -> " + e.To
}

type DuplicateNameError struct {
	Name string
}

func (e *DuplicateNameError) Error() string {
	return "duplicate name: " + e.Name
}

type DuplicateReferenceError struct {
	From, To string
}

func (e *DuplicateReferenceError) Error() string {
	return "duplicate reference: " + e.From + " -> " + e.To
}

type LimitError struct {
	Limit string // "MaxNodes" or "MaxEdges"
}

func (e *LimitError) Error() string {
	return "limit exceeded: " + e.Limit
}

// Resolve validates required edges, then admits wants in lexical (From, To)
// order unless they close a cycle. Kahn's ready set always selects the smallest
// name. Errors return no partial plan.
//
// Each node fits in one bit. Reachability updates scan at most MaxNodes (16)
// words per edge; ready-set selection takes one bit operation, not a heap.
// Work is O(V+E) under these fixed graph bounds, apart from reading name bytes.
// This is not an unbounded-graph algorithm: increasing the caps needs review.
func Resolve(nodes []Node) (Plan, []Diagnostic, error) {
	if len(nodes) > MaxNodes {
		return Plan{}, nil, &LimitError{Limit: "MaxNodes"}
	}
	edges := 0
	for _, node := range nodes {
		for _, refs := range [][]string{node.Requires, node.Wants} {
			if len(refs) > MaxEdges-edges {
				return Plan{}, nil, &LimitError{Limit: "MaxEdges"}
			}
			edges += len(refs)
		}
	}

	ordered := append([]Node(nil), nodes...)
	sort.Slice(ordered, func(i, j int) bool { return ordered[i].Name < ordered[j].Name })
	ids := make(map[string]int, len(ordered))
	for i, node := range ordered {
		if _, exists := ids[node.Name]; exists {
			return Plan{}, nil, &DuplicateNameError{Name: node.Name}
		}
		ids[node.Name] = i
	}

	// Sort private reference copies, never the caller's backing arrays.
	for i := range ordered {
		node := &ordered[i]
		node.Requires = append([]string(nil), node.Requires...)
		node.Wants = append([]string(nil), node.Wants...)
		sort.Strings(node.Requires)
		sort.Strings(node.Wants)
		seen := make(map[string]bool, len(node.Requires)+len(node.Wants))
		duplicate := ""
		hasDuplicate := false
		for _, refs := range [][]string{node.Requires, node.Wants} {
			for _, to := range refs {
				if seen[to] && (!hasDuplicate || to < duplicate) {
					duplicate, hasDuplicate = to, true
				}
				seen[to] = true
			}
		}
		if hasDuplicate {
			return Plan{}, nil, &DuplicateReferenceError{From: node.Name, To: duplicate}
		}
	}

	var prerequisites [MaxNodes]uint16
	for from, node := range ordered {
		for _, to := range node.Requires {
			id, exists := ids[to]
			if !exists {
				return Plan{}, nil, &MissingError{From: node.Name, To: to}
			}
			prerequisites[from] |= bit(id)
		}
	}

	// Transitive closure of the required graph. Self-reachability identifies
	// exactly the cycle members, not acyclic nodes waiting on those members.
	reach := prerequisites
	for via := range ordered {
		for from := range ordered {
			if reach[from]&bit(via) != 0 {
				reach[from] |= reach[via]
			}
		}
	}
	for start := range ordered {
		if reach[start]&bit(start) != 0 {
			return Plan{}, nil, &CycleError{Path: cyclePath(start, ordered, prerequisites, reach)}
		}
	}

	var diagnostics []Diagnostic
	for from, node := range ordered {
		for _, to := range node.Wants {
			id, exists := ids[to]
			if !exists {
				diagnostics = append(diagnostics, Diagnostic{From: node.Name, To: to, Reason: "missing-want"})
				continue
			}
			if id == from || reach[id]&bit(from) != 0 {
				diagnostics = append(diagnostics, Diagnostic{From: node.Name, To: to, Reason: "cyclic-want"})
				continue
			}
			prerequisites[from] |= bit(id)
			// Every predecessor of from now reaches id and all id reaches.
			added := bit(id) | reach[id]
			for ancestor := range ordered {
				if ancestor == from || reach[ancestor]&bit(from) != 0 {
					reach[ancestor] |= added
				}
			}
		}
	}

	p := Plan{Prerequisites: make(map[string][]string, len(ordered))}
	var dependents [MaxNodes]uint16
	var indegree [MaxNodes]int
	var ready uint16
	for from, node := range ordered {
		p.Prerequisites[node.Name] = nil
		indegree[from] = bits.OnesCount16(prerequisites[from])
		if indegree[from] == 0 {
			ready |= bit(from)
		}
		for refs := prerequisites[from]; refs != 0; refs &= refs - 1 {
			to := bits.TrailingZeros16(refs)
			p.Prerequisites[node.Name] = append(p.Prerequisites[node.Name], ordered[to].Name)
			dependents[to] |= bit(from)
		}
	}
	for ready != 0 {
		next := bits.TrailingZeros16(ready)
		ready &^= bit(next)
		p.Order = append(p.Order, ordered[next].Name)
		for waiting := dependents[next]; waiting != 0; waiting &= waiting - 1 {
			from := bits.TrailingZeros16(waiting)
			indegree[from]--
			if indegree[from] == 0 {
				ready |= bit(from)
			}
		}
	}
	return p, diagnostics, nil
}

func bit(id int) uint16 {
	return uint16(1) << id
}

// The first self-reachable name is the smallest member of any required cycle.
// Find a real path back to it, visiting outgoing edges in lexical order.
func cyclePath(start int, nodes []Node, edges, reach [MaxNodes]uint16) []string {
	path := []string{nodes[start].Name}
	var visited uint16
	var visit func(int) bool
	visit = func(from int) bool {
		visited |= bit(from)
		for refs := edges[from]; refs != 0; refs &= refs - 1 {
			to := bits.TrailingZeros16(refs)
			if to == start {
				path = append(path, nodes[start].Name)
				return true
			}
			if visited&bit(to) != 0 || reach[to]&bit(start) == 0 {
				continue
			}
			path = append(path, nodes[to].Name)
			if visit(to) {
				return true
			}
			path = path[:len(path)-1]
		}
		return false
	}
	visit(start)
	return path
}
