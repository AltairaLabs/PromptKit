package handlers

import (
	"context"

	"github.com/AltairaLabs/PromptKit/runtime/evals"
	"github.com/AltairaLabs/PromptKit/runtime/types"
)

// AudioDurationHandler checks that audio duration is within range.
// Params: min_seconds float64, max_seconds float64.
type AudioDurationHandler struct{}

// Type returns the eval type identifier.
func (h *AudioDurationHandler) Type() string { return "audio_duration" }

// Eval checks audio duration constraints.
func (h *AudioDurationHandler) Eval(
	_ context.Context,
	evalCtx *evals.EvalContext,
	params map[string]any,
) (*evals.EvalResult, error) {
	return evalMediaDuration(h.Type(), evalCtx.Messages, types.ContentTypeAudio, errNoAudioFound, "audio_count", params)
}
