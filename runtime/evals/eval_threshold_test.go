package evals

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// fixedScoreHandler scores every eval with a fixed value.
type fixedScoreHandler struct {
	typeName string
	score    float64
}

func (h *fixedScoreHandler) Type() string { return h.typeName }

func (h *fixedScoreHandler) Eval(context.Context, *EvalContext, map[string]any) (*EvalResult, error) {
	s := h.score
	return &EvalResult{Score: &s}, nil
}

// runThresholdEval runs one eval def through an EvalRunner whose registry holds
// the wrappers and a "fixed" inner eval scoring score.
func runThresholdEval(t *testing.T, score float64, def EvalDef) EvalResult {
	t.Helper()
	reg := NewEmptyEvalTypeRegistry()
	reg.Register(&fixedScoreHandler{typeName: "fixed", score: score})
	reg.Register(&AssertionEvalHandler{registry: reg})
	reg.Register(&GuardrailEvalHandler{registry: reg})
	if def.ID == "" {
		def.ID = "e1"
	}
	if def.Trigger == "" {
		def.Trigger = TriggerEveryTurn
	}
	results := NewEvalRunner(reg).RunTurnEvals(context.Background(), []EvalDef{def}, &EvalContext{})
	require.Len(t, results, 1)
	return results[0]
}

func specThreshold(op string, v float64) *Threshold { return &Threshold{Operator: op, Value: &v} }

func assertionDef(th *Threshold, extra map[string]any) EvalDef {
	params := map[string]any{"eval_type": "fixed"}
	for k, v := range extra {
		params[k] = v
	}
	return EvalDef{Type: WrapperTypeAssertion, Params: params, Threshold: th}
}

func TestAssertion_AppliesEvalDefThreshold(t *testing.T) {
	tests := []struct {
		name  string
		score float64
		th    *Threshold
		want  bool
	}{
		// Without a threshold an assertion needs 1.0, so 0.7 passing proves
		// the threshold was read.
		{"gte met", 0.7, specThreshold("gte", 0.6), true},
		{"gte missed", 0.5, specThreshold("gte", 0.6), false},
		{">= alias", 0.6, specThreshold(">=", 0.6), true},
		{"gt is strict", 0.6, specThreshold("gt", 0.6), false},
		{"gt met", 0.61, specThreshold(">", 0.6), true},
		{"lte met", 0.2, specThreshold("lte", 0.2), true},
		{"lte missed", 0.3, specThreshold("<=", 0.2), false},
		{"lt is strict", 0.2, specThreshold("lt", 0.2), false},
		{"lt met", 0.1, specThreshold("<", 0.2), true},
		{"eq met", 0.5, specThreshold("eq", 0.5), true},
		{"eq missed", 0.6, specThreshold("==", 0.5), false},
		{"operator case-insensitive", 0.7, specThreshold(" GTE ", 0.6), true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			res := runThresholdEval(t, tt.score, assertionDef(tt.th, nil))
			require.Empty(t, res.Error)
			require.NotNil(t, res.Passed)
			assert.Equal(t, tt.want, *res.Passed)
		})
	}
}

func TestGuardrail_AppliesEvalDefThreshold(t *testing.T) {
	def := func(th *Threshold) EvalDef {
		return EvalDef{Type: WrapperTypeGuardrail, Params: map[string]any{"eval_type": "fixed"}, Threshold: th}
	}

	// Without a threshold 0.7 would trigger (min defaults to 1.0).
	res := runThresholdEval(t, 0.7, def(specThreshold("gte", 0.6)))
	require.Empty(t, res.Error)
	assert.Equal(t, false, res.Details["triggered"])
	assert.True(t, *res.Passed)

	res = runThresholdEval(t, 0.6, def(specThreshold("gt", 0.6)))
	assert.Equal(t, true, res.Details["triggered"], "gt is strict: 0.6 does not clear > 0.6")

	res = runThresholdEval(t, 0.3, def(specThreshold("lt", 0.5)))
	assert.Equal(t, false, res.Details["triggered"])
	res = runThresholdEval(t, 0.5, def(specThreshold("lt", 0.5)))
	assert.Equal(t, true, res.Details["triggered"], "lt is strict: 0.5 does not clear < 0.5")
}

func TestThreshold_ConflictsWithMinMaxScore(t *testing.T) {
	res := runThresholdEval(t, 0.9, assertionDef(specThreshold("gte", 0.5), map[string]any{"min_score": 0.8}))
	assert.Contains(t, res.Error, "threshold")
	assert.Contains(t, res.Error, "min_score")
	assert.Nil(t, res.Passed)
}

func TestThreshold_ConflictsWithNonNumericMinScore(t *testing.T) {
	res := runThresholdEval(t, 0.9, assertionDef(specThreshold("gte", 0.5), map[string]any{"min_score": "0.8"}))
	assert.Contains(t, res.Error, "min_score")
}

func TestThreshold_InvalidIsAnError(t *testing.T) {
	res := runThresholdEval(t, 0.9, assertionDef(specThreshold("between", 0.5), nil))
	assert.Contains(t, res.Error, `"between"`)
	assert.Contains(t, res.Error, "gte")

	res = runThresholdEval(t, 0.9, assertionDef(&Threshold{Operator: "gte"}, nil))
	assert.Contains(t, res.Error, "threshold.value")
}

func TestThreshold_PlainEvalStaysAMeasurement(t *testing.T) {
	res := runThresholdEval(t, 0.9, EvalDef{Type: "fixed", Threshold: specThreshold("gte", 0.5)})
	assert.Empty(t, res.Error)
	assert.Nil(t, res.Passed, "a plain eval never states pass or fail, threshold or not")
	require.NotNil(t, res.Score)
	assert.InDelta(t, 0.9, *res.Score, 1e-9)
}

func TestThreshold_NilKeepsMinMaxBehavior(t *testing.T) {
	res := runThresholdEval(t, 0.9, assertionDef(nil, map[string]any{"min_score": 0.8}))
	assert.True(t, *res.Passed)
	res = runThresholdEval(t, 0.9, assertionDef(nil, nil))
	assert.False(t, *res.Passed, "the default min_score of 1.0 still applies")
}

func TestValidateEvalTypes_ReportsBadWrapperThreshold(t *testing.T) {
	reg := NewEmptyEvalTypeRegistry()
	reg.Register(&fixedScoreHandler{typeName: "fixed"})
	reg.Register(&AssertionEvalHandler{registry: reg})

	errs := ValidateEvalTypes([]EvalDef{
		{ID: "bad-op", Type: WrapperTypeAssertion, Params: map[string]any{"eval_type": "fixed"},
			Threshold: specThreshold("between", 0.5)},
		{ID: "both", Type: WrapperTypeAssertion, Params: map[string]any{"eval_type": "fixed", "max_score": 0.9},
			Threshold: specThreshold("gte", 0.5)},
		{ID: "ok", Type: WrapperTypeAssertion, Params: map[string]any{"eval_type": "fixed"},
			Threshold: specThreshold("gte", 0.5)},
		// A plain eval's threshold is not applied here, so it is not checked.
		{ID: "plain", Type: "fixed", Threshold: specThreshold("between", 0.5)},
	}, reg)
	require.Len(t, errs, 2, "%v", errs)
	assert.Contains(t, errs[0], `"bad-op"`)
	assert.Contains(t, errs[1], `"both"`)
}

func TestScoreThresholdsFromSpec(t *testing.T) {
	got, err := ScoreThresholdsFromSpec(nil)
	require.NoError(t, err)
	assert.Equal(t, ScoreThresholds{}, got)

	got, err = ScoreThresholdsFromSpec(specThreshold("gt", 0.4))
	require.NoError(t, err)
	assert.Equal(t, ScoreThresholds{Min: scorePtr(0.4), MinExclusive: true}, got)

	got, err = ScoreThresholdsFromSpec(specThreshold("eq", 0.4))
	require.NoError(t, err)
	assert.Equal(t, ScoreThresholds{Min: scorePtr(0.4), Max: scorePtr(0.4)}, got)
}
