package handlers

import (
	"context"
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
	return runJudgeEval(ctx, evalCtx, h.Type(), params, collectAssistantContent(evalCtx)), nil
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
