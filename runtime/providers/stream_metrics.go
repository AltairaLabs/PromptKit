package providers

import (
	"sync"
	"time"

	"github.com/prometheus/client_golang/prometheus"
)

// streamFirstChunkBuckets is tuned for reasoning-model streaming workloads.
// gpt-5-pro and similar often take tens of seconds to emit the first
// content chunk. The default Prometheus web-traffic buckets (up to 10s)
// would truncate everything into one overflow bucket and lose signal for
// exactly the workload this histogram exists to observe.
var streamFirstChunkBuckets = []float64{0.1, 0.25, 0.5, 1, 2.5, 5, 10, 30, 60, 120, 300}

// StreamMetrics holds the direct-update Prometheus metrics for streaming
// provider calls. These are updated inline at the source (not via the
// event bus) so that burst-load drops on the event bus cannot corrupt
// autoscaling signals. See docs/local-backlog/STREAMING_RETRY_AT_SCALE.md
// for the design rationale.
//
// All methods are nil-safe: if StreamMetrics is nil, the call is a no-op.
// This lets provider code unconditionally call s.StreamsInFlightInc(...)
// without guarding on whether metrics are configured.
type StreamMetrics struct {
	streamsInFlight         *prometheus.GaugeVec
	providerCallsInFlight   *prometheus.GaugeVec
	streamFirstChunkLatency *prometheus.HistogramVec
	streamRetriesTotal      *prometheus.CounterVec
}

// NewStreamMetrics creates and registers the Phase 1 streaming metrics
// into the given registerer under the given namespace. Const labels are
// applied to every metric.
//
// Returns a non-nil *StreamMetrics. Re-registration of the same metric
// name into the same registry will panic (Prometheus semantic), so the
// default registration path uses sync.Once via RegisterDefaultStreamMetrics.
func NewStreamMetrics(
	registerer prometheus.Registerer,
	namespace string,
	constLabels prometheus.Labels,
) *StreamMetrics {
	if namespace == "" {
		namespace = "promptkit"
	}
	m := &StreamMetrics{
		streamsInFlight: prometheus.NewGaugeVec(prometheus.GaugeOpts{
			Namespace:   namespace,
			Name:        "streams_in_flight",
			Help:        "Number of streaming provider calls currently in flight (per provider).",
			ConstLabels: constLabels,
		}, []string{"provider"}),
		providerCallsInFlight: prometheus.NewGaugeVec(prometheus.GaugeOpts{
			Namespace:   namespace,
			Name:        "provider_calls_in_flight",
			Help:        "Number of provider calls (streaming or not) currently in flight, per provider.",
			ConstLabels: constLabels,
		}, []string{"provider"}),
		streamFirstChunkLatency: prometheus.NewHistogramVec(prometheus.HistogramOpts{
			Namespace: namespace,
			Name:      "stream_first_chunk_latency_seconds",
			Help: "Time from request dispatch to first SSE data event observed, " +
				"per provider. Includes any pre-first-chunk retries.",
			Buckets:     streamFirstChunkBuckets,
			ConstLabels: constLabels,
		}, []string{"provider"}),
		streamRetriesTotal: prometheus.NewCounterVec(prometheus.CounterOpts{
			Namespace:   namespace,
			Name:        "stream_retries_total",
			Help:        "Total streaming retry attempts, labeled by outcome (success, failed, budget_exhausted).",
			ConstLabels: constLabels,
		}, []string{"provider", "outcome"}),
	}
	registerer.MustRegister(
		m.streamsInFlight,
		m.providerCallsInFlight,
		m.streamFirstChunkLatency,
		m.streamRetriesTotal,
	)
	return m
}

// StreamsInFlightInc increments the in-flight stream gauge for a provider.
// Nil-safe.
func (m *StreamMetrics) StreamsInFlightInc(provider string) {
	if m == nil {
		return
	}
	m.streamsInFlight.WithLabelValues(provider).Inc()
}

// StreamsInFlightDec decrements the in-flight stream gauge for a provider.
// Nil-safe.
func (m *StreamMetrics) StreamsInFlightDec(provider string) {
	if m == nil {
		return
	}
	m.streamsInFlight.WithLabelValues(provider).Dec()
}

// ProviderCallsInFlightInc increments the total in-flight provider call
// gauge. Nil-safe.
func (m *StreamMetrics) ProviderCallsInFlightInc(provider string) {
	if m == nil {
		return
	}
	m.providerCallsInFlight.WithLabelValues(provider).Inc()
}

// ProviderCallsInFlightDec decrements the total in-flight provider call
// gauge. Nil-safe.
func (m *StreamMetrics) ProviderCallsInFlightDec(provider string) {
	if m == nil {
		return
	}
	m.providerCallsInFlight.WithLabelValues(provider).Dec()
}

// ObserveFirstChunkLatency records the time from request dispatch to the
// first SSE data event being observed for a provider. Nil-safe.
func (m *StreamMetrics) ObserveFirstChunkLatency(provider string, d time.Duration) {
	if m == nil {
		return
	}
	m.streamFirstChunkLatency.WithLabelValues(provider).Observe(d.Seconds())
}

// RetryAttempt records one streaming retry attempt with an outcome label.
// Outcome values: "success" (attempt that produced a usable stream),
// "failed" (retryable transient failure that will be retried), or
// "exhausted" (last attempt failed, no more retries). Nil-safe.
func (m *StreamMetrics) RetryAttempt(provider, outcome string) {
	if m == nil {
		return
	}
	m.streamRetriesTotal.WithLabelValues(provider, outcome).Inc()
}

// Package-level default instance. Hosts register it by calling
// RegisterDefaultStreamMetrics during startup; all provider code reads
// via DefaultStreamMetrics() unconditionally.
var (
	defaultStreamMetrics   *StreamMetrics
	defaultStreamMetricsMu sync.RWMutex
)

// RegisterDefaultStreamMetrics creates and installs a process-wide
// StreamMetrics instance. Safe to call multiple times with the SAME
// registerer — subsequent calls are no-ops. Calling with a DIFFERENT
// registerer is not supported and is treated as a misconfiguration
// (second call is ignored and the first wins).
//
// Hosts (Arena, SDK, server) call this once during startup. Code that
// only cares about metrics being present calls DefaultStreamMetrics()
// and gets a nil on a misconfigured host, which is safe (methods no-op).
func RegisterDefaultStreamMetrics(
	registerer prometheus.Registerer,
	namespace string,
	constLabels prometheus.Labels,
) *StreamMetrics {
	defaultStreamMetricsMu.Lock()
	defer defaultStreamMetricsMu.Unlock()
	if defaultStreamMetrics != nil {
		return defaultStreamMetrics
	}
	defaultStreamMetrics = NewStreamMetrics(registerer, namespace, constLabels)
	return defaultStreamMetrics
}

// DefaultStreamMetrics returns the process-wide StreamMetrics instance or
// nil if none has been registered. All StreamMetrics methods are nil-safe.
func DefaultStreamMetrics() *StreamMetrics {
	defaultStreamMetricsMu.RLock()
	defer defaultStreamMetricsMu.RUnlock()
	return defaultStreamMetrics
}

// ResetDefaultStreamMetrics clears the process-wide instance. Intended for
// tests only — production code should not need to reset metrics.
func ResetDefaultStreamMetrics() {
	defaultStreamMetricsMu.Lock()
	defer defaultStreamMetricsMu.Unlock()
	defaultStreamMetrics = nil
}
