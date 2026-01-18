// Package adapters provides pluggable recording format adapters for Arena evaluation.
// It supports loading saved conversations from various formats (session recordings,
// arena output files, transcripts) into Arena-friendly structures.
package adapters

import (
	"time"

	"github.com/AltairaLabs/PromptKit/runtime/types"
)

// RecordingAdapter converts saved conversations from various formats
// into Arena-friendly structures for evaluation.
type RecordingAdapter interface {
	// CanHandle returns true if this adapter supports the given path/type hint.
	// The path is the file path to the recording, and typeHint is an optional
	// explicit format indicator from the eval config (e.g., "session", "arena_output", "transcript").
	CanHandle(path string, typeHint string) bool

	// Load converts the recording to Arena message format.
	// Returns the messages, metadata, and any error encountered.
	Load(path string) ([]types.Message, *RecordingMetadata, error)
}

// RecordingMetadata contains metadata extracted from the recording
// that should flow through to the evaluation context.
type RecordingMetadata struct {
	// JudgeTargets maps judge names to provider specifications.
	// Used by LLM judge assertions to determine which provider to use.
	JudgeTargets map[string]ProviderSpec `json:"judge_targets,omitempty" yaml:"judge_targets,omitempty"`

	// ProviderInfo contains information about the original provider(s)
	// that generated the recorded conversation.
	ProviderInfo map[string]interface{} `json:"provider_info,omitempty" yaml:"provider_info,omitempty"`

	// Tags are optional labels for categorizing/filtering recordings.
	Tags []string `json:"tags,omitempty" yaml:"tags,omitempty"`

	// Timestamps contains the timestamp for each turn in the conversation.
	// The length should match the number of messages.
	Timestamps []time.Time `json:"timestamps,omitempty" yaml:"timestamps,omitempty"`

	// SessionID is the unique identifier for the recorded session.
	SessionID string `json:"session_id,omitempty" yaml:"session_id,omitempty"`

	// Duration is the total duration of the conversation.
	Duration time.Duration `json:"duration,omitempty" yaml:"duration,omitempty"`

	// Extras holds any additional metadata from the recording.
	Extras map[string]interface{} `json:"extras,omitempty" yaml:"extras,omitempty"`
}

// ProviderSpec describes a provider configuration for judge targets.
type ProviderSpec struct {
	Type  string `json:"type" yaml:"type"`
	Model string `json:"model" yaml:"model"`
	ID    string `json:"id" yaml:"id"`
}

const (
	bytesPerKB       = 1024
	millisecondsPerS = 1000
)

// MediaSource defines the source data for media content conversion.
type MediaSource struct {
	MIMEType string
	Data     string
	URI      string
	Path     string
	Size     int64
	Width    int
	Height   int
	Duration int64 // milliseconds
}

// convertMediaToContent converts a MediaSource to types.MediaContent.
// This is a shared helper to avoid duplication across adapters.
func convertMediaToContent(media *MediaSource) *types.MediaContent {
	result := &types.MediaContent{
		MIMEType: media.MIMEType,
	}

	// Handle different data sources
	if media.Data != "" {
		result.Data = &media.Data
	} else if media.URI != "" {
		result.URL = &media.URI
	} else if media.Path != "" {
		result.FilePath = &media.Path
	}

	// Copy media-specific fields
	if media.Size > 0 {
		sizeKB := media.Size / bytesPerKB
		result.SizeKB = &sizeKB
	}
	if media.Width > 0 {
		result.Width = &media.Width
	}
	if media.Height > 0 {
		result.Height = &media.Height
	}
	if media.Duration > 0 {
		durationSec := int(media.Duration / millisecondsPerS)
		result.Duration = &durationSec
	}

	return result
}
