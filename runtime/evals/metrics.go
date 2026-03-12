package evals

import (
	"context"
)

// MetricRecorder records eval results as metrics. This interface is
// implemented by metrics.MetricContext and injected into MetricResultWriter
// to avoid circular dependencies.
type MetricRecorder interface {
	Record(result EvalResult, metric *MetricDef) error
}

// MetricResultWriter feeds eval results to a MetricRecorder for
// Prometheus exposition. Only results whose corresponding EvalDef
// has a Metric definition are recorded.
type MetricResultWriter struct {
	recorder MetricRecorder
	// defs maps eval ID to its definition for metric lookup.
	defs map[string]*EvalDef
}

// NewMetricResultWriter creates a writer that records metrics.
// The defs slice provides the metric definitions keyed by eval ID.
func NewMetricResultWriter(
	recorder MetricRecorder, defs []EvalDef,
) *MetricResultWriter {
	m := make(map[string]*EvalDef, len(defs))
	for i := range defs {
		m[defs[i].ID] = &defs[i]
	}
	return &MetricResultWriter{recorder: recorder, defs: m}
}

// WriteResults records each result that has an associated metric.
func (w *MetricResultWriter) WriteResults(
	_ context.Context, results []EvalResult,
) error {
	for i := range results {
		def, ok := w.defs[results[i].EvalID]
		if !ok || def.Metric == nil {
			continue
		}
		if err := w.recorder.Record(results[i], def.Metric); err != nil {
			return err
		}
	}
	return nil
}

// ExtractValue extracts the numeric value from an EvalResult.
// Prefers MetricValue, falls back to Score, then defaults to 0.
//
//nolint:gocritic // EvalResult passed by value to match MetricRecorder.Record signature
func ExtractValue(result EvalResult, _ *MetricDef) float64 {
	if result.MetricValue != nil {
		return *result.MetricValue
	}
	if result.Score != nil {
		return *result.Score
	}
	return 0
}
