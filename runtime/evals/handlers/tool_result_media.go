package handlers

import (
	"context"
	"fmt"
	"strings"

	"github.com/AltairaLabs/PromptKit/runtime/evals"
	"github.com/AltairaLabs/PromptKit/runtime/types"
)

// ToolResultHasMediaHandler asserts that a named tool call returned media content of a given type.
// Params: tool string, media_type string ("image", "audio", "video", "document").
type ToolResultHasMediaHandler struct{}

// Type returns the eval type identifier.
func (h *ToolResultHasMediaHandler) Type() string { return "tool_result_has_media" }

// Eval checks that a tool result contains ContentPart entries with the specified media type.
func (h *ToolResultHasMediaHandler) Eval(
	_ context.Context,
	evalCtx *evals.EvalContext,
	params map[string]any,
) (*evals.EvalResult, error) {
	tool, _ := params["tool"].(string)
	mediaType, _ := params["media_type"].(string)

	if mediaType == "" {
		return &evals.EvalResult{
			Type:        h.Type(),
			Passed:      false,
			Explanation: "no media_type specified",
		}, nil
	}

	for _, tc := range evalCtx.ToolCalls {
		if tool != "" && tc.ToolName != tool {
			continue
		}
		parts := extractContentParts(tc.Result)
		for _, p := range parts {
			if strings.EqualFold(p.Type, mediaType) {
				return &evals.EvalResult{
					Type:        h.Type(),
					Passed:      true,
					Explanation: fmt.Sprintf("tool %q returned media of type %q", tc.ToolName, mediaType),
					Details: map[string]any{
						"tool":       tc.ToolName,
						"media_type": mediaType,
					},
				}, nil
			}
		}
	}

	return &evals.EvalResult{
		Type:        h.Type(),
		Passed:      false,
		Explanation: fmt.Sprintf("no tool result contains media of type %q", mediaType),
		Details: map[string]any{
			"tool":       tool,
			"media_type": mediaType,
		},
	}, nil
}

// ToolResultMediaTypeHandler asserts the MIME type of media in a tool result.
// Params: tool string, mime_type string.
type ToolResultMediaTypeHandler struct{}

// Type returns the eval type identifier.
func (h *ToolResultMediaTypeHandler) Type() string { return "tool_result_media_type" }

// Eval checks that a tool result contains a non-text ContentPart with the specified MIME type.
func (h *ToolResultMediaTypeHandler) Eval(
	_ context.Context,
	evalCtx *evals.EvalContext,
	params map[string]any,
) (*evals.EvalResult, error) {
	tool, _ := params["tool"].(string)
	mimeType, _ := params["mime_type"].(string)

	if mimeType == "" {
		return &evals.EvalResult{
			Type:        h.Type(),
			Passed:      false,
			Explanation: "no mime_type specified",
		}, nil
	}

	for _, tc := range evalCtx.ToolCalls {
		if tool != "" && tc.ToolName != tool {
			continue
		}
		parts := extractContentParts(tc.Result)
		for _, p := range parts {
			if p.Type == types.ContentTypeText {
				continue
			}
			if p.Media != nil && strings.EqualFold(p.Media.MIMEType, mimeType) {
				return &evals.EvalResult{
					Type:        h.Type(),
					Passed:      true,
					Explanation: fmt.Sprintf("tool %q returned media with MIME type %q", tc.ToolName, mimeType),
					Details: map[string]any{
						"tool":      tc.ToolName,
						"mime_type": mimeType,
					},
				}, nil
			}
		}
	}

	return &evals.EvalResult{
		Type:        h.Type(),
		Passed:      false,
		Explanation: fmt.Sprintf("no tool result contains media with MIME type %q", mimeType),
		Details: map[string]any{
			"tool":      tool,
			"mime_type": mimeType,
		},
	}, nil
}

// extractContentParts attempts to extract []types.ContentPart from a tool result.
// The Result field is typed as `any`, so we try direct type assertion first,
// then fall back to []any with map-based extraction.
func extractContentParts(result any) []types.ContentPart {
	if result == nil {
		return nil
	}

	// Direct type assertion for []types.ContentPart
	if parts, ok := result.([]types.ContentPart); ok {
		return parts
	}

	// Try []any (e.g., from JSON deserialization)
	slice, ok := result.([]any)
	if !ok {
		return nil
	}

	var parts []types.ContentPart
	for _, item := range slice {
		if part, ok := parseContentPartFromMap(item); ok {
			parts = append(parts, part)
		}
	}

	return parts
}

// parseContentPartFromMap converts a map[string]any to a ContentPart.
// Returns false if the item is not a valid content part map.
func parseContentPartFromMap(item any) (types.ContentPart, bool) {
	m, ok := item.(map[string]any)
	if !ok {
		return types.ContentPart{}, false
	}

	partType, _ := m["type"].(string)
	if partType == "" {
		return types.ContentPart{}, false
	}

	part := types.ContentPart{Type: partType}

	if text, ok := m["text"].(string); ok {
		part.Text = &text
	}

	if mediaMap, ok := m["media"].(map[string]any); ok {
		part.Media = parseMediaFromMap(mediaMap)
	}

	return part, true
}

// parseMediaFromMap converts a map[string]any to a MediaContent.
func parseMediaFromMap(mediaMap map[string]any) *types.MediaContent {
	media := &types.MediaContent{}
	media.MIMEType, _ = mediaMap["mime_type"].(string)

	if data, ok := mediaMap["data"].(string); ok {
		media.Data = &data
	}
	if fp, ok := mediaMap["file_path"].(string); ok {
		media.FilePath = &fp
	}
	if u, ok := mediaMap["url"].(string); ok {
		media.URL = &u
	}

	return media
}
