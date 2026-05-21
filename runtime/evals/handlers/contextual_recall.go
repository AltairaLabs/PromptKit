package handlers

import (
	"context"

	"github.com/AltairaLabs/PromptKit/runtime/evals"
)

// ContextualRecallHandler scores how completely the retrieved chunks
// cover the information needed for the ground-truth answer. Equivalent
// in name to DeepEval / Ragas `contextual_recall`.
//
// Default prompts adapted from the public DeepEval / Ragas reference
// implementations (Apache 2.0). Override per-call by passing
// system_prompt or criteria in params.
//
// Params:
//   - contexts ([]string) | context (string) | context_field (string):
//     retrieved chunks (required)
//   - reference (string) | expected_output (string): the ground-truth
//     answer (required)
//   - question (string): override the auto-extracted last user turn
//   - rubric, model, system_prompt: standard llm_judge knobs
//
// Putting min_score / max_score on this handler is rejected — wrap
// with `type: assertion` and set the threshold on the wrapper.
type ContextualRecallHandler struct{}

// Type returns the eval type identifier.
func (h *ContextualRecallHandler) Type() string { return "contextual_recall" }

// Eval scores how completely the retrieved chunks cover the information
// the ground-truth answer relies on.
func (h *ContextualRecallHandler) Eval(
	ctx context.Context,
	evalCtx *evals.EvalContext,
	params map[string]any,
) (*evals.EvalResult, error) {
	return evalChunkContext(ctx, evalCtx, h.Type(), params, chunkContextSpec{
		otherLabel:   "REFERENCE ANSWER",
		otherValueFn: func(in ragInputs) string { return in.reference },
		otherMissing: ragNoReferenceMessage,
		systemPrompt: contextualRecallSystemPrompt,
		criteria:     contextualRecallCriteria,
	})
}

const contextualRecallSystemPrompt = "You are a retrieval-recall evaluator. " +
	"You will be shown a REFERENCE ANSWER (ground truth) and a list of RETRIEVED CHUNKS. " +
	"Decide what fraction of the information the reference answer relies on is present in the chunks." +
	"\n\n" +
	"Score on [0, 1]: " +
	"1.0 — every claim in the reference answer is supported by the retrieved chunks. " +
	"0.0 — none of the reference answer's claims are supported by the chunks. " +
	"Partial credit by claim count." +
	"\n\n" +
	"Respond with JSON: {\"passed\": bool, \"score\": float in [0,1], " +
	"\"reasoning\": string}. " +
	"In your reasoning, list reference-answer claims that are missing from the chunks." +
	"\n\n" +
	"(Prompt adapted from Ragas / DeepEval reference implementations, Apache 2.0.)"

const contextualRecallCriteria = "Compute the recall of the RETRIEVED CHUNKS against the REFERENCE ANSWER."
