package pipeline

import (
	"context"
	"time"

	"github.com/AltairaLabs/PromptKit/runtime/providers"
	"github.com/AltairaLabs/PromptKit/runtime/tools"
	"github.com/AltairaLabs/PromptKit/runtime/types"
)

// ExecutionContext is the execution state passed through the middleware chain.
// It contains all the data needed for pipeline execution and is modified by middleware.
type ExecutionContext struct {
	// Context for cancellation, deadlines, and request-scoped values
	Context context.Context

	// State (mutable by middleware)
	SystemPrompt     string                    // Populated by PromptAssemblyMiddleware
	Variables        map[string]string         // Populated by PromptAssemblyMiddleware and ContextExtractionMiddleware
	AllowedTools     []string                  // Populated by PromptAssemblyMiddleware
	Messages         []types.Message           // Conversation history + current messages
	Tools            []types.ToolDef           // Available tool definitions
	ToolResults      []types.MessageToolResult // Executed tool results
	PendingToolCalls []types.MessageToolCall   // Tool calls awaiting external completion (human-in-the-loop)
	Prompt           string                    // Assembled prompt (after variable substitution by TemplateMiddleware)

	// Output (populated by middleware)
	Trace       ExecutionTrace // Complete trace of all LLM calls and events
	Response    *Response      // Convenience pointer to the most recent response (= Trace.LLMCalls[len-1].Response)
	RawResponse interface{}    // Convenience pointer to most recent raw response (= Trace.LLMCalls[len-1].RawResponse)

	// Error tracking (middleware can check this to see if an error occurred earlier in the chain)
	Error error // First error encountered during execution (subsequent middleware still run)

	// Metadata (for passing data between middleware)
	Metadata map[string]interface{}

	// Cost tracking (aggregate across all calls)
	CostInfo types.CostInfo

	// Streaming support
	StreamMode        bool                       // If true, use streaming execution
	StreamOutput      chan providers.StreamChunk // Output channel for streaming chunks
	StreamInterrupted bool                       // Set to true by middleware to stop streaming
	InterruptReason   string                     // Reason for interruption

	// Internal: handler for processing chunks through middleware (set by Pipeline)
	streamChunkHandler func(*providers.StreamChunk) error
}

// InterruptStream interrupts the stream with the given reason.
// Middleware should call this to stop streaming when validation fails, rate limits are hit, etc.
func (ctx *ExecutionContext) InterruptStream(reason string) {
	ctx.StreamInterrupted = true
	ctx.InterruptReason = reason
}

// AddPendingToolCall adds a tool call to the pending list.
// Used by middleware when a tool returns ToolStatusPending.
func (ctx *ExecutionContext) AddPendingToolCall(toolCall types.MessageToolCall) {
	ctx.PendingToolCalls = append(ctx.PendingToolCalls, toolCall)
}

// HasPendingToolCalls returns true if there are any pending tool calls.
func (ctx *ExecutionContext) HasPendingToolCalls() bool {
	return len(ctx.PendingToolCalls) > 0
}

// GetPendingToolCall retrieves a pending tool call by ID.
// Returns nil if not found.
func (ctx *ExecutionContext) GetPendingToolCall(id string) *types.MessageToolCall {
	for i := range ctx.PendingToolCalls {
		if ctx.PendingToolCalls[i].ID == id {
			return &ctx.PendingToolCalls[i]
		}
	}
	return nil
}

// RemovePendingToolCall removes a tool call from the pending list by ID.
// Returns true if the tool call was found and removed.
func (ctx *ExecutionContext) RemovePendingToolCall(id string) bool {
	for i, tc := range ctx.PendingToolCalls {
		if tc.ID == id {
			ctx.PendingToolCalls = append(ctx.PendingToolCalls[:i], ctx.PendingToolCalls[i+1:]...)
			return true
		}
	}
	return false
}

// ClearPendingToolCalls removes all pending tool calls.
func (ctx *ExecutionContext) ClearPendingToolCalls() {
	ctx.PendingToolCalls = nil
}

// EmitStreamChunk emits a stream chunk to the output channel.
// Returns false if the stream has been interrupted or the channel is closed.
// Middleware that produces chunks should check the return value to know when to stop.
func (ctx *ExecutionContext) EmitStreamChunk(chunk providers.StreamChunk) bool {
	if ctx.StreamInterrupted {
		return false
	}

	// Run chunk through middleware StreamChunk() hooks if handler is set
	if ctx.streamChunkHandler != nil {
		if err := ctx.streamChunkHandler(&chunk); err != nil {
			// Error from middleware - don't emit chunk
			return false
		}
		// Check if middleware interrupted the stream
		if ctx.StreamInterrupted {
			return false
		}
	}

	select {
	case ctx.StreamOutput <- chunk:
		return true
	case <-ctx.Context.Done():
		return false
	}
}

// IsStreaming returns true if the execution context is in streaming mode.
func (ctx *ExecutionContext) IsStreaming() bool {
	return ctx.StreamMode
}

// Middleware defines the execution interface for pipeline steps.
//
// Middleware executes in a nested chain where each middleware explicitly calls next()
// to continue the pipeline. This makes the execution flow clear and explicit.
//
// Given middleware chain: [A, B, C]
// Execution order is:
//
//	A.Process(ctx, func() {
//	  return B.Process(ctx, func() {
//	    return C.Process(ctx, func() {
//	      return nil // End of chain
//	    })
//	  })
//	})
//
// Example implementation:
//
//	func (m *ProviderMiddleware) Process(ctx *ExecutionContext, next func() error) error {
//	  // Setup/processing logic
//	  response, err := m.provider.Generate(ctx)
//	  if err != nil {
//	    return err
//	  }
//	  ctx.Response = response
//
//	  // Continue to next middleware
//	  if err := next(); err != nil {
//	    return err
//	  }
//
//	  // Optional cleanup logic
//	  return nil
//	}
//
// Error Handling:
//   - If Process() returns an error, the error is captured in ExecutionContext.Error
//   - Errors stop the chain - subsequent middleware do not execute
//   - Middleware can check ExecutionContext.Error to see if earlier steps failed
//
// ExecutionContext is used internally by middleware but users should not create it directly.
type Middleware interface {
	Process(ctx *ExecutionContext, next func() error) error
	// StreamChunk is called for each chunk during streaming execution (if StreamMode is true).
	// Middleware can inspect, validate, or modify chunks. Return an error or call ctx.InterruptStream()
	// to stop streaming. Most middleware should return nil (no-op).
	StreamChunk(ctx *ExecutionContext, chunk *providers.StreamChunk) error
}

// Response represents the final output from a pipeline execution.
type Response struct {
	Role          string
	Content       string
	ToolCalls     []types.MessageToolCall
	FinalResponse string // If tools were used, this is the final response after tools
	Metadata      ResponseMetadata
}

// ResponseMetadata contains metadata about the response.
type ResponseMetadata struct {
	Provider     string
	Model        string
	Latency      time.Duration
	TokensInput  int
	TokensOutput int
	Cost         float64
}

// ExecutionConfig contains configuration for pipeline execution.
type ExecutionConfig struct {
	Provider     providers.Provider
	ToolRegistry *tools.Registry
	Temperature  float32
	MaxTokens    int
	Seed         *int
	ToolPolicy   *ToolPolicy
}

// ToolPolicy defines constraints on tool usage.
type ToolPolicy struct {
	ToolChoice          string   `json:"tool_choice,omitempty"` // "auto", "required", "none", or specific tool name
	MaxRounds           int      `json:"max_rounds,omitempty"`
	MaxToolCallsPerTurn int      `json:"max_tool_calls_per_turn,omitempty"`
	Blocklist           []string `json:"blocklist,omitempty"`
}

// PipelineConfig represents the complete pipeline configuration for pack format
type PipelineConfig struct {
	Stages     []string           `json:"stages"`               // Pipeline stages in order (e.g., ["template", "provider", "validator"])
	Middleware []MiddlewareConfig `json:"middleware,omitempty"` // Middleware configurations
}

// MiddlewareConfig represents configuration for a specific middleware
type MiddlewareConfig struct {
	Type   string                 `json:"type"`             // Middleware type (e.g., "template", "provider", "validator")
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
	AllowUndefined bool `json:"allow_undefined"` // Allow undefined variables (opposite of strict_mode)
}

// ProviderMiddlewareConfig contains configuration for provider middleware
type ProviderMiddlewareConfig struct {
	RetryPolicy  *RetryPolicy `json:"retry_policy,omitempty"`  // Retry policy
	TimeoutMs    int          `json:"timeout_ms,omitempty"`    // Request timeout in milliseconds
	DisableTrace bool         `json:"disable_trace,omitempty"` // Disable execution tracing (default: false = tracing enabled)
}

// ValidatorMiddlewareConfig contains configuration for validator middleware
type ValidatorMiddlewareConfig struct {
	FailFast         bool `json:"fail_fast"`          // Stop on first validation error
	CollectAllErrors bool `json:"collect_all_errors"` // Collect all errors before failing
}

// ExecutionTrace captures the complete execution history of a pipeline run.
// This includes all LLM calls, tool executions, and other significant events.
type ExecutionTrace struct {
	LLMCalls    []LLMCall    `json:"llm_calls"`              // All LLM API calls made during execution
	Events      []TraceEvent `json:"events,omitempty"`       // Other trace events (tool execution, context truncation, etc.)
	StartedAt   time.Time    `json:"started_at"`             // When pipeline execution started
	CompletedAt *time.Time   `json:"completed_at,omitempty"` // When pipeline execution completed (nil if still running)
}

// LLMCall represents a single LLM API call within a pipeline execution.
// In tool-enabled scenarios, multiple calls may occur in sequence.
type LLMCall struct {
	Sequence     int                     `json:"sequence"`               // Call number in sequence (1, 2, 3...)
	MessageIndex int                     `json:"message_index"`          // Index into ExecutionResult.Messages where assistant response is stored
	Request      interface{}             `json:"request,omitempty"`      // Raw request (if debugging enabled)
	Response     *Response               `json:"response"`               // Parsed response
	RawResponse  interface{}             `json:"raw_response,omitempty"` // Raw provider response (if debugging enabled)
	StartedAt    time.Time               `json:"started_at"`             // When call started
	Duration     time.Duration           `json:"duration"`               // How long the call took
	Cost         types.CostInfo          `json:"cost"`                   // Cost information for this call
	ToolCalls    []types.MessageToolCall `json:"tool_calls,omitempty"`   // If this call triggered tool execution
	Error        error                   `json:"error,omitempty"`        // If the call failed
}

// TraceEvent represents a significant event during pipeline execution.
type TraceEvent struct {
	Type      string      `json:"type"`              // Event type (e.g., "tool_execution", "context_truncation", "validation_failed")
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

// ExecutionResult is the output of a pipeline execution.
// It contains the final state after all middleware has been executed.
type ExecutionResult struct {
	Messages []types.Message        `json:"messages"`  // All messages including history and responses
	Response *Response              `json:"response"`  // The final response (convenience field)
	Trace    ExecutionTrace         `json:"trace"`     // Complete execution trace with all LLM calls
	CostInfo types.CostInfo         `json:"cost_info"` // Aggregate cost across all LLM calls
	Metadata map[string]interface{} `json:"metadata"`  // Metadata populated by middleware
}
