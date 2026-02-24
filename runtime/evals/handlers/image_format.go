package handlers

import (
	"context"

	"github.com/AltairaLabs/PromptKit/runtime/evals"
	"github.com/AltairaLabs/PromptKit/runtime/types"
)

// ImageFormatHandler checks that images in assistant messages have allowed formats.
// Params: formats []string.
type ImageFormatHandler struct{}

// Type returns the eval type identifier.
func (h *ImageFormatHandler) Type() string { return "image_format" }

// Eval checks image formats against the allowed list.
func (h *ImageFormatHandler) Eval(
	_ context.Context,
	evalCtx *evals.EvalContext,
	params map[string]any,
) (*evals.EvalResult, error) {
	return evalMediaFormats(
		h.Type(), evalCtx.Messages, types.ContentTypeImage,
		errNoImagesFound, "all images have allowed formats", "some images have disallowed formats",
		params,
	)
}
