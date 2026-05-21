package handlers

import (
	"context"
	"fmt"
	"strings"

	"github.com/AltairaLabs/PromptKit/runtime/evals"
)

// LLMJudgeSessionHandler is the session-level counterpart of
// LLMJudgeHandler — concatenates all assistant messages and runs the
// judge once over the lot. Same params, same conventions.
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
	if msg := rejectThresholdParams(params); msg != "" {
		return errorResult(h.Type(), msg), nil
	}
	provider, extractErr := extractJudgeProvider(evalCtx)
	if extractErr != nil {
		return &evals.EvalResult{
			Type:        h.Type(),
			Score:       boolScore(false),
			Explanation: extractErr.Error(),
		}, nil
	}

	content := collectAssistantContent(evalCtx)
	opts := buildJudgeOpts(content, params)
	opts.Emitter = emitterFromEvalCtx(evalCtx)

	judgeResult, judgeErr := provider.Judge(ctx, opts)
	if judgeErr != nil {
		return &evals.EvalResult{
			Type:        h.Type(),
			Score:       boolScore(false),
			Explanation: fmt.Sprintf("judge error: %v", judgeErr),
		}, nil
	}

	return buildEvalResult(h.Type(), judgeResult), nil
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
