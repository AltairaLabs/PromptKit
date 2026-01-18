package adapters

import (
	"fmt"
	"os"
	"time"

	"gopkg.in/yaml.v3"

	"github.com/AltairaLabs/PromptKit/runtime/types"
)

const (
	contentTypeText     = "text"
	contentTypeImage    = "image"
	contentTypeAudio    = "audio"
	contentTypeVideo    = "video"
	contentTypeDocument = "document"
)

// TranscriptAdapter loads transcript YAML files (*.transcript.yaml).
type TranscriptAdapter struct{}

// NewTranscriptAdapter creates a new transcript adapter.
func NewTranscriptAdapter() *TranscriptAdapter {
	return &TranscriptAdapter{}
}

// CanHandle returns true for *.transcript.yaml files or "transcript" type hint.
func (a *TranscriptAdapter) CanHandle(path, typeHint string) bool {
	if matchesTypeHint(typeHint, "transcript", "yaml") {
		return true
	}
	return hasExtension(path, ".transcript.yaml", ".transcript.yml")
}

// Load reads a transcript file and converts it to Arena messages.
func (a *TranscriptAdapter) Load(path string) ([]types.Message, *RecordingMetadata, error) {
	//nolint:gosec // File path is provided by user/config, not external input
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to read transcript: %w", err)
	}

	var transcript TranscriptFile
	if err := yaml.Unmarshal(data, &transcript); err != nil {
		return nil, nil, fmt.Errorf("failed to parse transcript YAML: %w", err)
	}

	messages, metadata := a.convertToMessages(&transcript)
	return messages, metadata, nil
}

// convertToMessages transforms transcript to Arena messages and metadata.
func (a *TranscriptAdapter) convertToMessages(transcript *TranscriptFile) ([]types.Message, *RecordingMetadata) {
	messages := make([]types.Message, len(transcript.Messages))
	timestamps := make([]time.Time, len(transcript.Messages))

	for i := range transcript.Messages {
		msg := &transcript.Messages[i]
		messages[i] = a.buildMessage(msg)
		timestamps[i] = a.parseTimestamp(msg.Timestamp)
	}

	metadata := &RecordingMetadata{
		Tags:       transcript.Metadata.Tags,
		Timestamps: timestamps,
		Extras:     make(map[string]interface{}),
	}

	// Extract provider info
	if transcript.Metadata.Provider != "" || transcript.Metadata.Model != "" {
		metadata.ProviderInfo = map[string]interface{}{
			"provider": transcript.Metadata.Provider,
			"model":    transcript.Metadata.Model,
		}
	}

	// Set session ID if available
	if transcript.Metadata.SessionID != "" {
		metadata.SessionID = transcript.Metadata.SessionID
	}

	// Extract judge targets if present
	if len(transcript.Metadata.JudgeTargets) > 0 {
		metadata.JudgeTargets = make(map[string]ProviderSpec)
		for name, spec := range transcript.Metadata.JudgeTargets {
			metadata.JudgeTargets[name] = ProviderSpec(spec)
		}
	}

	// Calculate duration if we have timestamps
	if len(timestamps) > 1 {
		first := timestamps[0]
		last := timestamps[len(timestamps)-1]
		if !first.IsZero() && !last.IsZero() {
			metadata.Duration = last.Sub(first)
		}
	}

	return messages, metadata
}

// buildMessage constructs a types.Message from a TranscriptMessage.
func (a *TranscriptAdapter) buildMessage(msg *TranscriptMessage) types.Message {
	result := types.Message{
		Role:    msg.Role,
		Content: msg.Content,
	}

	// Convert parts if present
	if len(msg.Parts) > 0 {
		result.Parts = make([]types.ContentPart, len(msg.Parts))
		for j, part := range msg.Parts {
			result.Parts[j] = a.convertContentPart(part)
		}
	}

	// Convert tool calls if present
	if len(msg.ToolCalls) > 0 {
		result.ToolCalls = make([]types.MessageToolCall, len(msg.ToolCalls))
		for j, tc := range msg.ToolCalls {
			result.ToolCalls[j] = types.MessageToolCall{
				ID:   tc.ID,
				Name: tc.Function.Name,
				Args: []byte(tc.Function.Arguments),
			}
		}
	}

	// Set tool result for tool role messages
	if msg.ToolCallID != "" {
		result.ToolResult = &types.MessageToolResult{
			ID:      msg.ToolCallID,
			Content: msg.Content,
		}
	}

	return result
}

// parseTimestamp parses a timestamp string, returning zero time if invalid.
func (a *TranscriptAdapter) parseTimestamp(timestamp string) time.Time {
	if timestamp == "" {
		return time.Time{}
	}
	ts, err := time.Parse(time.RFC3339, timestamp)
	if err != nil {
		return time.Time{}
	}
	return ts
}

// convertContentPart converts a transcript content part to types.ContentPart.
func (a *TranscriptAdapter) convertContentPart(part TranscriptContentPart) types.ContentPart {
	cp := types.ContentPart{
		Type: part.Type,
	}

	switch part.Type {
	case contentTypeText:
		if part.Text != nil && *part.Text != "" {
			cp.Text = part.Text
		}
	case contentTypeImage, contentTypeAudio, contentTypeVideo, contentTypeDocument:
		if part.Media != nil {
			cp.Media = a.convertTranscriptMedia(part.Media)
		}
	}

	return cp
}

// convertTranscriptMedia converts a TranscriptMediaPart to types.MediaContent.
func (a *TranscriptAdapter) convertTranscriptMedia(media *TranscriptMediaPart) *types.MediaContent {
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

// TranscriptFile represents the structure of a *.transcript.yaml file.
type TranscriptFile struct {
	Metadata TranscriptMetadata  `yaml:"metadata"`
	Messages []TranscriptMessage `yaml:"messages"`
}

// TranscriptMetadata contains metadata about the transcript.
type TranscriptMetadata struct {
	SessionID    string                            `yaml:"session_id,omitempty"`
	Provider     string                            `yaml:"provider,omitempty"`
	Model        string                            `yaml:"model,omitempty"`
	Tags         []string                          `yaml:"tags,omitempty"`
	JudgeTargets map[string]TranscriptProviderSpec `yaml:"judge_targets,omitempty"`
}

// TranscriptProviderSpec describes a provider for judge targets in transcripts.
type TranscriptProviderSpec struct {
	Type  string `yaml:"type"`
	Model string `yaml:"model"`
	ID    string `yaml:"id"`
}

// TranscriptMessage represents a message in the transcript.
type TranscriptMessage struct {
	Role       string                  `yaml:"role"`
	Content    string                  `yaml:"content"`
	Name       string                  `yaml:"name,omitempty"`
	Timestamp  string                  `yaml:"timestamp,omitempty"`
	Parts      []TranscriptContentPart `yaml:"parts,omitempty"`
	ToolCalls  []TranscriptToolCall    `yaml:"tool_calls,omitempty"`
	ToolCallID string                  `yaml:"tool_call_id,omitempty"`
}

// TranscriptContentPart represents a content part in the transcript.
type TranscriptContentPart struct {
	Type  string               `yaml:"type"`
	Text  *string              `yaml:"text,omitempty"`
	Media *TranscriptMediaPart `yaml:"media,omitempty"`
}

// TranscriptMediaPart represents media content in the transcript.
type TranscriptMediaPart struct {
	MIMEType string `yaml:"mime_type,omitempty"`
	Data     string `yaml:"data,omitempty"` // Base64 encoded
	URI      string `yaml:"uri,omitempty"`  // URL
	Path     string `yaml:"path,omitempty"` // File path
	Size     int64  `yaml:"size,omitempty"`
	Width    int    `yaml:"width,omitempty"`
	Height   int    `yaml:"height,omitempty"`
	Duration int64  `yaml:"duration,omitempty"` // milliseconds
}

// TranscriptToolCall represents a tool call in the transcript.
type TranscriptToolCall struct {
	ID       string                     `yaml:"id"`
	Type     string                     `yaml:"type"`
	Function TranscriptToolCallFunction `yaml:"function"`
}

// TranscriptToolCallFunction represents a function call within a tool call.
type TranscriptToolCallFunction struct {
	Name      string `yaml:"name"`
	Arguments string `yaml:"arguments"`
}
