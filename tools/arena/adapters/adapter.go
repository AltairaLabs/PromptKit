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
