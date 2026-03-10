package handlers

import (
	"context"
	"regexp"
	"strings"

	"github.com/AltairaLabs/PromptKit/runtime/evals"
)

// sentenceSplitter splits text on sentence-ending punctuation.
var sentenceSplitter = regexp.MustCompile(`[.!?]+`)

// SentenceCountHandler counts sentences in the current output.
// This is a pure measurement handler — it always passes.
// Params: (none required — returns count as measurement)
type SentenceCountHandler struct{}

// Type returns the eval type identifier.
func (h *SentenceCountHandler) Type() string { return "sentence_count" }

// Eval counts sentences in the current output.
func (h *SentenceCountHandler) Eval(
	_ context.Context,
	evalCtx *evals.EvalContext,
	_ map[string]any,
) (*evals.EvalResult, error) {
	count := countSentences(evalCtx.CurrentOutput)
	score := 1.0

	return &evals.EvalResult{
		Type:   h.Type(),
		Passed: true,
		Score:  &score,
		Details: map[string]any{
			"count": count,
		},
	}, nil
}

// countSentences counts sentences by splitting on [.!?]+ punctuation.
func countSentences(text string) int {
	text = strings.TrimSpace(text)
	if text == "" {
		return 0
	}
	parts := sentenceSplitter.Split(text, -1)
	count := 0
	for _, part := range parts {
		if strings.TrimSpace(part) != "" {
			count++
		}
	}
	if count == 0 && text != "" {
		count = 1
	}
	return count
}
