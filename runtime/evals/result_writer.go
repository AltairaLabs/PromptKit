package evals

import "context"

// ResultWriter controls WHERE eval results go. Implementations may
// write to Prometheus metrics, message metadata, telemetry spans,
// databases, or external APIs. Platform-specific writers are
// implemented outside PromptKit.
type ResultWriter interface {
	WriteResults(ctx context.Context, results []EvalResult) error
}

// MetricRecorder records eval results as metrics. This interface is
// implemented by MetricCollector (defined in the metrics package) and
// injected here to avoid circular dependencies.
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

// MetadataResultWriter stores eval results in the EvalContext metadata
// under the key "pack_evals". This is used by Arena and SDK to attach
// results to message metadata for reporting.
type MetadataResultWriter struct{}

// WriteResults is a no-op placeholder. The actual metadata attachment
// happens at the caller level since the writer doesn't have access to
// the message being constructed. Callers use the returned results from
// InProcDispatcher to populate msg.Meta["pack_evals"].
func (w *MetadataResultWriter) WriteResults(
	_ context.Context, _ []EvalResult,
) error {
	return nil
}

// CompositeResultWriter fans out WriteResults calls to multiple
// writers. All writers are called; the first error encountered is
// returned.
type CompositeResultWriter struct {
	writers []ResultWriter
}

// NewCompositeResultWriter creates a writer that delegates to multiple
// writers. Writers are called in order.
func NewCompositeResultWriter(writers ...ResultWriter) *CompositeResultWriter {
	return &CompositeResultWriter{writers: writers}
}

// WriteResults calls WriteResults on each writer in order. Returns
// the first error encountered.
func (w *CompositeResultWriter) WriteResults(
	ctx context.Context, results []EvalResult,
) error {
	for _, writer := range w.writers {
		if err := writer.WriteResults(ctx, results); err != nil {
			return err
		}
	}
	return nil
}
