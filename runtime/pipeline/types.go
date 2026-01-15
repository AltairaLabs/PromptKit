// Package pipeline provides types and configuration for stage-based pipeline execution.
// The legacy middleware-based pipeline has been removed in favor of the stage architecture.
// See runtime/pipeline/stage for the current implementation.
package pipeline

import (
	"errors"
	"time"

	"github.com/AltairaLabs/PromptKit/runtime/types"
)

// ToolPolicy defines constraints on tool usage.
type ToolPolicy struct {
	ToolChoice          string   `json:"tool_choice,omitempty"` // "auto", "required", "none", or specific tool name
	MaxRounds           int      `json:"max_rounds,omitempty"`
	MaxToolCallsPerTurn int      `json:"max_tool_calls_per_turn,omitempty"`
	Blocklist           []string `json:"blocklist,omitempty"`
}

// PipelineConfig represents the complete pipeline configuration for pack format
type Config struct {
	Stages     []string           `json:"stages"`               // Pipeline stages in order
	Middleware []MiddlewareConfig `json:"middleware,omitempty"` // Deprecated: for backward compatibility only
}

// MiddlewareConfig represents configuration for a specific middleware (deprecated)
type MiddlewareConfig struct {
	Type   string                 `json:"type"`             // Middleware type
	Config map[string]interface{} `json:"config,omitempty"` // Type-specific configuration
}

// RetryPolicy defines retry behavior for provider middleware
type RetryPolicy struct {
	MaxRetries     int    `json:"max_retries"`                // Maximum retry attempts
	Backoff        string `json:"backoff"`                    // Backoff strategy ("fixed", "exponential")
	InitialDelayMs int    `json:"initial_delay_ms,omitempty"` // Initial delay in milliseconds
}

// TemplateMiddlewareConfig contains configuration for template middleware
type TemplateMiddlewareConfig struct {
	StrictMode     bool `json:"strict_mode"`     // Fail on undefined variables
	AllowUndefined bool `json:"allow_undefined"` // Allow undefined variables
}

// ProviderMiddlewareConfig contains configuration for provider middleware
type ProviderMiddlewareConfig struct {
	RetryPolicy *RetryPolicy `json:"retry_policy,omitempty"` // Retry policy
	TimeoutMs   int          `json:"timeout_ms,omitempty"`   // Request timeout in milliseconds
}

// ValidatorMiddlewareConfig contains configuration for validator middleware
type ValidatorMiddlewareConfig struct {
	FailFast         bool `json:"fail_fast"`          // Stop on first validation error
	CollectAllErrors bool `json:"collect_all_errors"` // Collect all errors before failing
}

// ExecutionTrace captures the complete execution history of a pipeline run.
type ExecutionTrace struct {
	LLMCalls    []LLMCall    `json:"llm_calls"`              // All LLM API calls made during execution
	Events      []TraceEvent `json:"events,omitempty"`       // Other trace events
	StartedAt   time.Time    `json:"started_at"`             // When pipeline execution started
	CompletedAt *time.Time   `json:"completed_at,omitempty"` // When pipeline execution completed
}

// LLMCall represents a single LLM API call within a pipeline execution.
type LLMCall struct {
	Sequence     int                     `json:"sequence"`               // Call number in sequence
	MessageIndex int                     `json:"message_index"`          // Index into messages array
	Request      interface{}             `json:"request,omitempty"`      // Raw request (if debugging enabled)
	Response     interface{}             `json:"response"`               // Parsed response
	RawResponse  interface{}             `json:"raw_response,omitempty"` // Raw provider response
	StartedAt    time.Time               `json:"started_at"`             // When call started
	Duration     time.Duration           `json:"duration"`               // How long the call took
	Cost         types.CostInfo          `json:"cost"`                   // Cost information for this call
	ToolCalls    []types.MessageToolCall `json:"tool_calls,omitempty"`   // If this call triggered tool execution
	Error        *string                 `json:"error,omitempty"`        // Error message if the call failed
}

// SetError sets the error for this LLM call from an error value.
func (l *LLMCall) SetError(err error) {
	if err != nil {
		errMsg := err.Error()
		l.Error = &errMsg
	} else {
		l.Error = nil
	}
}

// GetError returns the error as an error type, or nil if no error occurred.
func (l *LLMCall) GetError() error {
	if l.Error == nil {
		return nil
	}
	return errors.New(*l.Error)
}

// TraceEvent represents a significant event during pipeline execution.
type TraceEvent struct {
	Type      string      `json:"type"`              // Event type
	Timestamp time.Time   `json:"timestamp"`         // When the event occurred
	Data      interface{} `json:"data"`              // Event-specific data
	Message   string      `json:"message,omitempty"` // Human-readable description
}

// StateStoreConfig contains configuration for state store middleware
type StateStoreConfig struct {
	Store          interface{}            // State store implementation (statestore.Store)
	ConversationID string                 // Unique conversation identifier
	UserID         string                 // User identifier (optional)
	Metadata       map[string]interface{} // Additional metadata to store (optional)
}

// ValidationError represents a validation failure.
type ValidationError struct {
	Type     string                   `json:"type"`
	Details  string                   `json:"details"`
	Failures []types.ValidationResult `json:"failures"` // All failed validations
}

// Error returns the error message for this validation error.
func (e *ValidationError) Error() string {
	return e.Type + ": " + e.Details
}

// Response represents the output from a pipeline execution.
type Response struct {
	Role      string                  `json:"role"`
	Content   string                  `json:"content"`
	ToolCalls []types.MessageToolCall `json:"tool_calls,omitempty"`
}

// ExecutionResult is the output of a pipeline execution.
type ExecutionResult struct {
	Messages []types.Message        `json:"messages"`  // All messages including history and responses
	Response *Response              `json:"response"`  // The final response
	Trace    ExecutionTrace         `json:"trace"`     // Complete execution trace with all LLM calls
	CostInfo types.CostInfo         `json:"cost_info"` // Aggregate cost across all LLM calls
	Metadata map[string]interface{} `json:"metadata"`  // Metadata populated by stages
}
