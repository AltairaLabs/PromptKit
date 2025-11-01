package types

import (
	"encoding/json"
	"time"
)

// Message represents a single message in a conversation.
// This is the canonical message type used throughout the system.
type Message struct {
	Role    string `json:"role"`    // "system", "user", "assistant", "tool"
	Content string `json:"content"` // Message content

	// Tool invocations (for assistant messages that call tools)
	ToolCalls []MessageToolCall `json:"tool_calls,omitempty"`

	// Tool result (for tool role messages)
	// When Role="tool", this contains the tool execution result
	ToolResult *MessageToolResult `json:"tool_result,omitempty"`

	// Source indicates where this message originated (runtime-only, not persisted in JSON)
	// Values: "statestore" (loaded from StateStore), "pipeline" (created during execution), "" (user input)
	Source string `json:"-"`

	// Metadata for observability and tracking
	Timestamp time.Time              `json:"timestamp,omitempty"`  // When the message was created
	LatencyMs int64                  `json:"latency_ms,omitempty"` // Time taken to generate (for assistant messages)
	CostInfo  *CostInfo              `json:"cost_info,omitempty"`  // Token usage and cost tracking
	Meta      map[string]interface{} `json:"meta,omitempty"`       // Custom metadata

	// Validation results (for assistant messages)
	Validations []ValidationResult `json:"validations,omitempty"`
}

// MessageToolCall represents a request to call a tool within a Message.
// The Args field contains the JSON-encoded arguments for the tool.
type MessageToolCall struct {
	ID   string          `json:"id"`   // Unique identifier for this tool call
	Name string          `json:"name"` // Name of the tool to invoke
	Args json.RawMessage `json:"args"` // JSON-encoded tool arguments
}

// MessageToolResult represents the result of a tool execution in a Message.
// When embedded in Message, the Message.Role should be "tool".
type MessageToolResult struct {
	ID        string `json:"id"`              // References the MessageToolCall.ID that triggered this result
	Name      string `json:"name"`            // Tool name that was executed
	Content   string `json:"content"`         // Result content or error message
	Error     string `json:"error,omitempty"` // Error message if tool execution failed
	LatencyMs int64  `json:"latency_ms"`      // Tool execution latency in milliseconds
}

// ToolDef represents a tool definition that can be provided to an LLM.
// The InputSchema and OutputSchema use JSON Schema format for validation.
type ToolDef struct {
	Name         string          `json:"name"`                    // Unique tool name
	Description  string          `json:"description"`             // Human-readable description of what the tool does
	InputSchema  json.RawMessage `json:"input_schema"`            // JSON Schema for input validation
	OutputSchema json.RawMessage `json:"output_schema,omitempty"` // Optional JSON Schema for output validation
}

// CostInfo tracks token usage and associated costs for LLM operations.
// All cost values are in USD. Used for both individual messages and aggregated tracking.
type CostInfo struct {
	InputTokens   int     `json:"input_tokens"`              // Number of input tokens consumed
	OutputTokens  int     `json:"output_tokens"`             // Number of output tokens generated
	CachedTokens  int     `json:"cached_tokens,omitempty"`   // Number of cached tokens used (reduces cost)
	InputCostUSD  float64 `json:"input_cost_usd"`            // Cost of input tokens in USD
	OutputCostUSD float64 `json:"output_cost_usd"`           // Cost of output tokens in USD
	CachedCostUSD float64 `json:"cached_cost_usd,omitempty"` // Cost savings from cached tokens
	TotalCost     float64 `json:"total_cost_usd"`            // Total cost in USD
}

// ToolStats tracks tool usage statistics across a conversation or run.
// Useful for monitoring which tools are being used and how frequently.
type ToolStats struct {
	TotalCalls int            `json:"total_calls"` // Total number of tool calls
	ByTool     map[string]int `json:"by_tool"`     // Count of calls per tool name
}

// ValidationError represents a validation failure in tool usage or message content.
// Used to provide structured error information when validation fails.
type ValidationError struct {
	Type   string `json:"type"`   // Error type: "args_invalid" | "result_invalid" | "policy_violation"
	Tool   string `json:"tool"`   // Name of the tool that failed validation
	Detail string `json:"detail"` // Human-readable error details
}

// ValidationResult represents the outcome of a validator check on a message.
// These are attached to assistant messages to show which validations passed or failed.
type ValidationResult struct {
	ValidatorType string                 `json:"validator_type"`      // Type of validator (e.g., "*validators.BannedWordsValidator")
	Passed        bool                   `json:"passed"`              // Whether the validation passed
	Details       map[string]interface{} `json:"details,omitempty"`   // Validator-specific details about the result
	Timestamp     time.Time              `json:"timestamp,omitempty"` // When the validation was performed
}
