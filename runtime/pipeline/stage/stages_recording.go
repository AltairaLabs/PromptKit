// Package stage provides the reactive streams architecture for pipeline execution.
package stage

import (
	"context"
	"encoding/json"
	"time"

	"github.com/AltairaLabs/PromptKit/runtime/events"
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

	// IncludeAudio records audio data (may be large).
	IncludeAudio bool

	// IncludeVideo records video data (may be large).
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
type RecordingStage struct {
	BaseStage
	eventBus  *events.EventBus
	config    RecordingStageConfig
	startTime time.Time
}

// NewRecordingStage creates a new recording stage.
func NewRecordingStage(eventBus *events.EventBus, config RecordingStageConfig) *RecordingStage {
	name := "recording_" + string(config.Position)
	return &RecordingStage{
		BaseStage: NewBaseStage(name, StageTypeTransform),
		eventBus:  eventBus,
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

// recordElement converts a StreamElement to events and publishes them.
func (rs *RecordingStage) recordElement(_ context.Context, elem *StreamElement) {
	if rs.eventBus == nil {
		return
	}

	// Skip control signals (errors, end-of-stream)
	if elem.IsControl() && elem.Error == nil {
		return
	}

	// Determine the actor based on position
	role := rs.determineRole()

	// Record based on content type
	switch {
	case elem.Text != nil:
		rs.recordTextElement(elem, role)
	case elem.Message != nil:
		rs.recordMessageElement(elem)
	case elem.Audio != nil && rs.config.IncludeAudio:
		rs.recordAudioElement(elem, role)
	case elem.Image != nil && rs.config.IncludeImages:
		rs.recordImageElement(elem, role)
	case elem.Video != nil && rs.config.IncludeVideo:
		rs.recordVideoElement(elem, role)
	case elem.ToolCall != nil:
		rs.recordToolCallElement(elem)
	case elem.Error != nil:
		rs.recordErrorElement(elem)
	}
}

// determineRole returns the message role based on recording position.
func (rs *RecordingStage) determineRole() string {
	if rs.config.Position == RecordingPositionInput {
		return roleUser
	}
	return roleAssistant
}

// recordTextElement records a text element as a message event.
func (rs *RecordingStage) recordTextElement(elem *StreamElement, role string) {
	rs.eventBus.Publish(&events.Event{
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
func (rs *RecordingStage) recordMessageElement(elem *StreamElement) {
	msg := elem.Message
	data := &events.MessageCreatedData{
		Role:    msg.Role,
		Content: msg.Content,
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
			ID:      msg.ToolResult.ID,
			Name:    msg.ToolResult.Name,
			Content: msg.ToolResult.Content,
		}
	}

	rs.eventBus.Publish(&events.Event{
		Type:           events.EventMessageCreated,
		Timestamp:      elem.Timestamp,
		SessionID:      rs.config.SessionID,
		ConversationID: rs.config.ConversationID,
		Data:           data,
	})
}

// recordAudioElement records an audio element.
func (rs *RecordingStage) recordAudioElement(elem *StreamElement, role string) {
	audio := elem.Audio

	// Create audio-specific event data
	data := map[string]interface{}{
		"role":        role,
		"encoding":    audio.Format.String(),
		"sample_rate": audio.SampleRate,
		"channels":    audio.Channels,
		"duration_ms": audio.Duration.Milliseconds(),
		"size_bytes":  len(audio.Samples),
	}

	dataJSON, _ := json.Marshal(data)

	rs.eventBus.Publish(&events.Event{
		Type:           events.EventMessageCreated,
		Timestamp:      elem.Timestamp,
		SessionID:      rs.config.SessionID,
		ConversationID: rs.config.ConversationID,
		Data: &events.MessageCreatedData{
			Role:    role,
			Content: string(dataJSON), // Store as JSON for now
		},
	})
}

// recordImageElement records an image element.
func (rs *RecordingStage) recordImageElement(elem *StreamElement, role string) {
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

	rs.eventBus.Publish(&events.Event{
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
func (rs *RecordingStage) recordVideoElement(elem *StreamElement, role string) {
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

	rs.eventBus.Publish(&events.Event{
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
func (rs *RecordingStage) recordToolCallElement(elem *StreamElement) {
	tc := elem.ToolCall

	rs.eventBus.Publish(&events.Event{
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
func (rs *RecordingStage) recordErrorElement(elem *StreamElement) {
	rs.eventBus.Publish(&events.Event{
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
