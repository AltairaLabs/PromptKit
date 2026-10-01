// Package pipeline provides types and configuration for stage-based pipeline execution.
// The legacy middleware-based pipeline has been removed in favor of the stage architecture.
// See runtime/pipeline/stage for the current implementation.
package pipeline

import (
	"errors"
	"time"

	"github.com/AltairaLabs/PromptKit/runtime/v2/packspec"
	"github.com/AltairaLabs/PromptKit/runtime/v2/tools"
	"github.com/AltairaLabs/PromptKit/runtime/v2/types"
)

// ToolPolicy defines constraints on tool usage.
type ToolPolicy struct {
	ToolChoice           string   `json:"tool_choice,omitempty"` // "auto", "required", "none", or specific tool name
	MaxRounds            int      `json:"max_rounds,omitempty"`
	MaxToolCallsPerTurn  int      `json:"max_tool_calls_per_turn,omitempty"`
	MaxParallelToolCalls int      `json:"max_parallel_tool_calls,omitempty"` // Max concurrent tool executions (default 10)
	MaxCallsPerMinute    int      `json:"max_calls_per_minute,omitempty"`    // Per-tool calls/minute (0=unlimited)
	MaxCostUSD           float64  `json:"max_cost_usd,omitempty"`            // Cost budget in USD (0=unlimited)
	Blocklist            []string `json:"blocklist,omitempty"`
	// MaxIdenticalToolCalls is the maximum number of times the same tool may be called
	// with identical arguments before the loop is aborted as an infinite-loop guard.
	// 0/unset means use the default (3). Never set to unlimited — use a large number instead.
	MaxIdenticalToolCalls int `json:"max_identical_tool_calls,omitempty"`
	// StopOnTool, when non-empty, stops the agent tool loop at the end of any round
	// in which a tool with this name was called (RFC 0010 termination.tool_called).
	StopOnTool string `json:"stop_on_tool,omitempty"`
}

// MergeToolPolicy combines the policy a caller configured for a provider stage
// with the tool_policy declared on the prompt that stage is running.
//
// Where the limits come from. Each layer bounds something different; only the
// first two bound the same thing, which is why they are merged here:
//
//   - prompts.<task>.tool_policy (PromptPack spec) bounds ONE turn of that
//     prompt: max_rounds is LLM → tool → LLM cycles, max_tool_calls_per_turn is
//     tool calls, across all of the turn's rounds.
//   - compositions.<name>.steps[].termination.max_steps (RFC 0010) bounds the
//     LLM-tool loop of ONE agent step — the same unit as max_rounds, not a count
//     of composition steps. The spec requires a composition's step graph to be
//     acyclic, so a composition never re-executes a step and has no "steps
//     executed" limit; its total is the sum of its steps' loops. The composition
//     executor passes max_steps in as the caller's MaxRounds.
//   - workflow.engine.budget (max_tool_calls, max_total_visits,
//     max_wall_time_sec) and per-state max_visits bound the WHOLE workflow run,
//     across states and turns. That is where looping lives (the spec says to
//     encode loops there, not in a composition), and it is enforced by the
//     workflow state machine on top of each turn's limits, never merged here.
//
// The merge only ever narrows: every layer can tighten a limit, none can widen
// one another layer set. So:
//
//   - MaxRounds and MaxToolCallsPerTurn: the smaller of the values that are set
//     (0 means unset). An agent step's max_steps therefore runs until the lower
//     of max_steps and the prompt's max_rounds.
//   - Blocklist: the union.
//   - ToolChoice: the caller's when set, otherwise the prompt's. It is a mode,
//     not a limit, and the caller (a composition step, an Arena scenario) is the
//     more specific context.
//   - Every other field is runtime-only (not in the spec) and comes from the
//     caller unchanged.
//
// Returns caller unchanged when prompt is nil, so the result may be nil.
func MergeToolPolicy(caller *ToolPolicy, prompt *packspec.ToolPolicy) *ToolPolicy {
	if prompt == nil {
		return caller
	}
	merged := ToolPolicy{}
	if caller != nil {
		merged = *caller
	}
	merged.MaxRounds = minPositive(merged.MaxRounds, packspec.Deref(prompt.MaxRounds, 0))
	merged.MaxToolCallsPerTurn = minPositive(merged.MaxToolCallsPerTurn, packspec.Deref(prompt.MaxToolCallsPerTurn, 0))
	if merged.ToolChoice == "" {
		merged.ToolChoice = packspec.Deref(prompt.ToolChoice, "")
	}
	merged.Blocklist = unionStrings(merged.Blocklist, prompt.Blocklist)
	return &merged
}

// minPositive returns the smaller of a and b, treating a non-positive value as
// unset.
func minPositive(a, b int) int {
	switch {
	case a <= 0:
		return b
	case b <= 0:
		return a
	default:
		return min(a, b)
	}
}

// unionStrings returns a followed by the members of b not already in a.
func unionStrings(a, b []string) []string {
	if len(b) == 0 {
		return a
	}
	out := make([]string, 0, len(a)+len(b))
	seen := make(map[string]bool, len(a)+len(b))
	for _, s := range append(append([]string{}, a...), b...) {
		if !seen[s] {
			seen[s] = true
			out = append(out, s)
		}
	}
	return out
}

// PipelineConfig represents the complete pipeline configuration for pack format
type Config struct {
	Stages []string `json:"stages"` // Pipeline stages in order
}

// RetryPolicy defines retry behavior for provider middleware
type RetryPolicy struct {
	MaxRetries     int    `json:"max_retries"`                // Maximum retry attempts
	Backoff        string `json:"backoff"`                    // Backoff strategy ("fixed", "exponential")
	InitialDelayMs int    `json:"initial_delay_ms,omitempty"` // Initial delay in milliseconds
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
	Parts     []types.ContentPart     `json:"parts,omitempty"`
	ToolCalls []types.MessageToolCall `json:"tool_calls,omitempty"`
}

// ExecutionResult is the output of a pipeline execution.
type ExecutionResult struct {
	Messages     []types.Message              `json:"messages"`
	Response     *Response                    `json:"response"`
	Trace        ExecutionTrace               `json:"trace"`
	CostInfo     types.CostInfo               `json:"cost_info"`
	Metadata     map[string]interface{}       `json:"metadata"`
	PendingTools []tools.PendingToolExecution `json:"pending_tools,omitempty"`
}
