// Package probes provides instrumented dependency wrappers and operation
// contracts for SDK integration tests.
//
// The package addresses a class of bug that PromptKit's existing test suite
// could not detect: stages whose element flow is correct (1 in → 1 out) but
// whose external side effects (Load, Save, render, emit) multiply with input
// cardinality. Without explicit operation counters, "did X happen" tests pass
// while "did X happen exactly N times" remains unenforced.
//
// Contracts declare per-Send operation budgets ("renderer.RenderDetailed
// must execute exactly once per Send, regardless of history depth").
// Probes capture the actual count. Tests run a fixture matrix that varies
// the dimensions which drive multiplication and assert contracts hold across
// all of them.
package probes

import (
	"fmt"
	"slices"
	"testing"
)

// Op is the name of a counted operation, e.g. "store.Load" or
// "events.template.rendered".
type Op string

// Bound describes the acceptable count range for an operation per Send.
// A Max of -1 means unbounded.
type Bound struct {
	Min int
	Max int
}

// Exactly returns a Bound that requires exactly n occurrences.
func Exactly(n int) Bound { return Bound{Min: n, Max: n} }

// AtMost returns a Bound that requires at most n occurrences.
func AtMost(n int) Bound { return Bound{Min: 0, Max: n} }

// AtLeast returns a Bound that requires at least n occurrences (no upper bound).
func AtLeast(n int) Bound { return Bound{Min: n, Max: -1} }

// Range returns a Bound that requires lo <= count <= hi.
func Range(lo, hi int) Bound { return Bound{Min: lo, Max: hi} }

// String returns a human-readable description of the Bound.
func (b Bound) String() string {
	switch {
	case b.Max == b.Min:
		return fmt.Sprintf("exactly %d", b.Min)
	case b.Max < 0:
		return fmt.Sprintf("at least %d", b.Min)
	case b.Min == 0:
		return fmt.Sprintf("at most %d", b.Max)
	default:
		return fmt.Sprintf("between %d and %d", b.Min, b.Max)
	}
}

// Contains reports whether n satisfies the bound.
func (b Bound) Contains(n int) bool {
	if n < b.Min {
		return false
	}
	if b.Max >= 0 && n > b.Max {
		return false
	}
	return true
}

// Ops maps an operation name to its expected per-Send bound.
type Ops map[Op]Bound

// StageContract declares the per-Send operation budget for a single stage.
// Used by tests as a first-class fixture: assert that a stage performs each
// declared operation within bounds across a matrix of inputs.
type StageContract struct {
	// Stage is a free-form label used in failure messages.
	Stage string
	// PerSend lists operations whose count is expected to be Send-scoped.
	PerSend Ops
}

// reporter is the subset of *testing.T that AssertHolds needs. Defined as a
// local interface (not testing.TB, which has unexported methods) so unit
// tests can verify the violation path with a stub.
type reporter interface {
	Helper()
	Errorf(format string, args ...any)
}

// AssertHolds checks that the snapshot satisfies every clause of the contract.
// Each violation produces a separate t.Errorf so all failures surface at once.
func (c StageContract) AssertHolds(t *testing.T, snap Snapshot) {
	t.Helper()
	c.assert(t, snap)
}

func (c StageContract) assert(t reporter, snap Snapshot) {
	t.Helper()
	for _, op := range sortedOps(c.PerSend) {
		bound := c.PerSend[op]
		got := snap.Count(op)
		if !bound.Contains(got) {
			t.Errorf("[contract %s] %s: got %d, want %s", c.Stage, op, got, bound)
		}
	}
}

// PipelineInvariants declares per-Send budgets that hold across the entire
// pipeline (cross-stage), not for any single stage.
type PipelineInvariants struct {
	// Label is a free-form name used in failure messages.
	Label string
	// PerSend lists pipeline-wide operations whose count is Send-scoped.
	PerSend Ops
}

// AssertHolds checks that the snapshot satisfies every clause.
func (p PipelineInvariants) AssertHolds(t *testing.T, snap Snapshot) {
	t.Helper()
	p.assert(t, snap)
}

func (p PipelineInvariants) assert(t reporter, snap Snapshot) {
	t.Helper()
	label := p.Label
	if label == "" {
		label = "pipeline"
	}
	for _, op := range sortedOps(p.PerSend) {
		bound := p.PerSend[op]
		got := snap.Count(op)
		if !bound.Contains(got) {
			t.Errorf("[invariant %s] %s: got %d, want %s", label, op, got, bound)
		}
	}
}

func sortedOps(ops Ops) []Op {
	keys := make([]Op, 0, len(ops))
	for k := range ops {
		keys = append(keys, k)
	}
	slices.Sort(keys)
	return keys
}
