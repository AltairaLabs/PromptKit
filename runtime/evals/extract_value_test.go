package evals

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestExtractValue_AbsenceIsNotZero is the core correction.
//
// ExtractValue returned a bare float64 ending in `return 0`, so an eval that
// deliberately produced no scalar — a judge returning a rubric, an eval calling
// a reasoning service and getting back a JSON object — was recorded as a gauge
// reading of ZERO. Indistinguishable on a dashboard from something that really
// measured zero, and a flatline where the caller expected no series at all.
func TestExtractValue_AbsenceIsNotZero(t *testing.T) {
	_, ok := ExtractValue(EvalResult{Value: map[string]any{"clarity": 0.9}}, nil)
	assert.False(t, ok,
		"an eval with no scalar must produce NO sample, not a sample of 0")
}

// TestExtractValue_RealZeroIsStillRecorded is the other half: a genuine zero
// must still be written, or the fix would silently drop real measurements.
func TestExtractValue_RealZeroIsStillRecorded(t *testing.T) {
	v, ok := ExtractValue(EvalResult{Score: float64Ptr(0)}, nil)
	require.True(t, ok, "a measured zero is a measurement")
	assert.Equal(t, 0.0, v)
}

// TestExtractValue_Precedence pins MetricValue over Score. MetricValue is the
// gauge number; Score is the normalized 0..1 that thresholds coerce against.
func TestExtractValue_Precedence(t *testing.T) {
	v, ok := ExtractValue(EvalResult{Score: float64Ptr(1), MetricValue: float64Ptr(842)}, nil)
	require.True(t, ok)
	assert.Equal(t, 842.0, v, "MetricValue is the gauge value")

	v, ok = ExtractValue(EvalResult{Score: float64Ptr(0.75)}, nil)
	require.True(t, ok)
	assert.Equal(t, 0.75, v, "Score is the fallback when no gauge value was set")
}

// TestExtractValue_ExtractsFromValue uses the metric definition that this
// function has accepted and ignored since it was written.
//
// The expression is authored in the pack under `metric:`, which the PromptPack
// schema permits via additionalProperties, so the series names stay bounded and
// validated at config time rather than coming from a model's JSON keys.
func TestExtractValue_ExtractsFromValue(t *testing.T) {
	rubric := EvalResult{
		Value: map[string]any{
			"score": map[string]any{"clarity": 0.9, "accuracy": 0.7},
		},
	}

	clarity := &MetricDef{
		Name:  "rubric_clarity",
		Type:  MetricGauge,
		Extra: map[string]any{"jmespath_expression": "score.clarity"},
	}
	v, ok := ExtractValue(rubric, clarity)
	require.True(t, ok, "a declared expression must pull the dimension out of Value")
	assert.InDelta(t, 0.9, v, 1e-9)

	accuracy := &MetricDef{
		Name:  "rubric_accuracy",
		Type:  MetricGauge,
		Extra: map[string]any{"jmespath_expression": "score.accuracy"},
	}
	v, ok = ExtractValue(rubric, accuracy)
	require.True(t, ok)
	assert.InDelta(t, 0.7, v, 1e-9)
}

// TestExtractValue_ExpressionWins pins precedence: an explicitly declared
// expression beats the scalar fields, or a pack could not override a handler
// that happens to set MetricValue too.
func TestExtractValue_ExpressionWins(t *testing.T) {
	r := EvalResult{
		MetricValue: float64Ptr(999),
		Value:       map[string]any{"latency_ms": 42.0},
	}
	m := &MetricDef{Extra: map[string]any{"jmespath_expression": "latency_ms"}}

	v, ok := ExtractValue(r, m)
	require.True(t, ok)
	assert.Equal(t, 42.0, v, "the declared expression is the author's explicit choice")
}

// TestExtractValue_ExpressionMissesIsAbsent — a path that does not resolve, or
// resolves to something non-numeric, produces NO sample rather than a zero.
// Same rule as the top: absence is not a measurement.
func TestExtractValue_ExpressionMissesIsAbsent(t *testing.T) {
	r := EvalResult{Value: map[string]any{"score": map[string]any{"clarity": 0.9}}}

	for _, expr := range []string{"score.nonexistent", "score", "not a valid ["} {
		t.Run(expr, func(t *testing.T) {
			_, ok := ExtractValue(r, &MetricDef{Extra: map[string]any{"jmespath_expression": expr}})
			assert.False(t, ok, "an unresolvable or non-numeric path yields no sample")
		})
	}
}

// TestExtractValue_IntegersFromJSON — a decoded JSON number may arrive as int
// or json.Number depending on the decoder, so a dimension of 1 must not be
// dropped as non-numeric.
func TestExtractValue_IntegersFromJSON(t *testing.T) {
	for name, val := range map[string]any{"int": 3, "int64": int64(3), "float32": float32(3)} {
		t.Run(name, func(t *testing.T) {
			r := EvalResult{Value: map[string]any{"n": val}}
			v, ok := ExtractValue(r, &MetricDef{Extra: map[string]any{"jmespath_expression": "n"}})
			require.True(t, ok, "%T must be usable as a gauge value", val)
			assert.Equal(t, 3.0, v)
		})
	}
}
