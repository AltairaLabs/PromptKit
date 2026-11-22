package core

// Message roles the stages compare and set.
const (
	// RoleUser is the role identifier for user messages.
	RoleUser = "user"
	// RoleAssistant is the role identifier for assistant messages.
	RoleAssistant = "assistant"
	// RoleSystem is the role identifier for system messages.
	RoleSystem = "system"
	// RoleTool is the role identifier for tool-result messages.
	RoleTool = "tool"
)

// Metadata keys the stages share.
const (
	// MetaKeySampleRate is the snapshot/metadata key for an audio sample rate.
	MetaKeySampleRate = "sample_rate"
	// MetaKeyMIMEType is the metadata key for a media MIME type.
	MetaKeyMIMEType = "mime_type"
)
