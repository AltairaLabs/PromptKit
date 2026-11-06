package providers

import (
	"time"

	"github.com/AltairaLabs/PromptKit/runtime/v2/tools"
	"github.com/AltairaLabs/PromptKit/runtime/v2/types"
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

// ExecutionResult is the complete result a stream's final chunk carries in
// StreamChunk.FinalResult. It is an empty interface so this package need not
// import the layers above it. A provider stream run through
// BaseProvider.RunStreamingRequest ends with a *PredictionResponse; the SDK's
// session stream ends with a *pipeline.ExecutionResult.
type ExecutionResult interface{}

// StreamChunk represents a batch of tokens with metadata
type StreamChunk struct {
	// Content is the accumulated content so far
	Content string `json:"content"`

	// Delta is the new content in this chunk
	Delta string `json:"delta"`

	// Reasoning carries reasoning/thinking tokens emitted by reasoning models
	// (e.g. OpenAI o-series) before any visible content. Reasoning chunks act
	// as keepalives that reset the idle timer; they carry no visible Delta and
	// must not be appended to the content stream.
	Reasoning string `json:"reasoning,omitempty"`

	// OpaqueReasoning carries provider round-trip reasoning tokens (signatures /
	// encrypted blocks) for this chunk. Never displayed, never content; preserved
	// so a provider can feed it back where required (intra-turn).
	OpaqueReasoning []types.OpaqueReasoning `json:"opaque_reasoning,omitempty"`

	// MediaData contains raw streaming media bytes (audio, video, images).
	// Data is always raw bytes, never base64. Providers decode at source.
	MediaData *StreamMediaData `json:"-"`

	// TokenCount is the total number of tokens so far
	TokenCount int `json:"token_count"`

	// DeltaTokens is the number of tokens in this delta
	DeltaTokens int `json:"delta_tokens"`

	// ToolCalls contains accumulated tool calls (for assistant messages that invoke tools)
	ToolCalls []types.MessageToolCall `json:"tool_calls,omitempty"`

	// FinishReason is nil until stream is complete
	// A provider reports the normalized types.FinishReason* constants where it
	// maps them (for example "stop"). Other values are "error" for a failed
	// stream, "canceled" for a canceled one, "validation_failed",
	// and "pending_tools"; an unmapped provider reason passes through verbatim.
	FinishReason *string `json:"finish_reason,omitempty"`

	// Interrupted indicates the response was interrupted (e.g., user started speaking)
	// When true, clients should clear any buffered audio and prepare for a new response
	Interrupted bool `json:"interrupted,omitempty"`

	// purgeAudio marks the in-band barge-in marker (see BargeMarker). The
	// StreamPump consumes it; it never reaches Response().
	purgeAudio bool

	// Reset signals that the stream is being retried from scratch after a
	// mid-stream failure. Consumers must discard all accumulated state
	// (content, tool calls, cost info) and treat subsequent chunks as a
	// fresh response. Only emitted when the provider's retry_window is
	// set to "always" and a mid-stream error triggered a full retry.
	// The retry costs tokens because the provider generates a new
	// response from scratch.
	Reset bool `json:"reset,omitempty"`

	// Error is set if an error occurred during streaming
	Error error `json:"error,omitempty"`

	// Metadata contains provider-specific metadata
	Metadata map[string]interface{} `json:"metadata,omitempty"`

	// FinalResult contains the complete execution result (only set in the final
	// chunk). For a provider stream that ends without an error it is a
	// *PredictionResponse holding the content, tool calls, cost, finish reason
	// and latency; see ExecutionResult.
	FinalResult ExecutionResult `json:"final_result,omitempty"`

	// CostInfo contains cost breakdown (only present in final chunk when FinishReason != nil)
	CostInfo *types.CostInfo `json:"cost_info,omitempty"`

	// PendingTools contains client-mode tools awaiting caller fulfillment.
	// Only set when FinishReason is "pending_tools".
	PendingTools []tools.PendingToolExecution `json:"pending_tools,omitempty"`

	// Source labels the input track/speaker for fan-out routing (e.g. "caller",
	// "agent"). Copied onto StreamElement.Source at the session boundary.
	Source string `json:"source,omitempty"`
}

// StreamEvent types.
const (
	StreamEventChunk    = "chunk"
	StreamEventComplete = "complete"
	StreamEventError    = "error"
)

// StreamEvent is one observation of a provider stream, built by the stream
// relay in BaseProvider.RunStreamingRequest for each StreamObserver callback.
// An observer that also implements StreamEventObserver receives it.
type StreamEvent struct {
	// Type is StreamEventChunk, StreamEventComplete or StreamEventError.
	Type string `json:"type"`

	// Chunk is the chunk being delivered. For a complete event it is the
	// stream's last chunk.
	Chunk *StreamChunk `json:"chunk,omitempty"`

	// Error is set for error events
	Error error `json:"error,omitempty"`

	// Timestamp is when the event occurred
	Timestamp time.Time `json:"timestamp"`
}

// StreamObserver receives the events of every stream a provider opens through
// BaseProvider.RunStreamingRequest, for monitoring. Install one with
// BaseProvider.SetStreamObserver or ProviderSpec.StreamObserver; with none
// installed, nothing is observed.
//
// The callbacks run on the stream's relay goroutine before the chunk reaches
// the caller, so they must return quickly. They run concurrently for
// concurrent streams. A callback that panics is recovered and the stream goes
// on.
//
//   - OnChunk is called for every chunk delivered without an error, including
//     a Reset chunk and the final chunk.
//   - OnError is called for the error chunk that ends a stream attempt.
//   - OnComplete is called once when the stream ends without an error (after
//     a Reset, the retried attempt decides), with the last TokenCount reported
//     and the time since the stream was requested.
type StreamObserver interface {
	OnChunk(chunk StreamChunk)
	OnComplete(totalTokens int, duration time.Duration)
	OnError(err error)
}

// StreamEventObserver is implemented by a StreamObserver that also wants each
// observation as a StreamEvent, carrying its timestamp. OnStreamEvent is called
// before the matching StreamObserver callback.
type StreamEventObserver interface {
	OnStreamEvent(event StreamEvent)
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

// IsValidationAbort reports whether err is a *ValidationAbortError. It does not
// unwrap wrapped errors.
func IsValidationAbort(err error) bool {
	_, ok := err.(*ValidationAbortError)
	return ok
}
