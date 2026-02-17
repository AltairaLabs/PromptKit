package workflow

import (
	"testing"
)

func TestScenario_Validate_Valid(t *testing.T) {
	t.Parallel()
	s := &Scenario{
		ID:   "test",
		Pack: "./test.pack.json",
		Steps: []Step{
			{Type: StepInput, Content: "hello"},
			{Type: StepEvent, Event: "Next"},
		},
	}
	if err := s.Validate(); err != nil {
		t.Fatalf("expected valid, got: %v", err)
	}
}

func TestScenario_Validate_MissingID(t *testing.T) {
	t.Parallel()
	s := &Scenario{Pack: "x.pack.json", Steps: []Step{{Type: StepInput, Content: "hi"}}}
	if err := s.Validate(); err != ErrMissingID {
		t.Fatalf("expected ErrMissingID, got: %v", err)
	}
}

func TestScenario_Validate_MissingPack(t *testing.T) {
	t.Parallel()
	s := &Scenario{ID: "x", Steps: []Step{{Type: StepInput, Content: "hi"}}}
	if err := s.Validate(); err != ErrMissingPack {
		t.Fatalf("expected ErrMissingPack, got: %v", err)
	}
}

func TestScenario_Validate_NoSteps(t *testing.T) {
	t.Parallel()
	s := &Scenario{ID: "x", Pack: "x.pack.json"}
	if err := s.Validate(); err != ErrNoSteps {
		t.Fatalf("expected ErrNoSteps, got: %v", err)
	}
}

func TestScenario_Validate_InputMissingContent(t *testing.T) {
	t.Parallel()
	s := &Scenario{
		ID:    "x",
		Pack:  "x.pack.json",
		Steps: []Step{{Type: StepInput}},
	}
	err := s.Validate()
	if err == nil {
		t.Fatal("expected error for input step without content")
	}
	stepErr, ok := err.(*StepError)
	if !ok {
		t.Fatalf("expected *StepError, got %T: %v", err, err)
	}
	if stepErr.Index != 0 {
		t.Fatalf("expected step index 0, got %d", stepErr.Index)
	}
}

func TestScenario_Validate_EventMissingEvent(t *testing.T) {
	t.Parallel()
	s := &Scenario{
		ID:    "x",
		Pack:  "x.pack.json",
		Steps: []Step{{Type: StepEvent}},
	}
	err := s.Validate()
	if err == nil {
		t.Fatal("expected error for event step without event name")
	}
}

func TestScenario_Validate_UnknownStepType(t *testing.T) {
	t.Parallel()
	s := &Scenario{
		ID:    "x",
		Pack:  "x.pack.json",
		Steps: []Step{{Type: "unknown"}},
	}
	err := s.Validate()
	if err == nil {
		t.Fatal("expected error for unknown step type")
	}
}

func TestStepError_Error(t *testing.T) {
	t.Parallel()
	e := &StepError{Index: 2, Msg: "bad step"}
	want := "workflow scenario step 2: bad step"
	if got := e.Error(); got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}
