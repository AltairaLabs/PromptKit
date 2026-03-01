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
	labels     map[string]string // merged MetricDef + base labels
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
	mu         sync.RWMutex
	metrics    map[string]*metricEntry
	namespace  string
	buckets    []float64
	baseLabels map[string]string
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

// WithLabels sets base labels that are merged into every recorded metric.
// These are typically platform-level labels (e.g. env, tenant_id, region).
// Base labels take precedence over MetricDef labels on conflict.
func WithLabels(labels map[string]string) MetricCollectorOption {
	return func(mc *MetricCollector) { mc.baseLabels = labels }
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

	merged := mergeLabels(metric.Labels, mc.baseLabels)
	key := name
	if lk := labelKey(merged); lk != "" {
		key = name + "|" + lk
	}

	mc.mu.Lock()
	defer mc.mu.Unlock()

	entry, ok := mc.metrics[key]
	if !ok {
		entry = &metricEntry{
			name:       name,
			labels:     merged,
			metricType: metric.Type,
		}
		mc.metrics[key] = entry
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

	// Sort map keys for deterministic output
	keys := make([]string, 0, len(mc.metrics))
	for k := range mc.metrics {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	lastTypeName := ""
	for _, key := range keys {
		entry := mc.metrics[key]
		if err := mc.writeEntry(w, entry, &lastTypeName); err != nil {
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
// lastTypeName tracks the last emitted TYPE line to deduplicate when the
// same metric name appears with different label sets.
func (mc *MetricCollector) writeEntry(w io.Writer, entry *metricEntry, lastTypeName *string) error {
	switch entry.metricType {
	case MetricGauge, MetricBoolean:
		return writeGaugeEntry(w, entry, lastTypeName)
	case MetricCounter:
		return writeCounterEntry(w, entry, lastTypeName)
	case MetricHistogram:
		return mc.writeHistogramEntry(w, entry, lastTypeName)
	}
	return nil
}

// writeTypeLine writes a TYPE comment if the metric name differs from the last emitted one.
func writeTypeLine(w io.Writer, name, metricType string, lastTypeName *string) error {
	if *lastTypeName == name {
		return nil
	}
	*lastTypeName = name
	_, err := fmt.Fprintf(w, "# TYPE %s %s\n", name, metricType)
	return err
}

// writeGaugeEntry writes a gauge or boolean metric.
func writeGaugeEntry(w io.Writer, entry *metricEntry, lastTypeName *string) error {
	if err := writeTypeLine(w, entry.name, "gauge", lastTypeName); err != nil {
		return err
	}
	labels := formatLabels(entry.labels)
	_, err := fmt.Fprintf(w, "%s%s %s\n", entry.name, labels, formatFloat(entry.value))
	return err
}

// writeCounterEntry writes a counter metric.
func writeCounterEntry(w io.Writer, entry *metricEntry, lastTypeName *string) error {
	if err := writeTypeLine(w, entry.name, "counter", lastTypeName); err != nil {
		return err
	}
	labels := formatLabels(entry.labels)
	_, err := fmt.Fprintf(w, "%s%s %s\n", entry.name, labels, formatFloat(entry.count))
	return err
}

// writeHistogramEntry writes a histogram metric with buckets.
func (mc *MetricCollector) writeHistogramEntry(
	w io.Writer, entry *metricEntry, lastTypeName *string,
) error {
	if err := writeTypeLine(w, entry.name, "histogram", lastTypeName); err != nil {
		return err
	}
	if err := mc.writeHistogramBuckets(w, entry); err != nil {
		return err
	}
	labels := formatLabels(entry.labels)
	if _, err := fmt.Fprintf(w, "%s_sum%s %s\n", entry.name, labels, formatFloat(entry.sum)); err != nil {
		return err
	}
	_, err := fmt.Fprintf(w, "%s_count%s %d\n", entry.name, labels, entry.obsCount)
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
		labelsWithLE := formatLabelsWithLE(entry.labels, formatFloat(bound))
		if _, err := fmt.Fprintf(w, "%s_bucket%s %d\n",
			entry.name, labelsWithLE, count); err != nil {
			return err
		}
	}
	// +Inf bucket
	labelsWithLE := formatLabelsWithLE(entry.labels, "+Inf")
	if _, err := fmt.Fprintf(w, "%s_bucket%s %d\n",
		entry.name, labelsWithLE, entry.obsCount); err != nil {
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

// mergeLabels merges def-level labels with base labels.
// Base labels take precedence on conflict.
func mergeLabels(defLabels, baseLabels map[string]string) map[string]string {
	if len(defLabels) == 0 && len(baseLabels) == 0 {
		return nil
	}
	merged := make(map[string]string, len(defLabels)+len(baseLabels))
	for k, v := range defLabels {
		merged[k] = v
	}
	for k, v := range baseLabels {
		merged[k] = v
	}
	return merged
}

// labelKey returns a deterministic string representation of labels for use as
// part of a composite map key. Returns "" if labels is empty.
func labelKey(labels map[string]string) string {
	if len(labels) == 0 {
		return ""
	}
	keys := make([]string, 0, len(labels))
	for k := range labels {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var b strings.Builder
	for i, k := range keys {
		if i > 0 {
			b.WriteByte(',')
		}
		b.WriteString(k)
		b.WriteByte('=')
		b.WriteString(labels[k])
	}
	return b.String()
}

// formatLabels returns labels in Prometheus format: {k1="v1",k2="v2"}.
// Returns "" if labels is empty.
func formatLabels(labels map[string]string) string {
	if len(labels) == 0 {
		return ""
	}
	keys := make([]string, 0, len(labels))
	for k := range labels {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var b strings.Builder
	b.WriteByte('{')
	for i, k := range keys {
		if i > 0 {
			b.WriteByte(',')
		}
		b.WriteString(k)
		b.WriteString(`="`)
		b.WriteString(labels[k])
		b.WriteByte('"')
	}
	b.WriteByte('}')
	return b.String()
}

// formatLabelsWithLE returns Prometheus-format labels that include the le
// bucket boundary alongside any custom labels.
func formatLabelsWithLE(labels map[string]string, le string) string {
	if len(labels) == 0 {
		return `{le="` + le + `"}`
	}
	keys := make([]string, 0, len(labels))
	for k := range labels {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var b strings.Builder
	b.WriteByte('{')
	for _, k := range keys {
		b.WriteString(k)
		b.WriteString(`="`)
		b.WriteString(labels[k])
		b.WriteString(`",`)
	}
	b.WriteString(`le="`)
	b.WriteString(le)
	b.WriteByte('"')
	b.WriteByte('}')
	return b.String()
}

// Ensure MetricCollector implements MetricRecorder.
var _ MetricRecorder = (*MetricCollector)(nil)
