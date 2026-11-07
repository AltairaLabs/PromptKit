package metrics

import (
	"reflect"
	"testing"

	"github.com/prometheus/client_golang/prometheus"

	"github.com/AltairaLabs/PromptKit/runtime/v2/evals"
)

// histogramBounds records one observation for metric and returns the upper
// bounds of the histogram family it registered (excluding +Inf).
func histogramBounds(t *testing.T, reg *prometheus.Registry, c *Collector, metric *evals.MetricDef) []float64 {
	t.Helper()
	score := 0.5
	if err := c.Bind(nil).Record(evals.EvalResult{Score: &score}, metric); err != nil {
		t.Fatalf("Record: %v", err)
	}
	families, err := reg.Gather()
	if err != nil {
		t.Fatalf("Gather: %v", err)
	}
	want := "test_eval_" + metric.Name
	for _, mf := range families {
		if mf.GetName() != want {
			continue
		}
		var bounds []float64
		for _, b := range mf.GetMetric()[0].GetHistogram().GetBucket() {
			bounds = append(bounds, b.GetUpperBound())
		}
		return bounds
	}
	t.Fatalf("histogram %q not registered", want)
	return nil
}

func TestEvalHistogram_DefaultBucketsCoverScoreRange(t *testing.T) {
	c, reg := newTestCollector()
	got := histogramBounds(t, reg, c, &evals.MetricDef{Name: "score_hist", Type: evals.MetricHistogram})
	want := []float64{0, .1, .2, .3, .4, .5, .6, .7, .8, .9, 1}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("default eval histogram buckets = %v, want %v", got, want)
	}
}

func TestEvalHistogram_CollectorOptsBuckets(t *testing.T) {
	c, reg := newTestCollector(func(o *CollectorOpts) {
		// Unsorted with a duplicate: the collector sorts and dedupes rather
		// than letting prometheus panic.
		o.EvalHistogramBuckets = []float64{10, 1, 5, 5}
	})
	got := histogramBounds(t, reg, c, &evals.MetricDef{Name: "opts_hist", Type: evals.MetricHistogram})
	want := []float64{1, 5, 10}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("eval histogram buckets = %v, want %v", got, want)
	}
}

func TestEvalHistogram_MetricRangeSetsBuckets(t *testing.T) {
	c, reg := newTestCollector(func(o *CollectorOpts) {
		o.EvalHistogramBuckets = []float64{1, 5, 10}
	})
	lo, hi := 0.0, 100.0
	metric := &evals.MetricDef{
		Name:  "range_hist",
		Type:  evals.MetricHistogram,
		Range: &evals.Range{Min: &lo, Max: &hi},
	}
	got := histogramBounds(t, reg, c, metric)
	want := []float64{0, 10, 20, 30, 40, 50, 60, 70, 80, 90, 100}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("range-derived eval histogram buckets = %v, want %v", got, want)
	}
}

func TestEvalHistogram_HalfOpenRangeFallsBack(t *testing.T) {
	c, reg := newTestCollector()
	hi := 100.0
	metric := &evals.MetricDef{Name: "half_hist", Type: evals.MetricHistogram, Range: &evals.Range{Max: &hi}}
	got := histogramBounds(t, reg, c, metric)
	if len(got) != 11 || got[len(got)-1] != 1 {
		t.Fatalf("half-open range should use the default 0..1 buckets, got %v", got)
	}
}
