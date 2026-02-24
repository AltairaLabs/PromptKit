package handlers

import (
	"context"

	"github.com/AltairaLabs/PromptKit/runtime/evals"
	"github.com/AltairaLabs/PromptKit/runtime/types"
)

// AudioFormatHandler checks that audio content has allowed formats.
// Params: formats []string.
type AudioFormatHandler struct{}

// Type returns the eval type identifier.
func (h *AudioFormatHandler) Type() string { return "audio_format" }

// Eval checks audio formats against the allowed list.
func (h *AudioFormatHandler) Eval(
	_ context.Context,
	evalCtx *evals.EvalContext,
	params map[string]any,
) (*evals.EvalResult, error) {
	return evalMediaFormats(
		h.Type(), evalCtx.Messages, types.ContentTypeAudio,
		errNoAudioFound, "all audio has allowed formats", "some audio has disallowed formats",
		params,
	)
}
