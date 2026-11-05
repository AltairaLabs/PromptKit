package handlers

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/AltairaLabs/PromptKit/runtime/v2/evals"
)

// rest_eval and a2a_eval are pure eval primitives: a threshold on the eval is
// rejected, and the rejection names the assertion wrapper. Each server answers
// with a valid score, so a missing rejection shows up as a clean result.
func TestExternalEvals_RejectThresholdParams(t *testing.T) {
	rest := newScoreServer(0.7)
	defer rest.Close()
	agent := mockA2AServer(t, `{"score": 0.8, "reasoning": "fine"}`)
	defer agent.Close()

	evalCtx := &evals.EvalContext{CurrentOutput: "x"}
	restParams := map[string]any{"url": rest.URL}
	agentParams := map[string]any{"agent_url": agent.URL}

	assertHandlerRejectsThresholdParams(t, &RestEvalHandler{}, evalCtx, restParams)
	assertHandlerRejectsThresholdParams(t, &RestEvalSessionHandler{}, evalCtx, restParams)
	assertHandlerRejectsThresholdParams(t, &A2AEvalHandler{}, evalCtx, agentParams)
	assertHandlerRejectsThresholdParams(t, &A2AEvalSessionHandler{}, evalCtx, agentParams)
}

// A rejected declaration does not call the endpoint.
func TestRestEval_ThresholdRejectionSkipsTheCall(t *testing.T) {
	called := false
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		called = true
		_, _ = w.Write([]byte(`{"score": 1}`))
	}))
	defer server.Close()

	res, err := (&RestEvalHandler{}).Eval(context.Background(), &evals.EvalContext{CurrentOutput: "x"},
		map[string]any{"url": server.URL, "min_score": 0.5})
	require.NoError(t, err)
	assert.NotEmpty(t, res.Error)
	assert.False(t, called, "a misconfigured check must not call the endpoint")
}

// The threshold belongs on the wrapper, which compares the endpoint's raw
// score: 0.7 passes at min_score 0.6 and fails at 0.8.
func TestRestEval_AssertionWrapperAppliesMinScore(t *testing.T) {
	server := newScoreServer(0.7)
	defer server.Close()
	wrapper, err := evals.NewEvalTypeRegistry().Get(evals.WrapperTypeAssertion)
	require.NoError(t, err)

	for _, tc := range []struct {
		min  float64
		pass bool
	}{{0.6, true}, {0.8, false}} {
		res, evalErr := wrapper.Eval(context.Background(), &evals.EvalContext{CurrentOutput: "x"}, map[string]any{
			"eval_type":   "rest_eval",
			"min_score":   tc.min,
			"eval_params": map[string]any{"url": server.URL},
		})
		require.NoError(t, evalErr)
		require.NotNil(t, res.Passed)
		assert.Equal(t, tc.pass, *res.Passed, "min_score %v", tc.min)
		assert.InDelta(t, 0.7, *res.Score, 1e-9, "the score stays raw")
	}
}

func cosineEvalCtx() *evals.EvalContext {
	// cos([1,0],[0.8,0.6]) = 0.8
	return &evals.EvalContext{Metadata: map[string]any{"embedding": []float64{0.8, 0.6}}}
}

// cosine_similarity reports the raw similarity and needs no threshold.
func TestCosineSimilarity_ReportsTheRawSimilarity(t *testing.T) {
	res, err := (&CosineSimilarityHandler{}).Eval(context.Background(), cosineEvalCtx(),
		map[string]any{"reference": []float64{1, 0}})
	require.NoError(t, err)
	require.Empty(t, res.Error)
	require.NotNil(t, res.Score)
	assert.InDelta(t, 0.8, *res.Score, 1e-9)
}

// min_similarity, min_score and max_score are all rejected, pointing at the
// assertion wrapper.
func TestCosineSimilarity_RejectsThresholdParams(t *testing.T) {
	base := map[string]any{"reference": []float64{1, 0}}
	assertHandlerRejectsThresholdParams(t, &CosineSimilarityHandler{}, cosineEvalCtx(), base)

	res, err := (&CosineSimilarityHandler{}).Eval(context.Background(), cosineEvalCtx(),
		map[string]any{"reference": []float64{1, 0}, "min_similarity": 0.5})
	require.NoError(t, err)
	assert.Contains(t, res.Error, "min_similarity is not a valid param")
	assert.True(t, strings.Contains(res.Error, "type: assertion"), res.Error)
}
