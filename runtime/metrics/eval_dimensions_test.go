package metrics

import (
	"fmt"
	"strings"
	"sync"
	"testing"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/AltairaLabs/PromptKit/runtime/v2/evals"
)

func rubricResult(metrics map[string]float64) evals.EvalResult {
	return evals.EvalResult{EvalID: "rubric", Score: float64Ptr(0.8), Metrics: metrics}
}

var rubricMetric = &evals.MetricDef{Name: "rubric_score", Type: evals.MetricGauge}

func withDimensionLimit(n int) func(*CollectorOpts) {
	return func(o *CollectorOpts) { o.EvalDimensionLimit = n }
}

func TestEvalDimensions_OffByDefault(t *testing.T) {
	c, reg := newTestCollector()
	require.NoError(t, c.Bind(nil).Record(rubricResult(map[string]float64{"clarity": 0.9}), rubricMetric))

	out := gatherMetrics(t, reg)
	assert.Contains(t, out, "test_eval_rubric_score 0.8")
	assert.NotContains(t, out, "_dimension", "no dimension family without opt-in")
	assert.NotContains(t, out, "eval_dimensions_dropped_total")
}

func TestEvalDimensions_ExportedToDerivedFamily(t *testing.T) {
	c, reg := newTestCollector(withDimensionLimit(4))
	mc := c.Bind(nil)
	require.NoError(t, mc.Record(rubricResult(map[string]float64{"clarity": 0.9, "accuracy": 0.7}), rubricMetric))

	out := gatherMetrics(t, reg)
	assert.Contains(t, out, "test_eval_rubric_score 0.8", "the eval's own series is unchanged")
	assert.Contains(t, out, `test_eval_rubric_score_dimension{dimension="clarity"} 0.9`)
	assert.Contains(t, out, `test_eval_rubric_score_dimension{dimension="accuracy"} 0.7`)

	// A later result updates the same series rather than adding new ones.
	require.NoError(t, mc.Record(rubricResult(map[string]float64{"clarity": 0.5}), rubricMetric))
	assert.Contains(t, gatherMetrics(t, reg), `test_eval_rubric_score_dimension{dimension="clarity"} 0.5`)
}

func TestEvalDimensions_CapBoundsCardinality(t *testing.T) {
	c, reg := newTestCollector(withDimensionLimit(2))
	mc := c.Bind(nil)
	require.NoError(t, mc.Record(rubricResult(map[string]float64{"a": 1, "b": 2, "c": 3}), rubricMetric))
	// A judge inventing new keys run after run cannot grow the family.
	require.NoError(t, mc.Record(rubricResult(map[string]float64{"d": 4, "e": 5, "a": 10}), rubricMetric))

	family := c.dimensionFamilies["test_eval_rubric_score_dimension|"]
	require.NotNil(t, family)
	assert.Equal(t, 2, testutil.CollectAndCount(family.gauge), "sorted order admits a and b, then nothing new")
	out := gatherMetrics(t, reg)
	assert.Contains(t, out, `test_eval_rubric_score_dimension{dimension="a"} 10`, "an admitted dimension keeps updating")
	assert.NotContains(t, out, `dimension="c"`)
	assert.Contains(t, out, `test_eval_dimensions_dropped_total{metric="rubric_score",reason="over_limit"} 3`)
}

func TestEvalDimensions_InvalidNamesDropped(t *testing.T) {
	c, reg := newTestCollector(withDimensionLimit(8))
	require.NoError(t, c.Bind(nil).Record(rubricResult(map[string]float64{
		"Clarity": 1, "clarity score": 1, "9lives": 1, strings.Repeat("x", 65): 1, "ok_name": 1,
	}), rubricMetric))

	out := gatherMetrics(t, reg)
	assert.Contains(t, out, `dimension="ok_name"`)
	assert.Equal(t, 1, strings.Count(out, "test_eval_rubric_score_dimension{"), "only the snake_case name is exported")
	assert.Contains(t, out, `test_eval_dimensions_dropped_total{metric="rubric_score",reason="invalid_name"} 4`)
}

func TestEvalDimensions_ErroredResultNotExported(t *testing.T) {
	c, reg := newTestCollector(withDimensionLimit(4))
	res := rubricResult(map[string]float64{"clarity": 0})
	res.Error = "app did not start"
	require.NoError(t, c.Bind(nil).Record(res, rubricMetric))
	assert.NotContains(t, gatherMetrics(t, reg), "_dimension{")
}

func TestEvalDimensions_ExportedWithoutHeadlineValue(t *testing.T) {
	c, reg := newTestCollector(withDimensionLimit(4))
	res := evals.EvalResult{EvalID: "rubric", Metrics: map[string]float64{"clarity": 0.6}}
	require.NoError(t, c.Bind(nil).Record(res, rubricMetric))
	assert.Contains(t, gatherMetrics(t, reg), `test_eval_rubric_score_dimension{dimension="clarity"} 0.6`)
}

func TestEvalDimensions_CarryInstanceAndPackLabels(t *testing.T) {
	c, reg := newTestCollector(withDimensionLimit(4), func(o *CollectorOpts) {
		o.InstanceLabels = []string{"tenant"}
	})
	metric := &evals.MetricDef{Name: "rubric_score", Type: evals.MetricGauge}
	evals.SetMetricLabels(metric, map[string]string{"team": "support"})
	require.NoError(t, c.Bind(map[string]string{"tenant": "acme"}).Record(
		rubricResult(map[string]float64{"clarity": 0.9, "Bad": 1}), metric))

	out := gatherMetrics(t, reg)
	assert.Contains(t, out, `test_eval_rubric_score_dimension{dimension="clarity",team="support",tenant="acme"} 0.9`)
	assert.Contains(t, out, `test_eval_dimensions_dropped_total{metric="rubric_score",reason="invalid_name",tenant="acme"} 1`)
}

// A pack label already named "dimension" cannot share the derived family's
// label; the eval's own metric must still record.
func TestEvalDimensions_LabelCollisionSkipsFamilyOnly(t *testing.T) {
	c, reg := newTestCollector(withDimensionLimit(4))
	metric := &evals.MetricDef{Name: "rubric_score", Type: evals.MetricGauge}
	evals.SetMetricLabels(metric, map[string]string{"dimension": "x"})
	mc := c.Bind(nil)
	require.NoError(t, mc.Record(rubricResult(map[string]float64{"clarity": 0.9}), metric))
	require.NoError(t, mc.Record(rubricResult(map[string]float64{"clarity": 0.9}), metric))

	out := gatherMetrics(t, reg)
	assert.Contains(t, out, `test_eval_rubric_score{dimension="x"} 0.8`)
	assert.NotContains(t, out, "_dimension{")
	family, known := c.dimensionFamilies["test_eval_rubric_score_dimension|dimension"]
	assert.True(t, known)
	assert.Nil(t, family, "a family that failed to register is remembered as nil")
}

func TestEvalDimensions_DisabledEvalMetricsIgnoresLimit(t *testing.T) {
	c, reg := newTestCollector(withDimensionLimit(4), func(o *CollectorOpts) { o.DisableEvalMetrics = true })
	require.NoError(t, c.Bind(nil).Record(rubricResult(map[string]float64{"clarity": 1}), rubricMetric))
	assert.NotContains(t, gatherMetrics(t, reg), "eval")
}

// The collector is shared by every conversation in a process; concurrent
// results inventing distinct names must still admit exactly the cap.
func TestEvalDimensions_CapHoldsUnderConcurrency(t *testing.T) {
	const limit, writers = 5, 40
	c, _ := newTestCollector(withDimensionLimit(limit))
	var wg sync.WaitGroup
	for i := range writers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_ = c.Bind(nil).Record(rubricResult(map[string]float64{fmt.Sprintf("dim_%02d", i): 1}), rubricMetric)
		}()
	}
	wg.Wait()

	family := c.dimensionFamilies["test_eval_rubric_score_dimension|"]
	require.NotNil(t, family)
	assert.Equal(t, limit, testutil.CollectAndCount(family.gauge))
	assert.InDelta(t, writers-limit,
		testutil.ToFloat64(c.dimensionsDropped.WithLabelValues("rubric_score", dropReasonOverLimit)), 0)
}

// Two evals declaring the same metric name with different label sets used to
// share one vector, and the second write panicked on the label count. Now the
// second is refused by the registry with an error, for the headline metric and
// its dimensions alike.
func TestEvalMetrics_SameNameDifferentLabelsDoesNotPanic(t *testing.T) {
	c, reg := newTestCollector(withDimensionLimit(4))
	labeled := &evals.MetricDef{Name: "rubric_score", Type: evals.MetricGauge}
	evals.SetMetricLabels(labeled, map[string]string{"team": "support"})
	mc := c.Bind(nil)
	require.NoError(t, mc.Record(rubricResult(map[string]float64{"clarity": 0.9}), labeled))

	assert.NotPanics(t, func() {
		err := mc.Record(rubricResult(map[string]float64{"clarity": 0.1}), rubricMetric)
		assert.Error(t, err, "the registry refuses a second label set under one name")
	})
	assert.NotPanics(t, func() {
		_ = mc.Record(evals.EvalResult{Metrics: map[string]float64{"clarity": 0.1}}, rubricMetric)
	})
	assert.Contains(t, gatherMetrics(t, reg),
		`test_eval_rubric_score_dimension{dimension="clarity",team="support"} 0.9`, "the first family is intact")
}

// Eval-only collectors are built per job against a shared registry; enabling
// dimensions must not make the second construction panic.
func TestEvalDimensions_SecondCollectorOnSameRegistry(t *testing.T) {
	reg := prometheus.NewRegistry()
	opts := CollectorOpts{Registerer: reg, Namespace: "test", EvalDimensionLimit: 2}
	first := NewEvalOnlyCollector(opts)
	var second *Collector
	require.NotPanics(t, func() { second = NewEvalOnlyCollector(opts) })

	bad := rubricResult(map[string]float64{"Bad": 1})
	require.NoError(t, first.Bind(nil).Record(bad, rubricMetric))
	_ = second.Bind(nil).Record(bad, &evals.MetricDef{Name: "other_score", Type: evals.MetricGauge})
	out := gatherMetrics(t, reg)
	assert.Contains(t, out, `test_eval_dimensions_dropped_total{metric="rubric_score",reason="invalid_name"} 1`)
	assert.Contains(t, out, `test_eval_dimensions_dropped_total{metric="other_score",reason="invalid_name"} 1`,
		"both collectors count into the one registered counter")
}
