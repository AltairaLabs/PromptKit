package engine

import (
	"fmt"

	"github.com/AltairaLabs/PromptKit/runtime/types"
)

// CollectMediaOutputs extracts media outputs from conversation messages
// Returns a slice of MediaOutput for tracking in RunResult
func CollectMediaOutputs(messages []types.Message) []MediaOutput {
	var outputs []MediaOutput

	for msgIdx, msg := range messages {
		// Only collect media from assistant messages (LLM-generated content)
		if msg.Role != "assistant" {
			continue
		}

		// Collect media from this message's parts
		outputs = append(outputs, collectMediaFromMessage(msg, msgIdx)...)
	}

	return outputs
}

// collectMediaFromMessage extracts media outputs from a single message
func collectMediaFromMessage(msg types.Message, msgIdx int) []MediaOutput {
	var outputs []MediaOutput

	for partIdx, part := range msg.Parts {
		if part.Media == nil {
			continue
		}

		output := createMediaOutput(part, msgIdx, partIdx)
		outputs = append(outputs, output)
	}

	return outputs
}

// createMediaOutput creates a MediaOutput from a content part
func createMediaOutput(part types.ContentPart, msgIdx, partIdx int) MediaOutput {
	output := MediaOutput{
		Type:       part.Type,
		MIMEType:   part.Media.MIMEType,
		MessageIdx: msgIdx,
		PartIdx:    partIdx,
	}

	// Extract metadata from media content
	output.Duration = part.Media.Duration
	output.Width = part.Media.Width
	output.Height = part.Media.Height

	// Calculate size if possible
	if size := calculateMediaSize(part.Media); size > 0 {
		output.SizeBytes = size
	}

	// Generate thumbnail for images if data is available
	if part.Type == types.ContentTypeImage {
		if thumbnail := generateThumbnail(part.Media); thumbnail != "" {
			output.Thumbnail = thumbnail
		}
	}

	// Store file path if available
	if part.Media.FilePath != nil {
		output.FilePath = *part.Media.FilePath
	}

	return output
}

// calculateMediaSize attempts to determine the size of media content
func calculateMediaSize(media *types.MediaContent) int64 {
	// If size is already provided in metadata, use it
	if media.SizeKB != nil {
		return *media.SizeKB * 1024
	}

	// If we have base64 data, calculate its decoded size
	if media.Data != nil && *media.Data != "" {
		// Base64 encoding increases size by ~33%, so decoded size is roughly 3/4
		encodedLen := len(*media.Data)
		return int64(encodedLen * 3 / 4)
	}

	// For file paths or URLs, we can't determine size without reading
	// Return 0 to indicate unknown size
	return 0
}

// generateThumbnail creates a base64-encoded thumbnail for image content
// Returns empty string if thumbnail generation fails or data is not available
func generateThumbnail(media *types.MediaContent) string {
	// Only generate thumbnails from base64 data to avoid file I/O
	if media.Data == nil || *media.Data == "" {
		return ""
	}

	// For now, just return the original data if it's reasonably sized
	// In a future enhancement, we could resize the image for thumbnails
	if len(*media.Data) <= 50000 { // ~37.5KB decoded
		return *media.Data
	}

	return ""
}

// GetMediaOutputStatistics calculates summary statistics for media outputs
func GetMediaOutputStatistics(outputs []MediaOutput) MediaOutputStats {
	stats := MediaOutputStats{
		ByType: make(map[string]int),
	}

	for _, output := range outputs {
		stats.Total++
		stats.ByType[output.Type]++
		stats.TotalSizeBytes += output.SizeBytes

		switch output.Type {
		case types.ContentTypeImage:
			stats.ImageCount++
		case types.ContentTypeAudio:
			stats.AudioCount++
		case types.ContentTypeVideo:
			stats.VideoCount++
		}
	}

	return stats
}

// MediaOutputStats contains summary statistics for media outputs
type MediaOutputStats struct {
	Total          int            `json:"total"`
	ImageCount     int            `json:"image_count"`
	AudioCount     int            `json:"audio_count"`
	VideoCount     int            `json:"video_count"`
	TotalSizeBytes int64          `json:"total_size_bytes"`
	ByType         map[string]int `json:"by_type"`
}

// FormatMediaType returns a human-readable label for media type
func FormatMediaType(mediaType string) string {
	switch mediaType {
	case types.ContentTypeImage:
		return "Image"
	case types.ContentTypeAudio:
		return "Audio"
	case types.ContentTypeVideo:
		return "Video"
	default:
		return mediaType
	}
}

// FormatFileSize formats bytes as human-readable size
func FormatFileSize(bytes int64) string {
	const (
		KB = 1024
		MB = KB * 1024
		GB = MB * 1024
	)

	switch {
	case bytes >= GB:
		return fmt.Sprintf("%.2f GB", float64(bytes)/float64(GB))
	case bytes >= MB:
		return fmt.Sprintf("%.2f MB", float64(bytes)/float64(MB))
	case bytes >= KB:
		return fmt.Sprintf("%.2f KB", float64(bytes)/float64(KB))
	default:
		return fmt.Sprintf("%d B", bytes)
	}
}
