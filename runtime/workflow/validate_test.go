package workflow

import (
	"testing"
)

func validSpec() *Spec {
	return &Spec{
		Version: 1,
		Entry:   "intake",
		States: map[string]*State{
			"intake": {
				PromptTask: "gather_requirements",
				OnEvent: map[string]string{
					"IssueUnderstood": "solving",
				},
			},
			"solving": {
				PromptTask: "create_solution",
				OnEvent: map[string]string{
					"SolutionAccepted": "done",
				},
			},
			"done": {
				PromptTask: "confirm_resolution",
			},
		},
	}
}

var allPrompts = []string{"gather_requirements", "create_solution", "confirm_resolution"}

func TestValidate_ValidSpec(t *testing.T) {
	r := Validate(validSpec(), allPrompts)
	if r.HasErrors() {
		t.Errorf("expected no errors, got: %v", r.Errors)
	}
	if len(r.Warnings) != 0 {
		t.Errorf("expected no warnings, got: %v", r.Warnings)
	}
}

func TestValidate_Rule1_VersionMustBeOne(t *testing.T) {
	spec := validSpec()
	spec.Version = 2
	r := Validate(spec, allPrompts)
	if !r.HasErrors() {
		t.Fatal("expected error for version != 1")
	}
	assertContains(t, r.Errors, "version must be 1")
}

func TestValidate_Rule2_StatesNonEmpty(t *testing.T) {
	spec := &Spec{Version: 1, Entry: "start", States: map[string]*State{}}
	r := Validate(spec, nil)
	if !r.HasErrors() {
		t.Fatal("expected error for empty states")
	}
	assertContains(t, r.Errors, "non-empty")
}

func TestValidate_Rule3_EntryMustExist(t *testing.T) {
	spec := validSpec()
	spec.Entry = "nonexistent"
	r := Validate(spec, allPrompts)
	assertContains(t, r.Errors, "does not reference a key in states")
}

func TestValidate_Rule4_EntryPromptTaskMustExist(t *testing.T) {
	spec := validSpec()
	spec.States["intake"].PromptTask = "missing_prompt"
	r := Validate(spec, allPrompts)
	assertContains(t, r.Errors, "does not reference a valid prompt")
}

func TestValidate_Rule5_AllPromptTasksMustExist(t *testing.T) {
	spec := validSpec()
	spec.States["solving"].PromptTask = "missing_prompt"
	r := Validate(spec, allPrompts)
	assertContains(t, r.Errors, "does not reference a valid prompt")
}

func TestValidate_Rule6_EventTargetsMustExist(t *testing.T) {
	spec := validSpec()
	spec.States["intake"].OnEvent["IssueUnderstood"] = "ghost_state"
	r := Validate(spec, allPrompts)
	assertContains(t, r.Errors, "does not exist in states")
}

func TestValidate_Rule7_PascalCaseWarning(t *testing.T) {
	spec := validSpec()
	spec.States["intake"].OnEvent["not_pascal_case"] = "solving"
	r := Validate(spec, allPrompts)
	if r.HasErrors() {
		t.Errorf("PascalCase violation should be a warning, not error: %v", r.Errors)
	}
	assertContains(t, r.Warnings, "PascalCase")
}

func TestValidate_Rule8_PersistenceEnum(t *testing.T) {
	spec := validSpec()
	spec.States["intake"].Persistence = "invalid"
	r := Validate(spec, allPrompts)
	assertContains(t, r.Errors, "persistence")

	// Valid values should not produce errors
	spec.States["intake"].Persistence = PersistenceTransient
	r = Validate(spec, allPrompts)
	for _, e := range r.Errors {
		if contains(e, "persistence") {
			t.Errorf("transient should be valid, got error: %s", e)
		}
	}
}

func TestValidate_Rule9_OrchestrationEnum(t *testing.T) {
	spec := validSpec()
	spec.States["intake"].Orchestration = "invalid"
	r := Validate(spec, allPrompts)
	assertContains(t, r.Errors, "orchestration")

	// Valid values
	for _, valid := range []Orchestration{OrchestrationInternal, OrchestrationExternal, OrchestrationHybrid} {
		spec.States["intake"].Orchestration = valid
		r = Validate(spec, allPrompts)
		for _, e := range r.Errors {
			if contains(e, "orchestration") {
				t.Errorf("%q should be valid, got error: %s", valid, e)
			}
		}
	}
}

func TestValidate_Rule10_CycleDetection(t *testing.T) {
	spec := &Spec{
		Version: 1,
		Entry:   "a",
		States: map[string]*State{
			"a": {PromptTask: "p1", OnEvent: map[string]string{"Next": "b"}},
			"b": {PromptTask: "p2", OnEvent: map[string]string{"Back": "a"}},
		},
	}
	r := Validate(spec, []string{"p1", "p2"})
	if r.HasErrors() {
		t.Errorf("cycles should not be errors: %v", r.Errors)
	}
	assertContains(t, r.Warnings, "cycle")
}

func TestValidate_SelfLoop(t *testing.T) {
	spec := &Spec{
		Version: 1,
		Entry:   "retry",
		States: map[string]*State{
			"retry": {PromptTask: "task", OnEvent: map[string]string{
				"Retry": "retry",
				"Done":  "end",
			}},
			"end": {PromptTask: "task"},
		},
	}
	r := Validate(spec, []string{"task"})
	assertContains(t, r.Warnings, "cycle")
}

func TestValidate_MultipleErrors(t *testing.T) {
	spec := &Spec{
		Version: 0,
		Entry:   "missing",
		States: map[string]*State{
			"a": {PromptTask: "no_exist", OnEvent: map[string]string{"bad": "ghost"}},
		},
	}
	r := Validate(spec, nil)
	if len(r.Errors) < 3 {
		t.Errorf("expected multiple errors, got %d: %v", len(r.Errors), r.Errors)
	}
}

// --- helpers ---

func assertContains(t *testing.T, strs []string, substr string) {
	t.Helper()
	for _, s := range strs {
		if contains(s, substr) {
			return
		}
	}
	t.Errorf("expected a string containing %q in %v", substr, strs)
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > 0 && containsStr(s, substr))
}

func containsStr(s, sub string) bool {
	for i := 0; i <= len(s)-len(sub); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
