package handlers

import (
	"context"

	"github.com/AltairaLabs/PromptKit/runtime/evals"
)

// TextToxicityHandler scores text against a toxicity classification model
// (e.g. `unitary/toxic-bert`, `s-nlp/roberta_toxicity_classifier`) and
// grades against a configurable threshold. Distinct from the legacy
// `toxicity` handler, which is an LLM-judge with the same name —
// `text_toxicity` is the deterministic classifier path that depends on
// `classify.TextClassifier` and an `inference` provider.
//
// The handler supports two grading modes:
//   - max_score (default usage): pass when the expected label's score
//     stays BELOW the threshold. Natural for "this output should NOT be
//     toxic" assertions, e.g. `expected_label: toxic, max_score: 0.3`.
//   - min_score: pass when the expected label's score is at-or-above the
//     threshold. Useful when the model emits a positive label like
//     "neutral" (s-nlp/roberta_toxicity_classifier) and the assertion
//     wants `expected_label: neutral, min_score: 0.7`.
//
// Specifying both min_score and max_score is rejected — pick one mode.
//
// Params:
//   - model         string  (required) — backend model id
//   - expected_label string (required) — label to grade
//   - max_score     float   (optional) — pass when label score < max_score
//   - min_score     float   (optional, default 0.5) — pass when label score >= min_score
//   - message_role  string  (optional, default "assistant") — which speaker's text to score
//   - message_index int     (optional, default -1 = latest match)
//   - classifier_id string  (optional) — explicit registry id; empty uses configured default
type TextToxicityHandler struct{}

// Type returns the eval type identifier.
func (h *TextToxicityHandler) Type() string { return "text_toxicity" }

// Eval runs the shared text-classify pipeline with max_score allowed —
// toxicity assertions almost always want the "stay below X" framing,
// but the upper-bound form is purely an opt-in.
func (h *TextToxicityHandler) Eval(
	ctx context.Context, evalCtx *evals.EvalContext, params map[string]any,
) (*evals.EvalResult, error) {
	return runTextClassifyEval(ctx, evalCtx, h.Type(), params, true), nil
}
