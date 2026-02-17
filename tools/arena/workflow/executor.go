package workflow

import (
	"context"
	"fmt"
	"time"

	asrt "github.com/AltairaLabs/PromptKit/tools/arena/assertions"
)

// Driver is the interface that a workflow engine must implement.
// The SDK's WorkflowConversation satisfies this through an adapter.
type Driver interface {
	// Send sends a user message and returns the assistant response text.
	Send(ctx context.Context, message string) (string, error)

	// Transition triggers a state machine event and returns the new state.
	Transition(event string) (string, error)

	// CurrentState returns the current workflow state name.
	CurrentState() string

	// IsComplete returns true if the workflow reached a terminal state.
	IsComplete() bool

	// Close releases resources.
	Close() error
}

// DriverFactory creates a Driver for a given scenario.
// The factory receives the pack path, variables, and whether to enable context carry-forward.
type DriverFactory func(packPath string, variables map[string]string, carryForward bool) (Driver, error)

// StepResult captures the outcome of a single step execution.
type StepResult struct {
	// Index is the step position (0-based).
	Index int `json:"index"`

	// Type is "input" or "event".
	Type StepType `json:"type"`

	// Response is the assistant text (input steps only).
	Response string `json:"response,omitempty"`

	// State is the workflow state after this step.
	State string `json:"state"`

	// Duration is how long the step took.
	Duration time.Duration `json:"duration"`

	// AssertionResults are the per-step assertion outcomes.
	AssertionResults []asrt.ConversationValidationResult `json:"assertion_results,omitempty"`

	// Error is non-empty if the step failed.
	Error string `json:"error,omitempty"`
}

// Result is the complete outcome of executing a workflow scenario.
type Result struct {
	// ScenarioID identifies which scenario was run.
	ScenarioID string `json:"scenario_id"`

	// Steps contains per-step results.
	Steps []StepResult `json:"steps"`

	// FinalState is the workflow state after all steps.
	FinalState string `json:"final_state"`

	// Duration is the total scenario execution time.
	Duration time.Duration `json:"duration"`

	// Failed is true if any step errored.
	Failed bool `json:"failed"`

	// Error is a summary error message (first failure).
	Error string `json:"error,omitempty"`
}

// Executor runs workflow scenarios against a Driver.
type Executor struct {
	factory DriverFactory
}

// NewExecutor creates a workflow scenario executor with the given driver factory.
func NewExecutor(factory DriverFactory) *Executor {
	return &Executor{factory: factory}
}

// Execute runs a single workflow scenario and returns the result.
func (e *Executor) Execute(ctx context.Context, scenario *Scenario) *Result {
	start := time.Now()

	if err := scenario.Validate(); err != nil {
		return &Result{
			ScenarioID: scenario.ID,
			Failed:     true,
			Error:      err.Error(),
			Duration:   time.Since(start),
		}
	}

	driver, err := e.factory(scenario.Pack, scenario.Variables, scenario.ContextCarryForward)
	if err != nil {
		return &Result{
			ScenarioID: scenario.ID,
			Failed:     true,
			Error:      fmt.Sprintf("failed to create workflow driver: %v", err),
			Duration:   time.Since(start),
		}
	}
	defer driver.Close()

	result := &Result{
		ScenarioID: scenario.ID,
		Steps:      make([]StepResult, 0, len(scenario.Steps)),
	}

	for i := range scenario.Steps {
		stepResult := e.executeStep(ctx, driver, i, &scenario.Steps[i])
		result.Steps = append(result.Steps, stepResult)

		if stepResult.Error != "" {
			result.Failed = true
			result.Error = stepResult.Error
			break
		}
	}

	result.FinalState = driver.CurrentState()
	result.Duration = time.Since(start)
	return result
}

// executeStep dispatches to the correct step handler.
func (e *Executor) executeStep(ctx context.Context, driver Driver, index int, step *Step) StepResult {
	switch step.Type {
	case StepInput:
		return e.executeInputStep(ctx, driver, index, step)
	case StepEvent:
		return e.executeEventStep(driver, index, step)
	default:
		return StepResult{
			Index: index,
			Type:  step.Type,
			State: driver.CurrentState(),
			Error: fmt.Sprintf("unknown step type: %s", step.Type),
		}
	}
}

// executeInputStep sends a message and validates assertions.
func (e *Executor) executeInputStep(ctx context.Context, driver Driver, index int, step *Step) StepResult {
	start := time.Now()

	response, err := driver.Send(ctx, step.Content)
	sr := StepResult{
		Index:    index,
		Type:     StepInput,
		Response: response,
		State:    driver.CurrentState(),
		Duration: time.Since(start),
	}
	if err != nil {
		sr.Error = err.Error()
		return sr
	}

	// Evaluate turn-level assertions against the response
	if len(step.Assertions) > 0 {
		sr.AssertionResults = evaluateAssertions(ctx, step.Assertions, response)
		for _, ar := range sr.AssertionResults {
			if !ar.Passed {
				sr.Error = fmt.Sprintf("assertion %q failed: %s", ar.Type, ar.Message)
				break
			}
		}
	}

	return sr
}

// executeEventStep triggers a transition and checks the expected state.
func (e *Executor) executeEventStep(driver Driver, index int, step *Step) StepResult {
	start := time.Now()

	newState, err := driver.Transition(step.Event)
	sr := StepResult{
		Index:    index,
		Type:     StepEvent,
		State:    newState,
		Duration: time.Since(start),
	}
	if err != nil {
		sr.Error = fmt.Sprintf("transition %q failed: %v", step.Event, err)
		return sr
	}

	if step.ExpectState != "" && newState != step.ExpectState {
		sr.Error = fmt.Sprintf("expected state %q but got %q", step.ExpectState, newState)
	}

	return sr
}
