package evals

import (
	"context"
	"encoding/json"

	"github.com/jmespath/go-jmespath"

	"github.com/AltairaLabs/PromptKit/runtime/logger"
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

// MetricExpressionKey is the metric property naming a JMESPath expression that
// selects this metric's value out of EvalResult.Value.
//
// It lives in MetricDef.Extra rather than a typed field because the PromptPack
// schema declares MetricDef with additionalProperties: true, so a pack may
// carry it today without a spec change. The name matches the json_path eval
// handler's parameter so there is one expression vocabulary, not two.
const MetricExpressionKey = "jmespath_expression"

// ExtractValue returns the number to record for this metric, and whether there
// is one at all.
//
// The bool is the point. This used to return a bare float64 ending in
// `return 0`, so an eval that produced no scalar — a judge answering with a
// rubric, an eval calling a service and getting back a JSON object — was
// recorded as a gauge reading of ZERO: a flatline indistinguishable from a real
// measurement of zero. Callers must skip the sample when ok is false rather
// than substituting anything.
//
// Precedence:
//
//  1. A JMESPath expression declared on the metric, evaluated against
//     result.Value. This is the author's explicit choice, so it wins — it is
//     how one complex value becomes several series, with the names authored in
//     the pack rather than taken from a model's JSON keys.
//  2. result.MetricValue — the gauge number.
//  3. result.Score — the normalized 0..1, as a fallback.
//
// An expression that does not resolve, or resolves to something non-numeric,
// yields no sample. Guessing would reintroduce the fabricated zero.
func ExtractValue(result EvalResult, metric *MetricDef) (float64, bool) {
	if expr := metricExpression(metric); expr != "" {
		return evaluateMetricExpression(expr, result.Value)
	}
	if result.MetricValue != nil {
		return *result.MetricValue, true
	}
	if result.Score != nil {
		return *result.Score, true
	}
	return 0, false
}

// metricExpression returns the declared JMESPath expression, or "" when the
// metric declares none.
func metricExpression(metric *MetricDef) string {
	if metric == nil || metric.Extra == nil {
		return ""
	}
	expr, _ := metric.Extra[MetricExpressionKey].(string)
	return expr
}

// evaluateMetricExpression runs expr against value and coerces the result to a
// float. Anything that does not resolve to a number is reported as absent.
func evaluateMetricExpression(expr string, value any) (float64, bool) {
	if value == nil {
		return 0, false
	}
	found, err := jmespath.Search(expr, value)
	if err != nil {
		logger.Warn("eval metric expression failed",
			"expression", expr, "error", err)
		return 0, false
	}
	return toFloat(found)
}

// toFloat coerces the numeric types a decoded JSON document can produce. A
// dimension of 1 may arrive as int, int64 or json.Number depending on the
// decoder, and dropping those as "non-numeric" would silently lose series.
func toFloat(v any) (float64, bool) {
	switch n := v.(type) {
	case float64:
		return n, true
	case float32:
		return float64(n), true
	case int:
		return float64(n), true
	case int32:
		return float64(n), true
	case int64:
		return float64(n), true
	case json.Number:
		f, err := n.Float64()
		return f, err == nil
	default:
		return 0, false
	}
}
