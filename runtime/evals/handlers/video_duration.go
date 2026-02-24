package handlers

import (
	"context"

	"github.com/AltairaLabs/PromptKit/runtime/evals"
	"github.com/AltairaLabs/PromptKit/runtime/types"
)

// VideoDurationHandler checks that video duration is within range.
// Params: min_seconds float64, max_seconds float64.
type VideoDurationHandler struct{}

// Type returns the eval type identifier.
func (h *VideoDurationHandler) Type() string { return "video_duration" }

// Eval checks video duration constraints.
func (h *VideoDurationHandler) Eval(
	_ context.Context,
	evalCtx *evals.EvalContext,
	params map[string]any,
) (*evals.EvalResult, error) {
	return evalMediaDuration(h.Type(), evalCtx.Messages, types.ContentTypeVideo, errNoVideoFound, "video_count", params)
}
