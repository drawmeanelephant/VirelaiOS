// Package svcgraph resolves bounded service dependencies without guest APIs.
package svcgraph

import "strings"

// Graph bounds, distinct from manifest and runtime admission limits:
// https://github.com/drawmeanelephant/VirelaiOS/issues/1986#issuecomment-6008103593
const (
	MaxNodes = 16
	MaxEdges = 64 // All requires + wants references, before optional drops.
)

type Node struct {
	Name     string
	Requires []string
	Wants    []string
}

type Plan struct {
	Order         []string
	Prerequisites map[string][]string
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

// Resolve is an input-order stub for the fail-before tests.
func Resolve(nodes []Node) (Plan, []Diagnostic, error) {
	p := Plan{Prerequisites: make(map[string][]string)}
	for _, node := range nodes {
		p.Order = append(p.Order, node.Name)
	}
	return p, nil, nil
}
