package handlers

import (
	"fmt"
	"strings"

	"github.com/AltairaLabs/PromptKit/runtime/evals"
	"github.com/AltairaLabs/PromptKit/runtime/types"
)

// Media error messages.
const (
	errNoImagesFound     = "no images found in messages"
	errNoAudioFound      = "no audio found in messages"
	errNoVideoFound      = "no video found in messages"
	errAtLeastOneFormat  = "at least one format must be specified"
	errMissingDimensions = "missing width/height metadata"
	errMissingDuration   = "missing duration metadata"
)

// Duration violation templates.
const (
	msgDurationBelowMin = "duration %.1fs below minimum %.1fs"
	msgDurationAboveMax = "duration %.1fs exceeds maximum %.1fs"
)

// Dimension violation templates.
const (
	msgWidthBelowMin  = "width %d below minimum %d"
	msgWidthAboveMax  = "width %d exceeds maximum %d"
	msgHeightBelowMin = "height %d below minimum %d"
	msgHeightAboveMax = "height %d exceeds maximum %d"
)

// collectMediaContentByRole returns the MediaContent of contentType attributed
// to the given role. Tool-produced media (a tool the agent called, e.g.
// image__generate) lands in a tool-role message's ToolResult.Parts rather than
// an inline assistant Part — but it is the agent's output for that turn, so
// role "assistant" yields tool-result parts as well. This is the role-filtered,
// MediaContent-returning sibling of extractMediaParts.
func collectMediaContentByRole(messages []types.Message, contentType, role string) []*types.MediaContent {
	var out []*types.MediaContent
	for i := range messages {
		msg := &messages[i]
		if msg.Role == role {
			out = appendMediaContent(out, msg.Parts, contentType)
		}
		if role == roleAssistant && msg.Role == roleTool && msg.ToolResult != nil {
			out = appendMediaContent(out, msg.ToolResult.Parts, contentType)
		}
	}
	return out
}

// appendMediaContent appends the MediaContent of every part matching contentType.
func appendMediaContent(
	dst []*types.MediaContent, parts []types.ContentPart, contentType string,
) []*types.MediaContent {
	for i := range parts {
		part := &parts[i]
		if part.Type == contentType && part.Media != nil {
			dst = append(dst, part.Media)
		}
	}
	return dst
}

// extractMediaParts finds media content parts of the given type produced by the
// agent's turn. This covers two sources: media the model emitted inline on an
// assistant message, and media a tool produced (e.g. image__generate), which
// lands in the tool-result parts of a tool-role message rather than on the
// message's inline Parts.
func extractMediaParts(messages []types.Message, contentType string) []types.ContentPart {
	var parts []types.ContentPart
	for i := range messages {
		msg := &messages[i]
		switch msg.Role {
		case roleAssistant:
			parts = appendMatchingMediaParts(parts, msg.Parts, contentType)
		case roleTool:
			if msg.ToolResult != nil {
				parts = appendMatchingMediaParts(parts, msg.ToolResult.Parts, contentType)
			}
		}
	}
	return parts
}

// appendMatchingMediaParts appends parts of contentType that carry media.
func appendMatchingMediaParts(
	dst, src []types.ContentPart, contentType string,
) []types.ContentPart {
	for _, part := range src {
		if part.Type == contentType && part.Media != nil {
			dst = append(dst, part)
		}
	}
	return dst
}

// mimeTypeParts is the expected number of parts when splitting a MIME type by "/".
const mimeTypeParts = 2

// extractFormatFromMIMEType extracts the format from a MIME type string.
func extractFormatFromMIMEType(mimeType string) string {
	parts := strings.Split(mimeType, "/")
	if len(parts) != mimeTypeParts {
		return mimeType
	}
	format := parts[1]
	if format == "mpeg" && strings.HasPrefix(mimeType, "audio/") {
		return "mp3"
	}
	return format
}

// isAllowedFormat checks if a format is in the allowed list (case-insensitive).
func isAllowedFormat(format string, allowed []string) bool {
	for _, a := range allowed {
		if strings.EqualFold(a, format) {
			return true
		}
	}
	return false
}

// checkDuration checks duration constraints on a media content.
func checkDuration(media *types.MediaContent, minSeconds, maxSeconds *float64) []string {
	if media.Duration == nil {
		return []string{errMissingDuration}
	}
	duration := float64(*media.Duration)
	var violations []string
	if minSeconds != nil && duration < *minSeconds {
		violations = append(violations, fmt.Sprintf(msgDurationBelowMin, duration, *minSeconds))
	}
	if maxSeconds != nil && duration > *maxSeconds {
		violations = append(violations, fmt.Sprintf(msgDurationAboveMax, duration, *maxSeconds))
	}
	return violations
}

// checkWidthRange checks width constraints.
func checkWidthRange(width int, minWidth, maxWidth *int) []string {
	var violations []string
	if minWidth != nil && width < *minWidth {
		violations = append(violations, fmt.Sprintf(msgWidthBelowMin, width, *minWidth))
	}
	if maxWidth != nil && width > *maxWidth {
		violations = append(violations, fmt.Sprintf(msgWidthAboveMax, width, *maxWidth))
	}
	return violations
}

// checkHeightRange checks height constraints.
func checkHeightRange(height int, minHeight, maxHeight *int) []string {
	var violations []string
	if minHeight != nil && height < *minHeight {
		violations = append(violations, fmt.Sprintf(msgHeightBelowMin, height, *minHeight))
	}
	if maxHeight != nil && height > *maxHeight {
		violations = append(violations, fmt.Sprintf(msgHeightAboveMax, height, *maxHeight))
	}
	return violations
}

// evalMediaDuration is a shared implementation for audio/video duration handlers.
func evalMediaDuration(
	typeName string,
	messages []types.Message,
	contentType string,
	errNoMedia string,
	countKey string,
	params map[string]any,
) (*evals.EvalResult, error) {
	minSeconds := extractFloat64Ptr(params, "min_seconds")
	maxSeconds := extractFloat64Ptr(params, "max_seconds")

	parts := extractMediaParts(messages, contentType)
	if len(parts) == 0 {
		return &evals.EvalResult{
			Type:        typeName,
			Score:       boolScore(false),
			Explanation: errNoMedia,
		}, nil
	}

	var violations []string
	var foundDurations []float64
	for _, part := range parts {
		if part.Media.Duration != nil {
			foundDurations = append(foundDurations, float64(*part.Media.Duration))
		}
		violations = append(violations, checkDuration(part.Media, minSeconds, maxSeconds)...)
	}

	passed := len(violations) == 0
	explanation := fmt.Sprintf("all %s duration within range", contentType)
	if !passed {
		explanation = fmt.Sprintf("some %s duration violations", contentType)
	}

	return &evals.EvalResult{
		Type:        typeName,
		Score:       boolScore(passed),
		Explanation: explanation,
		Value:       map[string]any{"durations_ms": foundDurations},
		Details: map[string]any{
			countKey:          len(parts),
			"found_durations": foundDurations,
			"violations":      violations,
		},
	}, nil
}

// evalMediaFormats is a shared implementation for image/audio format handlers.
func evalMediaFormats(
	typeName string,
	messages []types.Message,
	contentType string,
	errNoMedia string,
	passMsg string,
	failMsg string,
	params map[string]any,
) (*evals.EvalResult, error) {
	formats := extractStringSlice(params, "formats")
	if len(formats) == 0 {
		return &evals.EvalResult{
			Type:        typeName,
			Score:       boolScore(false),
			Explanation: errAtLeastOneFormat,
		}, nil
	}

	parts := extractMediaParts(messages, contentType)
	if len(parts) == 0 {
		return &evals.EvalResult{
			Type:        typeName,
			Score:       boolScore(false),
			Explanation: errNoMedia,
		}, nil
	}

	var foundFormats, invalidFormats []string
	for _, part := range parts {
		format := extractFormatFromMIMEType(part.Media.MIMEType)
		foundFormats = append(foundFormats, format)
		if !isAllowedFormat(format, formats) {
			invalidFormats = append(invalidFormats, format)
		}
	}

	passed := len(invalidFormats) == 0
	explanation := passMsg
	if !passed {
		explanation = failMsg
	}

	return &evals.EvalResult{
		Type:        typeName,
		Score:       boolScore(passed),
		Explanation: explanation,
		Value:       map[string]any{"formats": foundFormats, "expected": formats},
		Details: map[string]any{
			"found_formats":   foundFormats,
			"invalid_formats": invalidFormats,
			"allowed_formats": formats,
		},
	}, nil
}

// collectImageDimensions extracts width/height pairs from media content parts.
func collectImageDimensions(parts []types.ContentPart) map[string]any {
	var dims []map[string]any
	for _, part := range parts {
		if part.Media != nil && part.Media.Width != nil && part.Media.Height != nil {
			dims = append(dims, map[string]any{
				"width":  *part.Media.Width,
				"height": *part.Media.Height,
			})
		}
	}
	return map[string]any{"dimensions": dims}
}
