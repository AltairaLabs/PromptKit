package assertions

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/AltairaLabs/PromptKit/runtime/providers"
	"github.com/AltairaLabs/PromptKit/runtime/types"
	runtimeValidators "github.com/AltairaLabs/PromptKit/runtime/validators"
)

// llmJudgeToolCallsValidator evaluates tool call behavior via an LLM judge.
// Instead of judging the assistant's text response, it feeds tool call data
// (names, arguments, results) to the judge for evaluation.
type llmJudgeToolCallsValidator struct{}

// NewLLMJudgeToolCallsValidator creates a new llm_judge_tool_calls validator.
func NewLLMJudgeToolCallsValidator(params map[string]interface{}) runtimeValidators.Validator {
	return &llmJudgeToolCallsValidator{}
}

// Validate runs the judge provider on formatted tool call data from the turn.
func (v *llmJudgeToolCallsValidator) Validate(
	content string, params map[string]interface{},
) runtimeValidators.ValidationResult {
	trace, ok := resolveTurnToolTrace(params)
	if !ok {
		return runtimeValidators.ValidationResult{
			Passed: true,
			Details: map[string]interface{}{
				"skipped": true,
				"reason":  "turn tool trace not available (duplex path)",
			},
		}
	}

	filtered := filterToolCalls(trace, params)
	if len(filtered) == 0 {
		return runtimeValidators.ValidationResult{
			Passed: true,
			Details: map[string]interface{}{
				"skipped": true,
				"reason":  "no matching tool calls",
			},
		}
	}

	toolCallText := formatToolCallsForJudge(filtered)

	judgeSpec, err := selectJudgeSpec(params)
	if err != nil {
		return runtimeValidators.ValidationResult{
			Passed:  false,
			Details: map[string]interface{}{"error": err.Error()},
		}
	}

	req := buildToolCallJudgeRequest(toolCallText, params, judgeSpec.Model)

	provider, err := providers.CreateProviderFromSpec(judgeSpec)
	if err != nil {
		return runtimeValidators.ValidationResult{
			Passed:  false,
			Details: map[string]interface{}{"error": fmt.Sprintf("create judge provider: %v", err)},
		}
	}
	defer provider.Close()

	resp, err := provider.Predict(context.Background(), req)
	if err != nil {
		return runtimeValidators.ValidationResult{
			Passed:  false,
			Details: map[string]interface{}{"error": fmt.Sprintf("judge predict failed: %v", err)},
		}
	}

	verdict := parseJudgeVerdict(resp.Content)
	passed := verdict.Passed
	if minScore, ok := params["min_score"].(float64); ok {
		passed = passed && verdict.Score >= minScore
	}

	return runtimeValidators.ValidationResult{
		Passed: passed,
		Details: map[string]interface{}{
			"reasoning":       verdict.Reasoning,
			"score":           verdict.Score,
			"evidence":        verdict.Evidence,
			"raw":             resp.Content,
			"tool_calls_sent": len(filtered),
		},
	}
}

// filterToolCalls filters tool calls by optional tools and round_index params.
func filterToolCalls(trace []TurnToolCall, params map[string]interface{}) []TurnToolCall {
	toolNames := extractStringSlice(params, "tools")
	roundIndex := extractIntParam(params, "round_index", countNotSet)

	toolSet := make(map[string]bool, len(toolNames))
	for _, t := range toolNames {
		toolSet[t] = true
	}

	var filtered []TurnToolCall
	for _, tc := range trace {
		if len(toolSet) > 0 && !toolSet[tc.Name] {
			continue
		}
		if roundIndex != countNotSet && tc.RoundIndex != roundIndex {
			continue
		}
		filtered = append(filtered, tc)
	}
	return filtered
}

// formatToolCallsForJudge formats tool calls as structured text for the judge prompt.
func formatToolCallsForJudge(calls []TurnToolCall) string {
	var b strings.Builder
	for i, tc := range calls {
		if i > 0 {
			b.WriteString("\n")
		}
		fmt.Fprintf(&b, "TOOL CALL %d (round %d):\n", i+1, tc.RoundIndex)
		fmt.Fprintf(&b, "  Tool: %s\n", tc.Name)

		argsJSON, err := json.Marshal(tc.Args)
		if err != nil {
			fmt.Fprintf(&b, "  Arguments: %v\n", tc.Args)
		} else {
			fmt.Fprintf(&b, "  Arguments: %s\n", string(argsJSON))
		}

		if tc.Result != "" {
			fmt.Fprintf(&b, "  Result: %s\n", tc.Result)
		} else {
			b.WriteString("  Result: (none)\n")
		}

		if tc.Error != "" {
			fmt.Fprintf(&b, "  Error: %s\n", tc.Error)
		} else {
			b.WriteString("  Error: (none)\n")
		}
	}
	return b.String()
}

// buildToolCallJudgeRequest builds a PredictionRequest for judging tool calls.
func buildToolCallJudgeRequest(
	toolCallText string, params map[string]interface{}, model string,
) providers.PredictionRequest {
	var contextMsg string
	if convAware, _ := params["conversation_aware"].(bool); convAware {
		if msgs, ok := params["_execution_context_messages"].([]types.Message); ok {
			contextMsg = formatConversation(msgs)
		}
	}

	temp := float32(0.0)
	if t, ok := params["temperature"].(float64); ok {
		temp = float32(t)
	}
	maxTokens := 0
	if mt, ok := params["max_tokens"].(int); ok {
		maxTokens = mt
	}

	req := assembleToolCallJudgeRequest(toolCallText, contextMsg, params, model)
	req.Temperature = temp
	req.MaxTokens = maxTokens
	return req
}

// assembleToolCallJudgeRequest builds a PredictionRequest for judging tool calls.
// Shared by both turn-level and conversation-level validators.
func assembleToolCallJudgeRequest(
	toolCallText, conversationText string,
	params map[string]interface{},
	model string,
) providers.PredictionRequest {
	criteria, _ := params["criteria"].(string)
	rubric, _ := params["rubric"].(string)

	// Try prompt registry first
	if req := buildToolCallPromptRequest(
		toolCallText, criteria, rubric, conversationText, params, model,
	); req != nil {
		return *req
	}

	var sections []string
	if criteria != "" {
		sections = append(sections, fmt.Sprintf("CRITERIA:\n%s", criteria))
	}
	if rubric != "" {
		sections = append(sections, fmt.Sprintf("RUBRIC:\n%s", rubric))
	}

	system := "You are an impartial judge. Evaluate the tool calls and respond with JSON " +
		"{\"passed\":bool,\"score\":number,\"reasoning\":string}."

	var userBuilder strings.Builder
	if len(sections) > 0 {
		userBuilder.WriteString(strings.Join(sections, "\n\n"))
		userBuilder.WriteString("\n\n")
	}
	if conversationText != "" {
		userBuilder.WriteString("CONVERSATION:\n")
		userBuilder.WriteString(conversationText)
		userBuilder.WriteString("\n\n")
	}
	userBuilder.WriteString("TOOL CALLS:\n")
	userBuilder.WriteString(toolCallText)

	return providers.PredictionRequest{
		System:   system,
		Messages: []types.Message{{Role: "user", Content: userBuilder.String()}},
	}
}

// buildToolCallPromptRequest renders a prompt from the registry with tool_calls variable.
func buildToolCallPromptRequest(
	toolCallText, criteria, rubric, conversation string,
	params map[string]interface{},
	model string,
) *providers.PredictionRequest {
	registry := extractPromptRegistry(params)
	promptName := selectJudgePromptName(params)
	if registry == nil || promptName == "" {
		return nil
	}

	vars := map[string]string{
		"criteria":     criteria,
		"rubric":       rubric,
		"conversation": conversation,
		"tool_calls":   toolCallText,
	}

	assembled := registry.LoadWithVars(promptName, vars, model)
	if assembled == nil {
		return nil
	}

	return &providers.PredictionRequest{
		System:   assembled.SystemPrompt,
		Messages: []types.Message{{Role: "user", Content: "Return the JSON verdict now."}},
	}
}
