package events

import (
	"time"

	"github.com/AltairaLabs/PromptKit/runtime/types"
)

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

	// EventStageStarted marks stage start (for new streaming architecture).
	EventStageStarted EventType = "stage.started"
	// EventStageCompleted marks stage completion (for new streaming architecture).
	EventStageCompleted EventType = "stage.completed"
	// EventStageFailed marks stage failure (for new streaming architecture).
	EventStageFailed EventType = "stage.failed"

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

	// EventMessageCreated marks message creation.
	EventMessageCreated EventType = "message.created"
	// EventMessageUpdated marks message update (e.g., cost/latency after completion).
	EventMessageUpdated EventType = "message.updated"

	// EventConversationStarted marks the start of a new conversation.
	EventConversationStarted EventType = "conversation.started"

	// EventAudioInput marks audio input from user/environment (multimodal recording).
	EventAudioInput EventType = "audio.input"
	// EventAudioOutput marks audio output from agent (multimodal recording).
	EventAudioOutput EventType = "audio.output"
	// EventAudioTranscription marks speech-to-text transcription result.
	EventAudioTranscription EventType = "audio.transcription"

	// EventVideoFrame marks a video frame capture (multimodal recording).
	EventVideoFrame EventType = "video.frame"
	// EventScreenshot marks a screenshot capture.
	EventScreenshot EventType = "screenshot"

	// EventImageInput marks image input from user/environment (multimodal recording).
	EventImageInput EventType = "image.input"
	// EventImageOutput marks image output from agent (multimodal recording).
	EventImageOutput EventType = "image.output"

	// EventWorkflowTransitioned marks a workflow state transition.
	EventWorkflowTransitioned EventType = "workflow.transitioned"
	// EventWorkflowCompleted marks a workflow reaching a terminal state.
	EventWorkflowCompleted EventType = "workflow.completed"
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
	_ = 0 // no-op statement for coverage tracking
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

// StageStartedData contains data for stage start events (streaming architecture).
type StageStartedData struct {
	baseEventData
	Name      string
	Index     int
	StageType string // Type of stage (transform, accumulate, generate, sink, bidirectional)
}

// StageCompletedData contains data for stage completion events (streaming architecture).
type StageCompletedData struct {
	baseEventData
	Name      string
	Index     int
	Duration  time.Duration
	StageType string
}

// StageFailedData contains data for stage failure events (streaming architecture).
type StageFailedData struct {
	baseEventData
	Name      string
	Index     int
	Error     error
	Duration  time.Duration
	StageType string
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

// MessageToolCall represents a tool call in a message event (mirrors runtime/types.MessageToolCall).
type MessageToolCall struct {
	ID   string `json:"id"`   // Unique identifier for this tool call
	Name string `json:"name"` // Name of the tool to invoke
	Args string `json:"args"` // JSON-encoded tool arguments as string
}

// MessageToolResult represents a tool result in a message event (mirrors runtime/types.MessageToolResult).
type MessageToolResult struct {
	ID        string `json:"id"`                   // References the MessageToolCall.ID
	Name      string `json:"name"`                 // Tool name that was executed
	Content   string `json:"content"`              // Result content
	Error     string `json:"error,omitempty"`      // Error message if tool failed
	LatencyMs int64  `json:"latency_ms,omitempty"` // Tool execution latency
}

// MessageCreatedData contains data for message creation events.
type MessageCreatedData struct {
	baseEventData
	Role       string
	Content    string
	Index      int                 // Position in conversation history
	Parts      []types.ContentPart // Multimodal content parts (text, images, audio, video)
	ToolCalls  []MessageToolCall   // Tool calls requested by assistant (if any)
	ToolResult *MessageToolResult  // Tool result for tool messages (if any)
}

// MessageUpdatedData contains data for message update events.
type MessageUpdatedData struct {
	baseEventData
	Index        int // Position in conversation history
	LatencyMs    int64
	InputTokens  int
	OutputTokens int
	TotalCost    float64
}

// ConversationStartedData contains data for conversation start events.
type ConversationStartedData struct {
	baseEventData
	SystemPrompt string // The assembled system prompt for this conversation
}

// BinaryPayload represents a reference to binary data stored externally.
// This allows events to reference large payloads (audio, video, images) without
// embedding them directly in the event stream.
type BinaryPayload struct {
	// StorageRef is a URI or path to the stored binary data.
	// Examples: "file://recordings/audio/chunk-001.pcm", "s3://bucket/key"
	StorageRef string `json:"storage_ref"`
	// MIMEType is the MIME type of the binary data.
	MIMEType string `json:"mime_type"`
	// Size is the size of the binary data in bytes.
	Size int64 `json:"size"`
	// Checksum is an optional integrity checksum (e.g., SHA256).
	Checksum string `json:"checksum,omitempty"`
	// InlineData contains the raw bytes if small enough to embed directly.
	// If set, StorageRef may be empty.
	InlineData []byte `json:"inline_data,omitempty"`
}

// AudioMetadata contains format information for audio data.
type AudioMetadata struct {
	// SampleRate is the audio sample rate in Hz (e.g., 16000, 24000, 44100).
	SampleRate int `json:"sample_rate"`
	// Channels is the number of audio channels (1=mono, 2=stereo).
	Channels int `json:"channels"`
	// Encoding is the audio encoding format (e.g., "pcm", "pcm_linear16", "opus", "mp3").
	Encoding string `json:"encoding"`
	// BitsPerSample is the bit depth for PCM audio (e.g., 16, 24, 32).
	BitsPerSample int `json:"bits_per_sample,omitempty"`
	// DurationMs is the duration of the audio in milliseconds.
	DurationMs int64 `json:"duration_ms"`
}

// AudioInputData contains data for audio input events.
type AudioInputData struct {
	baseEventData
	// Actor identifies the source of the audio (e.g., "user", "environment").
	Actor string `json:"actor"`
	// Payload contains the audio data or reference.
	Payload BinaryPayload `json:"payload"`
	// Metadata contains audio format information.
	Metadata AudioMetadata `json:"metadata"`
	// TurnID links this audio to a specific conversation turn.
	TurnID string `json:"turn_id,omitempty"`
	// ChunkIndex is the sequence number for streaming audio (0-based).
	ChunkIndex int `json:"chunk_index"`
	// IsFinal indicates this is the last chunk in the stream.
	IsFinal bool `json:"is_final"`
}

// AudioOutputData contains data for audio output events.
type AudioOutputData struct {
	baseEventData
	// Payload contains the audio data or reference.
	Payload BinaryPayload `json:"payload"`
	// Metadata contains audio format information.
	Metadata AudioMetadata `json:"metadata"`
	// TurnID links this audio to a specific conversation turn.
	TurnID string `json:"turn_id,omitempty"`
	// ChunkIndex is the sequence number for streaming audio (0-based).
	ChunkIndex int `json:"chunk_index"`
	// IsFinal indicates this is the last chunk in the stream.
	IsFinal bool `json:"is_final"`
	// GeneratedFrom indicates what generated this audio (e.g., "tts", "model").
	GeneratedFrom string `json:"generated_from,omitempty"`
}

// AudioTranscriptionData contains data for transcription events.
type AudioTranscriptionData struct {
	baseEventData
	// Text is the transcribed text.
	Text string `json:"text"`
	// Language is the detected or specified language code (e.g., "en-US").
	Language string `json:"language,omitempty"`
	// Confidence is the confidence score (0.0 to 1.0) if available.
	Confidence float64 `json:"confidence,omitempty"`
	// TurnID links this transcription to a specific conversation turn.
	TurnID string `json:"turn_id,omitempty"`
	// AudioEventID references the audio event this transcription is derived from.
	AudioEventID string `json:"audio_event_id,omitempty"`
	// IsFinal indicates this is the final transcription (vs. interim results).
	IsFinal bool `json:"is_final"`
	// Provider is the STT provider used (e.g., "whisper", "google", "deepgram").
	Provider string `json:"provider,omitempty"`
}

// VideoMetadata contains format information for video data.
type VideoMetadata struct {
	// Width is the video frame width in pixels.
	Width int `json:"width"`
	// Height is the video frame height in pixels.
	Height int `json:"height"`
	// Encoding is the video encoding format (e.g., "h264", "vp8", "mjpeg", "raw").
	Encoding string `json:"encoding"`
	// FrameRate is the frames per second.
	FrameRate float64 `json:"frame_rate,omitempty"`
	// DurationMs is the duration in milliseconds (for video segments).
	DurationMs int64 `json:"duration_ms,omitempty"`
}

// VideoFrameData contains data for video frame events.
type VideoFrameData struct {
	baseEventData
	// Payload contains the frame data or reference.
	Payload BinaryPayload `json:"payload"`
	// Metadata contains video format information.
	Metadata VideoMetadata `json:"metadata"`
	// FrameIndex is the frame sequence number.
	FrameIndex int64 `json:"frame_index"`
	// TimestampMs is the frame timestamp in milliseconds from session start.
	TimestampMs int64 `json:"timestamp_ms"`
	// IsKeyframe indicates if this is a keyframe (for seeking).
	IsKeyframe bool `json:"is_keyframe"`
}

// ScreenshotData contains data for screenshot events.
type ScreenshotData struct {
	baseEventData
	// Payload contains the image data or reference.
	Payload BinaryPayload `json:"payload"`
	// Metadata contains image format information.
	Metadata VideoMetadata `json:"metadata"` // Reuse VideoMetadata for dimensions
	// WindowTitle is the title of the captured window (if applicable).
	WindowTitle string `json:"window_title,omitempty"`
	// WindowBounds contains the window position and size.
	WindowBounds *Rect `json:"window_bounds,omitempty"`
	// Reason describes why the screenshot was taken (e.g., "before_action", "after_action", "periodic").
	Reason string `json:"reason,omitempty"`
}

// Rect represents a rectangle for screen coordinates.
type Rect struct {
	X      int `json:"x"`
	Y      int `json:"y"`
	Width  int `json:"width"`
	Height int `json:"height"`
}

// ImageInputData contains data for image input events.
type ImageInputData struct {
	baseEventData
	// Actor identifies the source of the image (e.g., "user", "environment").
	Actor string `json:"actor"`
	// Payload contains the image data or reference.
	Payload BinaryPayload `json:"payload"`
	// Metadata contains image format information.
	Metadata VideoMetadata `json:"metadata"` // Reuse VideoMetadata for dimensions
	// Description is an optional description of the image content.
	Description string `json:"description,omitempty"`
}

// ImageOutputData contains data for image output events.
type ImageOutputData struct {
	baseEventData
	// Payload contains the image data or reference.
	Payload BinaryPayload `json:"payload"`
	// Metadata contains image format information.
	Metadata VideoMetadata `json:"metadata"` // Reuse VideoMetadata for dimensions
	// GeneratedFrom indicates what generated this image (e.g., "dalle", "stable-diffusion").
	GeneratedFrom string `json:"generated_from,omitempty"`
	// Prompt is the prompt used to generate the image (if applicable).
	Prompt string `json:"prompt,omitempty"`
}

// WorkflowTransitionedData contains data for workflow state transition events.
type WorkflowTransitionedData struct {
	baseEventData
	// FromState is the state before the transition.
	FromState string `json:"from_state"`
	// ToState is the state after the transition.
	ToState string `json:"to_state"`
	// Event is the event that triggered the transition.
	Event string `json:"event"`
	// PromptTask is the prompt_task of the new state.
	PromptTask string `json:"prompt_task"`
}

// WorkflowCompletedData contains data for workflow completion events.
type WorkflowCompletedData struct {
	baseEventData
	// FinalState is the terminal state the workflow reached.
	FinalState string `json:"final_state"`
	// TransitionCount is the total number of transitions that occurred.
	TransitionCount int `json:"transition_count"`
}
