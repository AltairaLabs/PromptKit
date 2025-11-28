package assertions

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/AltairaLabs/PromptKit/runtime/providers"
	"github.com/AltairaLabs/PromptKit/runtime/types"
)

// Conversation-level LLM judge validator using judge targets from metadata.
// Params:
// - criteria or rubric
// - judge (optional) -> name in metadata judge_targets
// - min_score (optional)
// - conversation_aware is implied (uses full ConversationContext)
func NewLLMJudgeConversationValidator() ConversationValidator {
	return &llmJudgeConversationValidator{}
}

type llmJudgeConversationValidator struct{}

func (v *llmJudgeConversationValidator) Type() string { return "llm_judge_conversation" }

func (v *llmJudgeConversationValidator) ValidateConversation(
	ctx context.Context,
	convCtx *ConversationContext,
	params map[string]interface{},
) ConversationValidationResult {
	judgeSpec, err := selectConversationJudgeSpec(convCtx, params)
	if err != nil {
		return ConversationValidationResult{Passed: false, Message: err.Error()}
	}

	req := buildConversationJudgeRequest(convCtx, params)
	provider, err := providers.CreateProviderFromSpec(judgeSpec)
	if err != nil {
		return ConversationValidationResult{Passed: false, Message: fmt.Sprintf("create judge provider: %v", err)}
	}
	defer provider.Close() // nolint: errcheck

	resp, err := provider.Predict(ctx, req)
	if err != nil {
		return ConversationValidationResult{Passed: false, Message: fmt.Sprintf("judge predict failed: %v", err)}
	}

	verdict := parseConversationJudgeVerdict(resp.Content)
	passed := verdict.Passed
	if minScore, ok := params["min_score"].(float64); ok {
		passed = passed && verdict.Score >= minScore
	}

	return ConversationValidationResult{
		Type:    v.Type(),
		Passed:  passed,
		Message: fmt.Sprintf("score=%.2f", verdict.Score),
		Details: map[string]interface{}{
			"reasoning": verdict.Reasoning,
			"score":     verdict.Score,
			"raw":       resp.Content,
		},
	}
}

func selectConversationJudgeSpec(convCtx *ConversationContext, params map[string]interface{}) (providers.ProviderSpec, error) {
	targets := coerceJudgeTargets(convCtx.Metadata.Extras["judge_targets"])
	if len(targets) == 0 {
		return providers.ProviderSpec{}, fmt.Errorf("judge_targets missing; ensure config.judges is loaded")
	}

	if name, ok := params["judge"].(string); ok && name != "" {
		if spec, found := targets[name]; found {
			return spec, nil
		}
		return providers.ProviderSpec{}, fmt.Errorf("judge %s not found", name)
	}

	for _, spec := range targets {
		return spec, nil
	}
	return providers.ProviderSpec{}, fmt.Errorf("no judge targets available")
}

func coerceJudgeTargets(raw interface{}) map[string]providers.ProviderSpec {
	switch t := raw.(type) {
	case map[string]providers.ProviderSpec:
		return t
	case map[string]interface{}:
		out := make(map[string]providers.ProviderSpec, len(t))
		for k, v := range t {
			if spec, ok := v.(providers.ProviderSpec); ok {
				out[k] = spec
			}
		}
		return out
	default:
		return nil
	}
}

func buildConversationJudgeRequest(convCtx *ConversationContext, params map[string]interface{}) providers.PredictionRequest {
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
	userBuilder := strings.Builder{}
	if len(sections) > 0 {
		userBuilder.WriteString(strings.Join(sections, "\n\n"))
		userBuilder.WriteString("\n\n")
	}
	userBuilder.WriteString("CONVERSATION:\n")
	userBuilder.WriteString(convText)

	return providers.PredictionRequest{
		System:   "You are an impartial judge. Respond with JSON {\"passed\":bool,\"score\":number,\"reasoning\":string}.",
		Messages: []types.Message{{Role: "user", Content: userBuilder.String()}},
	}
}

type convJudgeResult struct {
	Passed    bool    `json:"passed"`
	Reasoning string  `json:"reasoning"`
	Score     float64 `json:"score"`
}

func parseConversationJudgeVerdict(content string) convJudgeResult {
	var res convJudgeResult
	if err := json.Unmarshal([]byte(content), &res); err == nil {
		return res
	}
	lower := strings.ToLower(content)
	res.Passed = strings.Contains(lower, "\"passed\":true") || strings.Contains(lower, "passed: true")
	res.Reasoning = content
	return res
}
