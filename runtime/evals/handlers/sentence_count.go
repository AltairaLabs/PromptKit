package handlers

import (
	"context"
	"fmt"
	"regexp"
	"strings"

	"github.com/AltairaLabs/PromptKit/runtime/v2/evals"
)

// sentenceSplitter splits text on sentence-ending punctuation.
var sentenceSplitter = regexp.MustCompile(`[.!?]+`)

// SentenceCountHandler counts sentences in the current output.
// Params:
//   - max: int — the sentence cap (optional; the max_sentences alias's
//     legacy max_sentences param is normalized to it)
//
// Without max the score is 1 and the count is a measurement in
// Details["count"]. With max, a reply within the cap scores 1 and one over it
// scores max / count, so a default guardrail (floor 1.0) fires only over the
// cap and the score still says how far over. The handler states no pass or
// fail; a wrapper's threshold does.
type SentenceCountHandler struct{}

// Type returns the eval type identifier.
func (h *SentenceCountHandler) Type() string { return "sentence_count" }

// Eval counts sentences in the current output.
func (h *SentenceCountHandler) Eval(
	_ context.Context,
	evalCtx *evals.EvalContext,
	params map[string]any,
) (*evals.EvalResult, error) {
	count := countSentences(evalCtx.CurrentOutput)
	details := map[string]any{keyCount: count}

	maxSentences := extractIntPtr(params, keyMax)
	if maxSentences == nil || *maxSentences <= 0 {
		score := 1.0
		return &evals.EvalResult{Type: h.Type(), Score: &score, Details: details}, nil
	}
	details[keyMax] = *maxSentences

	if count > *maxSentences {
		return &evals.EvalResult{
			Type:        h.Type(),
			Score:       scorePtr(float64(*maxSentences) / float64(count)),
			Explanation: fmt.Sprintf("%d sentences exceeds max %d", count, *maxSentences),
			Details:     details,
		}, nil
	}

	return &evals.EvalResult{
		Type:        h.Type(),
		Score:       scorePtr(1),
		Explanation: fmt.Sprintf("%d sentences, max %d", count, *maxSentences),
		Details:     details,
	}, nil
}

// ValidateParams rejects a max that is not a positive integer, so a cap that
// can never be met (0) or that is the wrong type fails when the pack loads
// rather than measuring nothing.
func (h *SentenceCountHandler) ValidateParams(params map[string]any) error {
	raw, present := params[keyMax]
	if !present {
		return nil
	}
	if v := extractIntPtr(params, keyMax); v == nil || *v <= 0 {
		return fmt.Errorf("%s: max must be a positive integer, got %v", h.Type(), raw)
	}
	return nil
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
