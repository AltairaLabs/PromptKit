package handlers

import (
	"context"
	"fmt"
	"strings"

	"github.com/AltairaLabs/PromptKit/runtime/evals"
	"github.com/AltairaLabs/PromptKit/runtime/types"
)

// VideoResolutionHandler checks that video resolution meets requirements.
// Params: min_width, max_width, min_height, max_height, presets []string.
type VideoResolutionHandler struct{}

// Type returns the eval type identifier.
func (h *VideoResolutionHandler) Type() string { return "video_resolution" }

// Eval checks video resolution constraints.
func (h *VideoResolutionHandler) Eval(
	_ context.Context,
	evalCtx *evals.EvalContext,
	params map[string]any,
) (*evals.EvalResult, error) {
	minWidth := extractIntPtr(params, "min_width")
	maxWidth := extractIntPtr(params, "max_width")
	minHeight := extractIntPtr(params, "min_height")
	maxHeight := extractIntPtr(params, "max_height")
	presets := extractStringSlice(params, "presets")

	parts := extractMediaParts(evalCtx.Messages, types.ContentTypeVideo)
	if len(parts) == 0 {
		return &evals.EvalResult{
			Type: h.Type(), Passed: false,
			Explanation: errNoVideoFound,
		}, nil
	}

	var violations []string
	var foundResolutions []string
	for _, part := range parts {
		if part.Media.Width == nil || part.Media.Height == nil {
			violations = append(violations, "video missing width/height metadata")
			continue
		}
		w, ht := *part.Media.Width, *part.Media.Height
		foundResolutions = append(foundResolutions, fmt.Sprintf("%dx%d", w, ht))

		if len(presets) > 0 && !matchesAnyPreset(w, ht, presets) {
			violations = append(violations,
				fmt.Sprintf("resolution %dx%d does not match any preset: %v", w, ht, presets))
		}

		violations = append(violations, checkWidthRange(w, minWidth, maxWidth)...)
		violations = append(violations, checkHeightRange(ht, minHeight, maxHeight)...)
	}

	passed := len(violations) == 0
	explanation := "all videos meet resolution requirements"
	if !passed {
		explanation = "some videos violate resolution requirements"
	}

	return &evals.EvalResult{
		Type:        h.Type(),
		Passed:      passed,
		Explanation: explanation,
		Details: map[string]any{
			"video_count":       len(parts),
			"found_resolutions": foundResolutions,
			"violations":        violations,
		},
	}, nil
}

func matchesAnyPreset(width, height int, presets []string) bool {
	for _, preset := range presets {
		if matchesResolutionPreset(width, height, preset) {
			return true
		}
	}
	return false
}

// Standard video resolution heights.
const (
	resHeight480p  = 480
	resHeight720p  = 720
	resHeight1080p = 1080
	resHeight1440p = 1440
	resHeight2160p = 2160
	resHeight4320p = 4320
)

func matchesResolutionPreset(_, height int, preset string) bool {
	switch strings.ToLower(preset) {
	case "480p", "sd":
		return height == resHeight480p
	case "720p", "hd":
		return height == resHeight720p
	case "1080p", "fhd", "full_hd":
		return height == resHeight1080p
	case "1440p", "2k", "qhd":
		return height == resHeight1440p
	case "2160p", "4k", "uhd":
		return height == resHeight2160p
	case "4320p", "8k":
		return height == resHeight4320p
	default:
		return false
	}
}
