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
