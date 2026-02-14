package engine

import (
	"testing"

	"github.com/AltairaLabs/PromptKit/runtime/evals"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func floatPtr(f float64) *float64 { return &f }

func TestPackEvalAdapter_Convert_EmptyResults(t *testing.T) {
	adapter := &PackEvalAdapter{}

	assert.Nil(t, adapter.Convert(nil))
	assert.Nil(t, adapter.Convert([]evals.EvalResult{}))
}

func TestPackEvalAdapter_Convert_SinglePassedResult(t *testing.T) {
	adapter := &PackEvalAdapter{}

	results := []evals.EvalResult{
		{
			EvalID:      "eval-1",
			Type:        "llm_judge",
			Passed:      true,
			Explanation: "Looks good",
			DurationMs:  42,
		},
	}

	converted := adapter.Convert(results)
	require.Len(t, converted, 1)

	r := converted[0]
	assert.Equal(t, "pack_eval:llm_judge", r.Type)
	assert.True(t, r.Passed)
	assert.Equal(t, "Looks good", r.Message)
}

func TestPackEvalAdapter_Convert_FailedResultWithError(t *testing.T) {
	adapter := &PackEvalAdapter{}

	results := []evals.EvalResult{
		{
			EvalID:      "eval-2",
			Type:        "regex",
			Passed:      false,
			Explanation: "should not be used",
			Error:       "regex compilation failed",
			DurationMs:  10,
		},
	}

	converted := adapter.Convert(results)
	require.Len(t, converted, 1)

	r := converted[0]
	assert.False(t, r.Passed)
	assert.Equal(t, "regex compilation failed", r.Message)
	assert.Equal(t, "regex compilation failed", r.Details["error"])
}

func TestPackEvalAdapter_Convert_MultipleResults(t *testing.T) {
	adapter := &PackEvalAdapter{}

	results := []evals.EvalResult{
		{EvalID: "e1", Type: "a", Passed: true, DurationMs: 1},
		{EvalID: "e2", Type: "b", Passed: false, DurationMs: 2},
		{EvalID: "e3", Type: "c", Passed: true, DurationMs: 3},
	}

	converted := adapter.Convert(results)
	require.Len(t, converted, 3)
}

func TestPackEvalAdapter_convertOne_ScoreAndMetricValue(t *testing.T) {
	adapter := &PackEvalAdapter{}

	r := &evals.EvalResult{
		EvalID:      "eval-score",
		Type:        "llm_judge",
		Passed:      true,
		Score:       floatPtr(0.95),
		MetricValue: floatPtr(42.5),
		Explanation: "high quality",
		DurationMs:  100,
	}

	result := adapter.convertOne(r)

	assert.Equal(t, 0.95, result.Details["score"])
	assert.Equal(t, 42.5, result.Details["metric_value"])
	assert.Equal(t, "eval-score", result.Details["eval_id"])
}

func TestPackEvalAdapter_convertOne_DurationMs(t *testing.T) {
	adapter := &PackEvalAdapter{}

	r := &evals.EvalResult{
		EvalID:     "eval-dur",
		Type:       "latency",
		Passed:     true,
		DurationMs: 250,
	}

	result := adapter.convertOne(r)

	assert.Equal(t, int64(250), result.Details["duration_ms"])
}
