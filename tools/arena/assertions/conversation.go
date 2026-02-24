package assertions

import (
	"context"
	"time"

	"github.com/AltairaLabs/PromptKit/runtime/types"
)

// ConversationAssertion defines an assertion to evaluate across an entire conversation.
// Unlike turn-level assertions that check individual responses, conversation assertions
// evaluate patterns, behaviors, or constraints across all turns in a self-play scenario.
type ConversationAssertion struct {
	Type    string                 `json:"type" yaml:"type"`
	Params  map[string]interface{} `json:"params" yaml:"params"`
	Message string                 `json:"message" yaml:"message"`
	When    *AssertionWhen         `json:"when,omitempty" yaml:"when,omitempty"`
}

// ConversationContext provides all data needed to evaluate conversation-level assertions.
// This aggregates the complete conversation history, tool usage, and metadata for
// comprehensive validation across multiple turns.
type ConversationContext struct {
	// AllTurns contains the complete conversation history in chronological order.
	// Includes all messages from all roles (system, user, assistant, tool).
	AllTurns []types.Message

	// ToolCalls contains all tool invocations with their results.
	// Ordered chronologically to allow sequential analysis.
	ToolCalls []ToolCallRecord

	// Metadata provides scenario/execution context for the conversation.
	Metadata ConversationMetadata
}

// ToolCallRecord is an alias for types.ToolCallRecord so existing code
// referencing assertions.ToolCallRecord continues to compile unchanged.
type ToolCallRecord = types.ToolCallRecord

// ConversationMetadata provides context about the conversation execution.
// Useful for conditional validation based on scenario characteristics.
type ConversationMetadata struct {
	ScenarioID     string                 `json:"scenario_id"`      // The scenario being tested
	PersonaID      string                 `json:"persona_id"`       // Persona used for self-play (if any)
	Variables      map[string]interface{} `json:"variables"`        // Variables passed to prompts
	PromptConfigID string                 `json:"prompt_config_id"` // Which prompt configuration was used
	ProviderID     string                 `json:"provider_id"`      // Which LLM provider was used
	TotalCost      float64                `json:"total_cost"`       // Total cost in USD across all turns
	TotalTokens    int                    `json:"total_tokens"`     // Total tokens used (input + output)
	Extras         map[string]interface{} `json:"extras,omitempty"` // Additional metadata (e.g., judge targets/defaults)
}

// ConversationValidator evaluates assertions across entire conversations.
// Implementations check patterns, constraints, or behaviors that span multiple turns,
// such as "no forbidden tool arguments used" or "consistent behavior maintained".
type ConversationValidator interface {
	// Type returns the validator name (e.g., "tools_not_called_with_args").
	// Must match the type specified in ConversationAssertion configs.
	Type() string

	// ValidateConversation evaluates the assertion against the full conversation.
	// Returns a result indicating success/failure with detailed evidence.
	ValidateConversation(
		ctx context.Context,
		convCtx *ConversationContext,
		params map[string]interface{},
	) ConversationValidationResult
}

// ConversationValidationResult contains the outcome of a conversation-level assertion.
// Provides structured details for debugging and reporting when assertions fail.
type ConversationValidationResult struct {
	Type    string                 `json:"type,omitempty"`    // Validator type (e.g., tools_not_called_with_args)
	Passed  bool                   `json:"passed"`            // Whether the assertion passed
	Message string                 `json:"message"`           // Human-readable result explanation
	Details map[string]interface{} `json:"details,omitempty"` // Structured details for debugging

	// For aggregated assertions (e.g., checking all turns), evidence of individual violations.
	// Helps users understand exactly which turns or actions failed the assertion.
	Violations []ConversationViolation `json:"violations,omitempty"`
}

// ConversationViolation represents a single assertion violation within the conversation.
// Captures exactly where and how the assertion was violated for precise debugging.
type ConversationViolation struct {
	TurnIndex   int                    `json:"turn_index"`          // Which turn (index in AllTurns) had the violation
	Description string                 `json:"description"`         // What was violated (human-readable)
	Evidence    map[string]interface{} `json:"evidence,omitempty"`  // Data supporting the violation (e.g., actual values)
	Timestamp   time.Time              `json:"timestamp,omitempty"` // When the violation occurred (if available)
}
