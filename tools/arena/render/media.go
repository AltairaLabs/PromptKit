package render

import (
	"fmt"
	"html/template"
	"strings"

	"github.com/AltairaLabs/PromptKit/runtime/types"
	"github.com/AltairaLabs/PromptKit/tools/arena/engine"
)

const (
	divCloseTag = "</div>"

	// Content type strings
	sourceUnknown = "unknown"

	// Size constants
	bytesPerKB = 1024

	// MIME type constants
	mimeTypeWAV  = "audio/wav"
	mimeTypeMPEG = "audio/mpeg"
	mimeTypeOGG  = "audio/ogg"
	mimeTypeWEBM = "audio/webm"
	mimeTypeAAC  = "audio/aac"
)

// renderMediaSummaryBadge creates a compact badge showing media counts.
// Returns empty string if no media content (only text).
func renderMediaSummaryBadge(summary *types.MediaSummary) string {
	if summary == nil || summary.TotalParts <= summary.TextParts {
		return "" // No media, only text
	}

	badges := []string{}

	if summary.ImageParts > 0 {
		badges = append(badges,
			fmt.Sprintf(`<span class="media-badge image" title="%d image(s)">🖼️ %d</span>`,
				summary.ImageParts, summary.ImageParts))
	}

	if summary.AudioParts > 0 {
		badges = append(badges,
			fmt.Sprintf(`<span class="media-badge audio" title="%d audio file(s)">🎵 %d</span>`,
				summary.AudioParts, summary.AudioParts))
	}

	if summary.VideoParts > 0 {
		badges = append(badges,
			fmt.Sprintf(`<span class="media-badge video" title="%d video(s)">🎬 %d</span>`,
				summary.VideoParts, summary.VideoParts))
	}

	if summary.DocumentParts > 0 {
		badges = append(badges,
			fmt.Sprintf(`<span class="media-badge document" title="%d document(s)">📄 %d</span>`,
				summary.DocumentParts, summary.DocumentParts))
	}

	if len(badges) == 0 {
		return ""
	}

	html := `<div class="media-summary-badge">`
	html += strings.Join(badges, "")
	html += divCloseTag

	return html
}

// renderMediaItem creates detailed HTML for a single media item.
// Displays type icon, source, metadata (MIME type, size), and load status.
// For audio files, includes an HTML5 audio player if the file is playable.
func renderMediaItem(item types.MediaItemSummary) string {
	statusIcon := "✅"
	statusClass := "loaded"
	statusText := "Loaded"

	if item.Error != "" {
		statusIcon = "❌"
		statusClass = "error"
		statusText = item.Error
	} else if !item.Loaded {
		statusIcon = "⚠️"
		statusClass = "not-loaded"
		statusText = "Not loaded"
	}

	// Format size
	sizeStr := formatBytes(item.SizeBytes)

	// Get type icon
	typeIcon := getMediaTypeIcon(item.Type)

	// Build the audio player HTML if this is a playable audio file
	audioPlayer := ""
	if item.Type == types.ContentTypeAudio && item.Source != "" && item.Source != sourceUnknown {
		audioPlayer = renderAudioPlayer(item.Source, item.MIMEType)
	}

	html := fmt.Sprintf(`
        <div class="media-item %s %s" title="%s">
            <div class="media-icon">%s</div>
            <div class="media-info">
                <div class="media-source">%s</div>
                <div class="media-meta">
                    <span class="media-type">%s</span>
                    <span class="media-mime">%s</span>
                    <span class="media-size">%s</span>
                    <span class="media-status">%s %s</span>
                </div>
                %s
            </div>
        </div>
    `, item.Type, statusClass, template.HTMLEscapeString(statusText),
		typeIcon, template.HTMLEscapeString(truncateSource(item.Source, maxSourceLength)),
		item.Type, template.HTMLEscapeString(item.MIMEType), sizeStr,
		statusIcon, template.HTMLEscapeString(statusText),
		audioPlayer)

	return html
}

// renderAudioPlayer creates an HTML5 audio player for playable audio files.
// Returns empty string for non-playable formats like raw PCM.
func renderAudioPlayer(source, mimeType string) string {
	// Determine if the file is playable based on extension or MIME type
	playableMIME := getPlayableAudioMIME(source, mimeType)
	if playableMIME == "" {
		// Not a playable format
		return `<div class="audio-not-playable">(raw PCM - not directly playable)</div>`
	}

	// Make path relative to report location (report is in out/, media is in out/media/)
	// Strip "out/" prefix if present since report.html is served from out/
	relativePath := strings.TrimPrefix(source, "out/")

	return fmt.Sprintf(`
        <div class="audio-player">
            <audio controls preload="metadata">
                <source src="%s" type="%s">
                Your browser does not support audio playback.
            </audio>
        </div>
    `, template.HTMLEscapeString(relativePath), playableMIME)
}

// getPlayableAudioMIME returns the MIME type for playable audio, or empty if not playable.
func getPlayableAudioMIME(source, mimeType string) string {
	// Check by file extension first (more reliable for local files)
	source = strings.ToLower(source)
	switch {
	case strings.HasSuffix(source, ".wav"):
		return mimeTypeWAV
	case strings.HasSuffix(source, ".mp3"):
		return mimeTypeMPEG
	case strings.HasSuffix(source, ".ogg"), strings.HasSuffix(source, ".oga"):
		return mimeTypeOGG
	case strings.HasSuffix(source, ".webm"), strings.HasSuffix(source, ".weba"):
		return mimeTypeWEBM
	case strings.HasSuffix(source, ".m4a"), strings.HasSuffix(source, ".aac"):
		return mimeTypeAAC
	case strings.HasSuffix(source, ".pcm"):
		// Raw PCM is not playable in browsers
		return ""
	}

	// Fall back to MIME type check
	switch mimeType {
	case "audio/wav", "audio/wave", "audio/x-wav":
		return mimeTypeWAV
	case "audio/mpeg", "audio/mp3":
		return mimeTypeMPEG
	case "audio/ogg":
		return mimeTypeOGG
	case "audio/webm":
		return mimeTypeWEBM
	case "audio/aac", "audio/mp4":
		return mimeTypeAAC
	case "audio/pcm", "audio/L16":
		// Raw PCM is not playable
		return ""
	}

	return ""
}

// renderMessageWithMedia shows rich media content in a message.
// Returns HTML with text content and individual media item cards.
func renderMessageWithMedia(msg types.Message) string {
	var html strings.Builder

	html.WriteString(fmt.Sprintf("<div class='message %s'>", msg.Role))

	// Render text content - use GetContent() to extract text from Parts if needed
	textContent := msg.GetContent()
	if textContent != "" {
		// Render markdown for all messages except tool messages
		var renderedContent string
		if msg.Role == "tool" {
			renderedContent = template.HTMLEscapeString(textContent)
		} else {
			renderedContent = string(renderMarkdown(textContent))
		}
		html.WriteString(fmt.Sprintf("<div class='message-text'>%s</div>", renderedContent))
	}

	// Render inline images directly from Parts (to get actual image data)
	if len(msg.Parts) > 0 {
		for _, part := range msg.Parts {
			if part.Type == types.ContentTypeImage && part.Media != nil {
				html.WriteString(renderInlineImage(part))
			}
		}
	}

	// Render other media items (audio, video) with metadata cards
	var mediaSummary *types.MediaSummary
	if len(msg.Parts) > 0 {
		mediaSummary = getMediaSummaryFromParts(msg.Parts)
	}
	if mediaSummary != nil && len(mediaSummary.MediaItems) > 0 {
		// Check if there are any non-image media items
		hasNonImageMedia := false
		for _, item := range mediaSummary.MediaItems {
			if item.Type != types.ContentTypeImage {
				hasNonImageMedia = true
				break
			}
		}
		if hasNonImageMedia {
			html.WriteString("<div class='media-items'>")
			for _, item := range mediaSummary.MediaItems {
				// Skip images - they're rendered inline above
				if item.Type == types.ContentTypeImage {
					continue
				}
				html.WriteString(renderMediaItem(item))
			}
			html.WriteString(divCloseTag)
		}
	}

	html.WriteString(divCloseTag)
	return html.String()
}

// renderInlineImage renders an image part as an inline <img> element.
// Displays the actual image content using base64 data URL.
func renderInlineImage(part types.ContentPart) string {
	if part.Media == nil {
		return ""
	}

	// Get source description for alt text
	source := "Image"
	if part.Media.FilePath != nil && *part.Media.FilePath != "" {
		source = *part.Media.FilePath
	} else if part.Media.URL != nil && *part.Media.URL != "" {
		source = *part.Media.URL
	}

	// Build the image HTML
	var imgHTML string

	// Image styling for inline display
	const imgStyle = "max-width: 100%; max-height: 400px; border-radius: 4px; margin: 8px 0;"

	// Check if we have inline data
	if part.Media.Data != nil && *part.Media.Data != "" {
		mimeType := part.Media.MIMEType
		if mimeType == "" {
			mimeType = "image/png" // Default
		}
		imgHTML = fmt.Sprintf(
			`<div class="inline-image"><img src="data:%s;base64,%s" alt=%q title=%q style=%q /></div>`,
			template.HTMLEscapeString(mimeType),
			template.HTMLEscapeString(*part.Media.Data),
			truncateSource(source, maxSourceLength),
			source,
			imgStyle,
		)
	} else if part.Media.URL != nil && *part.Media.URL != "" {
		// Use URL directly if no inline data
		imgHTML = fmt.Sprintf(
			`<div class="inline-image"><img src=%q alt=%q title=%q style=%q /></div>`,
			*part.Media.URL,
			truncateSource(source, maxSourceLength),
			source,
			imgStyle,
		)
	} else {
		// No displayable data - show placeholder with metadata
		sizeStr := ""
		if part.Media.SizeKB != nil {
			sizeStr = fmt.Sprintf(" (%s)", formatBytes(int(*part.Media.SizeKB*bytesPerKB)))
		}
		imgHTML = fmt.Sprintf(
			`<div class="inline-image-placeholder">🖼️ <span class="image-source">%s</span>%s</div>`,
			template.HTMLEscapeString(truncateSource(source, maxSourceLength)),
			sizeStr,
		)
	}

	return imgHTML
}

// getMediaSummaryFromParts generates a MediaSummary from ContentParts.
// This mirrors the logic in Message.getMediaSummary() but is callable from render package.
func getMediaSummaryFromParts(parts []types.ContentPart) *types.MediaSummary {
	if len(parts) == 0 {
		return nil
	}

	summary := &types.MediaSummary{
		TotalParts: len(parts),
		MediaItems: make([]types.MediaItemSummary, 0),
	}

	for _, part := range parts {
		switch part.Type {
		case types.ContentTypeText:
			summary.TextParts++
		case types.ContentTypeImage:
			summary.ImageParts++
			summary.MediaItems = append(summary.MediaItems, getMediaItemSummaryFromPart(part))
		case types.ContentTypeAudio:
			summary.AudioParts++
			summary.MediaItems = append(summary.MediaItems, getMediaItemSummaryFromPart(part))
		case types.ContentTypeVideo:
			summary.VideoParts++
			summary.MediaItems = append(summary.MediaItems, getMediaItemSummaryFromPart(part))
		case types.ContentTypeDocument:
			summary.DocumentParts++
			summary.MediaItems = append(summary.MediaItems, getMediaItemSummaryFromPart(part))
		}
	}

	return summary
}

// getMediaItemSummaryFromPart extracts summary information from a media ContentPart.
// This mirrors the logic in message.go's getMediaItemSummary() function.
func getMediaItemSummaryFromPart(part types.ContentPart) types.MediaItemSummary {
	item := types.MediaItemSummary{
		Type:   part.Type,
		Loaded: false,
	}

	if part.Media == nil {
		item.Error = "no media content"
		return item
	}

	item.MIMEType = part.Media.MIMEType

	// Determine source - prefer FilePath/URL/StorageReference for display even if Data is set
	if part.Media.FilePath != nil {
		item.Source = *part.Media.FilePath
	} else if part.Media.URL != nil {
		item.Source = *part.Media.URL
	} else if part.Media.StorageReference != nil && *part.Media.StorageReference != "" {
		item.Source = *part.Media.StorageReference
		item.Loaded = true // Storage reference means the file exists
	} else if part.Media.Data != nil && *part.Media.Data != "" {
		item.Source = "inline data"
	} else {
		item.Source = "unknown"
		item.Error = "no data source"
	}

	// Check if data was loaded
	if part.Media.Data != nil && *part.Media.Data != "" {
		item.Loaded = true
		// Estimate size from base64 data if not provided (roughly 3/4 of base64 length)
		if part.Media.SizeKB == nil {
			const (
				base64Ratio     = 4
				base64Numerator = 3
			)
			item.SizeBytes = (len(*part.Media.Data) * base64Numerator) / base64Ratio
		}
	}

	// Add detail level for images
	if part.Type == types.ContentTypeImage && part.Media.Detail != nil {
		item.Detail = *part.Media.Detail
	}

	// Use size metadata if available
	if part.Media.SizeKB != nil {
		const bytesPerKB = 1024
		item.SizeBytes = int(*part.Media.SizeKB * bytesPerKB)
	}

	return item
}

// getMediaTypeIcon returns an emoji icon for the given media type.
func getMediaTypeIcon(mediaType string) string {
	switch mediaType {
	case types.ContentTypeImage:
		return "🖼️"
	case types.ContentTypeAudio:
		return "🎵"
	case types.ContentTypeVideo:
		return "🎬"
	case types.ContentTypeDocument:
		return "📄"
	default:
		return "📎"
	}
}

// formatBytes formats a byte count as a human-readable string.
// Uses KB, MB, or GB as appropriate.
func formatBytes(bytes int) string {
	if bytes == 0 {
		return "0 B"
	}

	const maxUnits = 3

	if bytes < bytesPerKB {
		return fmt.Sprintf("%d B", bytes)
	}

	units := []string{"KB", "MB", "GB"}
	div := float64(bytesPerKB)
	exp := 0

	for n := float64(bytes) / div; n >= bytesPerKB && exp < maxUnits-1; n /= bytesPerKB {
		div *= bytesPerKB
		exp++
	}

	return fmt.Sprintf("%.1f %s", float64(bytes)/div, units[exp])
}

// truncateSource truncates long file paths or URLs for display.
// Attempts to preserve the filename if possible.
func truncateSource(source string, maxLen int) string {
	if len(source) <= maxLen {
		return source
	}

	// Try to show filename for paths
	if strings.Contains(source, "/") {
		parts := strings.Split(source, "/")
		filename := parts[len(parts)-1]
		const (
			ellipsisPrefix = ".../"
			ellipsisSuffix = "..."
		)
		// If .../ + filename fits, show that
		if len(ellipsisPrefix)+len(filename) <= maxLen {
			return ellipsisPrefix + filename
		}
		// Otherwise truncate the filename itself
		remainingLen := maxLen - len(ellipsisPrefix) - len(ellipsisSuffix)
		if remainingLen > 0 {
			return ellipsisPrefix + filename[:remainingLen] + ellipsisSuffix
		}
	}

	// Fallback: truncate from start with ellipsis
	const ellipsisLen = 3
	if maxLen > ellipsisLen {
		return source[:maxLen-ellipsisLen] + "..."
	}
	return source[:maxLen]
}

const (
	maxSourceLength = 40
)

// MediaStats holds aggregate media statistics across multiple run results.
type MediaStats struct {
	TotalImages      int
	TotalAudio       int
	TotalVideo       int
	MediaLoadSuccess int
	MediaLoadErrors  int
	TotalMediaSize   int64
}

// calculateMediaStats computes aggregate media statistics from run results.
// It counts total media items, successful loads, errors, and total data size.
func calculateMediaStats(results []engine.RunResult) MediaStats {
	stats := MediaStats{}

	for _, result := range results {
		for _, msg := range result.Messages {
			addMessageMediaStats(&stats, msg)
		}
	}

	return stats
}

// addMessageMediaStats adds a message's media statistics to the aggregate stats.
func addMessageMediaStats(stats *MediaStats, msg types.Message) {
	if len(msg.Parts) == 0 {
		return
	}

	// Generate media summary from parts
	mediaSummary := getMediaSummaryFromParts(msg.Parts)
	if mediaSummary == nil {
		return
	}

	// Aggregate counts
	stats.TotalImages += mediaSummary.ImageParts
	stats.TotalAudio += mediaSummary.AudioParts
	stats.TotalVideo += mediaSummary.VideoParts

	// Aggregate load status and size
	for _, item := range mediaSummary.MediaItems {
		stats.TotalMediaSize += int64(item.SizeBytes)

		if item.Loaded && item.Error == "" {
			stats.MediaLoadSuccess++
		} else if item.Error != "" {
			stats.MediaLoadErrors++
		}
	}
}
