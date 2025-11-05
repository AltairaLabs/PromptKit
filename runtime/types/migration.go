package types

import "fmt"

// MigrateToMultimodal converts a legacy text-only message to use the Parts structure.
// This is useful when transitioning existing code to the new multimodal API.
func MigrateToMultimodal(msg *Message) {
	if msg.IsMultimodal() {
		return // Already multimodal, nothing to do
	}

	if msg.Content != "" {
		msg.Parts = []ContentPart{NewTextPart(msg.Content)}
		msg.Content = ""
	}
}

// MigrateToLegacy converts a multimodal message back to legacy text-only format.
// This is useful for backward compatibility with systems that don't support multimodal.
// Returns an error if the message contains non-text content.
func MigrateToLegacy(msg *Message) error {
	if !msg.IsMultimodal() {
		return nil // Already legacy format
	}

	if msg.HasMediaContent() {
		return fmt.Errorf("cannot migrate message with media content to legacy format")
	}

	// Extract text from parts
	msg.Content = msg.GetContent()
	msg.Parts = nil
	return nil
}

// MigrateMessagesToMultimodal converts a slice of legacy messages to multimodal format in-place
func MigrateMessagesToMultimodal(messages []Message) {
	for i := range messages {
		MigrateToMultimodal(&messages[i])
	}
}

// MigrateMessagesToLegacy converts a slice of multimodal messages to legacy format in-place.
// Returns an error if any message contains media content.
func MigrateMessagesToLegacy(messages []Message) error {
	for i := range messages {
		if err := MigrateToLegacy(&messages[i]); err != nil {
			return fmt.Errorf("failed to migrate message %d: %w", i, err)
		}
	}
	return nil
}

// CloneMessage creates a deep copy of a message
func CloneMessage(msg Message) Message {
	clone := msg

	// Deep copy Parts slice
	if len(msg.Parts) > 0 {
		clone.Parts = make([]ContentPart, len(msg.Parts))
		for i, part := range msg.Parts {
			clone.Parts[i] = cloneContentPart(part)
		}
	}

	// Deep copy ToolCalls slice
	if len(msg.ToolCalls) > 0 {
		clone.ToolCalls = make([]MessageToolCall, len(msg.ToolCalls))
		copy(clone.ToolCalls, msg.ToolCalls)
	}

	// Deep copy ToolResult if present
	if msg.ToolResult != nil {
		result := *msg.ToolResult
		clone.ToolResult = &result
	}

	// Deep copy CostInfo if present
	if msg.CostInfo != nil {
		cost := *msg.CostInfo
		clone.CostInfo = &cost
	}

	// Deep copy Meta map
	if msg.Meta != nil {
		clone.Meta = make(map[string]interface{}, len(msg.Meta))
		for k, v := range msg.Meta {
			clone.Meta[k] = v
		}
	}

	// Deep copy Validations slice
	if len(msg.Validations) > 0 {
		clone.Validations = make([]ValidationResult, len(msg.Validations))
		copy(clone.Validations, msg.Validations)
	}

	return clone
}

// cloneContentPart creates a deep copy of a content part
func cloneContentPart(part ContentPart) ContentPart {
	clone := ContentPart{
		Type: part.Type,
	}

	// Clone text pointer
	if part.Text != nil {
		text := *part.Text
		clone.Text = &text
	}

	// Clone media content
	if part.Media != nil {
		clone.Media = cloneMediaContent(part.Media)
	}

	return clone
}

// cloneMediaContent creates a deep copy of media content
func cloneMediaContent(media *MediaContent) *MediaContent {
	clone := &MediaContent{
		MIMEType: media.MIMEType,
	}

	// Clone pointer fields
	if media.Data != nil {
		data := *media.Data
		clone.Data = &data
	}
	if media.FilePath != nil {
		path := *media.FilePath
		clone.FilePath = &path
	}
	if media.URL != nil {
		url := *media.URL
		clone.URL = &url
	}
	if media.Format != nil {
		format := *media.Format
		clone.Format = &format
	}
	if media.SizeKB != nil {
		size := *media.SizeKB
		clone.SizeKB = &size
	}
	if media.Detail != nil {
		detail := *media.Detail
		clone.Detail = &detail
	}
	if media.Caption != nil {
		caption := *media.Caption
		clone.Caption = &caption
	}
	if media.Duration != nil {
		duration := *media.Duration
		clone.Duration = &duration
	}
	if media.BitRate != nil {
		bitRate := *media.BitRate
		clone.BitRate = &bitRate
	}
	if media.Channels != nil {
		channels := *media.Channels
		clone.Channels = &channels
	}
	if media.Width != nil {
		width := *media.Width
		clone.Width = &width
	}
	if media.Height != nil {
		height := *media.Height
		clone.Height = &height
	}
	if media.FPS != nil {
		fps := *media.FPS
		clone.FPS = &fps
	}

	return clone
}

// ConvertTextToMultimodal is a convenience function that creates a multimodal message
// from a role and text content. This helps with code migration.
func ConvertTextToMultimodal(role, content string) Message {
	return Message{
		Role:  role,
		Parts: []ContentPart{NewTextPart(content)},
	}
}

// ExtractTextContent extracts all text content from a message, regardless of format.
// This is useful for backward compatibility when you need just the text.
func ExtractTextContent(msg Message) string {
	return msg.GetContent()
}

// HasOnlyTextContent returns true if the message contains only text (no media)
func HasOnlyTextContent(msg Message) bool {
	if !msg.IsMultimodal() {
		return true // Legacy format is text-only
	}
	return !msg.HasMediaContent()
}

// SplitMultimodalMessage splits a multimodal message into separate text and media parts.
// Returns the text content and a slice of media content parts.
func SplitMultimodalMessage(msg Message) (text string, mediaParts []ContentPart) {
	if !msg.IsMultimodal() {
		return msg.Content, nil
	}

	var textBuilder string
	for _, part := range msg.Parts {
		if part.Type == ContentTypeText && part.Text != nil {
			textBuilder += *part.Text
		} else if part.Type == ContentTypeImage || part.Type == ContentTypeAudio || part.Type == ContentTypeVideo {
			mediaParts = append(mediaParts, part)
		}
	}

	return textBuilder, mediaParts
}

// CombineTextAndMedia creates a multimodal message from separate text and media parts.
// This is the inverse of SplitMultimodalMessage.
func CombineTextAndMedia(role, text string, mediaParts []ContentPart) Message {
	msg := Message{Role: role}
	
	if text != "" {
		msg.AddTextPart(text)
	}
	
	for _, part := range mediaParts {
		msg.AddPart(part)
	}
	
	return msg
}

// CountMediaParts returns the number of media parts (image, audio, video) in a message
func CountMediaParts(msg Message) int {
	if !msg.IsMultimodal() {
		return 0
	}

	count := 0
	for _, part := range msg.Parts {
		if part.Type == ContentTypeImage || part.Type == ContentTypeAudio || part.Type == ContentTypeVideo {
			count++
		}
	}
	return count
}

// CountPartsByType returns the number of parts of a specific type in a message
func CountPartsByType(msg Message, contentType string) int {
	if !msg.IsMultimodal() {
		if contentType == ContentTypeText && msg.Content != "" {
			return 1
		}
		return 0
	}

	count := 0
	for _, part := range msg.Parts {
		if part.Type == contentType {
			count++
		}
	}
	return count
}
