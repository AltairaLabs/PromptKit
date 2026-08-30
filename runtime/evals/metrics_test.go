package evals

import (
	"testing"
)

func TestExtractValue_PrefersMetricValue(t *testing.T) {
	result := EvalResult{
		Score:       float64Ptr(0.5),
		MetricValue: float64Ptr(0.9),
	}
	got, ok := ExtractValue(result, nil)
	if !ok || got != 0.9 {
		t.Errorf("expected 0.9 present, got %v (ok=%v)", got, ok)
	}
}

func TestExtractValue_FallsBackToScore(t *testing.T) {
	result := EvalResult{Score: float64Ptr(0.7)}
	got, ok := ExtractValue(result, nil)
	if !ok || got != 0.7 {
		t.Errorf("expected 0.7 present, got %v (ok=%v)", got, ok)
	}
}

// TestExtractValue_AbsentWhenNothingToRecord asserts the OPPOSITE of what this
// test did when it was named DefaultsToZero.
//
// Defaulting to zero meant an eval with no scalar was recorded as a gauge
// reading of 0 — a flatline indistinguishable from a real measurement. Absence
// is now reported as absence and the caller skips the sample.
func TestExtractValue_AbsentWhenNothingToRecord(t *testing.T) {
	result := EvalResult{}
	got, ok := ExtractValue(result, nil)
	if ok {
		t.Errorf("expected no value, got %v", got)
	}
}

func TestMetricResultWriter_UsesExplicitMetric(t *testing.T) {
	recorder := &mockRecorder{}
	defs := []EvalDef{
		{ID: "e1", Type: "contains", Metric: &MetricDef{Name: "custom_name", Type: MetricHistogram}},
	}
	writer := NewMetricResultWriter(recorder, defs)

	results := []EvalResult{{EvalID: "e1", Score: float64Ptr(0.9)}}
	if err := writer.WriteResults(nil, results); err != nil {
		t.Fatal(err)
	}

	if len(recorder.calls) != 1 {
		t.Fatalf("expected 1 record call, got %d", len(recorder.calls))
	}
	if recorder.calls[0].metric.Name != "custom_name" {
		t.Errorf("expected metric name custom_name, got %s", recorder.calls[0].metric.Name)
	}
	if recorder.calls[0].metric.Type != MetricHistogram {
		t.Errorf("expected metric type histogram, got %s", recorder.calls[0].metric.Type)
	}
}

func TestMetricResultWriter_AutoGeneratesMetricForEvalsWithoutOne(t *testing.T) {
	recorder := &mockRecorder{}
	defs := []EvalDef{
		{ID: "response-quality", Type: "llm_judge"},
		{ID: "tone-check", Type: "contains"},
	}
	writer := NewMetricResultWriter(recorder, defs)

	results := []EvalResult{
		{EvalID: "response-quality", Score: float64Ptr(0.85)},
		{EvalID: "tone-check", Score: float64Ptr(1.0)},
	}
	if err := writer.WriteResults(nil, results); err != nil {
		t.Fatal(err)
	}

	if len(recorder.calls) != 2 {
		t.Fatalf("expected 2 record calls, got %d", len(recorder.calls))
	}
	// Auto-generated metrics should use eval ID as name and gauge as type.
	if recorder.calls[0].metric.Name != "response-quality" {
		t.Errorf("expected metric name response-quality, got %s", recorder.calls[0].metric.Name)
	}
	if recorder.calls[0].metric.Type != MetricGauge {
		t.Errorf("expected default metric type gauge, got %s", recorder.calls[0].metric.Type)
	}
	if recorder.calls[1].metric.Name != "tone-check" {
		t.Errorf("expected metric name tone-check, got %s", recorder.calls[1].metric.Name)
	}
}

func TestMetricResultWriter_SkipsUnknownEvalIDs(t *testing.T) {
	recorder := &mockRecorder{}
	defs := []EvalDef{
		{ID: "e1", Type: "contains"},
	}
	writer := NewMetricResultWriter(recorder, defs)

	results := []EvalResult{
		{EvalID: "e1", Score: float64Ptr(0.5)},
		{EvalID: "unknown-eval"}, // not in defs — should be skipped
	}
	if err := writer.WriteResults(nil, results); err != nil {
		t.Fatal(err)
	}

	if len(recorder.calls) != 1 {
		t.Fatalf("expected 1 record call (unknown skipped), got %d", len(recorder.calls))
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
