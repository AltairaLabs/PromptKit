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
// Prometheus exposition. Every eval result is recorded: if the EvalDef
// includes an explicit Metric definition it is used; otherwise a default
// gauge metric named after the eval ID is created automatically.
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

// WriteResults records each eval result as a Prometheus metric.
// If the EvalDef has an explicit Metric definition, that is used.
// Otherwise a default gauge metric named after the eval ID is generated
// so that every eval produces a metric without requiring pack authors
// to define one explicitly.
func (w *MetricResultWriter) WriteResults(
	_ context.Context, results []EvalResult,
) error {
	for i := range results {
		metric := w.metricForEval(results[i].EvalID)
		if metric == nil {
			continue
		}
		if err := w.recorder.Record(results[i], metric); err != nil {
			return err
		}
	}
	return nil
}

// metricForEval returns the metric definition for an eval result.
// Returns the explicit definition if present, generates a default gauge
// if the eval is known but has no metric, or returns nil for unknown evals.
func (w *MetricResultWriter) metricForEval(evalID string) *MetricDef {
	def, ok := w.defs[evalID]
	if !ok {
		return nil
	}
	if def.Metric != nil {
		return def.Metric
	}
	// Auto-generate a default gauge metric for evals without an explicit definition.
	m := &MetricDef{Name: evalID, Type: MetricGauge}
	def.Metric = m
	return m
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
