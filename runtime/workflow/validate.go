package workflow

import (
	"fmt"
	"regexp"
	"sort"

	"github.com/AltairaLabs/PromptKit/runtime/packspec"
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
	validateAgentLoops(spec, r)

	return r
}

// validateVersion checks rule 1: version must be 1 or 2.
func validateVersion(spec *Spec, r *ValidationResult) {
	if spec.Version != 1 && spec.Version != 2 {
		r.Errors = append(r.Errors, fmt.Sprintf("workflow.version must be 1 or 2, got %d", spec.Version))
	}
}

// validateEntry checks rules 3-4: entry must reference a valid state and prompt.
func validateEntry(spec *Spec, promptSet map[string]bool, r *ValidationResult) {
	if _, ok := spec.States[spec.Entry]; !ok {
		r.Errors = append(r.Errors, fmt.Sprintf(
			"workflow.entry %q does not reference a key in states", spec.Entry))
		return
	}
	state := spec.States[spec.Entry]
	if OrchestrationOf(state) != OrchestrationComposition && !promptSet[state.PromptTask] {
		r.Errors = append(r.Errors, fmt.Sprintf(
			"workflow.states[%q].prompt_task %q does not reference a valid prompt",
			spec.Entry, state.PromptTask))
	}
}

// validateStates checks rules 5-9 for each state.
func validateStates(spec *Spec, promptSet map[string]bool, r *ValidationResult) {
	for name, state := range spec.States {
		if name != spec.Entry && OrchestrationOf(state) != OrchestrationComposition && !promptSet[state.PromptTask] {
			r.Errors = append(r.Errors, fmt.Sprintf(
				"workflow.states[%q].prompt_task %q does not reference a valid prompt",
				name, state.PromptTask))
		}
		validateEvents(spec, name, state, r)
		validatePersistence(name, state, r)
		validateOrchestration(name, state, r)
		validateControl(name, state, r)
		validateCompositionFields(name, state, r)
		validateLoopGuards(spec, name, state, r)
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
	// No "" case: OrchestrationOf resolves an undeclared state to internal.
	if OrchestrationOf(state) != OrchestrationInternal &&
		OrchestrationOf(state) != OrchestrationExternal &&
		OrchestrationOf(state) != OrchestrationHybrid &&
		OrchestrationOf(state) != OrchestrationComposition {
		r.Errors = append(r.Errors, fmt.Sprintf(
			"workflow.states[%q].orchestration %q is not valid"+
				" (must be \"internal\", \"external\", \"hybrid\", or \"composition\")",
			name, OrchestrationOf(state)))
	}
}

// validateControl enforces RFC 0014's `control` rules.
//
// An unrecognized value is an ERROR rather than something resolved to a
// default. A gate the runtime cannot honor should say so instead of silently
// doing something else — the same failure #1931 fixed for eval `when`, where an
// unimplemented key decoded to no conditions and the gate quietly disappeared.
//
// The terminal check is RFC rule 3: terminal wins regardless, so declaring
// control: agent beside it cannot do anything and almost certainly means the
// author expected another round.
func validateControl(name string, state *State, r *ValidationResult) {
	if state.Control != nil && *state.Control != "" &&
		*state.Control != ControlUser && *state.Control != ControlAgent {
		r.Errors = append(r.Errors, fmt.Sprintf(
			"workflow.states[%q].control %q is not valid (must be %q or %q)",
			name, *state.Control, ControlUser, ControlAgent))
		return
	}

	if packspec.Deref(state.Control, "") == ControlAgent && IsTerminal(state) {
		r.Warnings = append(r.Warnings, fmt.Sprintf(
			"workflow.states[%q]: control agent on a terminal state has no effect "+
				"(the workflow ends there regardless)", name))
	}
}

// validateAgentLoops implements RFC 0014 rule 4: warn when a cycle of
// agent-controlled states can never hand the turn back.
//
// Not an error — the workflow budget and max rounds still bound it at runtime —
// but an author who writes one has built a loop that talks to itself until a
// budget stops it, which is worth knowing before it runs.
//
// Only states that DECLARE control: agent start a check. HoldsFloor also
// returns true for an absent control — that is this runtime's pre-RFC
// behavior — so keying off it would fire this warning on every cycle in every
// pack written before the field existed, which is noise about something they
// did not do. Absent still counts as not-yielding when deciding whether the
// loop is bounded: such a state runs on just the same.
//
// A state is reported when it can reach itself and nothing in everything it can
// reach yields the floor, ends the workflow, or caps visits.
func validateAgentLoops(spec *Spec, r *ValidationResult) {
	// A workflow-level budget already bounds every loop in the pack, so there
	// is nothing to warn about: checkBudgetLocked runs on each transition and
	// max_total_visits / max_wall_time_sec stop it. Only max_tool_calls is left
	// out — its per-round counting is the subject of #1785, so a pack relying
	// on it alone is not demonstrably bounded.
	if budgetBoundsWorkflow(spec) {
		return
	}

	var offenders []string
	for name, state := range spec.States {
		if !declaresAgentControl(state) {
			continue
		}
		reachable := reachableFrom(spec, name)
		if !reachable[name] {
			continue // not in a cycle: the turn ends by running out of graph
		}
		bounded := false
		for target := range reachable {
			t := spec.States[target]
			if !HoldsFloor(t) || IsTerminal(t) || MaxVisitsOf(t) > 0 {
				bounded = true
				break
			}
		}
		if !bounded {
			offenders = append(offenders, name)
		}
	}
	if len(offenders) == 0 {
		return
	}
	sort.Strings(offenders)
	r.Warnings = append(r.Warnings, fmt.Sprintf(
		"workflow states %v form an agent-controlled cycle with no state that yields to the user, "+
			"no terminal state and no max_visits: it will run until the tool loop's round cap stops it "+
			"(declare max_visits on a state, or engine.budget.max_total_visits, to bound it deliberately)",
		offenders))
}

// budgetBoundsWorkflow reports whether the pack declares a workflow-level
// budget that is actually enforced on every transition, which bounds any loop
// without needing per-state guards.
func budgetBoundsWorkflow(spec *Spec) bool {
	if spec.Engine == nil || spec.Engine.Budget == nil {
		return false
	}
	b := spec.Engine.Budget
	return packspec.Deref(b.MaxTotalVisits, 0) > 0 || packspec.Deref(b.MaxWallTimeSec, 0) > 0
}

// declaresAgentControl reports whether a state explicitly asks to keep the
// turn, as opposed to keeping it by this runtime's default for an absent
// control. See HoldsFloor for why those two are not the same fact.
func declaresAgentControl(s *State) bool {
	return s != nil && s.Control != nil && *s.Control == ControlAgent
}

// reachableFrom returns every state reachable from start by following
// on_event transitions. start itself is included only when a cycle leads back
// to it.
func reachableFrom(spec *Spec, start string) map[string]bool {
	seen := make(map[string]bool, len(spec.States))
	var walk func(string)
	walk = func(name string) {
		s := spec.States[name]
		if s == nil {
			return
		}
		for _, target := range s.OnEvent {
			if seen[target] {
				continue
			}
			seen[target] = true
			walk(target)
		}
	}
	walk(start)
	return seen
}

// validateCompositionFields enforces RFC 0010 composition field rules:
// - composition states must set Composition
// - non-composition states must not set Composition
func validateCompositionFields(name string, state *State, r *ValidationResult) {
	if OrchestrationOf(state) == OrchestrationComposition {
		if state.Composition == "" {
			r.Errors = append(r.Errors, fmt.Sprintf(
				"workflow.states[%q]: orchestration composition requires a composition",
				name))
		}
	} else {
		if state.Composition != "" {
			r.Errors = append(r.Errors, fmt.Sprintf(
				"workflow.states[%q]: composition is only valid with orchestration composition",
				name))
		}
	}
}

// validateLoopGuards checks RFC 0009 loop guard fields.
func validateLoopGuards(spec *Spec, name string, state *State, r *ValidationResult) {
	// Terminal state with transitions is contradictory
	if packspec.Deref(state.Terminal, false) && len(state.OnEvent) > 0 {
		r.Warnings = append(r.Warnings, fmt.Sprintf(
			"workflow.states[%q]: terminal state has on_event transitions (they will never fire)",
			name))
	}

	// on_max_visits validation
	if state.OnMaxVisits != "" {
		target := spec.States[state.OnMaxVisits]
		if target == nil {
			r.Errors = append(r.Errors, fmt.Sprintf(
				"workflow.states[%q].on_max_visits %q does not exist in states",
				name, state.OnMaxVisits))
		} else if MaxVisitsOf(target) > 0 {
			r.Warnings = append(r.Warnings, fmt.Sprintf(
				"workflow.states[%q].on_max_visits target %q also has max_visits — potential redirect chain",
				name, state.OnMaxVisits))
		}
	}

	// Reachability: v2 opts into explicit terminal semantics. A state with no
	// transitions and no loop guard is a dead-end that should be marked terminal.
	if spec.Version >= 2 && !packspec.Deref(state.Terminal, false) &&
		len(state.OnEvent) == 0 && MaxVisitsOf(state) == 0 {
		r.Warnings = append(r.Warnings, fmt.Sprintf(
			"workflow.states[%q]: non-terminal state has no on_event and no max_visits (mark terminal: true to silence)",
			name))
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
