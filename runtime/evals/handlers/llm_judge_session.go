package handlers

import (
	"context"
	"fmt"
	"strings"

	"github.com/AltairaLabs/PromptKit/runtime/evals"
)

// LLMJudgeSessionHandler is the session-level counterpart of
// LLMJudgeHandler. It runs the judge once over a full, role-labeled
// transcript of the conversation — user (and other) turns, assistant
// text, and every tool call with its arguments and result — so the
// judge sees what the agent actually did, not only its prose. Same
// params, same conventions. Registered under `llm_judge_conversation`
// too (see register.go).
type LLMJudgeSessionHandler struct{}

// Type returns the eval type identifier.
func (h *LLMJudgeSessionHandler) Type() string {
	return "llm_judge_session"
}

// Eval runs the LLM judge over the full session transcript.
func (h *LLMJudgeSessionHandler) Eval(
	ctx context.Context,
	evalCtx *evals.EvalContext,
	params map[string]any,
) (result *evals.EvalResult, err error) {
	return runJudgeEval(ctx, evalCtx, h.Type(), params, renderSessionTranscript(evalCtx)), nil
}

// renderSessionTranscript builds the content the session/conversation judge
// sees: a role-labeled transcript of the interaction followed by the tool
// calls with their arguments and results.
//
// The previous view (collectAssistantContent) was assistant text only — it
// dropped user turns, every tool call, and every tool result, so a tool-using
// or observer agent (whose real output *is* its tool calls) was judged blind
// and scored meaninglessly (#1615). The data was always present on the eval
// context; it was simply discarded.
//
// System messages (setup, not interaction) and tool-result rows are omitted
// from the turn list — tool results are rendered with their calls below.
func renderSessionTranscript(evalCtx *evals.EvalContext) string {
	var b strings.Builder

	for i := range evalCtx.Messages {
		msg := &evalCtx.Messages[i]
		role := strings.ToLower(strings.TrimSpace(msg.Role))
		if role == "system" || role == roleTool {
			continue
		}
		content := strings.TrimSpace(msg.GetContent())
		if content == "" {
			continue
		}
		if b.Len() > 0 {
			b.WriteString("\n")
		}
		fmt.Fprintf(&b, "%s: %s", titleRole(msg.Role), content)
	}

	if len(evalCtx.ToolCalls) > 0 {
		if b.Len() > 0 {
			b.WriteString("\n\n")
		}
		b.WriteString("Tool calls (name, arguments, result):\n")
		b.WriteString(formatToolCallViews(viewsFromRecords(evalCtx.ToolCalls)))
	}

	return b.String()
}

// titleRole renders a message role for the transcript with an upper-case first
// letter, preserving non-standard roles (e.g. "customer", "agent") verbatim.
func titleRole(role string) string {
	role = strings.TrimSpace(role)
	if role == "" {
		return "Unknown"
	}
	return strings.ToUpper(role[:1]) + role[1:]
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
