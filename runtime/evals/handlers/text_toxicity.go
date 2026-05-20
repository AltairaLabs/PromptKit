package handlers

import (
	"context"

	"github.com/AltairaLabs/PromptKit/runtime/evals"
)

// TextToxicityHandler is a pure eval primitive: it scores text
// against a toxicity classification model (e.g. `unitary/toxic-bert`,
// `s-nlp/roberta_toxicity_classifier`) and emits the score for the
// chosen expected_label. Distinct from the legacy `toxicity` handler,
// which is an LLM-judge with the same name — `text_toxicity` is the
// deterministic classifier path that depends on
// `classify.TextClassifier` and an `inference` provider.
//
// Threshold judgment lives on the `type: assertion` wrapper:
//
//	# "this output should NOT be toxic"
//	- type: assertion
//	  params:
//	    eval_type: text_toxicity
//	    eval_params: { model: unitary/toxic-bert, expected_label: toxic }
//	    max_score: 0.3
//
//	# "this output should sit in the neutral class"
//	- type: assertion
//	  params:
//	    eval_type: text_toxicity
//	    eval_params: { model: s-nlp/roberta_toxicity_classifier, expected_label: neutral }
//	    min_score: 0.7
//
// Pack-level runtime eval (emits the raw signal, no judgment):
//
//	evals:
//	  - id: response-toxicity
//	    type: text_toxicity
//	    trigger: every_turn
//	    params: { model: unitary/toxic-bert, expected_label: toxic }
//
// Params:
//   - model         string  (required) — backend model id
//   - expected_label string (required) — label whose score is emitted
//   - message_role  string  (optional, default "assistant")
//   - message_index int     (optional, default -1)
//   - classifier_id string  (optional)
type TextToxicityHandler struct{}

// Type returns the eval type identifier.
func (h *TextToxicityHandler) Type() string { return "text_toxicity" }

// Eval runs the shared text-classify pipeline.
func (h *TextToxicityHandler) Eval(
	ctx context.Context, evalCtx *evals.EvalContext, params map[string]any,
) (*evals.EvalResult, error) {
	return runTextClassifyEval(ctx, evalCtx, h.Type(), params), nil
}
