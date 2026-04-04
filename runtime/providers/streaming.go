package providers

import (
	"time"

	"github.com/AltairaLabs/PromptKit/runtime/tools"
	"github.com/AltairaLabs/PromptKit/runtime/types"
)

// StreamMediaData carries raw media bytes for streaming.
// Data is always raw bytes, never base64. Providers decode at source.
//
// This type maps to pipeline-internal types at the StreamChunk/StreamElement boundary:
//   - stage.AudioData: SampleRate, Channels
//   - stage.ImageData: Width, Height, FrameNum
//   - stage.VideoData: Width, Height, FrameRate, IsKeyFrame, FrameNum
type StreamMediaData struct {
	// Common fields
	Data     []byte // Raw media bytes (PCM audio, JPEG frame, H.264 chunk, etc.)
	MIMEType string // e.g., "audio/pcm", "image/jpeg", "video/h264"

	// Audio metadata
	SampleRate int // Sample rate in Hz (e.g., 16000, 24000). 0 if not audio.
	Channels   int // Channel count (1=mono, 2=stereo). 0 if not audio.

	// Visual metadata (image and video)
	Width  int // Pixels. 0 if unknown or not visual.
	Height int // Pixels. 0 if unknown or not visual.

	// Video/image streaming metadata
	FrameRate  float64 // FPS. 0 if not video.
	IsKeyFrame bool    // True if this is a key frame (video only).
	FrameNum   int64   // Sequence number for ordering frames/chunks.
}

// DefaultStreamBufferSize is the default buffer size for streaming channels.
// A buffer of 32 prevents the streaming goroutine from blocking on every send
// when the consumer is slow, while keeping memory usage reasonable.
const DefaultStreamBufferSize = 32

// DefaultStreamIdleTimeout is the maximum time to wait between stream chunks
// before considering the stream stalled. If no data is received within this
// duration, the response body is closed to unblock the reader.
const DefaultStreamIdleTimeout = 30 * time.Second

// ExecutionResult is a forward declaration to avoid circular import.
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

	// MediaData contains raw streaming media bytes (audio, video, images).
	// Data is always raw bytes, never base64. Providers decode at source.
	// Prefer this over MediaDelta for all new code.
	// MediaDelta is deprecated and will be removed in a future release.
	MediaData *StreamMediaData `json:"-"`

	// TokenCount is the total number of tokens so far
	TokenCount int `json:"token_count"`

	// DeltaTokens is the number of tokens in this delta
	DeltaTokens int `json:"delta_tokens"`

	// ToolCalls contains accumulated tool calls (for assistant messages that invoke tools)
	ToolCalls []types.MessageToolCall `json:"tool_calls,omitempty"`

	// FinishReason is nil until stream is complete
	// Values: "stop", "length", "content_filter", "tool_calls", "error", "validation_failed", "cancelled"
	FinishReason *string `json:"finish_reason,omitempty"`

	// Interrupted indicates the response was interrupted (e.g., user started speaking)
	// When true, clients should clear any buffered audio and prepare for a new response
	Interrupted bool `json:"interrupted,omitempty"`

	// Error is set if an error occurred during streaming
	Error error `json:"error,omitempty"`

	// Metadata contains provider-specific metadata
	Metadata map[string]interface{} `json:"metadata,omitempty"`

	// FinalResult contains the complete execution result (only set in the final chunk)
	FinalResult ExecutionResult `json:"final_result,omitempty"`

	// CostInfo contains cost breakdown (only present in final chunk when FinishReason != nil)
	CostInfo *types.CostInfo `json:"cost_info,omitempty"`

	// PendingTools contains client-mode tools awaiting caller fulfillment.
	// Only set when FinishReason is "pending_tools".
	PendingTools []tools.PendingToolExecution `json:"pending_tools,omitempty"`
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
