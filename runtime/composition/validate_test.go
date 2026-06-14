package composition

import (
	"strings"
	"testing"
)

// av builds an Available with the given prompt/tool/eval keys.
func av(prompts, tools, evals []string) Available {
	return Available{Prompts: prompts, Tools: tools, Evals: evals}
}

func hasErr(r *ValidationResult, substr string) bool {
	for _, e := range r.Errors {
		if strings.Contains(e, substr) {
			return true
		}
	}
	return false
}

func TestValidate_DuplicateStepID(t *testing.T) {
	c := &Composition{Version: 1, Steps: []*Step{
		{ID: "a", Kind: KindPrompt, PromptTask: "p"},
		{ID: "a", Kind: KindPrompt, PromptTask: "p"},
	}}
	r := Validate("comp", c, av([]string{"p"}, nil, nil))
	if !hasErr(r, "duplicate step id") {
		t.Fatalf("want duplicate step id error, got %v", r.Errors)
	}
}

func TestValidate_BadStepIDPattern(t *testing.T) {
	c := &Composition{Version: 1, Steps: []*Step{
		{ID: "1bad", Kind: KindPrompt, PromptTask: "p"},
	}}
	r := Validate("comp", c, av([]string{"p"}, nil, nil))
	if !hasErr(r, "must match") {
		t.Fatalf("want pattern error, got %v", r.Errors)
	}
}

func TestValidate_NestedParallelIDsCounted(t *testing.T) {
	c := &Composition{Version: 1, Steps: []*Step{
		{ID: "dup", Kind: KindPrompt, PromptTask: "p"},
		{ID: "par", Kind: KindParallel, Reduce: &Reducer{Strategy: ReduceAppend, Into: "x"}, Branches: []*Step{
			{ID: "dup", Kind: KindTool, Tool: "t"},
			{ID: "ok", Kind: KindTool, Tool: "t"},
		}},
	}}
	r := Validate("comp", c, av([]string{"p"}, []string{"t"}, nil))
	if !hasErr(r, "duplicate step id") {
		t.Fatalf("want duplicate across nested branches, got %v", r.Errors)
	}
}

func TestValidateAll_ReservedNameCollision(t *testing.T) {
	comps := map[string]*Composition{
		"workflow__transition": {Version: 1, Steps: []*Step{{ID: "a", Kind: KindPrompt, PromptTask: "p"}}},
	}
	r := ValidateAll(comps, av([]string{"p"}, nil, nil))
	if !hasErr(r, "reserved") {
		t.Fatalf("want reserved-name error, got %v", r.Errors)
	}
}

func TestValidate_UnknownPromptTask(t *testing.T) {
	c := &Composition{Version: 1, Steps: []*Step{
		{ID: "a", Kind: KindPrompt, PromptTask: "missing"},
	}}
	r := Validate("comp", c, av([]string{"known"}, nil, nil))
	if !hasErr(r, `prompt_task "missing"`) {
		t.Fatalf("want unknown prompt error, got %v", r.Errors)
	}
}

func TestValidate_UnknownAgentToolAndEval(t *testing.T) {
	c := &Composition{Version: 1, Steps: []*Step{
		{ID: "a", Kind: KindAgent, PromptTask: "p", Tools: []string{"nope"},
			Termination: &Termination{MaxSteps: 1},
			Modifiers:   &StepModifiers{Eval: []string{"noeval"}}},
	}}
	r := Validate("comp", c, av([]string{"p"}, []string{"real"}, []string{"realeval"}))
	if !hasErr(r, `tool "nope"`) {
		t.Errorf("want unknown agent tool, got %v", r.Errors)
	}
	if !hasErr(r, `eval "noeval"`) {
		t.Errorf("want unknown eval, got %v", r.Errors)
	}
}

func TestValidate_UnknownRefRoot(t *testing.T) {
	c := &Composition{Version: 1, Steps: []*Step{
		{ID: "a", Kind: KindPrompt, PromptTask: "p", Input: "${ghost.output.x}"},
	}}
	r := Validate("comp", c, av([]string{"p"}, nil, nil))
	if !hasErr(r, `references unknown "ghost"`) {
		t.Fatalf("want unknown ref root, got %v", r.Errors)
	}
}

func TestValidate_InputAndPriorStepRefsOK(t *testing.T) {
	c := &Composition{Version: 1, Steps: []*Step{
		{ID: "first", Kind: KindPrompt, PromptTask: "p", Input: "${input.text}"},
		{ID: "second", Kind: KindPrompt, PromptTask: "p", Input: "${first.output.y}"},
	}}
	r := Validate("comp", c, av([]string{"p"}, nil, nil))
	if r.HasErrors() {
		t.Fatalf("unexpected errors: %v", r.Errors)
	}
}

func TestValidate_NilComposition(t *testing.T) {
	r := Validate("comp", nil, av(nil, nil, nil))
	if r.HasErrors() {
		t.Fatalf("nil composition should produce no errors, got %v", r.Errors)
	}
}

func TestValidate_UnknownToolKindRef(t *testing.T) {
	c := &Composition{Version: 1, Steps: []*Step{
		{ID: "a", Kind: KindTool, Tool: "missing"},
	}}
	r := Validate("comp", c, av(nil, []string{"real"}, nil))
	if !hasErr(r, `tool "missing"`) {
		t.Fatalf("want unknown tool error, got %v", r.Errors)
	}
}

func TestValidate_BranchPredicateRefRoot(t *testing.T) {
	// Branch step with predicate path referencing unknown root.
	c := &Composition{Version: 1, Steps: []*Step{
		{ID: "b", Kind: KindBranch,
			Predicate: &Predicate{Path: "${ghost.output.x}", Op: "equals", Value: 1},
			Then:      "n"},
		{ID: "n", Kind: KindPrompt, PromptTask: "p"},
	}}
	r := Validate("comp", c, av([]string{"p"}, nil, nil))
	if !hasErr(r, `references unknown "ghost"`) {
		t.Fatalf("want unknown predicate ref root, got %v", r.Errors)
	}
}

func TestValidate_PredicateAllOfAnyOfPaths(t *testing.T) {
	// Composite predicate — exercise all_of / any_of / not paths in predicatePaths.
	c := &Composition{Version: 1, Steps: []*Step{
		{ID: "b", Kind: KindBranch,
			Predicate: &Predicate{
				AllOf: []*Predicate{
					{Path: "${input.x}", Op: "equals", Value: 1},
					{AnyOf: []*Predicate{
						{Path: "${input.y}", Op: "equals", Value: 2},
					}},
					{Not: &Predicate{Path: "${input.z}", Op: "equals", Value: 3}},
				},
			},
			Then: "n"},
		{ID: "n", Kind: KindPrompt, PromptTask: "p"},
	}}
	r := Validate("comp", c, av([]string{"p"}, nil, nil))
	if r.HasErrors() {
		t.Fatalf("unexpected errors: %v", r.Errors)
	}
}

func TestValidate_AgentNeedsTermination(t *testing.T) {
	c := &Composition{Version: 1, Steps: []*Step{
		{ID: "a", Kind: KindAgent, PromptTask: "p"},
	}}
	r := Validate("comp", c, av([]string{"p"}, nil, nil))
	if !hasErr(r, "agent step") || !hasErr(r, "termination") {
		t.Fatalf("want termination error, got %v", r.Errors)
	}
}

func TestValidate_ParallelNeedsTwoBranchesAndReduce(t *testing.T) {
	c := &Composition{Version: 1, Steps: []*Step{
		{ID: "par", Kind: KindParallel, Branches: []*Step{{ID: "only", Kind: KindTool, Tool: "t"}}},
	}}
	r := Validate("comp", c, av(nil, []string{"t"}, nil))
	if !hasErr(r, "at least two branches") {
		t.Errorf("want >=2 branches error, got %v", r.Errors)
	}
	if !hasErr(r, "reduce") {
		t.Errorf("want reduce error, got %v", r.Errors)
	}
}

func TestValidate_ReduceNeedsStrategyAndInto(t *testing.T) {
	c := &Composition{Version: 1, Steps: []*Step{
		{ID: "par", Kind: KindParallel,
			Branches: []*Step{{ID: "x", Kind: KindTool, Tool: "t"}, {ID: "y", Kind: KindTool, Tool: "t"}},
			Reduce:   &Reducer{Strategy: "bogus"}},
	}}
	r := Validate("comp", c, av(nil, []string{"t"}, nil))
	if !hasErr(r, "reduce.strategy") {
		t.Errorf("want strategy error, got %v", r.Errors)
	}
	if !hasErr(r, "reduce.into") {
		t.Errorf("want into error, got %v", r.Errors)
	}
}

func TestValidate_BranchNeedsThenAndPredicate(t *testing.T) {
	c := &Composition{Version: 1, Steps: []*Step{
		{ID: "b", Kind: KindBranch, Predicate: &Predicate{Path: "${input.x}", Op: "bogus", Value: 1}},
		{ID: "n", Kind: KindPrompt, PromptTask: "p"},
	}}
	r := Validate("comp", c, av([]string{"p"}, nil, nil))
	if !hasErr(r, "branch step") || !hasErr(r, "then") {
		t.Errorf("want then error, got %v", r.Errors)
	}
	if !hasErr(r, "op") {
		t.Errorf("want invalid op error, got %v", r.Errors)
	}
}

func TestValidate_KindLegalFields(t *testing.T) {
	c := &Composition{Version: 1, Steps: []*Step{
		{ID: "a", Kind: KindPrompt, PromptTask: "p", Tool: "t"}, // tool field illegal on prompt
	}}
	r := Validate("comp", c, av([]string{"p"}, []string{"t"}, nil))
	if !hasErr(r, "field \"tool\" not allowed") {
		t.Fatalf("want kind-legal error, got %v", r.Errors)
	}
}

func TestValidate_PredicateExactlyOneVariant(t *testing.T) {
	mixed := &Predicate{Path: "${input.x}", Op: "equals", Value: 1, AllOf: []*Predicate{{Path: "${input.y}", Op: "equals", Value: 2}}}
	c := &Composition{Version: 1, Steps: []*Step{
		{ID: "b", Kind: KindBranch, Predicate: mixed, Then: "n"},
		{ID: "n", Kind: KindPrompt, PromptTask: "p"},
	}}
	r := Validate("comp", c, av([]string{"p"}, nil, nil))
	if !hasErr(r, "exactly one") {
		t.Fatalf("want single-variant error, got %v", r.Errors)
	}
}

func TestValidate_BranchTargetResolves(t *testing.T) {
	c := &Composition{Version: 1, Steps: []*Step{
		{ID: "b", Kind: KindBranch, Predicate: &Predicate{Path: "${input.x}", Op: "equals", Value: 1}, Then: "ghost"},
		{ID: "n", Kind: KindPrompt, PromptTask: "p"},
	}}
	r := Validate("comp", c, av([]string{"p"}, nil, nil))
	if !hasErr(r, `then "ghost"`) {
		t.Fatalf("want unresolved then, got %v", r.Errors)
	}
}

func TestValidate_DependsOnResolves(t *testing.T) {
	c := &Composition{Version: 1, Steps: []*Step{
		{ID: "a", Kind: KindPrompt, PromptTask: "p"},
		{ID: "b", Kind: KindPrompt, PromptTask: "p", DependsOn: []string{"ghost"}},
	}}
	r := Validate("comp", c, av([]string{"p"}, nil, nil))
	if !hasErr(r, `depends_on "ghost"`) {
		t.Fatalf("want unresolved depends_on, got %v", r.Errors)
	}
}

func TestValidate_Acyclic(t *testing.T) {
	c := &Composition{Version: 1, Steps: []*Step{
		{ID: "a", Kind: KindPrompt, PromptTask: "p", DependsOn: []string{"b"}},
		{ID: "b", Kind: KindPrompt, PromptTask: "p", DependsOn: []string{"a"}},
	}}
	r := Validate("comp", c, av([]string{"p"}, nil, nil))
	if !hasErr(r, "cycle") {
		t.Fatalf("want cycle error, got %v", r.Errors)
	}
}

func TestValidate_OutputResolves(t *testing.T) {
	c := &Composition{Version: 1, Output: "ghost", Steps: []*Step{
		{ID: "a", Kind: KindPrompt, PromptTask: "p"},
	}}
	r := Validate("comp", c, av([]string{"p"}, nil, nil))
	if !hasErr(r, `output "ghost"`) {
		t.Fatalf("want unresolved output, got %v", r.Errors)
	}
}

func TestValidate_JoinWarning(t *testing.T) {
	c := &Composition{Version: 1, Steps: []*Step{
		{ID: "b", Kind: KindBranch, Predicate: &Predicate{Path: "${input.x}", Op: "equals", Value: 1}, Then: "next"},
		{ID: "next", Kind: KindPrompt, PromptTask: "p"}, // follows a branch without depends_on
	}}
	r := Validate("comp", c, av([]string{"p"}, nil, nil))
	found := false
	for _, w := range r.Warnings {
		if strings.Contains(w, "depends_on") {
			found = true
		}
	}
	if !found {
		t.Fatalf("want join-point warning, got warnings %v", r.Warnings)
	}
}

// --- TDD tests for fixes #1, #2, #3 ---

// Fix #1: cycle detection must walk nested parallel.branches.
func TestValidate_NestedBranchCycle(t *testing.T) {
	// Two branches inside a parallel that depend on each other → cycle.
	c := &Composition{Version: 1, Steps: []*Step{
		{ID: "par", Kind: KindParallel,
			Reduce: &Reducer{Strategy: ReduceAppend, Into: "x"},
			Branches: []*Step{
				{ID: "left", Kind: KindTool, Tool: "t", DependsOn: []string{"right"}},
				{ID: "right", Kind: KindTool, Tool: "t", DependsOn: []string{"left"}},
			},
		},
	}}
	r := Validate("comp", c, av(nil, []string{"t"}, nil))
	if !hasErr(r, "cycle") {
		t.Fatalf("want cycle error for nested branch mutual dependency, got %v", r.Errors)
	}
}

// Fix #2: compare predicate must declare path.
func TestValidate_ComparePredicateMissingPath(t *testing.T) {
	// Predicate with op+value but no path → error.
	c := &Composition{Version: 1, Steps: []*Step{
		{ID: "b", Kind: KindBranch,
			Predicate: &Predicate{Op: "equals", Value: 1}, // no Path
			Then:      "n"},
		{ID: "n", Kind: KindPrompt, PromptTask: "p"},
	}}
	r := Validate("comp", c, av([]string{"p"}, nil, nil))
	if !hasErr(r, "must declare path") {
		t.Fatalf("want 'must declare path' error for compare predicate with no path, got %v", r.Errors)
	}
}

// Fix #3: Composition.Version must be 1.
func TestValidate_VersionMustBeOne(t *testing.T) {
	c := &Composition{Version: 2, Steps: []*Step{
		{ID: "a", Kind: KindPrompt, PromptTask: "p"},
	}}
	r := Validate("comp", c, av([]string{"p"}, nil, nil))
	if !hasErr(r, "version must be 1") {
		t.Fatalf("want version error, got %v", r.Errors)
	}
}

func TestValidate_FullValidCompositionNoErrors(t *testing.T) {
	c := &Composition{
		Version: 1, Output: "synthesize",
		Steps: []*Step{
			{ID: "classify", Kind: KindPrompt, PromptTask: "doc_classifier", Input: "${input.text}"},
			{ID: "route", Kind: KindBranch,
				Predicate: &Predicate{Path: "${classify.output.type}", Op: "equals", Value: "paper"},
				Then:      "paper", Else: "general"},
			{ID: "paper", Kind: KindPrompt, PromptTask: "paper_extractor", Input: "${input.text}", DependsOn: []string{"route"}},
			{ID: "general", Kind: KindPrompt, PromptTask: "general_extractor", Input: "${input.text}", DependsOn: []string{"route"}},
			{ID: "meta", Kind: KindParallel, DependsOn: []string{"paper", "general"},
				Branches: []*Step{
					{ID: "structure", Kind: KindTool, Tool: "doc.parse", Args: map[string]any{"content": "${input.text}"}},
					{ID: "citations", Kind: KindTool, Tool: "doc.cite", Args: map[string]any{"content": "${input.text}"}},
				},
				Reduce: &Reducer{Strategy: ReduceBarrier, Into: "metadata"}},
			{ID: "synthesize", Kind: KindAgent, PromptTask: "doc_analyzer", Input: "${meta.output.metadata}",
				Tools: []string{"ref.search"}, Termination: &Termination{MaxSteps: 10}, DependsOn: []string{"meta"},
				Modifiers: &StepModifiers{Eval: []string{"analysis_quality"}}},
		},
	}
	r := Validate("analyze", c,
		av([]string{"doc_classifier", "paper_extractor", "general_extractor", "doc_analyzer"},
			[]string{"doc.parse", "doc.cite", "ref.search"},
			[]string{"analysis_quality"}))
	if r.HasErrors() {
		t.Fatalf("expected no errors, got %v (warnings %v)", r.Errors, r.Warnings)
	}
}
