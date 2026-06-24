// Package stage provides the reactive streams architecture for pipeline execution.
package stage

import (
	"context"
	"encoding/json"
	"time"

	"github.com/AltairaLabs/PromptKit/runtime/events"
	"github.com/AltairaLabs/PromptKit/runtime/logger"
)

// recordingAudioMIMEType is the MIME type used when recording PCM audio.
// Matches the value the duplex-provider bus emit uses so consumers
// (MediaTimeline, replay) see a consistent shape regardless of source.
const recordingAudioMIMEType = "audio/pcm"

// recordingAudioEncoding is the encoding string used when recording PCM audio.
// Matches the duplex-provider bus emit format ("pcm_linear16").
const recordingAudioEncoding = "pcm_linear16"

// recordingAudioBytesPerSample assumes 16-bit PCM audio (2 bytes/sample),
// matching the duplex-provider's duration computation.
const recordingAudioBytesPerSample = 2

// AudioEventData.Direction values, used by recordAudioElement.
const (
	audioDirectionInput  = "input"
	audioDirectionOutput = "output"
)

// RecordingPosition indicates where in the pipeline the recording stage is placed.
type RecordingPosition string

const (
	// RecordingPositionInput records elements entering the pipeline (user input).
	RecordingPositionInput RecordingPosition = "input"
	// RecordingPositionOutput records elements leaving the pipeline (agent output).
	RecordingPositionOutput RecordingPosition = "output"
)

// Role constants for message recording.
const (
	roleUser      = "user"
	roleAssistant = "assistant"
)

// RecordingStageConfig configures the recording stage behavior.
type RecordingStageConfig struct {
	// Position indicates where this stage is in the pipeline.
	Position RecordingPosition

	// SessionID is the session identifier for recorded events.
	SessionID string

	// ConversationID groups events within a session.
	ConversationID string

	// IncludeStreamingText records individual text streaming deltas.
	// When false (default), only the final accumulated Message is recorded.
	// Enable for session replay that needs token-level timing.
	IncludeStreamingText bool

	// IncludeAudio records audio streaming chunks (may be very high volume).
	IncludeAudio bool

	// IncludeVideo records video streaming chunks (may be very large).
	IncludeVideo bool

	// IncludeImages records image data.
	IncludeImages bool
}

// DefaultRecordingStageConfig returns sensible defaults.
func DefaultRecordingStageConfig() RecordingStageConfig {
	return RecordingStageConfig{
		Position:      RecordingPositionInput,
		IncludeAudio:  true,
		IncludeVideo:  false, // Video can be very large
		IncludeImages: true,
	}
}

// RecordingStage captures pipeline elements as events for session recording.
// It observes elements flowing through without modifying them.
//
// Writes synchronously to the EventStore. Slow disk applies back-pressure
// to upstream — recording correctness wins over pipeline throughput. For
// production use cases where this trade-off is wrong, inject a buffered
// EventStore implementation via Engine.EnableSessionRecordingWithStore.
type RecordingStage struct {
	BaseStage
	store     events.EventStore
	config    RecordingStageConfig
	startTime time.Time
}

// NewRecordingStage creates a new recording stage.
func NewRecordingStage(store events.EventStore, config RecordingStageConfig) *RecordingStage {
	name := "recording_" + string(config.Position)
	return &RecordingStage{
		BaseStage: NewBaseStage(name, StageTypeTransform),
		store:     store,
		config:    config,
		startTime: time.Now(),
	}
}

// Process observes elements and records them as events.
func (rs *RecordingStage) Process(
	ctx context.Context,
	input <-chan StreamElement,
	output chan<- StreamElement,
) error {
	defer close(output)

	for elem := range input {
		// Record the element as event(s)
		rs.recordElement(ctx, &elem)

		// Pass through unchanged
		select {
		case output <- elem:
		case <-ctx.Done():
			return ctx.Err()
		}
	}

	return nil
}

// recordElement converts a StreamElement to events and persists them.
func (rs *RecordingStage) recordElement(ctx context.Context, elem *StreamElement) {
	if rs.store == nil {
		return
	}

	// Skip control signals (end-of-stream, end-of-turn, interrupt) — they carry
	// no content to record. Errors fall through to be recorded below.
	if elem.IsControl() && elem.Error == nil {
		return
	}

	// Determine the actor based on position
	role := rs.determineRole()

	// Record based on content type.
	// Streaming content (text deltas, audio/video chunks) is opt-in per modality.
	// Complete messages are always recorded.
	switch {
	case elem.Text != nil && rs.config.IncludeStreamingText:
		rs.recordTextElement(ctx, elem, role)
	case elem.Message != nil:
		rs.recordMessageElement(ctx, elem)
	case elem.Audio != nil && rs.config.IncludeAudio:
		rs.recordAudioElement(ctx, elem, role)
	case elem.Image != nil && rs.config.IncludeImages:
		rs.recordImageElement(ctx, elem, role)
	case elem.Video != nil && rs.config.IncludeVideo:
		rs.recordVideoElement(ctx, elem, role)
	case elem.ToolCall != nil:
		rs.recordToolCallElement(ctx, elem)
	case elem.Error != nil:
		rs.recordErrorElement(ctx, elem)
	}
}

// determineRole returns the message role based on recording position.
func (rs *RecordingStage) determineRole() string {
	if rs.config.Position == RecordingPositionInput {
		return roleUser
	}
	return roleAssistant
}

// appendOrWarn writes the event to the store using the pipeline's context and
// logs a warning on failure. Threading ctx ensures recording honors pipeline
// cancellation (timeouts, client disconnects) instead of writing past it.
func (rs *RecordingStage) appendOrWarn(ctx context.Context, evt *events.Event) {
	if err := rs.store.Append(ctx, evt); err != nil {
		logger.Warn("recording stage append failed",
			"error", err,
			"session_id", rs.config.SessionID,
			"conversation_id", rs.config.ConversationID,
			"event_type", string(evt.Type))
	}
}

// recordTextElement records a streaming text delta.
func (rs *RecordingStage) recordTextElement(ctx context.Context, elem *StreamElement, role string) {
	rs.appendOrWarn(ctx, &events.Event{
		Type:           events.EventMessageCreated,
		Timestamp:      elem.Timestamp,
		SessionID:      rs.config.SessionID,
		ConversationID: rs.config.ConversationID,
		Data: &events.MessageCreatedData{
			Role:    role,
			Content: *elem.Text,
		},
	})
}

// recordMessageElement records a complete message.
func (rs *RecordingStage) recordMessageElement(ctx context.Context, elem *StreamElement) {
	msg := elem.Message
	data := &events.MessageCreatedData{
		Role:    msg.Role,
		Content: msg.Content,
		Parts:   msg.Parts,
	}

	// Convert tool calls if present
	if len(msg.ToolCalls) > 0 {
		data.ToolCalls = make([]events.MessageToolCall, len(msg.ToolCalls))
		for i, tc := range msg.ToolCalls {
			data.ToolCalls[i] = events.MessageToolCall{
				ID:   tc.ID,
				Name: tc.Name,
				Args: string(tc.Args),
			}
		}
	}

	// Convert tool result if present
	if msg.ToolResult != nil {
		data.ToolResult = &events.MessageToolResult{
			ID:    msg.ToolResult.ID,
			Name:  msg.ToolResult.Name,
			Parts: msg.ToolResult.Parts,
		}
	}

	rs.appendOrWarn(ctx, &events.Event{
		Type:           events.EventMessageCreated,
		Timestamp:      elem.Timestamp,
		SessionID:      rs.config.SessionID,
		ConversationID: rs.config.ConversationID,
		Data:           data,
	})
}

// recordAudioElement records an audio element using the canonical AudioEventData.
// Direction and event type are derived from the role: user → input, assistant → output.
// The event shape matches what the duplex-provider bus emit produces, so MediaTimeline
// and replay paths consume both sources uniformly.
func (rs *RecordingStage) recordAudioElement(ctx context.Context, elem *StreamElement, role string) {
	audio := elem.Audio

	// Direction, event type, and source attribution are all derived from role.
	direction := audioDirectionOutput
	eventType := events.EventAudioOutput
	actor := ""
	generatedFrom := "model"
	if role == roleUser {
		direction = audioDirectionInput
		eventType = events.EventAudioInput
		actor = "user"
		generatedFrom = ""
	}

	// Default to 16kHz mono if metadata is missing — matches the duplex-provider fallback.
	sampleRate := audio.SampleRate
	if sampleRate == 0 {
		sampleRate = 16000
	}
	channels := audio.Channels
	if channels == 0 {
		channels = 1
	}

	// Prefer the carried Duration when present; otherwise derive from byte count
	// using the same formula as the duplex-provider bus emit.
	durationMs := audio.Duration.Milliseconds()
	if durationMs == 0 && len(audio.Samples) > 0 {
		durationMs = int64(len(audio.Samples)) * 1000 /
			int64(sampleRate) / int64(channels) / recordingAudioBytesPerSample
	}

	data := &events.AudioEventData{
		Direction:     direction,
		Actor:         actor,
		GeneratedFrom: generatedFrom,
		ChunkIndex:    0, // Recording stage observes per-chunk; sequencing is implicit in event order.
		IsFinal:       false,
		Payload: events.BinaryPayload{
			InlineData: audio.Samples,
			MIMEType:   recordingAudioMIMEType,
			Size:       int64(len(audio.Samples)),
		},
		Metadata: events.AudioMetadata{
			SampleRate: sampleRate,
			Channels:   channels,
			Encoding:   recordingAudioEncoding,
			DurationMs: durationMs,
		},
	}

	rs.appendOrWarn(ctx, &events.Event{
		Type:           eventType,
		Timestamp:      elem.Timestamp,
		SessionID:      rs.config.SessionID,
		ConversationID: rs.config.ConversationID,
		Data:           data,
	})
}

// recordImageElement records an image element.
func (rs *RecordingStage) recordImageElement(ctx context.Context, elem *StreamElement, role string) {
	img := elem.Image

	data := map[string]interface{}{
		"role":       role,
		"mime_type":  img.MIMEType,
		"width":      img.Width,
		"height":     img.Height,
		"format":     img.Format,
		"size_bytes": len(img.Data),
	}

	dataJSON, _ := json.Marshal(data)

	rs.appendOrWarn(ctx, &events.Event{
		Type:           events.EventMessageCreated,
		Timestamp:      elem.Timestamp,
		SessionID:      rs.config.SessionID,
		ConversationID: rs.config.ConversationID,
		Data: &events.MessageCreatedData{
			Role:    role,
			Content: string(dataJSON),
		},
	})
}

// recordVideoElement records a video element.
func (rs *RecordingStage) recordVideoElement(ctx context.Context, elem *StreamElement, role string) {
	video := elem.Video

	data := map[string]interface{}{
		"role":        role,
		"mime_type":   video.MIMEType,
		"width":       video.Width,
		"height":      video.Height,
		"frame_rate":  video.FrameRate,
		"duration_ms": video.Duration.Milliseconds(),
		"format":      video.Format,
		"size_bytes":  len(video.Data),
	}

	dataJSON, _ := json.Marshal(data)

	rs.appendOrWarn(ctx, &events.Event{
		Type:           events.EventMessageCreated,
		Timestamp:      elem.Timestamp,
		SessionID:      rs.config.SessionID,
		ConversationID: rs.config.ConversationID,
		Data: &events.MessageCreatedData{
			Role:    role,
			Content: string(dataJSON),
		},
	})
}

// recordToolCallElement records a tool call.
func (rs *RecordingStage) recordToolCallElement(ctx context.Context, elem *StreamElement) {
	tc := elem.ToolCall

	rs.appendOrWarn(ctx, &events.Event{
		Type:           events.EventToolCallStarted,
		Timestamp:      elem.Timestamp,
		SessionID:      rs.config.SessionID,
		ConversationID: rs.config.ConversationID,
		Data: &events.ToolCallStartedData{
			ToolName: tc.Name,
			CallID:   tc.ID,
		},
	})
}

// recordErrorElement records an error.
func (rs *RecordingStage) recordErrorElement(ctx context.Context, elem *StreamElement) {
	rs.appendOrWarn(ctx, &events.Event{
		Type:           events.EventStreamInterrupted,
		Timestamp:      elem.Timestamp,
		SessionID:      rs.config.SessionID,
		ConversationID: rs.config.ConversationID,
		Data: &events.StreamInterruptedData{
			Reason: elem.Error.Error(),
		},
	})
}

// WithSessionID sets the session ID for recorded events.
func (rs *RecordingStage) WithSessionID(sessionID string) *RecordingStage {
	rs.config.SessionID = sessionID
	return rs
}

// WithConversationID sets the conversation ID for recorded events.
func (rs *RecordingStage) WithConversationID(conversationID string) *RecordingStage {
	rs.config.ConversationID = conversationID
	return rs
}
