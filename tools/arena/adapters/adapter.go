// Package adapters provides pluggable recording format adapters for Arena evaluation.
// It supports loading saved conversations from various formats (session recordings,
// arena output files, transcripts) into Arena-friendly structures.
package adapters

import (
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"github.com/AltairaLabs/PromptKit/runtime/types"
)

// RecordingReference is an opaque reference to a single recording.
// It abstracts the underlying storage mechanism (file, database, API, etc.)
// allowing adapters to enumerate and load recordings from various sources.
type RecordingReference struct {
	// ID is a unique identifier for this recording reference.
	// For file-based adapters, this is typically the file path.
	// For database adapters, this could be a record ID.
	ID string `json:"id" yaml:"id"`

	// Source is the original source pattern/query that produced this reference.
	// For file-based adapters, this is the original glob pattern or path.
	Source string `json:"source" yaml:"source"`

	// TypeHint is an optional format indicator (e.g., "session", "arena_output", "transcript").
	TypeHint string `json:"type_hint,omitempty" yaml:"type_hint,omitempty"`

	// Metadata contains adapter-specific metadata about this reference.
	// This can include file size, modification time, database metadata, etc.
	Metadata map[string]interface{} `json:"metadata,omitempty" yaml:"metadata,omitempty"`
}

// RecordingAdapter converts saved conversations from various formats
// into Arena-friendly structures for evaluation.
type RecordingAdapter interface {
	// CanHandle returns true if this adapter supports the given source/type hint.
	// The source could be a file path, glob pattern, database query, etc.
	// typeHint is an optional explicit format indicator from the eval config.
	CanHandle(source string, typeHint string) bool

	// Enumerate expands a source into individual recording references.
	// For file-based adapters, this expands glob patterns to matching files.
	// For database adapters, this could execute a query and return record IDs.
	// Returns a single-element slice for non-expandable sources.
	Enumerate(source string) ([]RecordingReference, error)

	// Load converts a recording to Arena message format.
	// The reference should have been obtained from Enumerate.
	// Returns the messages, metadata, and any error encountered.
	Load(ref RecordingReference) ([]types.Message, *RecordingMetadata, error)
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

// EnumerateFiles is a helper for file-based adapters to expand glob patterns.
// It returns recording references for each matching file.
// If the source doesn't contain glob characters, it returns a single reference.
func EnumerateFiles(source, typeHint string) ([]RecordingReference, error) {
	// Check if source contains glob characters
	if !containsGlobChars(source) {
		return []RecordingReference{{
			ID:       source,
			Source:   source,
			TypeHint: typeHint,
		}}, nil
	}

	// Expand the glob pattern
	matches, err := filepath.Glob(source)
	if err != nil {
		return nil, fmt.Errorf("invalid glob pattern: %w", err)
	}

	if len(matches) == 0 {
		return nil, fmt.Errorf("no files matched glob pattern: %s", source)
	}

	refs := make([]RecordingReference, len(matches))
	for i, match := range matches {
		refs[i] = RecordingReference{
			ID:       match,
			Source:   source,
			TypeHint: typeHint,
		}
	}

	return refs, nil
}

// containsGlobChars checks if a path contains glob metacharacters.
func containsGlobChars(path string) bool {
	return strings.ContainsAny(path, "*?[")
}
