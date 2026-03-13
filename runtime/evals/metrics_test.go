package evals

import (
	"testing"
)

func TestExtractValue_PrefersMetricValue(t *testing.T) {
	result := EvalResult{
		Score:       float64Ptr(0.5),
		MetricValue: float64Ptr(0.9),
	}
	got := ExtractValue(result, nil)
	if got != 0.9 {
		t.Errorf("expected 0.9, got %v", got)
	}
}

func TestExtractValue_FallsBackToScore(t *testing.T) {
	result := EvalResult{Score: float64Ptr(0.7)}
	got := ExtractValue(result, nil)
	if got != 0.7 {
		t.Errorf("expected 0.7, got %v", got)
	}
}

func TestExtractValue_DefaultsToZero(t *testing.T) {
	result := EvalResult{}
	got := ExtractValue(result, nil)
	if got != 0 {
		t.Errorf("expected 0, got %v", got)
	}
}

func TestMetricResultWriter_SkipsEvalsWithoutMetric(t *testing.T) {
	recorder := &mockRecorder{}
	defs := []EvalDef{
		{ID: "e1", Type: "contains"},
		{ID: "e2", Type: "contains", Metric: &MetricDef{Name: "m2", Type: MetricGauge}},
	}
	writer := NewMetricResultWriter(recorder, defs)

	results := []EvalResult{
		{EvalID: "e1"},
		{EvalID: "e2", Score: float64Ptr(0.9)},
		{EvalID: "e3"}, // unknown eval ID
	}
	if err := writer.WriteResults(nil, results); err != nil {
		t.Fatal(err)
	}

	if len(recorder.calls) != 1 {
		t.Fatalf("expected 1 record call, got %d", len(recorder.calls))
	}
	if recorder.calls[0].metric.Name != "m2" {
		t.Errorf("expected metric name m2, got %s", recorder.calls[0].metric.Name)
	}
}

type recordCall struct {
	result EvalResult
	metric *MetricDef
}

type mockRecorder struct {
	calls []recordCall
}

func (r *mockRecorder) Record(result EvalResult, metric *MetricDef) error {
	r.calls = append(r.calls, recordCall{result: result, metric: metric})
	return nil
}
