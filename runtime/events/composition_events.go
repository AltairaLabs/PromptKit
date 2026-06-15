package events

import "encoding/json"

// Composition events (RFC 0010 observability). Started/completed pairs allow the
// OTel listener to open/close spans; branch and parallel are instantaneous.
const (
	// EventCompositionStarted marks the start of a composition flow execution.
	EventCompositionStarted EventType = "composition.started"
	// EventCompositionCompleted marks the successful or failed completion of a composition flow.
	EventCompositionCompleted EventType = "composition.completed"
	// EventCompositionStepStarted marks the start of an individual step within a composition.
	EventCompositionStepStarted EventType = "composition.step.started"
	// EventCompositionStepCompleted marks the completion (or failure) of an individual composition step.
	EventCompositionStepCompleted EventType = "composition.step.completed"
	// EventCompositionBranchEvaluated marks a branch decision within a composition flow.
	EventCompositionBranchEvaluated EventType = "composition.branch.evaluated"
	// EventCompositionParallelCompleted marks the completion of a parallel fan-out step.
	EventCompositionParallelCompleted EventType = "composition.parallel.completed"
)

// CompositionStartedData is the payload for composition.started events.
type CompositionStartedData struct {
	baseEventData
	// Composition is the name of the composition flow being executed.
	Composition string `json:"composition"`
	// Input is the JSON-encoded input passed to the composition.
	Input json.RawMessage `json:"input,omitempty"`
}

// CompositionCompletedData is the payload for composition.completed events.
type CompositionCompletedData struct {
	baseEventData
	// Composition is the name of the composition flow that completed.
	Composition string `json:"composition"`
	// Output is the JSON-encoded final output of the composition.
	Output json.RawMessage `json:"output,omitempty"`
	// Error is the error message if the composition failed; empty on success.
	Error string `json:"error,omitempty"`
	// DurationMs is the wall-clock duration of the composition in milliseconds.
	DurationMs int64 `json:"duration_ms"`
}

// CompositionStepStartedData is the payload for composition.step.started events.
type CompositionStepStartedData struct {
	baseEventData
	// StepID is the identifier of the step within the composition.
	StepID string `json:"step_id"`
	// Kind is the step type (e.g. "prompt", "tool", "branch", "parallel").
	Kind string `json:"kind"`
	// Input is the JSON-encoded input passed to this step.
	Input json.RawMessage `json:"input,omitempty"`
}

// CompositionStepCompletedData is the payload for composition.step.completed events.
type CompositionStepCompletedData struct {
	baseEventData
	// StepID is the identifier of the step within the composition.
	StepID string `json:"step_id"`
	// Kind is the step type (e.g. "prompt", "tool", "branch", "parallel").
	Kind string `json:"kind"`
	// Input is the JSON-encoded input passed to this step.
	Input json.RawMessage `json:"input,omitempty"`
	// Output is the JSON-encoded output produced by this step.
	Output json.RawMessage `json:"output,omitempty"`
	// Attempt is the 1-based attempt number (>1 indicates a retry).
	Attempt int `json:"attempt"`
	// Error is the error message if the step failed; empty on success.
	Error string `json:"error,omitempty"`
}

// CompositionBranchEvaluatedData is the payload for composition.branch.evaluated events.
type CompositionBranchEvaluatedData struct {
	baseEventData
	// StepID is the identifier of the branch step.
	StepID string `json:"step_id"`
	// Taken is the branch label or target step ID that was selected.
	Taken string `json:"taken"`
}

// CompositionParallelBranch describes a single branch within a parallel fan-out step.
type CompositionParallelBranch struct {
	// ID is the branch identifier.
	ID string `json:"id"`
	// Status is the outcome of this branch (e.g. "complete", "failed", "skipped").
	Status string `json:"status"`
}

// CompositionParallelCompletedData is the payload for composition.parallel.completed events.
type CompositionParallelCompletedData struct {
	baseEventData
	// StepID is the identifier of the parallel fan-out step.
	StepID string `json:"step_id"`
	// Branches contains the per-branch outcomes for the parallel step.
	Branches []CompositionParallelBranch `json:"branches"`
}
