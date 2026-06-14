package composition

import (
	"fmt"
	"regexp"
	"sort"
)

var stepIDRe = regexp.MustCompile(`^[a-zA-Z_][a-zA-Z0-9_]*$`)

// fieldInput / fieldOutputSchema / fieldPromptTask are kind-field name constants
// used in the allowed-fields map and presentKindFields to avoid goconst warnings.
const (
	fieldInput        = "input"
	fieldOutputSchema = "output_schema"
	fieldPromptTask   = "prompt_task"
)

// inputRefRoot is the special root token that refers to the composition's input.
const inputRefRoot = "input"

// minParallelBranches is the minimum number of branches a parallel step must have.
const minParallelBranches = 2

// reservedCompositionNames must not be used as composition keys.
var reservedCompositionNames = map[string]bool{
	"workflow__transition":   true,
	"workflow__set_artifact": true,
}

// Available holds the pack key sets a composition's references resolve against.
type Available struct {
	Prompts []string
	Tools   []string
	Evals   []string
}

func (a Available) set(which []string) map[string]bool {
	m := make(map[string]bool, len(which))
	for _, k := range which {
		m[k] = true
	}
	return m
}

// ValidationResult holds blocking errors and non-blocking warnings.
type ValidationResult struct {
	Errors   []string
	Warnings []string
}

// HasErrors reports whether there are blocking validation errors.
func (r *ValidationResult) HasErrors() bool { return len(r.Errors) > 0 }

func (r *ValidationResult) merge(other *ValidationResult) {
	r.Errors = append(r.Errors, other.Errors...)
	r.Warnings = append(r.Warnings, other.Warnings...)
}

// ValidateAll validates every composition in a pack map and checks pack-wide
// rules (rule 1: key uniqueness is inherent to the map; reserved-name collision).
func ValidateAll(comps map[string]*Composition, avail Available) *ValidationResult {
	r := &ValidationResult{}
	names := make([]string, 0, len(comps))
	for name := range comps {
		names = append(names, name)
	}
	sort.Strings(names) // deterministic error order
	for _, name := range names {
		if reservedCompositionNames[name] {
			r.Errors = append(r.Errors,
				fmt.Sprintf("compositions[%q]: name collides with a reserved runtime name", name))
		}
		r.merge(Validate(name, comps[name], avail))
	}
	return r
}

// Validate checks a single composition against the available pack key sets.
// It implements composition-internal rules 2–11 from the design (rule 1 and the
// workflow/pack-level rules live in ValidateAll and Plan 3 respectively).
func Validate(name string, c *Composition, avail Available) *ValidationResult {
	r := &ValidationResult{}
	if c == nil {
		return r
	}
	stepIDs := validateStepIDs(name, c, r)
	prompts, tools, evals := avail.set(avail.Prompts), avail.set(avail.Tools), avail.set(avail.Evals)
	forEachStep(c.Steps, func(s *Step) {
		validateReferences(name, s, prompts, tools, evals, r)
		validateRefRoots(name, s, stepIDs, r)
		validateKind(name, s, r)
	})
	return r
}

// validateReferences checks rule 3.
func validateReferences(name string, s *Step, prompts, tools, evals map[string]bool, r *ValidationResult) {
	pfx := fmt.Sprintf("compositions[%q].steps[%q]", name, s.ID)
	switch s.Kind {
	case KindPrompt, KindAgent:
		if s.PromptTask != "" && !prompts[s.PromptTask] {
			r.Errors = append(r.Errors, fmt.Sprintf("%s: prompt_task %q does not reference a valid prompt", pfx, s.PromptTask))
		}
	case KindTool:
		if s.Tool != "" && !tools[s.Tool] {
			r.Errors = append(r.Errors, fmt.Sprintf("%s: tool %q does not reference a valid tool", pfx, s.Tool))
		}
	case KindBranch, KindParallel:
		// no key-set references for branch/parallel — tools and prompts are on child steps
	}
	validateAgentTools(pfx, s, tools, r)
	validateModifierEvals(pfx, s, evals, r)
}

// validateAgentTools checks agent-step tool references (rule 3 sub-check).
func validateAgentTools(pfx string, s *Step, tools map[string]bool, r *ValidationResult) {
	for _, tname := range s.Tools {
		if !tools[tname] {
			r.Errors = append(r.Errors, fmt.Sprintf("%s: tool %q does not reference a valid tool", pfx, tname))
		}
	}
}

// validateModifierEvals checks modifier eval references (rule 3 sub-check).
func validateModifierEvals(pfx string, s *Step, evals map[string]bool, r *ValidationResult) {
	if s.Modifiers == nil {
		return
	}
	for _, ev := range s.Modifiers.Eval {
		if !evals[ev] {
			r.Errors = append(r.Errors, fmt.Sprintf("%s: eval %q does not reference a valid eval", pfx, ev))
		}
	}
}

// validateStepIDs checks rule 2 and returns the set of all step ids (including
// nested parallel.branches) for downstream reference checks.
func validateStepIDs(name string, c *Composition, r *ValidationResult) map[string]bool {
	ids := map[string]bool{}
	var walk func(steps []*Step)
	walk = func(steps []*Step) {
		for _, s := range steps {
			if !stepIDRe.MatchString(s.ID) {
				r.Errors = append(r.Errors, fmt.Sprintf(
					"compositions[%q].steps: step id %q must match ^[a-zA-Z_][a-zA-Z0-9_]*$", name, s.ID))
			}
			if ids[s.ID] {
				r.Errors = append(r.Errors, fmt.Sprintf(
					"compositions[%q].steps: duplicate step id %q", name, s.ID))
			}
			ids[s.ID] = true
			if s.Kind == KindParallel {
				walk(s.Branches)
			}
		}
	}
	walk(c.Steps)
	return ids
}

// forEachStep walks every step including nested parallel.branches.
func forEachStep(steps []*Step, fn func(*Step)) {
	for _, s := range steps {
		fn(s)
		if s.Kind == KindParallel {
			forEachStep(s.Branches, fn)
		}
	}
}

// validateRefRoots checks rule 4: every ${...} root is "input" or a known step id.
func validateRefRoots(name string, s *Step, stepIDs map[string]bool, r *ValidationResult) {
	roots := collectRefRoots(s.Input)
	roots = append(roots, collectRefRoots(s.Args)...)
	roots = append(roots, collectRefRoots(predicatePaths(s.Predicate))...)
	for _, root := range roots {
		if root == inputRefRoot || stepIDs[root] {
			continue
		}
		r.Errors = append(r.Errors, fmt.Sprintf(
			"compositions[%q].steps[%q]: references unknown %q (not 'input' or a step id)", name, s.ID, root))
	}
}

// predicatePaths flattens all path strings in a predicate tree into a slice so
// collectRefRoots can extract their roots.
func predicatePaths(p *Predicate) []any {
	if p == nil {
		return nil
	}
	var out []any
	if p.Path != "" {
		out = append(out, p.Path)
	}
	for _, c := range p.AllOf {
		out = append(out, predicatePaths(c)...)
	}
	for _, c := range p.AnyOf {
		out = append(out, predicatePaths(c)...)
	}
	out = append(out, predicatePaths(p.Not)...)
	return out
}

// validateKind checks rules 5, 6, 7, 11 for a single step.
func validateKind(name string, s *Step, r *ValidationResult) {
	pfx := fmt.Sprintf("compositions[%q].steps[%q]", name, s.ID)
	allowed := map[StepKind]map[string]bool{
		KindPrompt:   {fieldPromptTask: true, fieldInput: true, fieldOutputSchema: true},
		KindAgent:    {fieldPromptTask: true, fieldInput: true, fieldOutputSchema: true, "tools": true, "termination": true},
		KindTool:     {"tool": true, "args": true},
		KindBranch:   {"predicate": true, "then": true, "else": true},
		KindParallel: {"branches": true, "reduce": true},
	}
	set, known := allowed[s.Kind]
	if !known {
		r.Errors = append(r.Errors, fmt.Sprintf("%s: unknown kind %q", pfx, s.Kind))
		return
	}
	for _, f := range presentKindFields(s) {
		if !set[f] {
			r.Errors = append(r.Errors, fmt.Sprintf("%s: field %q not allowed for kind %q", pfx, f, s.Kind))
		}
	}
	switch s.Kind {
	case KindPrompt, KindTool:
		// no additional structural checks beyond the allowed-field set
	case KindAgent:
		if s.Termination == nil || (s.Termination.MaxSteps == 0 && s.Termination.ToolCalled == "") {
			r.Errors = append(r.Errors, fmt.Sprintf("%s: agent step must declare termination (max_steps or tool_called)", pfx))
		}
	case KindParallel:
		if len(s.Branches) < minParallelBranches {
			r.Errors = append(r.Errors, fmt.Sprintf("%s: parallel step must have at least two branches", pfx))
		}
		validateReducer(pfx, s.Reduce, r)
	case KindBranch:
		if s.Then == "" {
			r.Errors = append(r.Errors, fmt.Sprintf("%s: branch step must declare then", pfx))
		}
		validatePredicate(pfx, s.Predicate, r)
	}
}

// presentKindFields lists the kind-specific fields that are set on s.
func presentKindFields(s *Step) []string {
	var f []string
	if s.PromptTask != "" {
		f = append(f, fieldPromptTask)
	}
	if s.Input != nil {
		f = append(f, fieldInput)
	}
	if s.OutputSchema != "" {
		f = append(f, fieldOutputSchema)
	}
	if len(s.Tools) > 0 {
		f = append(f, "tools")
	}
	if s.Termination != nil {
		f = append(f, "termination")
	}
	if s.Tool != "" {
		f = append(f, "tool")
	}
	if len(s.Args) > 0 {
		f = append(f, "args")
	}
	if s.Predicate != nil {
		f = append(f, "predicate")
	}
	if s.Then != "" {
		f = append(f, "then")
	}
	if s.Else != "" {
		f = append(f, "else")
	}
	if len(s.Branches) > 0 {
		f = append(f, "branches")
	}
	if s.Reduce != nil {
		f = append(f, "reduce")
	}
	return f
}

// validateReducer checks rule 6's reducer half.
func validateReducer(pfx string, red *Reducer, r *ValidationResult) {
	if red == nil {
		r.Errors = append(r.Errors, fmt.Sprintf("%s: parallel step must declare reduce", pfx))
		return
	}
	switch red.Strategy {
	case ReduceAppend, ReduceReplace, ReduceBarrier:
	default:
		r.Errors = append(r.Errors, fmt.Sprintf(
			"%s: reduce.strategy %q is not valid (append|replace|barrier)", pfx, red.Strategy))
	}
	if red.Into == "" {
		r.Errors = append(r.Errors, fmt.Sprintf("%s: reduce.into is required", pfx))
	}
}

// validatePredicate checks rule 7's predicate half: exactly one variant, valid op.
func validatePredicate(pfx string, p *Predicate, r *ValidationResult) {
	if p == nil {
		r.Errors = append(r.Errors, fmt.Sprintf("%s: branch step must declare predicate", pfx))
		return
	}
	isCompare, variants := countPredicateVariants(p)
	if variants != 1 {
		r.Errors = append(r.Errors, fmt.Sprintf(
			"%s: predicate must set exactly one variant (compare|exists|all_of|any_of|not)", pfx))
	}
	if isCompare && !CompareOps[p.Op] {
		r.Errors = append(r.Errors, fmt.Sprintf("%s: predicate op %q is not valid", pfx, p.Op))
	}
	for _, c := range p.AllOf {
		validatePredicate(pfx, c, r)
	}
	for _, c := range p.AnyOf {
		validatePredicate(pfx, c, r)
	}
	if p.Not != nil {
		validatePredicate(pfx, p.Not, r)
	}
}

// countPredicateVariants returns whether the predicate is a compare variant and
// the total number of set variants (used by validatePredicate to enforce exactly-one).
func countPredicateVariants(p *Predicate) (isCompare bool, variants int) {
	isCompare = p.Op != "" || p.Value != nil
	if isCompare {
		variants++
	}
	if p.Exists != nil {
		variants++
	}
	if len(p.AllOf) > 0 {
		variants++
	}
	if len(p.AnyOf) > 0 {
		variants++
	}
	if p.Not != nil {
		variants++
	}
	return isCompare, variants
}
