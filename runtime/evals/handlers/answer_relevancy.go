package handlers

import (
	"context"
	"fmt"

	"github.com/AltairaLabs/PromptKit/runtime/evals"
)

// AnswerRelevancyHandler scores how directly the assistant's answer
// addresses the user's question. Equivalent in name to DeepEval /
// Ragas `answer_relevancy`.
//
// Default prompts adapted from the public DeepEval / Ragas reference
// implementations (Apache 2.0). Override per-call by passing
// system_prompt or criteria in params.
//
// Params (all optional):
//   - question (string): override the auto-extracted last user turn
//   - rubric, model, system_prompt: standard llm_judge knobs
type AnswerRelevancyHandler struct{}

// Type returns the eval type identifier.
func (h *AnswerRelevancyHandler) Type() string { return "answer_relevancy" }

// Eval scores the current assistant output against the question for
// on-topic focus and directness.
func (h *AnswerRelevancyHandler) Eval(
	ctx context.Context,
	evalCtx *evals.EvalContext,
	params map[string]any,
) (*evals.EvalResult, error) {
	in := buildRAGInputs(evalCtx, params)
	if in.question == "" {
		return ragErrorResult(
			h.Type(),
			"no question found: supply 'question' param or include a user turn in the session",
		), nil
	}

	content := fmt.Sprintf(
		"QUESTION:\n%s\n\nANSWER:\n%s",
		in.question, in.answer,
	)
	return ragJudgeCall(
		ctx, evalCtx, h.Type(), params, content,
		answerRelevancySystemPrompt, answerRelevancyCriteria,
	)
}

const answerRelevancySystemPrompt = "You are an answer-relevancy evaluator. " +
	"You will be shown a QUESTION and an ANSWER. " +
	"Decide how directly and on-topic the answer addresses the question. " +
	"\n\n" +
	"Score on [0, 1]: " +
	"1.0 — the answer directly and entirely addresses the question. " +
	"0.0 — the answer is entirely off-topic or evasive. " +
	"Partial credit for answers that address the question with significant off-topic content. " +
	"\n\n" +
	"Respond with JSON: {\"passed\": bool, \"score\": float in [0,1], " +
	"\"reasoning\": string}. " +
	"In your reasoning, identify any off-topic content." +
	"\n\n" +
	"(Prompt adapted from Ragas / DeepEval reference implementations, Apache 2.0.)"

const answerRelevancyCriteria = "Score the ANSWER's relevance to the QUESTION."
