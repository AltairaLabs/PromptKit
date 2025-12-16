package sdk

import (
	"time"

	"github.com/AltairaLabs/PromptKit/runtime/types"
)

// Response represents the result of a conversation turn.
//
// Response wraps the assistant's message with convenience methods and
// additional metadata like timing and validation results.
//
// Basic usage:
//
//	resp, _ := conv.Send(ctx, "Hello!")
//	fmt.Println(resp.Text())           // Text content
//	fmt.Println(resp.TokensUsed())     // Total tokens
//	fmt.Println(resp.Cost())           // Total cost in USD
//
// For multimodal responses:
//
//	if resp.HasMedia() {
//	    for _, part := range resp.Parts() {
//	        if part.Media != nil {
//	            fmt.Printf("Media: %s\n", part.Media.URL)
//	        }
//	    }
//	}
type Response struct {
	// The assistant's message - uses runtime types directly
	message *types.Message

	// Tool calls made during this turn
	toolCalls []types.MessageToolCall

	// Validation results from pack-defined validators
	validations []types.ValidationResult

	// Request timing
	duration time.Duration

	// HITL state - tools awaiting approval
	pendingTools []PendingTool
}

// Text returns the text content of the response.
//
// This is a convenience method that extracts all text parts and joins them.
// For responses with only text content, this returns the full response.
// For multimodal responses, use [Response.Parts] to access all content.
func (r *Response) Text() string {
	if r.message == nil {
		return ""
	}
	return r.message.GetContent()
}

// Message returns the underlying runtime Message.
//
// Use this when you need direct access to the message structure,
// such as for serialization or passing to other runtime components.
func (r *Response) Message() *types.Message {
	return r.message
}

// Parts returns all content parts in the response.
//
// Use this for multimodal responses that may contain text, images,
// audio, or other content types.
func (r *Response) Parts() []types.ContentPart {
	if r.message == nil {
		return nil
	}
	return r.message.Parts
}

// HasMedia returns true if the response contains any media content.
func (r *Response) HasMedia() bool {
	if r.message == nil {
		return false
	}
	return r.message.HasMediaContent()
}

// ToolCalls returns the tool calls made during this turn.
//
// Tool calls are requests from the LLM to execute functions.
// If you have registered handlers via [Conversation.OnTool], they
// will be executed automatically and the results sent back to the LLM.
func (r *Response) ToolCalls() []types.MessageToolCall {
	return r.toolCalls
}

// HasToolCalls returns true if the response contains tool calls.
func (r *Response) HasToolCalls() bool {
	return len(r.toolCalls) > 0
}

// Validations returns the results of all validators that ran.
//
// Validators are defined in the pack and run automatically on responses.
// Check this to see which validators passed or failed.
func (r *Response) Validations() []types.ValidationResult {
	return r.validations
}

// TokensUsed returns the total number of tokens used (input + output).
func (r *Response) TokensUsed() int {
	if r.message == nil || r.message.CostInfo == nil {
		return 0
	}
	return r.message.CostInfo.InputTokens + r.message.CostInfo.OutputTokens
}

// InputTokens returns the number of input (prompt) tokens used.
func (r *Response) InputTokens() int {
	if r.message == nil || r.message.CostInfo == nil {
		return 0
	}
	return r.message.CostInfo.InputTokens
}

// OutputTokens returns the number of output (completion) tokens used.
func (r *Response) OutputTokens() int {
	if r.message == nil || r.message.CostInfo == nil {
		return 0
	}
	return r.message.CostInfo.OutputTokens
}

// Cost returns the total cost in USD for this response.
func (r *Response) Cost() float64 {
	if r.message == nil || r.message.CostInfo == nil {
		return 0
	}
	return r.message.CostInfo.TotalCost
}

// Duration returns how long the request took.
func (r *Response) Duration() time.Duration {
	return r.duration
}

// PendingTools returns tools that are awaiting external approval.
//
// This is used for Human-in-the-Loop (HITL) workflows where certain
// tools require approval before execution.
func (r *Response) PendingTools() []PendingTool {
	return r.pendingTools
}

// PendingTool represents a tool call that requires external approval.
type PendingTool struct {
	// Unique identifier for this pending call
	ID string

	// Tool name
	Name string

	// Arguments passed to the tool
	Arguments map[string]any

	// Reason the tool requires approval
	Reason string

	// Human-readable message about why approval is needed
	Message string
}
