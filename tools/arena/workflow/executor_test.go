package workflow

import (
	"context"
	"errors"
	"testing"

)

// mockDriver implements WorkflowDriver for testing.
type mockDriver struct {
	state     string
	responses []string
	sendIdx   int
	sendErr   error
	transErr  error
	states    map[string]string // event -> new state
	terminal  bool
	closed    bool
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

func (m *mockDriver) Transition(event string) (string, error) {
	if m.transErr != nil {
		return "", m.transErr
	}
	if newState, ok := m.states[event]; ok {
		m.state = newState
		return newState, nil
	}
	return "", errors.New("invalid event: " + event)
}

func (m *mockDriver) CurrentState() string { return m.state }
func (m *mockDriver) IsComplete() bool     { return m.terminal }
func (m *mockDriver) Close() error         { m.closed = true; return nil }

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
		states:    map[string]string{"Escalate": "specialist"},
	}

	scenario := &Scenario{
		ID:   "support-flow",
		Pack: "./support.pack.json",
		Steps: []Step{
			{Type: StepInput, Content: "I need help with billing"},
			{Type: StepEvent, Event: "Escalate", ExpectState: "specialist"},
			{Type: StepInput, Content: "My invoice is wrong"},
		},
	}

	exec := NewExecutor(newMockFactory(driver, nil))
	result := exec.Execute(context.Background(), scenario)

	if result.Failed {
		t.Fatalf("expected success, got error: %s", result.Error)
	}
	if len(result.Steps) != 3 {
		t.Fatalf("expected 3 steps, got %d", len(result.Steps))
	}
	if result.Steps[0].Response != "I can help with billing" {
		t.Fatalf("unexpected response: %s", result.Steps[0].Response)
	}
	if result.Steps[1].State != "specialist" {
		t.Fatalf("expected state 'specialist', got %q", result.Steps[1].State)
	}
	if result.Steps[2].Response != "Looking at your invoice" {
		t.Fatalf("unexpected response: %s", result.Steps[2].Response)
	}
	if result.FinalState != "specialist" {
		t.Fatalf("expected final state 'specialist', got %q", result.FinalState)
	}
	if !driver.closed {
		t.Fatal("expected driver to be closed")
	}
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

func TestExecutor_Execute_TransitionError(t *testing.T) {
	t.Parallel()

	driver := &mockDriver{
		state:  "intake",
		states: map[string]string{},
	}

	scenario := &Scenario{
		ID:   "test",
		Pack: "./test.pack.json",
		Steps: []Step{
			{Type: StepEvent, Event: "Unknown"},
		},
	}

	exec := NewExecutor(newMockFactory(driver, nil))
	result := exec.Execute(context.Background(), scenario)

	if !result.Failed {
		t.Fatal("expected failure for invalid transition")
	}
}

func TestExecutor_Execute_ExpectStateMismatch(t *testing.T) {
	t.Parallel()

	driver := &mockDriver{
		state:  "intake",
		states: map[string]string{"Next": "processing"},
	}

	scenario := &Scenario{
		ID:   "test",
		Pack: "./test.pack.json",
		Steps: []Step{
			{Type: StepEvent, Event: "Next", ExpectState: "specialist"},
		},
	}

	exec := NewExecutor(newMockFactory(driver, nil))
	result := exec.Execute(context.Background(), scenario)

	if !result.Failed {
		t.Fatal("expected failure for state mismatch")
	}
	if result.Steps[0].State != "processing" {
		t.Fatalf("expected state 'processing', got %q", result.Steps[0].State)
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
