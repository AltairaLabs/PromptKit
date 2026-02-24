package assertions

import (
	"context"
	"fmt"
	"strings"

	"github.com/AltairaLabs/PromptKit/runtime/providers"
	"github.com/AltairaLabs/PromptKit/runtime/types"
)

// llmJudgeToolCallsConversationValidator evaluates tool call behavior across
// the entire conversation via an LLM judge.
type llmJudgeToolCallsConversationValidator struct{}

// NewLLMJudgeToolCallsConversationValidator creates a conversation-level LLM judge for tool calls.
func NewLLMJudgeToolCallsConversationValidator() ConversationValidator {
	return &llmJudgeToolCallsConversationValidator{}
}

// Type returns the validator type name.
func (v *llmJudgeToolCallsConversationValidator) Type() string { return "llm_judge_tool_calls" }

// ValidateConversation runs the judge provider across tool calls in the conversation.
func (v *llmJudgeToolCallsConversationValidator) ValidateConversation(
	ctx context.Context,
	convCtx *ConversationContext,
	params map[string]interface{},
) ConversationValidationResult {
	params = cloneParamsWithMetadata(params, convCtx)

	// Filter by tool names if specified
	toolNames := extractStringSlice(params, "tools")
	views := filterConversationToolCalls(convCtx.ToolCalls, toolNames)

	if len(views) == 0 {
		return ConversationValidationResult{
			Type:    v.Type(),
			Passed:  true,
			Message: "no matching tool calls to judge",
			Details: map[string]interface{}{"skipped": true},
		}
	}

	toolCallText := formatToolCallViewsForJudge(views)

	judgeSpec, err := selectConversationJudgeSpec(convCtx, params)
	if err != nil {
		return ConversationValidationResult{Passed: false, Message: err.Error()}
	}

	req := buildConversationToolCallJudgeRequest(convCtx, toolCallText, params, judgeSpec.Model)

	provider, err := providers.CreateProviderFromSpec(judgeSpec)
	if err != nil {
		return ConversationValidationResult{
			Passed:  false,
			Message: fmt.Sprintf("create judge provider: %v", err),
		}
	}
	defer provider.Close()

	resp, err := provider.Predict(ctx, req)
	if err != nil {
		return ConversationValidationResult{
			Passed:  false,
			Message: fmt.Sprintf("judge predict failed: %v", err),
		}
	}

	verdict := parseJudgeVerdict(resp.Content)
	passed := verdict.Passed
	if minScore, ok := params["min_score"].(float64); ok {
		passed = passed && verdict.Score >= minScore
	}

	return ConversationValidationResult{
		Type:    v.Type(),
		Passed:  passed,
		Message: fmt.Sprintf("score=%.2f", verdict.Score),
		Details: map[string]interface{}{
			"reasoning":       verdict.Reasoning,
			"score":           verdict.Score,
			"raw":             resp.Content,
			"tool_calls_sent": len(views),
		},
	}
}

// filterConversationToolCalls filters records by tool names, converting to ToolCallView.
func filterConversationToolCalls(records []ToolCallRecord, toolNames []string) []ToolCallView {
	views := toolCallViewsFromRecords(records)
	if len(toolNames) == 0 {
		return views
	}

	toolSet := make(map[string]bool, len(toolNames))
	for _, t := range toolNames {
		toolSet[t] = true
	}

	var filtered []ToolCallView
	for _, v := range views {
		if toolSet[v.Name] {
			filtered = append(filtered, v)
		}
	}
	return filtered
}

// formatToolCallViewsForJudge formats ToolCallView slice as structured text.
func formatToolCallViewsForJudge(views []ToolCallView) string {
	var b strings.Builder
	for i, v := range views {
		if i > 0 {
			b.WriteString("\n")
		}
		fmt.Fprintf(&b, "TOOL CALL %d (turn %d):\n", i+1, v.Index)
		fmt.Fprintf(&b, "  Tool: %s\n", v.Name)
		fmt.Fprintf(&b, "  Arguments: %v\n", v.Args)
		if v.Result != "" {
			fmt.Fprintf(&b, "  Result: %s\n", v.Result)
		} else {
			b.WriteString("  Result: (none)\n")
		}
		if v.Error != "" {
			fmt.Fprintf(&b, "  Error: %s\n", v.Error)
		} else {
			b.WriteString("  Error: (none)\n")
		}
	}
	return b.String()
}

// buildConversationToolCallJudgeRequest builds a request for judging conversation tool calls.
func buildConversationToolCallJudgeRequest(
	convCtx *ConversationContext,
	toolCallText string,
	params map[string]interface{},
	model string,
) providers.PredictionRequest {
	criteria, _ := params["criteria"].(string)
	rubric, _ := params["rubric"].(string)

	var sections []string
	if criteria != "" {
		sections = append(sections, fmt.Sprintf("CRITERIA:\n%s", criteria))
	}
	if rubric != "" {
		sections = append(sections, fmt.Sprintf("RUBRIC:\n%s", rubric))
	}

	convText := formatConversation(convCtx.AllTurns)

	// Try prompt registry
	if promptReq := buildToolCallPromptRequest(toolCallText, criteria, rubric, convText, params, model); promptReq != nil {
		return *promptReq
	}

	system := "You are an impartial judge. Evaluate the tool calls and respond with JSON " +
		"{\"passed\":bool,\"score\":number,\"reasoning\":string}."

	var userBuilder strings.Builder
	if len(sections) > 0 {
		userBuilder.WriteString(strings.Join(sections, "\n\n"))
		userBuilder.WriteString("\n\n")
	}
	userBuilder.WriteString("CONVERSATION:\n")
	userBuilder.WriteString(convText)
	userBuilder.WriteString("\n\nTOOL CALLS:\n")
	userBuilder.WriteString(toolCallText)

	return providers.PredictionRequest{
		System:   system,
		Messages: []types.Message{{Role: "user", Content: userBuilder.String()}},
	}
}
