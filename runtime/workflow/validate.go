package workflow

import (
	"fmt"
	"regexp"
)

var pascalCaseRe = regexp.MustCompile(`^[A-Z][a-zA-Z0-9]*$`)

// ValidationResult holds errors and warnings from workflow validation.
type ValidationResult struct {
	Errors   []string // Blocking: invalid references, missing fields
	Warnings []string // Non-blocking: PascalCase violations, circular refs
}

// HasErrors returns true if there are blocking validation errors.
func (r *ValidationResult) HasErrors() bool {
	return len(r.Errors) > 0
}

// Validate checks a Spec against the available prompt keys.
// It implements all 10 validation rules from RFC 0005.
func Validate(spec *Spec, promptKeys []string) *ValidationResult {
	r := &ValidationResult{}
	promptSet := make(map[string]bool, len(promptKeys))
	for _, k := range promptKeys {
		promptSet[k] = true
	}

	validateVersion(spec, r)
	if len(spec.States) == 0 {
		r.Errors = append(r.Errors, "workflow.states must be non-empty")
		return r
	}
	validateEntry(spec, promptSet, r)
	validateStates(spec, promptSet, r)
	validateCycles(spec, r)

	return r
}

// validateVersion checks rule 1: version must equal 1.
func validateVersion(spec *Spec, r *ValidationResult) {
	if spec.Version != 1 {
		r.Errors = append(r.Errors, fmt.Sprintf("workflow.version must be 1, got %d", spec.Version))
	}
}

// validateEntry checks rules 3-4: entry must reference a valid state and prompt.
func validateEntry(spec *Spec, promptSet map[string]bool, r *ValidationResult) {
	if _, ok := spec.States[spec.Entry]; !ok {
		r.Errors = append(r.Errors, fmt.Sprintf(
			"workflow.entry %q does not reference a key in states", spec.Entry))
		return
	}
	if !promptSet[spec.States[spec.Entry].PromptTask] {
		r.Errors = append(r.Errors, fmt.Sprintf(
			"workflow.states[%q].prompt_task %q does not reference a valid prompt",
			spec.Entry, spec.States[spec.Entry].PromptTask))
	}
}

// validateStates checks rules 5-9 for each state.
func validateStates(spec *Spec, promptSet map[string]bool, r *ValidationResult) {
	for name, state := range spec.States {
		if name != spec.Entry && !promptSet[state.PromptTask] {
			r.Errors = append(r.Errors, fmt.Sprintf(
				"workflow.states[%q].prompt_task %q does not reference a valid prompt",
				name, state.PromptTask))
		}
		validateEvents(spec, name, state, r)
		validatePersistence(name, state, r)
		validateOrchestration(name, state, r)
	}
}

// validateEvents checks rules 6-7: event targets and PascalCase.
func validateEvents(spec *Spec, name string, state *State, r *ValidationResult) {
	for event, target := range state.OnEvent {
		if _, ok := spec.States[target]; !ok {
			r.Errors = append(r.Errors, fmt.Sprintf(
				"workflow.states[%q].on_event[%q] target %q does not exist in states",
				name, event, target))
		}
		if !pascalCaseRe.MatchString(event) {
			r.Warnings = append(r.Warnings, fmt.Sprintf(
				"workflow.states[%q].on_event[%q]: event name should be PascalCase",
				name, event))
		}
	}
}

// validatePersistence checks rule 8.
func validatePersistence(name string, state *State, r *ValidationResult) {
	if state.Persistence != "" &&
		state.Persistence != PersistenceTransient &&
		state.Persistence != PersistencePersistent {
		r.Errors = append(r.Errors, fmt.Sprintf(
			"workflow.states[%q].persistence %q is not valid (must be \"transient\" or \"persistent\")",
			name, state.Persistence))
	}
}

// validateOrchestration checks rule 9.
func validateOrchestration(name string, state *State, r *ValidationResult) {
	if state.Orchestration != "" &&
		state.Orchestration != OrchestrationInternal &&
		state.Orchestration != OrchestrationExternal &&
		state.Orchestration != OrchestrationHybrid {
		r.Errors = append(r.Errors, fmt.Sprintf(
			"workflow.states[%q].orchestration %q is not valid (must be \"internal\", \"external\", or \"hybrid\")",
			name, state.Orchestration))
	}
}

// validateCycles checks rule 10: DFS cycle detection (warn only).
func validateCycles(spec *Spec, r *ValidationResult) {
	for _, cycle := range detectCycles(spec) {
		r.Warnings = append(r.Warnings, fmt.Sprintf("workflow contains a cycle: %s", cycle))
	}
}

// detectCycles uses DFS to find cycles in the state graph.
func detectCycles(spec *Spec) []string {
	const (
		white = iota // unvisited
		gray         // in current DFS path
		black        // fully explored
	)

	color := make(map[string]int, len(spec.States))
	var cycles []string

	var dfs func(state string)
	dfs = func(state string) {
		color[state] = gray
		s := spec.States[state]
		if s == nil {
			color[state] = black
			return
		}
		for _, target := range s.OnEvent {
			switch color[target] {
			case gray:
				cycles = append(cycles, fmt.Sprintf("%s -> %s", state, target))
			case white:
				dfs(target)
			}
		}
		color[state] = black
	}

	for name := range spec.States {
		if color[name] == white {
			dfs(name)
		}
	}

	return cycles
}
