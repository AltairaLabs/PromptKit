package workflow

import (
	"testing"

	"github.com/AltairaLabs/PromptKit/runtime/packspec"
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

func TestValidate_Rule1_VersionMustBeOneOrTwo(t *testing.T) {
	// Version 1 is valid (tested via validSpec in other tests)
	// Version 2 is valid (RFC 0009)
	spec := validSpec()
	spec.Version = 2
	r := Validate(spec, allPrompts)
	if r.HasErrors() {
		t.Fatalf("version 2 should be valid: %v", r.Errors)
	}

	// Version 3 is invalid
	spec.Version = 3
	r = Validate(spec, allPrompts)
	if !r.HasErrors() {
		t.Fatal("expected error for version 3")
	}
	assertContains(t, r.Errors, "version must be 1 or 2")
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
	spec.States["intake"].Orchestration = packspec.Ptr("invalid")
	r := Validate(spec, allPrompts)
	assertContains(t, r.Errors, "orchestration")

	// Valid values
	for _, valid := range []string{OrchestrationInternal, OrchestrationExternal, OrchestrationHybrid} {
		spec.States["intake"].Orchestration = packspec.Ptr(valid)
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

func TestValidate_OnMaxVisitsTargetMustExist(t *testing.T) {
	spec := &Spec{
		Version: 2,
		Entry:   "a",
		States: map[string]*State{
			"a": {PromptTask: "p1", MaxVisits: packspec.Ptr(3), OnMaxVisits: "ghost",
				OnEvent: map[string]string{"Next": "a"}},
		},
	}
	r := Validate(spec, []string{"p1"})
	if !r.HasErrors() {
		t.Fatal("expected error for on_max_visits referencing non-existent state")
	}
	assertContains(t, r.Errors, "on_max_visits")
	assertContains(t, r.Errors, "ghost")
}

func TestValidate_TerminalWithOnEventWarns(t *testing.T) {
	spec := &Spec{
		Version: 2,
		Entry:   "a",
		States: map[string]*State{
			"a": {PromptTask: "p1", Terminal: packspec.Ptr(true),
				OnEvent: map[string]string{"Next": "a"}},
		},
	}
	r := Validate(spec, []string{"p1"})
	if r.HasErrors() {
		t.Fatalf("terminal+on_event should be a warning, not error: %v", r.Errors)
	}
	assertContains(t, r.Warnings, "terminal")
}

func TestValidate_NonTerminalWithoutExitWarns(t *testing.T) {
	spec := &Spec{
		Version: 2,
		Entry:   "a",
		States: map[string]*State{
			"a": {PromptTask: "p1"}, // no Terminal, no OnEvent, no MaxVisits — dead-end
		},
	}
	r := Validate(spec, []string{"p1"})
	if r.HasErrors() {
		t.Fatalf("dead-end state should be a warning, not error: %v", r.Errors)
	}
	assertContains(t, r.Warnings, "no on_event and no max_visits")
}

func TestValidate_NonTerminalWithoutExit_TerminalSilences(t *testing.T) {
	spec := &Spec{
		Version: 2,
		Entry:   "a",
		States: map[string]*State{
			"a": {PromptTask: "p1", Terminal: packspec.Ptr(true)},
		},
	}
	r := Validate(spec, []string{"p1"})
	for _, w := range r.Warnings {
		if contains(w, "no on_event and no max_visits") {
			t.Errorf("terminal: true should silence reachability warning, got: %s", w)
		}
	}
}

func TestValidate_NonTerminalWithoutExit_V1Silent(t *testing.T) {
	// v1 predates RFC 0009; dead-end states are implicitly terminal and
	// should not trigger the reachability warning.
	spec := &Spec{
		Version: 1,
		Entry:   "a",
		States: map[string]*State{
			"a": {PromptTask: "p1"},
		},
	}
	r := Validate(spec, []string{"p1"})
	for _, w := range r.Warnings {
		if contains(w, "no on_event and no max_visits") {
			t.Errorf("v1 should not trigger RFC 0009 reachability warning, got: %s", w)
		}
	}
}

func TestValidate_RedirectChainWarns(t *testing.T) {
	spec := &Spec{
		Version: 2,
		Entry:   "a",
		States: map[string]*State{
			"a": {PromptTask: "p1", MaxVisits: packspec.Ptr(2), OnMaxVisits: "b",
				OnEvent: map[string]string{"Next": "a"}},
			"b": {PromptTask: "p2", MaxVisits: packspec.Ptr(2), OnMaxVisits: "c",
				OnEvent: map[string]string{"Next": "a"}},
			"c": {PromptTask: "p3"},
		},
	}
	r := Validate(spec, []string{"p1", "p2", "p3"})
	if r.HasErrors() {
		t.Fatalf("redirect chain should be a warning, not error: %v", r.Errors)
	}
	assertContains(t, r.Warnings, "redirect chain")
}

func TestValidate_CompositionStateNoPromptTaskOK(t *testing.T) {
	spec := &Spec{
		Version: 1, Entry: "analyze",
		States: map[string]*State{
			"analyze": {Orchestration: packspec.Ptr(OrchestrationComposition), Composition: "analyze_doc", Terminal: packspec.Ptr(true)},
		},
	}
	res := Validate(spec, []string{})
	if res.HasErrors() {
		t.Errorf("composition state should validate without prompt_task: %v", res.Errors)
	}
}

func TestValidate_CompositionStateMissingCompositionErrors(t *testing.T) {
	spec := &Spec{
		Version: 1, Entry: "analyze",
		States: map[string]*State{"analyze": {Orchestration: packspec.Ptr(OrchestrationComposition), Terminal: packspec.Ptr(true)}},
	}
	res := Validate(spec, []string{})
	if !res.HasErrors() {
		t.Error("composition state without composition must error")
	}
	assertContains(t, res.Errors, "composition")
}

func TestValidate_CompositionSetOnNonCompositionStateErrors(t *testing.T) {
	spec := &Spec{
		Version: 1, Entry: "chat",
		States: map[string]*State{"chat": {PromptTask: "p", Composition: "x"}},
	}
	res := Validate(spec, []string{"p"})
	if !res.HasErrors() {
		t.Error("composition set on a non-composition state must error")
	}
	assertContains(t, res.Errors, "composition")
}

func TestValidate_NonCompositionStateStillRequiresPromptTask(t *testing.T) {
	spec := &Spec{
		Version: 1, Entry: "chat",
		States: map[string]*State{"chat": {}},
	}
	res := Validate(spec, []string{})
	if !res.HasErrors() {
		t.Error("non-composition state without prompt_task must still error")
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

func TestValidateControl_RejectsUnrecognizedValue(t *testing.T) {
	// An unhonorable gate must say so rather than resolve to a default. This
	// is the #1931 failure in a different field: an unimplemented `when` key
	// decoded to no conditions and the gate silently disappeared.
	spec := validSpec()
	spec.States["intake"].Control = packspec.Ptr("agent_but_typoed")

	r := Validate(spec, []string{"gather_requirements", "create_solution", "confirm_resolution"})

	if !r.HasErrors() {
		t.Fatal("an unrecognized control value must be an error, not a silent default")
	}
	assertContains(t, r.Errors, "control")
	assertContains(t, r.Errors, "agent_but_typoed")
}

func TestValidateControl_AcceptsSpecValues(t *testing.T) {
	for _, v := range []string{ControlUser, ControlAgent} {
		t.Run(v, func(t *testing.T) {
			spec := validSpec()
			spec.States["intake"].Control = packspec.Ptr(v)

			r := Validate(spec, []string{"gather_requirements", "create_solution", "confirm_resolution"})

			if r.HasErrors() {
				t.Errorf("control %q must be valid, got %v", v, r.Errors)
			}
		})
	}
}

func TestValidateControl_WarnsOnAgentPlusTerminal(t *testing.T) {
	// RFC 0014 rule 3: terminal wins regardless, so the pair cannot do what
	// the author is asking for.
	spec := validSpec()
	spec.States["solving"].Control = packspec.Ptr(ControlAgent)
	spec.States["solving"].Terminal = packspec.Ptr(true)

	r := Validate(spec, []string{"gather_requirements", "create_solution", "confirm_resolution"})

	if r.HasErrors() {
		t.Fatalf("agent + terminal is a warning, not an error: %v", r.Errors)
	}
	assertContains(t, r.Warnings, "has no effect")
}

func TestValidateAgentLoops(t *testing.T) {
	// RFC 0014 rule 4. Two agent states pointing at each other never hand the
	// turn back; the variants each add one thing that bounds the loop.
	agentCycle := func() *Spec {
		return &Spec{
			Version: 1,
			Entry:   "a",
			States: map[string]*State{
				"a": {
					PromptTask: "p",
					Control:    packspec.Ptr(ControlAgent),
					OnEvent:    map[string]string{"Next": "b"},
				},
				"b": {
					PromptTask: "p",
					Control:    packspec.Ptr(ControlAgent),
					OnEvent:    map[string]string{"Back": "a"},
				},
			},
		}
	}

	t.Run("unbounded agent cycle warns", func(t *testing.T) {
		r := Validate(agentCycle(), []string{"p"})
		assertContains(t, r.Warnings, "agent-controlled cycle")
	})

	t.Run("a state that yields bounds it", func(t *testing.T) {
		spec := agentCycle()
		spec.States["b"].Control = packspec.Ptr(ControlUser)
		r := Validate(spec, []string{"p"})
		for _, w := range r.Warnings {
			if contains(w, "agent-controlled cycle") {
				t.Errorf("a state yielding to the user bounds the loop; got %q", w)
			}
		}
	})

	t.Run("max_visits bounds it", func(t *testing.T) {
		spec := agentCycle()
		spec.States["b"].MaxVisits = packspec.Ptr(3)
		r := Validate(spec, []string{"p"})
		for _, w := range r.Warnings {
			if contains(w, "agent-controlled cycle") {
				t.Errorf("max_visits bounds the loop; got %q", w)
			}
		}
	})

	t.Run("an agent chain that does not loop does not warn", func(t *testing.T) {
		spec := agentCycle()
		spec.States["b"].OnEvent = nil // b becomes terminal: no way back to a
		r := Validate(spec, []string{"p"})
		for _, w := range r.Warnings {
			if contains(w, "agent-controlled cycle") {
				t.Errorf("a chain with an end is bounded; got %q", w)
			}
		}
	})

	t.Run("a plain cycle with no control declared does not warn", func(t *testing.T) {
		// Absent control keeps this runtime's pre-RFC behavior, so such a
		// cycle is exactly as (un)bounded as it has always been. Warning here
		// would fire on packs that predate the field entirely.
		spec := agentCycle()
		spec.States["a"].Control = nil
		spec.States["b"].Control = nil
		r := Validate(spec, []string{"p"})
		for _, w := range r.Warnings {
			if contains(w, "agent-controlled cycle") {
				t.Errorf("undeclared control must not trip the RFC 0014 loop warning; got %q", w)
			}
		}
	})
}

func TestValidateAgentLoops_BudgetBoundsTheLoop(t *testing.T) {
	// A workflow budget is enforced on every transition, so a pack that
	// declares one has already bounded its loops deliberately. Warning anyway
	// would be a false positive on exactly the packs that did the right thing.
	agentCycle := func() *Spec {
		return &Spec{
			Version: 1,
			Entry:   "a",
			States: map[string]*State{
				"a": {PromptTask: "p", Control: packspec.Ptr(ControlAgent), OnEvent: map[string]string{"Next": "b"}},
				"b": {PromptTask: "p", Control: packspec.Ptr(ControlAgent), OnEvent: map[string]string{"Back": "a"}},
			},
		}
	}

	assertNoLoopWarning := func(t *testing.T, spec *Spec) {
		t.Helper()
		for _, w := range Validate(spec, []string{"p"}).Warnings {
			if contains(w, "agent-controlled cycle") {
				t.Errorf("a declared budget bounds the loop; got %q", w)
			}
		}
	}

	t.Run("max_total_visits bounds it", func(t *testing.T) {
		spec := agentCycle()
		spec.Engine = &packspec.WorkflowConfigEngine{
			Budget: &packspec.WorkflowBudget{MaxTotalVisits: packspec.Ptr(20)},
		}
		assertNoLoopWarning(t, spec)
	})

	t.Run("max_wall_time_sec bounds it", func(t *testing.T) {
		spec := agentCycle()
		spec.Engine = &packspec.WorkflowConfigEngine{
			Budget: &packspec.WorkflowBudget{MaxWallTimeSec: packspec.Ptr(30)},
		}
		assertNoLoopWarning(t, spec)
	})

	t.Run("max_tool_calls alone still warns", func(t *testing.T) {
		// Its per-round counting is #1785, so a pack relying on it alone is
		// not demonstrably bounded.
		spec := agentCycle()
		spec.Engine = &packspec.WorkflowConfigEngine{
			Budget: &packspec.WorkflowBudget{MaxToolCalls: packspec.Ptr(10)},
		}
		assertContains(t, Validate(spec, []string{"p"}).Warnings, "agent-controlled cycle")
	})
}
