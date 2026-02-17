package workflow

import (
	"context"
	"errors"
	"testing"

	asrt "github.com/AltairaLabs/PromptKit/tools/arena/assertions"
)

// mockDriver implements Driver for testing.
type mockDriver struct {
	state           string
	responses       []string
	sendIdx         int
	sendErr         error
	lastTransitions []TransitionRecord // transitions to return from Transitions()
	terminal        bool
	closed          bool
}

func (m *mockDriver) Send(_ context.Context, _ string) (string, error) {
	if m.sendErr != nil {
		return "", m.sendErr
	}
	resp := ""
	if m.sendIdx < len(m.responses) {
		resp = m.responses[m.sendIdx]
		m.sendIdx++
	}
	return resp, nil
}

func (m *mockDriver) Transitions() []TransitionRecord { return m.lastTransitions }
func (m *mockDriver) CurrentState() string            { return m.state }
func (m *mockDriver) IsComplete() bool                { return m.terminal }
func (m *mockDriver) Close() error                    { m.closed = true; return nil }

func newMockFactory(driver *mockDriver, err error) DriverFactory {
	return func(string, map[string]string, bool) (Driver, error) {
		if err != nil {
			return nil, err
		}
		return driver, nil
	}
}

func TestExecutor_Execute_FullScenario(t *testing.T) {
	t.Parallel()

	driver := &mockDriver{
		state:     "intake",
		responses: []string{"I can help with billing", "Looking at your invoice"},
	}

	scenario := &Scenario{
		ID:   "support-flow",
		Pack: "./support.pack.json",
		Steps: []Step{
			{Type: StepInput, Content: "I need help with billing"},
			{Type: StepInput, Content: "My invoice is wrong"},
		},
	}

	exec := NewExecutor(newMockFactory(driver, nil))
	result := exec.Execute(context.Background(), scenario)

	if result.Failed {
		t.Fatalf("expected success, got error: %s", result.Error)
	}
	if len(result.Steps) != 2 {
		t.Fatalf("expected 2 steps, got %d", len(result.Steps))
	}
	if result.Steps[0].Response != "I can help with billing" {
		t.Fatalf("unexpected response: %s", result.Steps[0].Response)
	}
	if result.Steps[1].Response != "Looking at your invoice" {
		t.Fatalf("unexpected response: %s", result.Steps[1].Response)
	}
	if result.FinalState != "intake" {
		t.Fatalf("expected final state 'intake', got %q", result.FinalState)
	}
	if !driver.closed {
		t.Fatal("expected driver to be closed")
	}
}

func TestExecutor_Execute_WithTransitions(t *testing.T) {
	t.Parallel()

	callCount := 0
	driver := &mockDriver{
		state:     "intake",
		responses: []string{"I can help", "Escalating now", "Specialist here"},
	}

	// Override Send to simulate transitions on the second call
	origSend := driver.Send
	_ = origSend
	factory := func(string, map[string]string, bool) (Driver, error) {
		return &transitionMockDriver{
			mockDriver:  driver,
			callCount:   &callCount,
			transitions: []TransitionRecord{{From: "intake", To: "specialist", Event: "Escalate", Context: "billing issue"}},
		}, nil
	}

	scenario := &Scenario{
		ID:   "transition-test",
		Pack: "./test.pack.json",
		Steps: []Step{
			{Type: StepInput, Content: "help with billing"},
			{Type: StepInput, Content: "escalate please"},
			{Type: StepInput, Content: "specialist question"},
		},
	}

	exec := NewExecutor(factory)
	result := exec.Execute(context.Background(), scenario)

	if result.Failed {
		t.Fatalf("expected success, got error: %s", result.Error)
	}
	if len(result.Steps) != 3 {
		t.Fatalf("expected 3 steps, got %d", len(result.Steps))
	}

	// Verify transitions were accumulated by the executor
	if len(exec.transitions) != 1 {
		t.Fatalf("expected 1 transition, got %d", len(exec.transitions))
	}
	if exec.transitions[0].Event != "Escalate" {
		t.Fatalf("expected event 'Escalate', got %q", exec.transitions[0].Event)
	}
	if exec.transitions[0].Context != "billing issue" {
		t.Fatalf("expected context 'billing issue', got %q", exec.transitions[0].Context)
	}
}

// transitionMockDriver returns transitions on the second Send() call.
type transitionMockDriver struct {
	*mockDriver
	callCount   *int
	transitions []TransitionRecord
}

func (m *transitionMockDriver) Send(ctx context.Context, msg string) (string, error) {
	*m.callCount++
	return m.mockDriver.Send(ctx, msg)
}

func (m *transitionMockDriver) Transitions() []TransitionRecord {
	if *m.callCount == 2 {
		return m.transitions
	}
	return nil
}

func TestExecutor_Execute_InvalidScenario(t *testing.T) {
	t.Parallel()

	scenario := &Scenario{ID: "", Pack: ""} // missing required fields
	exec := NewExecutor(newMockFactory(nil, nil))
	result := exec.Execute(context.Background(), scenario)

	if !result.Failed {
		t.Fatal("expected failure for invalid scenario")
	}
}

func TestExecutor_Execute_DriverFactoryError(t *testing.T) {
	t.Parallel()

	scenario := &Scenario{
		ID:    "test",
		Pack:  "./test.pack.json",
		Steps: []Step{{Type: StepInput, Content: "hi"}},
	}

	exec := NewExecutor(newMockFactory(nil, errors.New("factory boom")))
	result := exec.Execute(context.Background(), scenario)

	if !result.Failed {
		t.Fatal("expected failure")
	}
	if result.Error == "" {
		t.Fatal("expected error message")
	}
}

func TestExecutor_Execute_SendError(t *testing.T) {
	t.Parallel()

	driver := &mockDriver{
		state:   "intake",
		sendErr: errors.New("send failed"),
	}

	scenario := &Scenario{
		ID:    "test",
		Pack:  "./test.pack.json",
		Steps: []Step{{Type: StepInput, Content: "hi"}},
	}

	exec := NewExecutor(newMockFactory(driver, nil))
	result := exec.Execute(context.Background(), scenario)

	if !result.Failed {
		t.Fatal("expected failure")
	}
	if len(result.Steps) != 1 {
		t.Fatalf("expected 1 step, got %d", len(result.Steps))
	}
	if result.Steps[0].Error == "" {
		t.Fatal("expected step error")
	}
}

func TestExecutor_Execute_EventStepRejected(t *testing.T) {
	t.Parallel()

	scenario := &Scenario{
		ID:   "test",
		Pack: "./test.pack.json",
		Steps: []Step{
			{Type: StepEvent, Event: "Escalate"},
		},
	}

	exec := NewExecutor(newMockFactory(nil, nil))
	result := exec.Execute(context.Background(), scenario)

	if !result.Failed {
		t.Fatal("expected failure for event step")
	}
}

func TestExecutor_Execute_InputNoAssertions(t *testing.T) {
	t.Parallel()

	driver := &mockDriver{
		state:     "intake",
		responses: []string{"I can help with billing"},
	}

	scenario := &Scenario{
		ID:   "test-no-assertions",
		Pack: "./test.pack.json",
		Steps: []Step{
			{Type: StepInput, Content: "help with billing"},
		},
	}

	exec := NewExecutor(newMockFactory(driver, nil))
	result := exec.Execute(context.Background(), scenario)

	if result.Failed {
		t.Fatalf("expected success, got error: %s", result.Error)
	}
	if result.Steps[0].Response != "I can help with billing" {
		t.Fatalf("unexpected response: %s", result.Steps[0].Response)
	}
}

func TestExecutor_Execute_UnknownStepType(t *testing.T) {
	t.Parallel()

	// This tests the default branch in executeStep. We bypass validation by
	// constructing the executor manually and calling executeStep directly.
	driver := &mockDriver{state: "s"}
	exec := &Executor{}
	step := &Step{Type: "bogus"}
	sr := exec.executeStep(context.Background(), driver, 0, step)
	if sr.Error == "" {
		t.Fatal("expected error for unknown step type")
	}
}

func TestExecutor_Execute_WithAssertions_StateIs(t *testing.T) {
	t.Parallel()

	driver := &mockDriver{
		state:     "processing",
		responses: []string{"I'm working on it"},
	}

	scenario := &Scenario{
		ID:   "test-assertions",
		Pack: "./test.pack.json",
		Steps: []Step{
			{
				Type:    StepInput,
				Content: "what state are we in?",
				Assertions: []asrt.AssertionConfig{
					{Type: "state_is", Params: map[string]interface{}{"state": "processing"}},
				},
			},
		},
	}

	exec := NewExecutor(newMockFactory(driver, nil))
	result := exec.Execute(context.Background(), scenario)

	if result.Failed {
		t.Fatalf("expected success, got error: %s", result.Error)
	}
	if len(result.Steps[0].AssertionResults) != 1 {
		t.Fatalf("expected 1 assertion result, got %d", len(result.Steps[0].AssertionResults))
	}
	if !result.Steps[0].AssertionResults[0].Passed {
		t.Fatalf("expected assertion to pass: %s", result.Steps[0].AssertionResults[0].Message)
	}
}

func TestExecutor_Execute_WithAssertions_Failure(t *testing.T) {
	t.Parallel()

	driver := &mockDriver{
		state:     "intake",
		responses: []string{"hello"},
	}

	scenario := &Scenario{
		ID:   "test-assertion-fail",
		Pack: "./test.pack.json",
		Steps: []Step{
			{
				Type:    StepInput,
				Content: "check state",
				Assertions: []asrt.AssertionConfig{
					{Type: "state_is", Params: map[string]interface{}{"state": "done"}},
				},
			},
		},
	}

	exec := NewExecutor(newMockFactory(driver, nil))
	result := exec.Execute(context.Background(), scenario)

	if !result.Failed {
		t.Fatal("expected failure for assertion mismatch")
	}
	if result.Steps[0].AssertionResults[0].Passed {
		t.Fatal("expected assertion to fail")
	}
}

func TestExecutor_Execute_StopsOnFirstError(t *testing.T) {
	t.Parallel()

	driver := &mockDriver{
		state:   "intake",
		sendErr: errors.New("boom"),
	}

	scenario := &Scenario{
		ID:   "test",
		Pack: "./test.pack.json",
		Steps: []Step{
			{Type: StepInput, Content: "first"},
			{Type: StepInput, Content: "second"},
		},
	}

	exec := NewExecutor(newMockFactory(driver, nil))
	result := exec.Execute(context.Background(), scenario)

	if len(result.Steps) != 1 {
		t.Fatalf("expected execution to stop after first error, got %d steps", len(result.Steps))
	}
}
