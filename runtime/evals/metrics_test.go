package evals

import (
	"bytes"
	"strings"
	"sync"
	"testing"
)

func TestMetricCollector_GaugeSet(t *testing.T) {
	mc := NewMetricCollector()

	result := EvalResult{EvalID: "e1", Score: float64Ptr(0.85)}
	metric := &MetricDef{Name: "quality", Type: MetricGauge}

	if err := mc.Record(result, metric); err != nil {
		t.Fatal(err)
	}

	var buf bytes.Buffer
	if err := mc.WritePrometheus(&buf); err != nil {
		t.Fatal(err)
	}

	output := buf.String()
	if !strings.Contains(output, "# TYPE promptpack_quality gauge") {
		t.Errorf("expected TYPE line, got:\n%s", output)
	}
	if !strings.Contains(output, "promptpack_quality 0.85") {
		t.Errorf("expected gauge value 0.85, got:\n%s", output)
	}
}

func TestMetricCollector_GaugeOverwrite(t *testing.T) {
	mc := NewMetricCollector()
	metric := &MetricDef{Name: "quality", Type: MetricGauge}

	_ = mc.Record(EvalResult{Score: float64Ptr(0.5)}, metric)
	_ = mc.Record(EvalResult{Score: float64Ptr(0.9)}, metric)

	var buf bytes.Buffer
	_ = mc.WritePrometheus(&buf)

	if !strings.Contains(buf.String(), "promptpack_quality 0.9") {
		t.Errorf("gauge should overwrite to 0.9, got:\n%s", buf.String())
	}
}

func TestMetricCollector_CounterAccumulate(t *testing.T) {
	mc := NewMetricCollector()
	metric := &MetricDef{Name: "eval_runs", Type: MetricCounter}

	for i := 0; i < 3; i++ {
		if err := mc.Record(EvalResult{EvalID: "e1"}, metric); err != nil {
			t.Fatal(err)
		}
	}

	var buf bytes.Buffer
	_ = mc.WritePrometheus(&buf)

	output := buf.String()
	if !strings.Contains(output, "# TYPE promptpack_eval_runs counter") {
		t.Errorf("expected counter TYPE, got:\n%s", output)
	}
	if !strings.Contains(output, "promptpack_eval_runs 3") {
		t.Errorf("expected counter 3, got:\n%s", output)
	}
}

func TestMetricCollector_HistogramBuckets(t *testing.T) {
	mc := NewMetricCollector()
	metric := &MetricDef{Name: "latency", Type: MetricHistogram}

	values := []float64{0.003, 0.05, 0.5, 2.0, 8.0}
	for _, v := range values {
		if err := mc.Record(EvalResult{MetricValue: float64Ptr(v)}, metric); err != nil {
			t.Fatal(err)
		}
	}

	var buf bytes.Buffer
	_ = mc.WritePrometheus(&buf)

	output := buf.String()
	if !strings.Contains(output, "# TYPE promptpack_latency histogram") {
		t.Errorf("expected histogram TYPE, got:\n%s", output)
	}
	// .005 bucket should contain 1 (0.003)
	if !strings.Contains(output, `promptpack_latency_bucket{le="0.005"} 1`) {
		t.Errorf("expected bucket 0.005 = 1, got:\n%s", output)
	}
	// +Inf bucket should contain all 5
	if !strings.Contains(output, `promptpack_latency_bucket{le="+Inf"} 5`) {
		t.Errorf("expected +Inf bucket = 5, got:\n%s", output)
	}
	if !strings.Contains(output, "promptpack_latency_sum 10.553") {
		t.Errorf("expected sum 10.553, got:\n%s", output)
	}
	if !strings.Contains(output, "promptpack_latency_count 5") {
		t.Errorf("expected count 5, got:\n%s", output)
	}
}

func TestMetricCollector_BooleanPassFail(t *testing.T) {
	mc := NewMetricCollector()
	metric := &MetricDef{Name: "contains_check", Type: MetricBoolean}

	_ = mc.Record(EvalResult{Passed: true}, metric)
	var buf bytes.Buffer
	_ = mc.WritePrometheus(&buf)
	if !strings.Contains(buf.String(), "promptpack_contains_check 1") {
		t.Errorf("expected 1 for passed, got:\n%s", buf.String())
	}

	_ = mc.Record(EvalResult{Passed: false}, metric)
	buf.Reset()
	_ = mc.WritePrometheus(&buf)
	if !strings.Contains(buf.String(), "promptpack_contains_check 0") {
		t.Errorf("expected 0 for failed, got:\n%s", buf.String())
	}
}

func TestMetricCollector_AutoPrefix(t *testing.T) {
	mc := NewMetricCollector()
	metric := &MetricDef{Name: "quality", Type: MetricGauge}
	_ = mc.Record(EvalResult{Score: float64Ptr(1)}, metric)

	var buf bytes.Buffer
	_ = mc.WritePrometheus(&buf)
	if !strings.Contains(buf.String(), "promptpack_quality") {
		t.Errorf("expected auto-prefixed name, got:\n%s", buf.String())
	}
}

func TestMetricCollector_AlreadyPrefixed(t *testing.T) {
	mc := NewMetricCollector()
	metric := &MetricDef{Name: "promptpack_quality", Type: MetricGauge}
	_ = mc.Record(EvalResult{Score: float64Ptr(1)}, metric)

	var buf bytes.Buffer
	_ = mc.WritePrometheus(&buf)
	// Should not double-prefix
	if strings.Contains(buf.String(), "promptpack_promptpack_quality") {
		t.Errorf("name was double-prefixed:\n%s", buf.String())
	}
}

func TestMetricCollector_CustomNamespace(t *testing.T) {
	mc := NewMetricCollector(WithNamespace("myapp"))
	metric := &MetricDef{Name: "quality", Type: MetricGauge}
	_ = mc.Record(EvalResult{Score: float64Ptr(1)}, metric)

	var buf bytes.Buffer
	_ = mc.WritePrometheus(&buf)
	if !strings.Contains(buf.String(), "myapp_quality") {
		t.Errorf("expected custom namespace, got:\n%s", buf.String())
	}
}

func TestMetricCollector_RangeValidationWarning(t *testing.T) {
	mc := NewMetricCollector()
	min, max := 0.0, 1.0
	metric := &MetricDef{
		Name: "score",
		Type: MetricGauge,
		Range: &Range{
			Min: &min,
			Max: &max,
		},
	}

	// Should not error, just warn
	err := mc.Record(EvalResult{Score: float64Ptr(1.5)}, metric)
	if err != nil {
		t.Errorf("range violation should not return error: %v", err)
	}

	err = mc.Record(EvalResult{Score: float64Ptr(-0.5)}, metric)
	if err != nil {
		t.Errorf("range violation should not return error: %v", err)
	}
}

func TestMetricCollector_ConcurrentRecord(t *testing.T) {
	mc := NewMetricCollector()
	metric := &MetricDef{Name: "concurrent", Type: MetricCounter}

	var wg sync.WaitGroup
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_ = mc.Record(EvalResult{}, metric)
		}()
	}
	wg.Wait()

	var buf bytes.Buffer
	_ = mc.WritePrometheus(&buf)
	if !strings.Contains(buf.String(), "promptpack_concurrent 100") {
		t.Errorf("expected 100 after concurrent writes, got:\n%s", buf.String())
	}
}

func TestMetricCollector_EmptyWritePrometheus(t *testing.T) {
	mc := NewMetricCollector()
	var buf bytes.Buffer
	if err := mc.WritePrometheus(&buf); err != nil {
		t.Fatal(err)
	}
	if buf.Len() != 0 {
		t.Errorf("expected empty output, got: %q", buf.String())
	}
}

func TestMetricCollector_Reset(t *testing.T) {
	mc := NewMetricCollector()
	_ = mc.Record(EvalResult{Score: float64Ptr(1)}, &MetricDef{Name: "q", Type: MetricGauge})
	mc.Reset()

	var buf bytes.Buffer
	_ = mc.WritePrometheus(&buf)
	if buf.Len() != 0 {
		t.Errorf("expected empty after reset, got: %q", buf.String())
	}
}

func TestMetricCollector_NilMetricDef(t *testing.T) {
	mc := NewMetricCollector()
	err := mc.Record(EvalResult{}, nil)
	if err == nil {
		t.Error("expected error for nil metric def")
	}
}

func TestMetricCollector_MetricValuePreferred(t *testing.T) {
	mc := NewMetricCollector()
	metric := &MetricDef{Name: "val", Type: MetricGauge}

	// Both Score and MetricValue set — MetricValue should win
	result := EvalResult{
		Score:       float64Ptr(0.5),
		MetricValue: float64Ptr(0.9),
	}
	_ = mc.Record(result, metric)

	var buf bytes.Buffer
	_ = mc.WritePrometheus(&buf)
	if !strings.Contains(buf.String(), "promptpack_val 0.9") {
		t.Errorf("expected MetricValue to take priority, got:\n%s", buf.String())
	}
}

func TestMetricCollector_IntegrationWithMetricResultWriter(t *testing.T) {
	mc := NewMetricCollector()
	defs := []EvalDef{
		{
			ID:   "e1",
			Type: "contains",
			Metric: &MetricDef{
				Name: "contains_result",
				Type: MetricBoolean,
			},
		},
	}
	writer := NewMetricResultWriter(mc, defs)

	results := []EvalResult{
		{EvalID: "e1", Passed: true},
	}
	if err := writer.WriteResults(nil, results); err != nil {
		t.Fatal(err)
	}

	var buf bytes.Buffer
	_ = mc.WritePrometheus(&buf)
	if !strings.Contains(buf.String(), "promptpack_contains_result 1") {
		t.Errorf("expected metric from writer roundtrip, got:\n%s", buf.String())
	}
}

func TestMetricCollector_GaugeWithDefLabels(t *testing.T) {
	mc := NewMetricCollector()
	metric := &MetricDef{
		Name:   "quality",
		Type:   MetricGauge,
		Labels: map[string]string{"eval_type": "contains"},
	}
	_ = mc.Record(EvalResult{Score: float64Ptr(0.85)}, metric)

	var buf bytes.Buffer
	_ = mc.WritePrometheus(&buf)
	output := buf.String()

	if !strings.Contains(output, "# TYPE promptpack_quality gauge") {
		t.Errorf("expected TYPE line, got:\n%s", output)
	}
	if !strings.Contains(output, `promptpack_quality{eval_type="contains"} 0.85`) {
		t.Errorf("expected labeled gauge, got:\n%s", output)
	}
}

func TestMetricCollector_WithBaseLabels(t *testing.T) {
	mc := NewMetricCollector(WithLabels(map[string]string{"env": "prod", "tenant": "acme"}))
	metric := &MetricDef{Name: "quality", Type: MetricGauge}
	_ = mc.Record(EvalResult{Score: float64Ptr(0.9)}, metric)

	var buf bytes.Buffer
	_ = mc.WritePrometheus(&buf)
	output := buf.String()

	if !strings.Contains(output, `env="prod"`) {
		t.Errorf("expected env label, got:\n%s", output)
	}
	if !strings.Contains(output, `tenant="acme"`) {
		t.Errorf("expected tenant label, got:\n%s", output)
	}
}

func TestMetricCollector_LabelMergeBaseWins(t *testing.T) {
	mc := NewMetricCollector(WithLabels(map[string]string{"env": "prod"}))
	metric := &MetricDef{
		Name:   "quality",
		Type:   MetricGauge,
		Labels: map[string]string{"env": "staging", "category": "tone"},
	}
	_ = mc.Record(EvalResult{Score: float64Ptr(0.7)}, metric)

	var buf bytes.Buffer
	_ = mc.WritePrometheus(&buf)
	output := buf.String()

	// base label "env=prod" should win over def "env=staging"
	if !strings.Contains(output, `env="prod"`) {
		t.Errorf("base label should win on conflict, got:\n%s", output)
	}
	if !strings.Contains(output, `category="tone"`) {
		t.Errorf("non-conflicting def label should be present, got:\n%s", output)
	}
}

func TestMetricCollector_HistogramWithLabels(t *testing.T) {
	mc := NewMetricCollector(WithBuckets([]float64{1, 5, 10}))
	metric := &MetricDef{
		Name:   "latency",
		Type:   MetricHistogram,
		Labels: map[string]string{"eval_type": "custom"},
	}
	_ = mc.Record(EvalResult{MetricValue: float64Ptr(3)}, metric)

	var buf bytes.Buffer
	_ = mc.WritePrometheus(&buf)
	output := buf.String()

	// Buckets should have both custom label and le
	if !strings.Contains(output, `promptpack_latency_bucket{eval_type="custom",le="1"} 0`) {
		t.Errorf("expected labeled bucket, got:\n%s", output)
	}
	if !strings.Contains(output, `promptpack_latency_bucket{eval_type="custom",le="+Inf"} 1`) {
		t.Errorf("expected labeled +Inf bucket, got:\n%s", output)
	}
	// _sum and _count should have labels
	if !strings.Contains(output, `promptpack_latency_sum{eval_type="custom"} 3`) {
		t.Errorf("expected labeled sum, got:\n%s", output)
	}
	if !strings.Contains(output, `promptpack_latency_count{eval_type="custom"} 1`) {
		t.Errorf("expected labeled count, got:\n%s", output)
	}
}

func TestMetricCollector_NoLabelsBackwardCompat(t *testing.T) {
	mc := NewMetricCollector()
	metric := &MetricDef{Name: "quality", Type: MetricGauge}
	_ = mc.Record(EvalResult{Score: float64Ptr(0.85)}, metric)

	var buf bytes.Buffer
	_ = mc.WritePrometheus(&buf)
	output := buf.String()

	// No labels should produce output without braces
	if strings.Contains(output, "{") {
		t.Errorf("no-label metric should not have braces, got:\n%s", output)
	}
	if !strings.Contains(output, "promptpack_quality 0.85") {
		t.Errorf("expected plain gauge value, got:\n%s", output)
	}
}

func TestMetricCollector_SameNameDifferentLabels(t *testing.T) {
	mc := NewMetricCollector()
	metricA := &MetricDef{
		Name:   "quality",
		Type:   MetricGauge,
		Labels: map[string]string{"eval_type": "contains"},
	}
	metricB := &MetricDef{
		Name:   "quality",
		Type:   MetricGauge,
		Labels: map[string]string{"eval_type": "regex"},
	}

	_ = mc.Record(EvalResult{Score: float64Ptr(0.8)}, metricA)
	_ = mc.Record(EvalResult{Score: float64Ptr(0.9)}, metricB)

	var buf bytes.Buffer
	_ = mc.WritePrometheus(&buf)
	output := buf.String()

	// Should have both time series
	if !strings.Contains(output, `promptpack_quality{eval_type="contains"} 0.8`) {
		t.Errorf("expected contains series, got:\n%s", output)
	}
	if !strings.Contains(output, `promptpack_quality{eval_type="regex"} 0.9`) {
		t.Errorf("expected regex series, got:\n%s", output)
	}
	// TYPE line should appear only once
	if strings.Count(output, "# TYPE promptpack_quality gauge") != 1 {
		t.Errorf("TYPE line should appear once, got:\n%s", output)
	}
}

func TestMetricCollector_CounterWithLabels(t *testing.T) {
	mc := NewMetricCollector(WithLabels(map[string]string{"env": "test"}))
	metric := &MetricDef{Name: "eval_runs", Type: MetricCounter}
	_ = mc.Record(EvalResult{}, metric)
	_ = mc.Record(EvalResult{}, metric)

	var buf bytes.Buffer
	_ = mc.WritePrometheus(&buf)
	output := buf.String()

	if !strings.Contains(output, `promptpack_eval_runs{env="test"} 2`) {
		t.Errorf("expected labeled counter, got:\n%s", output)
	}
}

func TestMetricCollector_BooleanWithLabels(t *testing.T) {
	mc := NewMetricCollector()
	metric := &MetricDef{
		Name:   "check",
		Type:   MetricBoolean,
		Labels: map[string]string{"category": "safety"},
	}
	_ = mc.Record(EvalResult{Passed: true}, metric)

	var buf bytes.Buffer
	_ = mc.WritePrometheus(&buf)
	output := buf.String()

	if !strings.Contains(output, `promptpack_check{category="safety"} 1`) {
		t.Errorf("expected labeled boolean, got:\n%s", output)
	}
}

func TestMetricCollector_CustomBuckets(t *testing.T) {
	mc := NewMetricCollector(WithBuckets([]float64{1, 5, 10}))
	metric := &MetricDef{Name: "custom_hist", Type: MetricHistogram}

	_ = mc.Record(EvalResult{MetricValue: float64Ptr(3)}, metric)
	_ = mc.Record(EvalResult{MetricValue: float64Ptr(7)}, metric)

	var buf bytes.Buffer
	_ = mc.WritePrometheus(&buf)

	output := buf.String()
	// bucket 1: 0 observations <= 1
	if !strings.Contains(output, `promptpack_custom_hist_bucket{le="1"} 0`) {
		t.Errorf("expected bucket 1 = 0, got:\n%s", output)
	}
	// bucket 5: 1 observation <= 5 (3)
	if !strings.Contains(output, `promptpack_custom_hist_bucket{le="5"} 1`) {
		t.Errorf("expected bucket 5 = 1, got:\n%s", output)
	}
	// bucket 10: 2 observations <= 10
	if !strings.Contains(output, `promptpack_custom_hist_bucket{le="10"} 2`) {
		t.Errorf("expected bucket 10 = 2, got:\n%s", output)
	}
}
