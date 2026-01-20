package adapters

import (
	"encoding/json"
	"fmt"
	"os"
	"time"

	"github.com/AltairaLabs/PromptKit/runtime/types"
)

// SessionRecordingAdapter loads PromptKit session recordings (*.recording.json).
type SessionRecordingAdapter struct{}

// NewSessionRecordingAdapter creates a new session recording adapter.
func NewSessionRecordingAdapter() *SessionRecordingAdapter {
	return &SessionRecordingAdapter{}
}

// CanHandle returns true for *.recording.json files or "session" type hint.
func (a *SessionRecordingAdapter) CanHandle(source, typeHint string) bool {
	if matchesTypeHint(typeHint, "session", "recording", "session_recording") {
		return true
	}
	// Check if any part of a glob pattern would match our extensions
	return hasExtension(source, ".recording.json")
}

// Enumerate expands a source into individual recording references.
// For file-based sources, this expands glob patterns to matching files.
func (a *SessionRecordingAdapter) Enumerate(source string) ([]RecordingReference, error) {
	return EnumerateFiles(source, "session")
}

// Load reads a session recording file and converts it to Arena messages.
func (a *SessionRecordingAdapter) Load(ref RecordingReference) ([]types.Message, *RecordingMetadata, error) {
	//nolint:gosec // File path is provided by user/config, not external input
	data, err := os.ReadFile(ref.ID)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to read session recording: %w", err)
	}

	var recording SessionRecordingFile
	if err := json.Unmarshal(data, &recording); err != nil {
		return nil, nil, fmt.Errorf("failed to parse session recording JSON: %w", err)
	}

	messages, metadata := a.convertToMessages(&recording)
	return messages, metadata, nil
}

// convertToMessages transforms session recording to Arena messages and metadata.
func (a *SessionRecordingAdapter) convertToMessages(rec *SessionRecordingFile) ([]types.Message, *RecordingMetadata) {
	messages := make([]types.Message, 0, len(rec.Events))
	timestamps := make([]time.Time, 0, len(rec.Events))

	for i := range rec.Events {
		event := &rec.Events[i]
		if event.Type != "message" {
			continue // Skip non-message events
		}

		msg := a.buildMessage(&event.Message)
		messages = append(messages, msg)
		timestamps = append(timestamps, event.Timestamp)
	}

	metadata := &RecordingMetadata{
		SessionID:  rec.Metadata.SessionID,
		Tags:       rec.Metadata.Tags,
		Timestamps: timestamps,
		Extras:     make(map[string]interface{}),
	}

	// Extract provider info
	if rec.Metadata.ProviderID != "" || rec.Metadata.Model != "" {
		metadata.ProviderInfo = map[string]interface{}{
			"provider_id": rec.Metadata.ProviderID,
			"model":       rec.Metadata.Model,
		}
	}

	// Calculate duration if we have timestamps
	if len(timestamps) > 1 {
		metadata.Duration = timestamps[len(timestamps)-1].Sub(timestamps[0])
	}

	return messages, metadata
}

// buildMessage constructs a types.Message from a RecordedMsg.
func (a *SessionRecordingAdapter) buildMessage(recordedMsg *RecordedMsg) types.Message {
	msg := types.Message{
		Role:    recordedMsg.Role,
		Content: recordedMsg.Content,
	}

	// Convert content parts (multimodal support)
	if len(recordedMsg.Parts) > 0 {
		msg.Parts = make([]types.ContentPart, len(recordedMsg.Parts))
		for i, part := range recordedMsg.Parts {
			msg.Parts[i] = a.convertContentPart(part)
		}
	}

	// Convert tool calls
	if len(recordedMsg.ToolCalls) > 0 {
		msg.ToolCalls = make([]types.MessageToolCall, len(recordedMsg.ToolCalls))
		for i, tc := range recordedMsg.ToolCalls {
			msg.ToolCalls[i] = types.MessageToolCall{
				ID:   tc.ID,
				Name: tc.Function.Name,
				Args: json.RawMessage(tc.Function.Arguments),
			}
		}
	}

	// Set tool result for tool role messages
	if recordedMsg.ToolCallID != "" {
		msg.ToolResult = &types.MessageToolResult{
			ID:      recordedMsg.ToolCallID,
			Content: recordedMsg.Content,
		}
	}

	return msg
}

// convertContentPart converts a recorded content part to types.ContentPart.
func (a *SessionRecordingAdapter) convertContentPart(part RecordedContentPart) types.ContentPart {
	cp := types.ContentPart{
		Type: part.Type,
	}

	switch part.Type {
	case "text":
		if part.Text != nil && *part.Text != "" {
			cp.Text = part.Text
		}
	case "image", "audio", "video", "document":
		if part.Media != nil {
			cp.Media = a.convertMediaPart(part.Media)
		}
	}

	return cp
}

// convertMediaPart converts a RecordedMediaPart to types.MediaContent.
func (a *SessionRecordingAdapter) convertMediaPart(media *RecordedMediaPart) *types.MediaContent {
	return convertMediaToContent(&MediaSource{
		MIMEType: media.MIMEType,
		Data:     media.Data,
		URI:      media.URI,
		Path:     media.Path,
		Size:     media.Size,
		Width:    media.Width,
		Height:   media.Height,
		Duration: media.Duration,
	})
}

// SessionRecordingFile represents the structure of a *.recording.json file.
type SessionRecordingFile struct {
	Metadata RecordingMetadataFile `json:"metadata"`
	Events   []RecordingEvent      `json:"events"`
}

// RecordingMetadataFile contains metadata about the recording session.
type RecordingMetadataFile struct {
	SessionID  string   `json:"session_id"`
	ProviderID string   `json:"provider_id,omitempty"`
	Model      string   `json:"model,omitempty"`
	Tags       []string `json:"tags,omitempty"`
}

// RecordingEvent represents a single event in the recording.
type RecordingEvent struct {
	Type      string      `json:"type"`
	Timestamp time.Time   `json:"timestamp"`
	Message   RecordedMsg `json:"message,omitempty"`
	Event     interface{} `json:"event,omitempty"`
}

// RecordedMsg represents a message in the recording.
type RecordedMsg struct {
	Role       string                `json:"role"`
	Content    string                `json:"content"`
	Name       string                `json:"name,omitempty"`
	Parts      []RecordedContentPart `json:"parts,omitempty"`
	ToolCalls  []RecordedToolCall    `json:"tool_calls,omitempty"`
	ToolCallID string                `json:"tool_call_id,omitempty"`
}

// RecordedContentPart represents a content part in the recording.
type RecordedContentPart struct {
	Type  string             `json:"type"`
	Text  *string            `json:"text,omitempty"`
	Media *RecordedMediaPart `json:"media,omitempty"`
}

// RecordedMediaPart represents media content in the recording.
type RecordedMediaPart struct {
	MIMEType string `json:"mime_type,omitempty"`
	Data     string `json:"data,omitempty"` // Base64 encoded
	URI      string `json:"uri,omitempty"`  // URL
	Path     string `json:"path,omitempty"` // File path
	Size     int64  `json:"size,omitempty"`
	Width    int    `json:"width,omitempty"`
	Height   int    `json:"height,omitempty"`
	Duration int64  `json:"duration,omitempty"` // milliseconds
}

// RecordedToolCall represents a tool call in the recording.
type RecordedToolCall struct {
	ID       string                   `json:"id"`
	Type     string                   `json:"type"`
	Function RecordedToolCallFunction `json:"function"`
}

// RecordedToolCallFunction represents a function call within a tool call.
type RecordedToolCallFunction struct {
	Name      string `json:"name"`
	Arguments string `json:"arguments"`
}
