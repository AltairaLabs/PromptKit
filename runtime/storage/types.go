package storage

import (
	"time"
)

// MediaMetadata contains metadata about stored media for organization and policy enforcement.
// This metadata is used to organize media files in storage and apply retention policies.
type MediaMetadata struct {
	// RunID identifies the test run that generated this media
	RunID string `json:"run_id"`

	// ConversationID identifies the conversation containing this media
	ConversationID string `json:"conversation_id,omitempty"`

	// SessionID identifies the session (for streaming sessions)
	SessionID string `json:"session_id,omitempty"`

	// MessageIdx is the index of the message containing this media (0-based)
	MessageIdx int `json:"message_idx"`

	// PartIdx is the index of the content part containing this media (0-based)
	PartIdx int `json:"part_idx"`

	// MIMEType is the media MIME type (e.g., "image/jpeg", "audio/mp3")
	MIMEType string `json:"mime_type"`

	// SizeBytes is the size of the media content in bytes
	SizeBytes int64 `json:"size_bytes"`

	// ProviderID identifies the provider that generated this media
	ProviderID string `json:"provider_id,omitempty"`

	// Timestamp is when the media was stored
	Timestamp time.Time `json:"timestamp"`

	// PolicyName is the retention policy to apply to this media
	PolicyName string `json:"policy_name,omitempty"`
}

// OrganizationMode defines how media files are organized in storage.
type OrganizationMode string

const (
	// OrganizationBySession organizes media by session ID
	OrganizationBySession OrganizationMode = "by-session"

	// OrganizationByConversation organizes media by conversation ID
	OrganizationByConversation OrganizationMode = "by-conversation"

	// OrganizationByRun organizes media by run ID
	OrganizationByRun OrganizationMode = "by-run"
)

// Reference is a reference to media stored in a backend.
// The format and meaning is backend-specific.
type Reference string
