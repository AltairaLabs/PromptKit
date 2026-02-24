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

func TestConvertEvalResults_SkippedResult(t *testing.T) {
	results := []evals.EvalResult{
		{
			EvalID:      "eval-skip",
			Type:        "contains",
			Passed:      true,
			Skipped:     true,
			SkipReason:  "when condition not met",
			Explanation: "skipped",
			DurationMs:  0,
		},
	}

	converted := assertions.ConvertEvalResults(results)
	require.Len(t, converted, 1)

	c := converted[0]
	assert.Equal(t, true, c.Details["skipped"])
	assert.Equal(t, "when condition not met", c.Details["skip_reason"])
}

func TestExtractScore(t *testing.T) {
	tests := []struct {
		name    string
		details map[string]interface{}
		want    float64
		wantOK  bool
	}{
		{"nil map", nil, 0, false},
		{"missing key", map[string]interface{}{}, 0, false},
		{"float64", map[string]interface{}{"score": 0.85}, 0.85, true},
		{"float32", map[string]interface{}{"score": float32(0.5)}, 0.5, true},
		{"int", map[string]interface{}{"score": 1}, 1, true},
		{"int64", map[string]interface{}{"score": int64(2)}, 2, true},
		{"wrong type string", map[string]interface{}{"score": "high"}, 0, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := assertions.ExtractScore(tt.details)
			assert.Equal(t, tt.wantOK, ok)
			if ok {
				assert.InDelta(t, tt.want, got, 0.001)
			}
		})
	}
}

func TestExtractDurationMs(t *testing.T) {
	tests := []struct {
		name    string
		details map[string]interface{}
		want    int64
		wantOK  bool
	}{
		{"nil map", nil, 0, false},
		{"missing key", map[string]interface{}{}, 0, false},
		{"int64", map[string]interface{}{"duration_ms": int64(42)}, 42, true},
		{"float64", map[string]interface{}{"duration_ms": float64(100)}, 100, true},
		{"int", map[string]interface{}{"duration_ms": 5}, 5, true},
		{"wrong type", map[string]interface{}{"duration_ms": "slow"}, 0, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := assertions.ExtractDurationMs(tt.details)
			assert.Equal(t, tt.wantOK, ok)
			if ok {
				assert.Equal(t, tt.want, got)
			}
		})
	}
}

func TestExtractEvalID(t *testing.T) {
	tests := []struct {
		name    string
		details map[string]interface{}
		want    string
		wantOK  bool
	}{
		{"nil map", nil, "", false},
		{"missing key", map[string]interface{}{}, "", false},
		{"string", map[string]interface{}{"eval_id": "eval-1"}, "eval-1", true},
		{"wrong type", map[string]interface{}{"eval_id": 42}, "", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := assertions.ExtractEvalID(tt.details)
			assert.Equal(t, tt.wantOK, ok)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestIsPackEval(t *testing.T) {
	assert.True(t, assertions.IsPackEval(assertions.ConversationValidationResult{
		Type: "pack_eval:contains",
	}))
	assert.False(t, assertions.IsPackEval(assertions.ConversationValidationResult{
		Type: "tools_called",
	}))
	assert.False(t, assertions.IsPackEval(assertions.ConversationValidationResult{
		Type: "",
	}))
}

func TestIsSkipped(t *testing.T) {
	tests := []struct {
		name       string
		details    map[string]interface{}
		wantReason string
		wantOK     bool
	}{
		{"nil map", nil, "", false},
		{"no skipped key", map[string]interface{}{}, "", false},
		{"skipped false", map[string]interface{}{"skipped": false}, "", false},
		{"skipped wrong type", map[string]interface{}{"skipped": "yes"}, "", false},
		{"skipped true no reason", map[string]interface{}{"skipped": true}, "", true},
		{"skipped true with reason", map[string]interface{}{
			"skipped": true, "skip_reason": "condition not met",
		}, "condition not met", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			reason, ok := assertions.IsSkipped(tt.details)
			assert.Equal(t, tt.wantOK, ok)
			assert.Equal(t, tt.wantReason, reason)
		})
	}
}
