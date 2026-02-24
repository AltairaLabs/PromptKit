package handlers

import (
	"context"
	"fmt"

	"github.com/AltairaLabs/PromptKit/runtime/evals"
	"github.com/AltairaLabs/PromptKit/runtime/types"
)

// ImageDimensionsHandler checks that images meet dimension requirements.
// Params: min_width, max_width, min_height, max_height, width, height.
type ImageDimensionsHandler struct{}

// Type returns the eval type identifier.
func (h *ImageDimensionsHandler) Type() string { return "image_dimensions" }

// Eval checks image dimensions against constraints.
func (h *ImageDimensionsHandler) Eval(
	_ context.Context,
	evalCtx *evals.EvalContext,
	params map[string]any,
) (*evals.EvalResult, error) {
	minWidth := extractIntPtr(params, "min_width")
	maxWidth := extractIntPtr(params, "max_width")
	minHeight := extractIntPtr(params, "min_height")
	maxHeight := extractIntPtr(params, "max_height")
	exactWidth := extractIntPtr(params, "width")
	exactHeight := extractIntPtr(params, "height")

	parts := extractMediaParts(evalCtx.Messages, types.ContentTypeImage)
	if len(parts) == 0 {
		return &evals.EvalResult{
			Type: h.Type(), Passed: false,
			Explanation: errNoImagesFound,
		}, nil
	}

	var violations []string
	for _, part := range parts {
		if part.Media.Width == nil || part.Media.Height == nil {
			violations = append(violations, errMissingDimensions)
			continue
		}
		w, ht := *part.Media.Width, *part.Media.Height
		if exactWidth != nil && w != *exactWidth {
			violations = append(violations, fmt.Sprintf("width %d does not match required %d", w, *exactWidth))
		}
		if exactHeight != nil && ht != *exactHeight {
			violations = append(violations, fmt.Sprintf("height %d does not match required %d", ht, *exactHeight))
		}
		violations = append(violations, checkWidthRange(w, minWidth, maxWidth)...)
		violations = append(violations, checkHeightRange(ht, minHeight, maxHeight)...)
	}

	passed := len(violations) == 0
	explanation := "all images meet dimension requirements"
	if !passed {
		explanation = "some images violate dimension requirements"
	}

	return &evals.EvalResult{
		Type:        h.Type(),
		Passed:      passed,
		Explanation: explanation,
		Details: map[string]any{
			"image_count": len(parts),
			"violations":  violations,
		},
	}, nil
}
