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

// llmJudgeResult mirrors expected judge output.
type llmJudgeResult struct {
	Passed    bool     `json:"passed"`
	Reasoning string   `json:"reasoning,omitempty"`
	Score     float64  `json:"score,omitempty"`
	Evidence  []string `json:"evidence,omitempty"`
}

// NewLLMJudgeValidator evaluates a single assistant response via an LLM judge.
// Params:
// - criteria (string, required) or rubric (string)
// - judge (string, optional) -> name from metadata judge_targets
// - temperature, max_tokens (optional) for judge call
// - conversation_aware (bool) to include prior messages
// Requires metadata to carry judge_targets (map[string]providers.ProviderSpec).
func NewLLMJudgeValidator(params map[string]interface{}) runtimeValidators.Validator {
	return &llmJudgeValidator{}
}

type llmJudgeValidator struct{}

// Validate runs the judge provider on a single assistant response.
func (v *llmJudgeValidator) Validate(content string, params map[string]interface{}) runtimeValidators.ValidationResult {
	judgeSpec, err := selectJudgeSpec(params)
	if err != nil {
		return runtimeValidators.ValidationResult{
			Passed:  false,
			Details: map[string]interface{}{"error": err.Error()},
		}
	}

	req := buildJudgeRequest(content, params)

	provider, err := providers.CreateProviderFromSpec(judgeSpec)
	if err != nil {
		return runtimeValidators.ValidationResult{
			Passed: false,
			Details: map[string]interface{}{
				"error": fmt.Sprintf("create judge provider: %v", err),
			},
		}
	}
	defer provider.Close()

	resp, err := provider.Predict(context.Background(), req)
	if err != nil {
		return runtimeValidators.ValidationResult{
			Passed: false,
			Details: map[string]interface{}{
				"error": fmt.Sprintf("judge predict failed: %v", err),
			},
		}
	}

	verdict := parseJudgeVerdict(resp.Content)
	return runtimeValidators.ValidationResult{
		Passed: verdict.Passed,
		Details: map[string]interface{}{
			"reasoning": verdict.Reasoning,
			"score":     verdict.Score,
			"evidence":  verdict.Evidence,
			"raw":       resp.Content,
		},
	}
}

func selectJudgeSpec(params map[string]interface{}) (providers.ProviderSpec, error) {
	meta, _ := params["_metadata"].(map[string]interface{})
	targets := coerceJudgeTargets(meta["judge_targets"])
	if len(targets) == 0 {
		return providers.ProviderSpec{}, fmt.Errorf("judge_targets missing; ensure config.judges is loaded")
	}

	name, _ := params["judge"].(string)
	return selectJudgeFromTargets(targets, name)
}

func buildJudgeRequest(content string, params map[string]interface{}) providers.PredictionRequest {
	criteria, _ := params["criteria"].(string)
	rubric, _ := params["rubric"].(string)
	var sections []string
	if criteria != "" {
		sections = append(sections, fmt.Sprintf("CRITERIA:\n%s", criteria))
	}
	if rubric != "" {
		sections = append(sections, fmt.Sprintf("RUBRIC:\n%s", rubric))
	}

	var contextMsg string
	if convAware, _ := params["conversation_aware"].(bool); convAware {
		if msgs, ok := params["_execution_context_messages"].([]types.Message); ok {
			contextMsg = formatConversation(msgs)
		}
	}

	system := "You are an impartial judge. Respond with JSON {\"passed\":bool,\"score\":number,\"reasoning\":string}."
	var userBuilder strings.Builder
	if len(sections) > 0 {
		userBuilder.WriteString(strings.Join(sections, "\n\n"))
		userBuilder.WriteString("\n\n")
	}
	if contextMsg != "" {
		userBuilder.WriteString("CONVERSATION:\n")
		userBuilder.WriteString(contextMsg)
		userBuilder.WriteString("\n\n")
	}
	userBuilder.WriteString("ASSISTANT RESPONSE:\n")
	userBuilder.WriteString(content)

	temp := float32(0.0)
	if t, ok := params["temperature"].(float64); ok {
		temp = float32(t)
	}
	maxTokens := 0
	if mt, ok := params["max_tokens"].(int); ok {
		maxTokens = mt
	}

	return providers.PredictionRequest{
		System:      system,
		Messages:    []types.Message{{Role: "user", Content: userBuilder.String()}},
		Temperature: temp,
		MaxTokens:   maxTokens,
	}
}

func parseJudgeVerdict(content string) llmJudgeResult {
	var res llmJudgeResult
	if json.Unmarshal([]byte(content), &res) == nil {
		return res
	}
	// naive fallback: check for passed true/false
	lower := strings.ToLower(content)
	res.Passed = strings.Contains(lower, `"passed":true`) ||
		strings.Contains(lower, `passed": true`) ||
		strings.Contains(lower, "passed: true")
	res.Reasoning = content
	return res
}

func formatConversation(msgs []types.Message) string {
	var b strings.Builder
	for i := range msgs {
		m := &msgs[i]
		b.WriteString(m.Role)
		b.WriteString(": ")
		b.WriteString(m.GetContent())
		b.WriteString("\n")
	}
	return b.String()
}
