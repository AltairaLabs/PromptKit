package events

import "time"

// EventType identifies the type of event emitted by the runtime.
type EventType string

const (
	// EventPipelineStarted marks pipeline start.
	EventPipelineStarted EventType = "pipeline.started"
	// EventPipelineCompleted marks pipeline completion.
	EventPipelineCompleted EventType = "pipeline.completed"
	// EventPipelineFailed marks pipeline failure.
	EventPipelineFailed EventType = "pipeline.failed"

	// EventMiddlewareStarted marks middleware start.
	EventMiddlewareStarted EventType = "middleware.started"
	// EventMiddlewareCompleted marks middleware completion.
	EventMiddlewareCompleted EventType = "middleware.completed"
	// EventMiddlewareFailed marks middleware failure.
	EventMiddlewareFailed EventType = "middleware.failed"

	// EventProviderCallStarted marks provider call start.
	EventProviderCallStarted EventType = "provider.call.started"
	// EventProviderCallCompleted marks provider call completion.
	EventProviderCallCompleted EventType = "provider.call.completed"
	// EventProviderCallFailed marks provider call failure.
	EventProviderCallFailed EventType = "provider.call.failed"

	// EventToolCallStarted marks tool call start.
	EventToolCallStarted EventType = "tool.call.started"
	// EventToolCallCompleted marks tool call completion.
	EventToolCallCompleted EventType = "tool.call.completed"
	// EventToolCallFailed marks tool call failure.
	EventToolCallFailed EventType = "tool.call.failed"

	// EventValidationStarted marks validation start.
	EventValidationStarted EventType = "validation.started"
	// EventValidationPassed marks validation success.
	EventValidationPassed EventType = "validation.passed"
	// EventValidationFailed marks validation failure.
	EventValidationFailed EventType = "validation.failed"

	// EventContextBuilt marks context creation.
	EventContextBuilt EventType = "context.built"
	// EventTokenBudgetExceeded marks token budget overflow.
	EventTokenBudgetExceeded EventType = "context.token_budget_exceeded"
	// EventStateLoaded marks state load.
	EventStateLoaded EventType = "state.loaded"
	// EventStateSaved marks state save.
	EventStateSaved EventType = "state.saved"

	// EventStreamInterrupted marks a stream interruption.
	EventStreamInterrupted EventType = "stream.interrupted"
)

// EventData is a marker interface for event payloads.
type EventData interface {
	eventData()
}

// Event represents a runtime event delivered to listeners.
type Event struct {
	Type           EventType
	Timestamp      time.Time
	RunID          string
	SessionID      string
	ConversationID string
	Data           EventData
}

// baseEventData provides a shared marker implementation for all event payloads.
type baseEventData struct{}

func (baseEventData) eventData() {
	// marker method to satisfy EventData
}

// PipelineStartedData contains data for pipeline start events.
type PipelineStartedData struct {
	baseEventData
	MiddlewareCount int
}

// PipelineCompletedData contains data for pipeline completion events.
type PipelineCompletedData struct {
	baseEventData
	Duration     time.Duration
	TotalCost    float64
	InputTokens  int
	OutputTokens int
	MessageCount int
}

// PipelineFailedData contains data for pipeline failure events.
type PipelineFailedData struct {
	baseEventData
	Error    error
	Duration time.Duration
}

// MiddlewareStartedData contains data for middleware start events.
type MiddlewareStartedData struct {
	baseEventData
	Name  string
	Index int
}

// MiddlewareCompletedData contains data for middleware completion events.
type MiddlewareCompletedData struct {
	baseEventData
	Name     string
	Index    int
	Duration time.Duration
}

// MiddlewareFailedData contains data for middleware failure events.
type MiddlewareFailedData struct {
	baseEventData
	Name     string
	Index    int
	Error    error
	Duration time.Duration
}

// ProviderCallStartedData contains data for provider call start events.
type ProviderCallStartedData struct {
	baseEventData
	Provider     string
	Model        string
	MessageCount int
	ToolCount    int
}

// ProviderCallCompletedData contains data for provider call completion events.
type ProviderCallCompletedData struct {
	baseEventData
	Provider      string
	Model         string
	Duration      time.Duration
	InputTokens   int
	OutputTokens  int
	CachedTokens  int
	Cost          float64
	FinishReason  string
	ToolCallCount int
}

// ProviderCallFailedData contains data for provider call failure events.
type ProviderCallFailedData struct {
	baseEventData
	Provider string
	Model    string
	Error    error
	Duration time.Duration
}

// ToolCallStartedData contains data for tool call start events.
type ToolCallStartedData struct {
	baseEventData
	ToolName string
	CallID   string
	Args     map[string]interface{}
}

// ToolCallCompletedData contains data for tool call completion events.
type ToolCallCompletedData struct {
	baseEventData
	ToolName string
	CallID   string
	Duration time.Duration
	Status   string // e.g. "success", "error", "pending"
}

// ToolCallFailedData contains data for tool call failure events.
type ToolCallFailedData struct {
	baseEventData
	ToolName string
	CallID   string
	Error    error
	Duration time.Duration
}

// ValidationStartedData contains data for validation start events.
type ValidationStartedData struct {
	baseEventData
	ValidatorName string
	ValidatorType string // e.g. "input", "output", "semantic"
}

// ValidationPassedData contains data for validation success events.
type ValidationPassedData struct {
	baseEventData
	ValidatorName string
	ValidatorType string
	Duration      time.Duration
}

// ValidationFailedData contains data for validation failure events.
type ValidationFailedData struct {
	baseEventData
	ValidatorName string
	ValidatorType string
	Error         error
	Duration      time.Duration
	Violations    []string
}

// ContextBuiltData contains data for context building events.
type ContextBuiltData struct {
	baseEventData
	MessageCount int
	TokenCount   int
	TokenBudget  int
	Truncated    bool
}

// TokenBudgetExceededData contains data for token budget exceeded events.
type TokenBudgetExceededData struct {
	baseEventData
	RequiredTokens int
	Budget         int
	Excess         int
}

// StateLoadedData contains data for state load events.
type StateLoadedData struct {
	baseEventData
	ConversationID string
	MessageCount   int
}

// StateSavedData contains data for state save events.
type StateSavedData struct {
	baseEventData
	ConversationID string
	MessageCount   int
}

// StreamInterruptedData contains data for stream interruption events.
type StreamInterruptedData struct {
	baseEventData
	Reason string
}

// CustomEventData allows middleware to emit arbitrary structured events.
type CustomEventData struct {
	baseEventData
	MiddlewareName string
	EventName      string
	Data           map[string]interface{}
	Message        string
}
