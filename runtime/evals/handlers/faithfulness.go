package handlers

import (
	"context"
	"fmt"

	"github.com/AltairaLabs/PromptKit/runtime/evals"
)

// FaithfulnessHandler scores how well the answer is supported by the
// provided context. A faithful answer makes only claims backed by the
// context; an unfaithful answer asserts facts the context does not
// support. Equivalent in name to DeepEval / Ragas `faithfulness`.
//
// Default prompts adapted from the public DeepEval / Ragas reference
// implementations (Apache 2.0). Override per-call by passing
// system_prompt or criteria in params.
//
// Params (all optional):
//   - contexts ([]string) | context (string) | context_field (string):
//     retrieved chunks the answer should be grounded in
//   - rubric, model, system_prompt: standard llm_judge knobs
//
// Putting min_score / max_score on this handler is rejected — wrap
// with `type: assertion` and set the threshold on the wrapper.
type FaithfulnessHandler struct{}

// Type returns the eval type identifier.
func (h *FaithfulnessHandler) Type() string { return "faithfulness" }

// Eval scores the current assistant output against the supplied
// context for factual consistency.
func (h *FaithfulnessHandler) Eval(
	ctx context.Context,
	evalCtx *evals.EvalContext,
	params map[string]any,
) (*evals.EvalResult, error) {
	in := buildRAGInputs(evalCtx, params)
	if len(in.contexts) == 0 {
		return ragErrorResult(h.Type(), ragNoContextMessage), nil
	}

	content := fmt.Sprintf(
		"CONTEXT:\n%s\n\nANSWER:\n%s",
		formatContexts(in.contexts), in.answer,
	)
	return ragJudgeCall(
		ctx, evalCtx, h.Type(), params, content,
		faithfulnessSystemPrompt, faithfulnessCriteria,
	)
}

const faithfulnessSystemPrompt = "You are a faithfulness evaluator. " +
	"You will be shown a CONTEXT and an ANSWER. " +
	"Decide how well each factual claim in the ANSWER is supported by the CONTEXT. " +
	"\n\n" +
	"Score on [0, 1]: " +
	"1.0 — every factual claim in the answer is directly supported by the context. " +
	"0.0 — the answer's claims are entirely unsupported or contradicted by the context. " +
	"For partial credit, return the proportion of supported claims. " +
	"\n\n" +
	"Respond with JSON: {\"passed\": bool, \"score\": float in [0,1], " +
	"\"reasoning\": string}. " +
	"In your reasoning, list any unsupported or contradicted claims." +
	"\n\n" +
	"(Prompt adapted from Ragas / DeepEval reference implementations, Apache 2.0.)"

const faithfulnessCriteria = "Score the ANSWER's faithfulness to the CONTEXT."
