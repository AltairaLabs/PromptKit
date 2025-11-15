package providers

import (
	"time"

	"github.com/AltairaLabs/PromptKit/runtime/types"
)

// Forward declare ExecutionResult to avoid circular import
type ExecutionResult interface{}

// StreamChunk represents a batch of tokens with metadata
type StreamChunk struct {
	// Content is the accumulated content so far
	Content string `json:"content"`

	// Delta is the new content in this chunk
	Delta string `json:"delta"`

	// MediaDelta contains new media content in this chunk (audio, video, images)
	// Uses the same MediaContent type as non-streaming messages for API consistency.
	MediaDelta *types.MediaContent `json:"media_delta,omitempty"`

	// TokenCount is the total number of tokens so far
	TokenCount int `json:"token_count"`

	// DeltaTokens is the number of tokens in this delta
	DeltaTokens int `json:"delta_tokens"`

	// ToolCalls contains accumulated tool calls (for assistant messages that invoke tools)
	ToolCalls []types.MessageToolCall `json:"tool_calls,omitempty"`

	// FinishReason is nil until stream is complete
	// Values: "stop", "length", "content_filter", "tool_calls", "error", "validation_failed", "cancelled"
	FinishReason *string `json:"finish_reason,omitempty"`

	// Error is set if an error occurred during streaming
	Error error `json:"error,omitempty"`

	// Metadata contains provider-specific metadata
	Metadata map[string]interface{} `json:"metadata,omitempty"`

	// FinalResult contains the complete execution result (only set in the final chunk)
	FinalResult ExecutionResult `json:"final_result,omitempty"`

	// CostInfo contains cost breakdown (only present in final chunk when FinishReason != nil)
	CostInfo *types.CostInfo `json:"cost_info,omitempty"`
}

// StreamEvent is sent to observers for monitoring
type StreamEvent struct {
	// Type is the event type: "chunk", "complete", "error"
	Type string `json:"type"`

	// Chunk contains the stream chunk data
	Chunk *StreamChunk `json:"chunk,omitempty"`

	// Error is set for error events
	Error error `json:"error,omitempty"`

	// Timestamp is when the event occurred
	Timestamp time.Time `json:"timestamp"`
}

// StreamObserver receives stream events for monitoring
type StreamObserver interface {
	OnChunk(chunk StreamChunk)
	OnComplete(totalTokens int, duration time.Duration)
	OnError(err error)
}

// ValidationAbortError is returned when a streaming validator aborts a stream
type ValidationAbortError struct {
	Reason string
	Chunk  StreamChunk
}

// Error returns the error message for this validation abort error.
func (e *ValidationAbortError) Error() string {
	return "validation aborted stream: " + e.Reason
}

// IsValidationAbort checks if an error is a validation abort
func IsValidationAbort(err error) bool {
	_, ok := err.(*ValidationAbortError)
	return ok
}

// ptr is a helper to get a pointer to a string
func ptr(s string) *string {
	return &s
}
