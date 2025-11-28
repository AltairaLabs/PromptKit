package assertions

import (
	"context"
	"fmt"
	"strings"

	"github.com/AltairaLabs/PromptKit/runtime/providers"
	"github.com/AltairaLabs/PromptKit/runtime/types"
)

// NewLLMJudgeConversationValidator creates a conversation-level LLM judge validator.
// Params include criteria/rubric, optional judge name (from metadata judge_targets), and min_score.
func NewLLMJudgeConversationValidator() ConversationValidator {
	return &llmJudgeConversationValidator{}
}

type llmJudgeConversationValidator struct{}

// Type returns validator type name.
func (v *llmJudgeConversationValidator) Type() string { return "llm_judge_conversation" }

// ValidateConversation runs the judge provider across the full conversation context.
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
	defer provider.Close()

	resp, err := provider.Predict(ctx, req)
	if err != nil {
		return ConversationValidationResult{Passed: false, Message: fmt.Sprintf("judge predict failed: %v", err)}
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
			"reasoning": verdict.Reasoning,
			"score":     verdict.Score,
			"raw":       resp.Content,
		},
	}
}

func selectConversationJudgeSpec(
	convCtx *ConversationContext,
	params map[string]interface{},
) (providers.ProviderSpec, error) {
	targets := coerceJudgeTargets(convCtx.Metadata.Extras["judge_targets"])
	if len(targets) == 0 {
		return providers.ProviderSpec{}, fmt.Errorf("judge_targets missing; ensure config.judges is loaded")
	}

	name, _ := params["judge"].(string)
	return selectJudgeFromTargets(targets, name)
}

func buildConversationJudgeRequest(
	convCtx *ConversationContext,
	params map[string]interface{},
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
