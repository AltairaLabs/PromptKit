package workflow

import (
	"slices"
	"strings"
	"testing"
)

// cyclicSpec has two cycles back to "a", entered through two events of "a",
// plus a lowercase event and a missing target in more than one state, so the
// order of every map-driven finding is visible in the result.
func cyclicSpec() *Spec {
	return &Spec{
		Version: 1,
		Entry:   "a",
		States: map[string]*State{
			"a": {PromptTask: "p", OnEvent: map[string]string{"X": "b", "Y": "c", "z": "missing1"}},
			"b": {PromptTask: "p", OnEvent: map[string]string{"Back": "a", "w": "missing2"}},
			"c": {PromptTask: "p", OnEvent: map[string]string{"Back": "a"}},
			"d": {PromptTask: "p", OnEvent: map[string]string{"Next": "e"}},
			"e": {PromptTask: "p", OnEvent: map[string]string{"Next": "d"}},
		},
	}
}

func TestValidate_CycleWarningsAreDeterministic(t *testing.T) {
	want := []string{
		"workflow contains a cycle: b -> a",
		"workflow contains a cycle: c -> a",
		"workflow contains a cycle: e -> d",
	}
	for i := 0; i < 50; i++ {
		got := detectCycleWarnings(Validate(cyclicSpec(), []string{"p"}).Warnings)
		if !slices.Equal(got, want) {
			t.Fatalf("run %d: cycle warnings = %q, want %q", i, got, want)
		}
	}
}

func TestValidate_FindingsOrderIsStable(t *testing.T) {
	first := Validate(cyclicSpec(), []string{"p"})
	for i := 0; i < 50; i++ {
		r := Validate(cyclicSpec(), []string{"p"})
		if !slices.Equal(r.Errors, first.Errors) || !slices.Equal(r.Warnings, first.Warnings) {
			t.Fatalf("run %d: findings differ between runs:\n%q\n%q\nvs\n%q\n%q",
				i, r.Errors, r.Warnings, first.Errors, first.Warnings)
		}
	}
	wantErrors := []string{
		`workflow.states["a"].on_event["z"] target "missing1" does not exist in states`,
		`workflow.states["b"].on_event["w"] target "missing2" does not exist in states`,
	}
	if !slices.Equal(first.Errors, wantErrors) {
		t.Fatalf("errors = %q, want %q", first.Errors, wantErrors)
	}
}

func detectCycleWarnings(warnings []string) []string {
	var out []string
	for _, w := range warnings {
		if strings.HasPrefix(w, "workflow contains a cycle: ") {
			out = append(out, w)
		}
	}
	return out
}
