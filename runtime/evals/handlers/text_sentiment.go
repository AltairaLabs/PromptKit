package handlers

import (
	"context"

	"github.com/AltairaLabs/PromptKit/runtime/evals"
)

// TextSentimentHandler is a pure eval primitive: it scores text against
// a sentiment classification model (e.g.
// `cardiffnlp/twitter-roberta-base-sentiment-latest`,
// `distilbert-base-uncased-finetuned-sst-2-english`) and emits the
// score for the chosen expected_label.
//
// Threshold judgment lives on the `type: assertion` wrapper:
//
//   - type: assertion
//     params:
//     eval_type: text_sentiment
//     eval_params:
//     model: cardiffnlp/twitter-roberta-base-sentiment-latest
//     expected_label: positive
//     min_score: 0.7
//
// Pack-level runtime eval (emits raw signal, no judgment):
//
//	evals:
//	  - id: response-sentiment
//	    type: text_sentiment
//	    trigger: every_turn
//	    params:
//	      model: cardiffnlp/twitter-roberta-base-sentiment-latest
//	      expected_label: positive
//
// Params:
//   - model         string  (required)
//   - expected_label string (required)
//   - message_role  string  (optional, default "assistant")
//   - message_index int     (optional, default -1)
//   - classifier_id string  (optional)
type TextSentimentHandler struct{}

// Type returns the eval type identifier.
func (h *TextSentimentHandler) Type() string { return "text_sentiment" }

// Eval runs the shared text-classify pipeline.
func (h *TextSentimentHandler) Eval(
	ctx context.Context, evalCtx *evals.EvalContext, params map[string]any,
) (*evals.EvalResult, error) {
	return runTextClassifyEval(ctx, evalCtx, h.Type(), params), nil
}
