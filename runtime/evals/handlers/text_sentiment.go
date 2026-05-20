package handlers

import (
	"context"

	"github.com/AltairaLabs/PromptKit/runtime/evals"
)

// TextSentimentHandler scores text against a sentiment classification
// model (e.g. `cardiffnlp/twitter-roberta-base-sentiment-latest`,
// `distilbert-base-uncased-finetuned-sst-2-english`) and grades against
// a configurable threshold.
//
// Mirrors `audio_emotion` exactly: the assertion passes when the chosen
// label's score is at-or-above `min_score`. No max_score support —
// "this output should have positive sentiment" is the canonical use
// case, and the lower-bound framing is what naturally fits. If the
// inverse is wanted (avoid a label), point the assertion at the
// opposing label and keep the same min_score semantics.
//
// Params:
//   - model         string  (required) — backend model id
//   - expected_label string (required) — label that must score >= min_score
//   - min_score     float   (optional, default 0.5)
//   - message_role  string  (optional, default "assistant")
//   - message_index int     (optional, default -1 = latest match)
//   - classifier_id string  (optional)
type TextSentimentHandler struct{}

// Type returns the eval type identifier.
func (h *TextSentimentHandler) Type() string { return "text_sentiment" }

// Eval runs the shared text-classify pipeline with max_score disallowed
// so the parameter surface stays unambiguous.
func (h *TextSentimentHandler) Eval(
	ctx context.Context, evalCtx *evals.EvalContext, params map[string]any,
) (*evals.EvalResult, error) {
	return runTextClassifyEval(ctx, evalCtx, h.Type(), params, false), nil
}
