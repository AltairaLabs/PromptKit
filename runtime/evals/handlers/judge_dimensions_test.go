package handlers

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/AltairaLabs/PromptKit/runtime/v2/evals"
	"github.com/AltairaLabs/PromptKit/runtime/v2/providers"
	"github.com/AltairaLabs/PromptKit/runtime/v2/providers/mock"
)

const rubricJudgeReply = "```json\n" +
	`{"score": 0.8, "clarity": 0.9, "accuracy": 0.7, "passed": true, "reasoning": "clear", "notes": {"x": 1}}` +
	"\n```"

func TestJudgeDimensions_NumericFieldsBesidesScore(t *testing.T) {
	jr, err := parseJudgeResponse(rubricJudgeReply)
	require.NoError(t, err)
	assert.InDelta(t, 0.8, jr.Score, 1e-9)
	assert.Equal(t, map[string]float64{"clarity": 0.9, "accuracy": 0.7}, jr.Metrics,
		"score, passed, reasoning and nested objects are not dimensions")

	plain, err := parseJudgeResponse(`{"score": 1, "passed": true, "reasoning": "ok"}`)
	require.NoError(t, err)
	assert.Nil(t, plain.Metrics, "a judge with no extra numbers reports no dimensions")
}

// One judge call yields every dimension: llm_judge with the real spec-driven
// judge provider, parsing a rubric reply from a provider.
func TestLLMJudge_RubricDimensionsFromOneJudgeCall(t *testing.T) {
	judge := NewSpecJudgeProvider(&providers.ProviderSpec{
		Type:  "mock",
		Model: "judge",
		AdditionalConfig: map[string]any{
			"repository": mock.NewInMemoryMockRepository(rubricJudgeReply),
		},
	})
	res, err := (&LLMJudgeHandler{}).Eval(context.Background(), &evals.EvalContext{
		CurrentOutput: "answer",
		Metadata:      map[string]any{"judge_provider": judge},
	}, map[string]any{"criteria": "Score clarity and accuracy"})
	require.NoError(t, err)
	require.Empty(t, res.Error)
	assert.InDelta(t, 0.8, *res.Score, 1e-9)
	assert.Equal(t, map[string]float64{"clarity": 0.9, "accuracy": 0.7}, res.Metrics)
}
