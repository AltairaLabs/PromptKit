package handlers

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/AltairaLabs/PromptKit/runtime/v2/evals"
)

// thresholdCase is one handler-declared threshold against a raw measurement
// of 0.8 (0.7 for the REST endpoint): met, and missed.
type thresholdCase struct {
	threshold float64
	wantScore float64
	wantPass  bool
}

// assertThroughWrapper runs an eval inside a `type: assertion` that sets no
// threshold of its own, so the default (a perfect score) decides.
func assertThroughWrapper(
	t *testing.T, evalCtx *evals.EvalContext, evalType string, evalParams map[string]any,
) *evals.EvalResult {
	t.Helper()
	wrapper, err := evals.NewEvalTypeRegistry().Get(evals.WrapperTypeAssertion)
	require.NoError(t, err)
	res, err := wrapper.Eval(context.Background(), evalCtx, map[string]any{
		"eval_type":   evalType,
		"eval_params": evalParams,
	})
	require.NoError(t, err)
	require.NotNil(t, res.Passed)
	return res
}

// A handler-declared threshold scores the way min_length and latency_budget
// do: 1.0 when the measurement meets it, the shortfall ratio below it. An
// assertion's default perfect-score gate therefore passes exactly when the
// threshold is met. The raw measurement is kept in Details.
func TestRestEval_MinScoreAppliesAsTheThreshold(t *testing.T) {
	server := newScoreServer(0.7)
	defer server.Close()

	for _, tc := range []thresholdCase{
		{threshold: 0.6, wantScore: 1.0, wantPass: true},
		{threshold: 0.8, wantScore: 0.875, wantPass: false},
	} {
		params := map[string]any{"url": server.URL, "min_score": tc.threshold}
		res, err := (&RestEvalHandler{}).Eval(context.Background(), &evals.EvalContext{CurrentOutput: "x"}, params)
		require.NoError(t, err)
		require.NotNil(t, res.Score)
		assert.InDelta(t, tc.wantScore, *res.Score, 1e-9, "min_score %v", tc.threshold)
		assert.InDelta(t, 0.7, res.Details[keyRawScore], 1e-9)
		assert.Nil(t, res.Passed, "an eval states no verdict")

		wrapped := assertThroughWrapper(t, &evals.EvalContext{CurrentOutput: "x"}, "rest_eval", params)
		assert.Equal(t, tc.wantPass, *wrapped.Passed, "min_score %v", tc.threshold)
	}
}

func TestA2AEval_MinScoreAppliesAsTheThreshold(t *testing.T) {
	server := mockA2AServer(t, `{"score": 0.8, "reasoning": "fine"}`)
	defer server.Close()

	for _, tc := range []thresholdCase{
		{threshold: 0.75, wantScore: 1.0, wantPass: true},
		{threshold: 1.0, wantScore: 0.8, wantPass: false},
	} {
		params := map[string]any{"agent_url": server.URL, "min_score": tc.threshold}
		res, err := (&A2AEvalHandler{}).Eval(context.Background(), &evals.EvalContext{CurrentOutput: "x"}, params)
		require.NoError(t, err)
		require.NotNil(t, res.Score)
		assert.InDelta(t, tc.wantScore, *res.Score, 1e-9, "min_score %v", tc.threshold)
		assert.InDelta(t, 0.8, res.Details[keyRawScore], 1e-9)

		wrapped := assertThroughWrapper(t, &evals.EvalContext{CurrentOutput: "x"}, "a2a_eval", params)
		assert.Equal(t, tc.wantPass, *wrapped.Passed, "min_score %v", tc.threshold)
	}
}

// Without min_score the external score is reported raw, as before.
func TestRestEval_NoMinScoreReportsTheRawScore(t *testing.T) {
	server := newScoreServer(0.7)
	defer server.Close()
	res, err := (&RestEvalHandler{}).Eval(context.Background(), &evals.EvalContext{CurrentOutput: "x"},
		map[string]any{"url": server.URL})
	require.NoError(t, err)
	assert.InDelta(t, 0.7, *res.Score, 1e-9)
	assert.NotContains(t, res.Details, keyRawScore)
}

func TestCosineSimilarity_MinSimilarityAppliesAsTheThreshold(t *testing.T) {
	// cos([1,0],[0.8,0.6]) = 0.8
	evalCtx := &evals.EvalContext{Metadata: map[string]any{"embedding": []float64{0.8, 0.6}}}
	for _, tc := range []thresholdCase{
		{threshold: 0.7, wantScore: 1.0, wantPass: true},
		{threshold: 0.9, wantScore: 0.8 / 0.9, wantPass: false},
	} {
		params := map[string]any{"reference": []float64{1, 0}, "min_similarity": tc.threshold}
		res, err := (&CosineSimilarityHandler{}).Eval(context.Background(), evalCtx, params)
		require.NoError(t, err)
		require.NotNil(t, res.Score)
		assert.InDelta(t, tc.wantScore, *res.Score, 1e-9, "min_similarity %v", tc.threshold)
		assert.InDelta(t, 0.8, res.Details[keySimilarity], 1e-9)

		wrapped := assertThroughWrapper(t, evalCtx, "cosine_similarity", params)
		assert.Equal(t, tc.wantPass, *wrapped.Passed, "min_similarity %v", tc.threshold)
	}
}

func TestThresholdScore(t *testing.T) {
	for _, tc := range []struct{ value, threshold, want float64 }{
		{0.9, 0.8, 1.0},
		{0.8, 0.8, 1.0},
		{0.4, 0.8, 0.5},
		{-0.2, 0.5, 0},
		{-0.2, -0.1, 0},
		{0.1, 0, 1.0},
	} {
		assert.InDelta(t, tc.want, thresholdScore(tc.value, tc.threshold), 1e-9, "%v vs %v", tc.value, tc.threshold)
	}
}
