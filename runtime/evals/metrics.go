package evals

import (
	"fmt"
	"io"
	"log"
	"math"
	"sort"
	"strings"
	"sync"
)

// DefaultBuckets are the default Prometheus histogram bucket boundaries.
// These match prometheus.DefBuckets.
var DefaultBuckets = []float64{.005, .01, .025, .05, .1, .25, .5, 1, 2.5, 5, 10}

// metricEntry stores the state for a single named metric.
type metricEntry struct {
	name       string
	metricType MetricType
	// Gauge / Boolean
	value float64
	// Counter
	count float64
	// Histogram
	observations []float64
	sum          float64
	obsCount     uint64
}

// MetricCollector implements MetricRecorder and provides Prometheus text
// exposition. It is safe for concurrent use.
type MetricCollector struct {
	mu        sync.RWMutex
	metrics   map[string]*metricEntry
	namespace string
	buckets   []float64
}

// MetricCollectorOption configures a MetricCollector.
type MetricCollectorOption func(*MetricCollector)

// WithNamespace sets the metric name prefix (e.g. "promptpack").
func WithNamespace(ns string) MetricCollectorOption {
	return func(mc *MetricCollector) { mc.namespace = ns }
}

// WithBuckets sets custom histogram bucket boundaries.
func WithBuckets(buckets []float64) MetricCollectorOption {
	return func(mc *MetricCollector) { mc.buckets = buckets }
}

// NewMetricCollector creates a new MetricCollector with the given options.
func NewMetricCollector(opts ...MetricCollectorOption) *MetricCollector {
	mc := &MetricCollector{
		metrics:   make(map[string]*metricEntry),
		namespace: "promptpack",
		buckets:   DefaultBuckets,
	}
	for _, opt := range opts {
		opt(mc)
	}
	return mc
}

// Record records an eval result for the given metric definition.
// Thread-safe.
//
//nolint:gocritic // EvalResult passed by value to satisfy MetricRecorder interface
func (mc *MetricCollector) Record(result EvalResult, metric *MetricDef) error {
	if metric == nil {
		return fmt.Errorf("nil metric definition")
	}

	name := mc.prefixedName(metric.Name)
	value := extractValue(result, metric)

	mc.validateRange(name, value, metric.Range)

	mc.mu.Lock()
	defer mc.mu.Unlock()

	entry, ok := mc.metrics[name]
	if !ok {
		entry = &metricEntry{
			name:       name,
			metricType: metric.Type,
		}
		mc.metrics[name] = entry
	}

	switch metric.Type {
	case MetricGauge:
		entry.value = value
	case MetricCounter:
		entry.count++
	case MetricHistogram:
		entry.observations = append(entry.observations, value)
		entry.sum += value
		entry.obsCount++
	case MetricBoolean:
		if result.Passed {
			entry.value = 1.0
		} else {
			entry.value = 0.0
		}
	default:
		return fmt.Errorf("unknown metric type: %q", metric.Type)
	}

	return nil
}

// WritePrometheus writes all metrics in Prometheus text exposition format.
func (mc *MetricCollector) WritePrometheus(w io.Writer) error {
	mc.mu.RLock()
	defer mc.mu.RUnlock()

	// Sort metric names for deterministic output
	names := make([]string, 0, len(mc.metrics))
	for name := range mc.metrics {
		names = append(names, name)
	}
	sort.Strings(names)

	for _, name := range names {
		entry := mc.metrics[name]
		if err := mc.writeEntry(w, entry); err != nil {
			return err
		}
	}

	return nil
}

// Reset clears all metrics. Primarily for testing.
func (mc *MetricCollector) Reset() {
	mc.mu.Lock()
	defer mc.mu.Unlock()
	mc.metrics = make(map[string]*metricEntry)
}

// writeEntry writes a single metric entry in Prometheus format.
func (mc *MetricCollector) writeEntry(w io.Writer, entry *metricEntry) error {
	switch entry.metricType {
	case MetricGauge, MetricBoolean:
		return writeGaugeEntry(w, entry)
	case MetricCounter:
		return writeCounterEntry(w, entry)
	case MetricHistogram:
		return mc.writeHistogramEntry(w, entry)
	}
	return nil
}

// writeGaugeEntry writes a gauge or boolean metric.
func writeGaugeEntry(w io.Writer, entry *metricEntry) error {
	if _, err := fmt.Fprintf(w, "# TYPE %s gauge\n", entry.name); err != nil {
		return err
	}
	_, err := fmt.Fprintf(w, "%s %s\n", entry.name, formatFloat(entry.value))
	return err
}

// writeCounterEntry writes a counter metric.
func writeCounterEntry(w io.Writer, entry *metricEntry) error {
	if _, err := fmt.Fprintf(w, "# TYPE %s counter\n", entry.name); err != nil {
		return err
	}
	_, err := fmt.Fprintf(w, "%s %s\n", entry.name, formatFloat(entry.count))
	return err
}

// writeHistogramEntry writes a histogram metric with buckets.
func (mc *MetricCollector) writeHistogramEntry(w io.Writer, entry *metricEntry) error {
	if _, err := fmt.Fprintf(w, "# TYPE %s histogram\n", entry.name); err != nil {
		return err
	}
	if err := mc.writeHistogramBuckets(w, entry); err != nil {
		return err
	}
	if _, err := fmt.Fprintf(w, "%s_sum %s\n", entry.name, formatFloat(entry.sum)); err != nil {
		return err
	}
	_, err := fmt.Fprintf(w, "%s_count %d\n", entry.name, entry.obsCount)
	return err
}

// writeHistogramBuckets writes the bucket lines for a histogram.
func (mc *MetricCollector) writeHistogramBuckets(w io.Writer, entry *metricEntry) error {
	for _, bound := range mc.buckets {
		count := uint64(0)
		for _, obs := range entry.observations {
			if obs <= bound {
				count++
			}
		}
		if _, err := fmt.Fprintf(w, "%s_bucket{le=\"%s\"} %d\n",
			entry.name, formatFloat(bound), count); err != nil {
			return err
		}
	}
	// +Inf bucket
	if _, err := fmt.Fprintf(w, "%s_bucket{le=\"+Inf\"} %d\n",
		entry.name, entry.obsCount); err != nil {
		return err
	}
	return nil
}

// prefixedName prepends the namespace if not already prefixed.
func (mc *MetricCollector) prefixedName(name string) string {
	prefix := mc.namespace + "_"
	if strings.HasPrefix(name, prefix) {
		return name
	}
	return prefix + name
}

// validateRange logs a warning if the value is outside the metric's range.
func (mc *MetricCollector) validateRange(name string, value float64, r *Range) {
	if r == nil {
		return
	}
	if r.Min != nil && value < *r.Min {
		log.Printf("WARNING: metric %q value %g below range minimum %g", name, value, *r.Min)
	}
	if r.Max != nil && value > *r.Max {
		log.Printf("WARNING: metric %q value %g above range maximum %g", name, value, *r.Max)
	}
}

// extractValue extracts the numeric value from an EvalResult.
// Prefers MetricValue, falls back to Score, then defaults to 0.
//
//nolint:gocritic // EvalResult passed by value to match MetricRecorder.Record signature
func extractValue(result EvalResult, _ *MetricDef) float64 {
	if result.MetricValue != nil {
		return *result.MetricValue
	}
	if result.Score != nil {
		return *result.Score
	}
	return 0
}

// formatFloat formats a float64 for Prometheus output.
func formatFloat(v float64) string {
	if v == math.Trunc(v) && !math.IsInf(v, 0) && !math.IsNaN(v) {
		return fmt.Sprintf("%g", v)
	}
	return fmt.Sprintf("%g", v)
}

// Ensure MetricCollector implements MetricRecorder.
var _ MetricRecorder = (*MetricCollector)(nil)
