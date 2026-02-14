package handlers

import (
	"context"
	"fmt"
	"strings"

	"github.com/AltairaLabs/PromptKit/runtime/evals"
)

// LLMJudgeSessionHandler evaluates an entire conversation using
// an LLM judge. It concatenates all assistant messages into a
// single content string for evaluation.
//
// The JudgeProvider must be supplied in
// evalCtx.Metadata["judge_provider"].
//
// Params:
//   - criteria (string, required): what to evaluate
//   - rubric (string, optional): detailed scoring guidance
//   - model (string, optional): model override for the judge
//   - system_prompt (string, optional): override default system prompt
//   - min_score (float64, optional): minimum score to pass
type LLMJudgeSessionHandler struct{}

// Type returns the eval type identifier.
func (h *LLMJudgeSessionHandler) Type() string {
	return "llm_judge_session"
}

// Eval runs the LLM judge on all assistant messages in the session.
func (h *LLMJudgeSessionHandler) Eval(
	ctx context.Context,
	evalCtx *evals.EvalContext,
	params map[string]any,
) (result *evals.EvalResult, err error) {
	provider, extractErr := extractJudgeProvider(evalCtx)
	if extractErr != nil {
		return &evals.EvalResult{
			Type:        h.Type(),
			Passed:      false,
			Explanation: extractErr.Error(),
		}, nil
	}

	content := collectAssistantContent(evalCtx)
	opts := buildJudgeOpts(content, params)

	judgeResult, judgeErr := provider.Judge(ctx, opts)
	if judgeErr != nil {
		return &evals.EvalResult{
			Type:        h.Type(),
			Passed:      false,
			Explanation: fmt.Sprintf("judge error: %v", judgeErr),
		}, nil
	}

	return buildEvalResult(h.Type(), judgeResult, params), nil
}

// collectAssistantContent concatenates all assistant message
// content from the eval context, separated by newlines.
func collectAssistantContent(
	evalCtx *evals.EvalContext,
) string {
	var parts []string
	for i := range evalCtx.Messages {
		msg := &evalCtx.Messages[i]
		if strings.EqualFold(msg.Role, roleAssistant) {
			parts = append(parts, msg.GetContent())
		}
	}
	return strings.Join(parts, "\n")
}
