package types

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

// Message represents a single message in a conversation.
// This is the canonical message type used throughout the system.
type Message struct {
	Role    string `json:"role"`    // "system", "user", "assistant", "tool"
	Content string `json:"content"` // Message content (legacy text-only, maintained for backward compatibility)

	// Multimodal content parts (text, images, audio, video)
	// If Parts is non-empty, it takes precedence over Content.
	// For backward compatibility, if Parts is empty, Content will be used.
	Parts []ContentPart `json:"parts,omitempty"`

	// Tool invocations (for assistant messages that call tools)
	ToolCalls []MessageToolCall `json:"tool_calls,omitempty"`

	// Tool result (for tool role messages)
	// When Role="tool", this contains the tool execution result
	ToolResult *MessageToolResult `json:"tool_result,omitempty"`

	// Source indicates where this message originated (runtime-only, not persisted in JSON)
	// Values: "statestore" (loaded from StateStore), "pipeline" (created during execution), "" (user input)
	Source string `json:"-"`

	// Metadata for observability and tracking
	Timestamp time.Time              `json:"timestamp,omitempty"`  // When the message was created
	LatencyMs int64                  `json:"latency_ms,omitempty"` // Time taken to generate (for assistant messages)
	CostInfo  *CostInfo              `json:"cost_info,omitempty"`  // Token usage and cost tracking
	Meta      map[string]interface{} `json:"meta,omitempty"`       // Custom metadata

	// Validation results (for assistant messages)
	Validations []ValidationResult `json:"validations,omitempty"`
}

// MessageToolCall represents a request to call a tool within a Message.
// The Args field contains the JSON-encoded arguments for the tool.
type MessageToolCall struct {
	ID   string          `json:"id"`   // Unique identifier for this tool call
	Name string          `json:"name"` // Name of the tool to invoke
	Args json.RawMessage `json:"args"` // JSON-encoded tool arguments
}

// MessageToolResult represents the result of a tool execution in a Message.
// When embedded in Message, the Message.Role should be "tool".
type MessageToolResult struct {
	ID        string `json:"id"`              // References the MessageToolCall.ID that triggered this result
	Name      string `json:"name"`            // Tool name that was executed
	Content   string `json:"content"`         // Result content or error message
	Error     string `json:"error,omitempty"` // Error message if tool execution failed
	LatencyMs int64  `json:"latency_ms"`      // Tool execution latency in milliseconds
}

// ToolDef represents a tool definition that can be provided to an LLM.
// The InputSchema and OutputSchema use JSON Schema format for validation.
type ToolDef struct {
	Name         string          `json:"name"`                    // Unique tool name
	Description  string          `json:"description"`             // Human-readable description of what the tool does
	InputSchema  json.RawMessage `json:"input_schema"`            // JSON Schema for input validation
	OutputSchema json.RawMessage `json:"output_schema,omitempty"` // Optional JSON Schema for output validation
}

// CostInfo tracks token usage and associated costs for LLM operations.
// All cost values are in USD. Used for both individual messages and aggregated tracking.
type CostInfo struct {
	InputTokens   int     `json:"input_tokens"`              // Number of input tokens consumed
	OutputTokens  int     `json:"output_tokens"`             // Number of output tokens generated
	CachedTokens  int     `json:"cached_tokens,omitempty"`   // Number of cached tokens used (reduces cost)
	InputCostUSD  float64 `json:"input_cost_usd"`            // Cost of input tokens in USD
	OutputCostUSD float64 `json:"output_cost_usd"`           // Cost of output tokens in USD
	CachedCostUSD float64 `json:"cached_cost_usd,omitempty"` // Cost savings from cached tokens
	TotalCost     float64 `json:"total_cost_usd"`            // Total cost in USD
}

// ToolStats tracks tool usage statistics across a conversation or run.
// Useful for monitoring which tools are being used and how frequently.
type ToolStats struct {
	TotalCalls int            `json:"total_calls"` // Total number of tool calls
	ByTool     map[string]int `json:"by_tool"`     // Count of calls per tool name
}

// ValidationError represents a validation failure in tool usage or message content.
// Used to provide structured error information when validation fails.
type ValidationError struct {
	Type   string `json:"type"`   // Error type: "args_invalid" | "result_invalid" | "policy_violation"
	Tool   string `json:"tool"`   // Name of the tool that failed validation
	Detail string `json:"detail"` // Human-readable error details
}

// ValidationResult represents the outcome of a validator check on a message.
// These are attached to assistant messages to show which validations passed or failed.
type ValidationResult struct {
	ValidatorType string                 `json:"validator_type"`      // Type of validator
	Passed        bool                   `json:"passed"`              // Whether the validation passed
	Details       map[string]interface{} `json:"details,omitempty"`   // Validator-specific details
	Timestamp     time.Time              `json:"timestamp,omitempty"` // When validation was performed
}

// GetContent returns the content of the message.
// This is the recommended way to access message content as it handles all cases:
// 1. For tool messages (Role="tool"): returns ToolResult.Content (authoritative source)
// 2. For multimodal messages: returns concatenated text parts
// 3. For legacy messages: returns the Content field
func (m *Message) GetContent() string {
	// Tool messages: ToolResult.Content is the authoritative source
	// This handles the case where Content field may be empty but ToolResult.Content is set
	if m.Role == "tool" && m.ToolResult != nil {
		return m.ToolResult.Content
	}

	// Multimodal messages: extract text from parts
	if len(m.Parts) > 0 {
		var text string
		for _, part := range m.Parts {
			if part.Type == ContentTypeText && part.Text != nil {
				text += *part.Text
			}
		}
		return text
	}

	return m.Content
}

// IsMultimodal returns true if the message contains multimodal content (Parts)
func (m *Message) IsMultimodal() bool {
	return len(m.Parts) > 0
}

// HasMediaContent returns true if the message contains any media (image, audio, video, document)
func (m *Message) HasMediaContent() bool {
	for _, part := range m.Parts {
		if part.Type == ContentTypeImage || part.Type == ContentTypeAudio ||
			part.Type == ContentTypeVideo || part.Type == ContentTypeDocument {
			return true
		}
	}
	return false
}

// SetTextContent sets the message content to simple text.
// This clears any existing Parts and sets the legacy Content field.
func (m *Message) SetTextContent(text string) {
	m.Content = text
	m.Parts = nil
}

// SetMultimodalContent sets the message content to multimodal parts.
// This clears the legacy Content field.
func (m *Message) SetMultimodalContent(parts []ContentPart) {
	m.Parts = parts
	m.Content = ""
}

// AddPart adds a content part to the message.
// If this is the first part added, it clears the legacy Content field.
func (m *Message) AddPart(part ContentPart) {
	if len(m.Parts) == 0 {
		m.Content = ""
	}
	m.Parts = append(m.Parts, part)
}

// AddTextPart adds a text content part to the message
func (m *Message) AddTextPart(text string) {
	m.AddPart(NewTextPart(text))
}

// AddImagePart adds an image content part from a file path
func (m *Message) AddImagePart(filePath string, detail *string) error {
	part, err := NewImagePart(filePath, detail)
	if err != nil {
		return err
	}
	m.AddPart(part)
	return nil
}

// AddImagePartFromURL adds an image content part from a URL
func (m *Message) AddImagePartFromURL(url string, detail *string) {
	m.AddPart(NewImagePartFromURL(url, detail))
}

// AddAudioPart adds an audio content part from a file path
func (m *Message) AddAudioPart(filePath string) error {
	part, err := NewAudioPart(filePath)
	if err != nil {
		return err
	}
	m.AddPart(part)
	return nil
}

// AddVideoPart adds a video content part from a file path
func (m *Message) AddVideoPart(filePath string) error {
	part, err := NewVideoPart(filePath)
	if err != nil {
		return err
	}
	m.AddPart(part)
	return nil
}

// AddDocumentPart adds a document content part from a file path
func (m *Message) AddDocumentPart(filePath string) error {
	part, err := NewDocumentPart(filePath)
	if err != nil {
		return err
	}
	m.AddPart(part)
	return nil
}

// =============================================================================
// Message Constructors
//
// These constructors ensure proper initialization and field synchronization.
// Always prefer these over direct struct initialization.
// =============================================================================

// NewTextMessage creates a simple text message.
// Use this for user, assistant, or system messages with text-only content.
func NewTextMessage(role, content string) Message {
	return Message{
		Role:    role,
		Content: content,
	}
}

// NewUserMessage creates a user message with text content.
func NewUserMessage(content string) Message {
	return NewTextMessage("user", content)
}

// NewAssistantMessage creates an assistant message with text content.
func NewAssistantMessage(content string) Message {
	return NewTextMessage("assistant", content)
}

// NewSystemMessage creates a system message with text content.
func NewSystemMessage(content string) Message {
	return NewTextMessage("system", content)
}

// NewToolResultMessage creates a properly normalized tool result message.
// This ensures Content and ToolResult.Content are always synchronized,
// which is required for provider compatibility and consistent behavior.
//
// IMPORTANT: Always use this constructor instead of directly creating
// Message{Role: "tool", ToolResult: ...} to avoid Content/ToolResult.Content
// synchronization issues.
func NewToolResultMessage(result MessageToolResult) Message {
	return Message{
		Role:       "tool",
		Content:    result.Content, // Sync Content for backward compatibility
		ToolResult: &result,
	}
}

// NewMultimodalMessage creates a message with multimodal content parts.
// When using Parts, the Content field is intentionally left empty as
// GetContent() will extract text from Parts.
func NewMultimodalMessage(role string, parts []ContentPart) Message {
	return Message{
		Role:  role,
		Parts: parts,
	}
}

// MediaSummary provides a high-level overview of media content in a message.
// This is included in JSON output to make multimodal messages more observable.
type MediaSummary struct {
	TotalParts    int                `json:"total_parts"`           // Total number of content parts
	TextParts     int                `json:"text_parts"`            // Number of text parts
	ImageParts    int                `json:"image_parts"`           // Number of image parts
	AudioParts    int                `json:"audio_parts"`           // Number of audio parts
	VideoParts    int                `json:"video_parts"`           // Number of video parts
	DocumentParts int                `json:"document_parts"`        // Number of document parts
	MediaItems    []MediaItemSummary `json:"media_items,omitempty"` // Details of each media item
}

// MediaItemSummary provides details about a single media item in a message.
type MediaItemSummary struct {
	Type      string `json:"type"`             // Content type: "image", "audio", "video"
	Source    string `json:"source"`           // Source description (file path, URL, or "inline data")
	MIMEType  string `json:"mime_type"`        // MIME type
	SizeBytes int    `json:"size_bytes"`       // Size in bytes (0 if unknown)
	Detail    string `json:"detail,omitempty"` // Detail level for images
	Loaded    bool   `json:"loaded"`           // Whether media was successfully loaded
	Error     string `json:"error,omitempty"`  // Error message if loading failed
}

// MarshalJSON implements custom JSON marshaling for Message.
// This enhances the output by:
// 1. Populating the Content field with a human-readable summary when Parts exist
// 2. Adding a MediaSummary field for observability of multimodal content
// 3. Omitting Content field when ToolResult is present to avoid duplication
func (m Message) MarshalJSON() ([]byte, error) {
	// Create a type alias to avoid infinite recursion
	type MessageAlias Message

	// Convert to alias to get default marshaling behavior
	aux := struct {
		MessageAlias
		MediaSummary *MediaSummary `json:"media_summary,omitempty"`
		Content      string        `json:"content,omitempty"` // Override to control omission
	}{
		MessageAlias: MessageAlias(m),
		Content:      m.Content, // Default to actual content
	}

	// If Parts exist and Content is empty, populate content with summary
	if len(m.Parts) > 0 && m.Content == "" {
		aux.Content = m.getContentSummary()
	}

	// Add media summary if Parts exist
	if len(m.Parts) > 0 {
		aux.MediaSummary = m.getMediaSummary()
	}

	// If ToolResult is present, omit Content to avoid duplication
	if m.ToolResult != nil {
		aux.Content = "" // Empty string with omitempty will omit the field
	}

	return json.Marshal(aux)
}

// UnmarshalJSON implements custom JSON unmarshaling for Message.
// After unmarshaling, if ToolResult is present, copy its Content to Message.Content
// for provider compatibility (providers expect Content field to be populated).
func (m *Message) UnmarshalJSON(data []byte) error {
	type MessageAlias Message
	aux := (*MessageAlias)(m)
	if err := json.Unmarshal(data, aux); err != nil {
		return err
	}
	// If tool result is present but Content is empty, copy from ToolResult
	if m.ToolResult != nil && m.Content == "" {
		m.Content = m.ToolResult.Content
	}
	return nil
}

// getContentSummary returns a human-readable summary of the message content.
// For multimodal messages, this includes text parts and a summary of media.
func (m *Message) getContentSummary() string {
	if len(m.Parts) == 0 {
		return m.Content
	}

	var parts []string

	// Collect text parts
	for _, part := range m.Parts {
		if part.Type == ContentTypeText && part.Text != nil {
			parts = append(parts, *part.Text)
		}
	}

	// Add media summary
	mediaCounts := make(map[string]int)
	for _, part := range m.Parts {
		if part.Type != ContentTypeText {
			mediaCounts[part.Type]++
		}
	}

	if len(mediaCounts) > 0 {
		var mediaDesc []string
		if count := mediaCounts[ContentTypeImage]; count > 0 {
			mediaDesc = append(mediaDesc, fmt.Sprintf("%d image(s)", count))
		}
		if count := mediaCounts[ContentTypeAudio]; count > 0 {
			mediaDesc = append(mediaDesc, fmt.Sprintf("%d audio file(s)", count))
		}
		if count := mediaCounts[ContentTypeVideo]; count > 0 {
			mediaDesc = append(mediaDesc, fmt.Sprintf("%d video(s)", count))
		}
		parts = append(parts, "["+strings.Join(mediaDesc, ", ")+"]")
	}

	return strings.Join(parts, " ")
}

// getMediaSummary generates a MediaSummary with details about all media in the message.
func (m *Message) getMediaSummary() *MediaSummary {
	if len(m.Parts) == 0 {
		return nil
	}

	summary := &MediaSummary{
		TotalParts: len(m.Parts),
		MediaItems: make([]MediaItemSummary, 0),
	}

	for _, part := range m.Parts {
		switch part.Type {
		case ContentTypeText:
			summary.TextParts++
		case ContentTypeImage:
			summary.ImageParts++
			summary.MediaItems = append(summary.MediaItems, getMediaItemSummary(part))
		case ContentTypeAudio:
			summary.AudioParts++
			summary.MediaItems = append(summary.MediaItems, getMediaItemSummary(part))
		case ContentTypeVideo:
			summary.VideoParts++
			summary.MediaItems = append(summary.MediaItems, getMediaItemSummary(part))
		case ContentTypeDocument:
			summary.DocumentParts++
			summary.MediaItems = append(summary.MediaItems, getMediaItemSummary(part))
		}
	}

	return summary
}

// getMediaItemSummary extracts summary information from a media ContentPart.
func getMediaItemSummary(part ContentPart) MediaItemSummary {
	item := MediaItemSummary{
		Type:   part.Type,
		Loaded: false, // Will be true if Data is set
	}

	if part.Media == nil {
		item.Error = "no media content"
		return item
	}

	item.MIMEType = part.Media.MIMEType

	// Determine source - prefer FilePath/URL/StorageReference over "inline data" for better display
	if part.Media.FilePath != nil {
		item.Source = *part.Media.FilePath
		// If Data field is also set, media was successfully loaded
		if part.Media.Data != nil && *part.Media.Data != "" {
			item.Loaded = true
			// Estimate size from base64 data (roughly 3/4 of base64 length)
			const (
				base64Ratio     = 4
				base64Numerator = 3
			)
			item.SizeBytes = (len(*part.Media.Data) * base64Numerator) / base64Ratio
		}
	} else if part.Media.URL != nil {
		item.Source = *part.Media.URL
		if part.Media.Data != nil && *part.Media.Data != "" {
			item.Loaded = true
		}
	} else if part.Media.StorageReference != nil {
		item.Source = *part.Media.StorageReference
		// StorageReference means media was externalized to storage
	} else if part.Media.Data != nil && *part.Media.Data != "" {
		item.Source = "inline data"
		item.Loaded = true
		// Estimate size from base64 data (roughly 3/4 of base64 length)
		const (
			base64Ratio     = 4
			base64Numerator = 3
		)
		item.SizeBytes = (len(*part.Media.Data) * base64Numerator) / base64Ratio
	} else {
		item.Source = "unknown"
		item.Error = "no data source"
	}

	// Add detail level for images
	if part.Type == ContentTypeImage && part.Media.Detail != nil {
		item.Detail = *part.Media.Detail
	}

	// Use size metadata if available
	if part.Media.SizeKB != nil {
		const bytesPerKB = 1024
		item.SizeBytes = int(*part.Media.SizeKB * bytesPerKB)
	}

	return item
}
