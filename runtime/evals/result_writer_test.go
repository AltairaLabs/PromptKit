package evals

import (
	"context"
	"errors"
	"testing"
)

// mockRecorder captures metric recordings.
type mockRecorder struct {
	recordings []recordedMetric
	err        error
}

type recordedMetric struct {
	Result EvalResult
	Metric MetricDef
}

func (m *mockRecorder) Record(
	result EvalResult, metric *MetricDef,
) error {
	m.recordings = append(m.recordings, recordedMetric{
		Result: result,
		Metric: *metric,
	})
	return m.err
}

func TestMetricResultWriter_RecordsMetrics(t *testing.T) {
	rec := &mockRecorder{}
	defs := []EvalDef{
		{
			ID:   "e1",
			Type: "test",
			Metric: &MetricDef{
				Name: "test_metric",
				Type: MetricGauge,
			},
		},
		{ID: "e2", Type: "test"}, // no metric
	}
	writer := NewMetricResultWriter(rec, defs)

	results := []EvalResult{
		{EvalID: "e1", Passed: true},
		{EvalID: "e2", Passed: true},
	}
	err := writer.WriteResults(context.Background(), results)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Only e1 has a metric, so only 1 recording.
	if len(rec.recordings) != 1 {
		t.Fatalf("got %d recordings, want 1", len(rec.recordings))
	}
	if rec.recordings[0].Metric.Name != "test_metric" {
		t.Errorf(
			"metric name = %q, want %q",
			rec.recordings[0].Metric.Name, "test_metric",
		)
	}
}

func TestMetricResultWriter_UnknownEvalIDSkipped(t *testing.T) {
	rec := &mockRecorder{}
	defs := []EvalDef{
		{
			ID:     "e1",
			Type:   "test",
			Metric: &MetricDef{Name: "m", Type: MetricGauge},
		},
	}
	writer := NewMetricResultWriter(rec, defs)

	results := []EvalResult{
		{EvalID: "unknown", Passed: true},
	}
	err := writer.WriteResults(context.Background(), results)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(rec.recordings) != 0 {
		t.Errorf("expected no recordings, got %d", len(rec.recordings))
	}
}

func TestMetricResultWriter_RecorderError(t *testing.T) {
	rec := &mockRecorder{err: errors.New("record failed")}
	defs := []EvalDef{
		{
			ID:     "e1",
			Type:   "test",
			Metric: &MetricDef{Name: "m", Type: MetricGauge},
		},
	}
	writer := NewMetricResultWriter(rec, defs)

	results := []EvalResult{
		{EvalID: "e1", Passed: true},
	}
	err := writer.WriteResults(context.Background(), results)
	if err == nil {
		t.Fatal("expected recorder error")
	}
}

func TestMetadataResultWriter_NoOp(t *testing.T) {
	writer := &MetadataResultWriter{}
	err := writer.WriteResults(context.Background(), []EvalResult{
		{EvalID: "e1", Passed: true},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestCompositeResultWriter_FansOut(t *testing.T) {
	w1 := &recordingWriter{}
	w2 := &recordingWriter{}
	composite := NewCompositeResultWriter(w1, w2)

	results := []EvalResult{
		{EvalID: "e1", Passed: true},
	}
	err := composite.WriteResults(context.Background(), results)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(w1.batches) != 1 {
		t.Errorf("w1 got %d batches, want 1", len(w1.batches))
	}
	if len(w2.batches) != 1 {
		t.Errorf("w2 got %d batches, want 1", len(w2.batches))
	}
}

func TestCompositeResultWriter_StopsOnError(t *testing.T) {
	w1 := &recordingWriter{err: errors.New("w1 failed")}
	w2 := &recordingWriter{}
	composite := NewCompositeResultWriter(w1, w2)

	results := []EvalResult{
		{EvalID: "e1", Passed: true},
	}
	err := composite.WriteResults(context.Background(), results)
	if err == nil {
		t.Fatal("expected error from w1")
	}
	// w2 should not have been called.
	if len(w2.batches) != 0 {
		t.Errorf("w2 should not be called after w1 error")
	}
}

func TestCompositeResultWriter_Empty(t *testing.T) {
	composite := NewCompositeResultWriter()
	err := composite.WriteResults(context.Background(), []EvalResult{
		{EvalID: "e1"},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}
