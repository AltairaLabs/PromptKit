package assertions_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/AltairaLabs/PromptKit/runtime/evals"
	"github.com/AltairaLabs/PromptKit/tools/arena/assertions"
)

func TestConvertEvalResults_Empty(t *testing.T) {
	result := assertions.ConvertEvalResults(nil)
	assert.Nil(t, result)

	result = assertions.ConvertEvalResults([]evals.EvalResult{})
	assert.Nil(t, result)
}

func TestConvertEvalResults_PassingResult(t *testing.T) {
	score := 0.95
	results := []evals.EvalResult{
		{
			EvalID:      "eval-1",
			Type:        "contains",
			Passed:      true,
			Score:       &score,
			Explanation: "Content contains expected text",
			DurationMs:  42,
		},
	}

	converted := assertions.ConvertEvalResults(results)
	require.Len(t, converted, 1)

	c := converted[0]
	assert.Equal(t, "pack_eval:contains", c.Type)
	assert.True(t, c.Passed)
	assert.Equal(t, "Content contains expected text", c.Message)
	assert.Equal(t, "eval-1", c.Details["eval_id"])
	assert.Equal(t, int64(42), c.Details["duration_ms"])
	assert.Equal(t, 0.95, c.Details["score"])
	_, hasError := c.Details["error"]
	assert.False(t, hasError)
}

func TestConvertEvalResults_FailingResultWithError(t *testing.T) {
	results := []evals.EvalResult{
		{
			EvalID:      "eval-2",
			Type:        "llm_judge",
			Passed:      false,
			Explanation: "Judge determined response is off-topic",
			Error:       "provider timeout",
			DurationMs:  5000,
		},
	}

	converted := assertions.ConvertEvalResults(results)
	require.Len(t, converted, 1)

	c := converted[0]
	assert.Equal(t, "pack_eval:llm_judge", c.Type)
	assert.False(t, c.Passed)
	// When failed and error is set, message should be the error
	assert.Equal(t, "provider timeout", c.Message)
	assert.Equal(t, "provider timeout", c.Details["error"])
}

func TestConvertEvalResults_MultipleResults(t *testing.T) {
	metricVal := 0.87
	results := []evals.EvalResult{
		{
			EvalID:      "eval-a",
			Type:        "contains",
			Passed:      true,
			Explanation: "ok",
			DurationMs:  10,
		},
		{
			EvalID:      "eval-b",
			Type:        "cosine_similarity",
			Passed:      true,
			MetricValue: &metricVal,
			Explanation: "similarity check passed",
			DurationMs:  20,
		},
		{
			EvalID:      "eval-c",
			Type:        "json_valid",
			Passed:      false,
			Explanation: "invalid JSON",
			DurationMs:  5,
		},
	}

	converted := assertions.ConvertEvalResults(results)
	require.Len(t, converted, 3)

	assert.Equal(t, "pack_eval:contains", converted[0].Type)
	assert.True(t, converted[0].Passed)

	assert.Equal(t, "pack_eval:cosine_similarity", converted[1].Type)
	assert.Equal(t, 0.87, converted[1].Details["metric_value"])

	assert.Equal(t, "pack_eval:json_valid", converted[2].Type)
	assert.False(t, converted[2].Passed)
}
