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

// streamErrorChunksForwardedBuckets partitions the "chunks forwarded
// before error" histogram. The critical boundary is 0 vs 1: zero
// chunks means Phase 1 pre-first-chunk retry could have caught the
// failure (and the caller saw no content), whereas one or more means
// the stream leaked through the safe-retry window and content has
// already been emitted downstream — the regime where only Phase 4
// dedup-aware resume can help. The upper tail covers full reasoning
// responses that can run into tens of thousands of chunks.
//
// See AltairaLabs/PromptKit#864: this histogram is the load-bearing
// measurement for deciding whether Phase 4 is worth building.
var streamErrorChunksForwardedBuckets = []float64{
	0, 1, 5, 20, 100, 500, 2000, 10000,
}

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
	streamsInFlight             *prometheus.GaugeVec
	providerCallsInFlight       *prometheus.GaugeVec
	streamFirstChunkLatency     *prometheus.HistogramVec
	streamErrorChunksForwarded  *prometheus.HistogramVec
	streamRetriesTotal          *prometheus.CounterVec
	streamRetryBudgetAvailable  *prometheus.GaugeVec
	streamConcurrencyRejections *prometheus.CounterVec
	httpConnsInUse              *prometheus.GaugeVec
	pipelineStageElements       *prometheus.CounterVec
	pipelineStageAudioBytes     *prometheus.CounterVec
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
		streamErrorChunksForwarded: prometheus.NewHistogramVec(prometheus.HistogramOpts{
			Namespace: namespace,
			Name:      "stream_error_chunks_forwarded",
			Help: "Number of content chunks forwarded downstream before a stream " +
				"terminated with an error, per provider. Observed once per failed " +
				"stream on the terminal error chunk. The bucket at 0 counts errors " +
				"that reached the consumer with no content already emitted (a regime " +
				"Phase 1 pre-first-chunk retry could cover if the classifier recognized " +
				"the error as retryable); buckets at 1 and above count mid-stream " +
				"failures that leaked past the safe-retry window and would require " +
				"Phase 4 dedup-aware resume to recover (see AltairaLabs/PromptKit#864). " +
				"This histogram is the load-bearing measurement for deciding whether " +
				"Phase 4 is worth building.",
			Buckets:     streamErrorChunksForwardedBuckets,
			ConstLabels: constLabels,
		}, []string{"provider"}),
		streamRetriesTotal: prometheus.NewCounterVec(prometheus.CounterOpts{
			Namespace:   namespace,
			Name:        "stream_retries_total",
			Help:        "Total streaming retry attempts, labeled by outcome (success, failed, budget_exhausted).",
			ConstLabels: constLabels,
		}, []string{"provider", "outcome"}),
		streamRetryBudgetAvailable: prometheus.NewGaugeVec(prometheus.GaugeOpts{
			Namespace: namespace,
			Name:      "stream_retry_budget_available",
			Help: "Current tokens available in the streaming retry budget, per (provider, host). " +
				"Early-warning signal for upstream degradation — a provider whose budget is " +
				"trending toward zero is about to start failing retries.",
			ConstLabels: constLabels,
		}, []string{"provider", "host"}),
		streamConcurrencyRejections: prometheus.NewCounterVec(prometheus.CounterOpts{
			Namespace: namespace,
			Name:      "stream_concurrency_rejections_total",
			Help: "Streaming requests rejected by the per-provider concurrency " +
				"semaphore, labeled by reason (context_canceled, deadline_exceeded). " +
				"A healthy signal that back-pressure is working; sustained spikes " +
				"indicate the semaphore limit is undersized or upstream is saturated.",
			ConstLabels: constLabels,
		}, []string{"provider", "reason"}),
		pipelineStageElements: prometheus.NewCounterVec(prometheus.CounterOpts{
			Namespace: namespace,
			Name:      "pipeline_stage_elements_total",
			Help: "Total elements processed by each pipeline stage. " +
				"Labeled by stage name so operators can see exactly where element " +
				"flow stops in a multi-stage pipeline.",
			ConstLabels: constLabels,
		}, []string{"stage"}),
		pipelineStageAudioBytes: prometheus.NewCounterVec(prometheus.CounterOpts{
			Namespace: namespace,
			Name:      "pipeline_stage_audio_bytes_total",
			Help: "Total audio bytes processed by each pipeline stage. " +
				"Tracks raw PCM audio volume through the pipeline. A stage that " +
				"shows zero audio bytes while its predecessor shows nonzero " +
				"indicates the stage is dropping or not forwarding audio elements.",
			ConstLabels: constLabels,
		}, []string{"stage"}),
		httpConnsInUse: prometheus.NewGaugeVec(prometheus.GaugeOpts{
			Namespace: namespace,
			Name:      "http_conns_in_use",
			Help: "HTTP requests currently holding a connection slot to each upstream host. " +
				"With HTTP/2 multiplexing, multiple requests may share one TCP connection, so " +
				"this is an upper bound on physical connections in use rather than an exact " +
				"count. Operationally this is the pool-pressure signal for tuning " +
				"MaxConnsPerHost (see AltairaLabs/PromptKit#873): when this gauge approaches " +
				"the configured MaxConnsPerHost × upstream SETTINGS_MAX_CONCURRENT_STREAMS, " +
				"the transport is pool-saturated and new streams will serialize behind in-use " +
				"connections.",
			ConstLabels: constLabels,
		}, []string{"host"}),
	}
	registerer.MustRegister(
		m.streamsInFlight,
		m.providerCallsInFlight,
		m.streamFirstChunkLatency,
		m.streamErrorChunksForwarded,
		m.streamRetriesTotal,
		m.streamRetryBudgetAvailable,
		m.streamConcurrencyRejections,
		m.httpConnsInUse,
		m.pipelineStageElements,
		m.pipelineStageAudioBytes,
	)
	return m
}

// ObserveStreamErrorChunksForwarded records how many content chunks were
// forwarded downstream before a streaming request terminated with an
// error. Called exactly once per errored stream by the RunStreamingRequest
// relay goroutine, with the count of non-empty content chunks observed
// prior to the terminal error chunk. Nil-safe.
func (m *StreamMetrics) ObserveStreamErrorChunksForwarded(provider string, chunks int) {
	if m == nil {
		return
	}
	m.streamErrorChunksForwarded.WithLabelValues(provider).Observe(float64(chunks))
}

// HTTPConnsInUseInc increments the in-use HTTP connection gauge for a
// host. Called by the conn-tracking transport wrapper at the start of
// each RoundTrip. Nil-safe.
func (m *StreamMetrics) HTTPConnsInUseInc(host string) {
	if m == nil {
		return
	}
	m.httpConnsInUse.WithLabelValues(host).Inc()
}

// HTTPConnsInUseDec decrements the in-use HTTP connection gauge for a
// host. Called by the conn-tracking transport wrapper when a request's
// response body is closed (or when the RoundTrip errored before
// returning a body). Nil-safe.
func (m *StreamMetrics) HTTPConnsInUseDec(host string) {
	if m == nil {
		return
	}
	m.httpConnsInUse.WithLabelValues(host).Dec()
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
// "failed" (retryable transient failure that will be retried), "exhausted"
// (last attempt failed, no more retries), or "budget_exhausted" (retry
// was rejected because the per-provider retry budget had no tokens).
// Nil-safe.
func (m *StreamMetrics) RetryAttempt(provider, outcome string) {
	if m == nil {
		return
	}
	m.streamRetriesTotal.WithLabelValues(provider, outcome).Inc()
}

// ConcurrencyRejected records one streaming request rejected by the
// per-provider concurrency semaphore. Reason distinguishes between
// caller-initiated cancellation ("context_canceled") and deadline
// timeout ("deadline_exceeded"); sustained spikes in either indicate
// the semaphore limit is undersized or upstream is saturated. Nil-safe.
func (m *StreamMetrics) ConcurrencyRejected(provider, reason string) {
	if m == nil {
		return
	}
	m.streamConcurrencyRejections.WithLabelValues(provider, reason).Inc()
}

// ObserveRetryBudgetAvailable samples the current token count of a retry
// budget and publishes it to the stream_retry_budget_available gauge.
// Intended to be called whenever a retry attempts to acquire a token —
// the gauge then reflects the budget state at the moment of highest
// interest (right before a retry decision).
//
// A nil budget publishes 0, which is intentional: it lets operators
// distinguish "no budget configured" (gauge absent) from "budget fully
// drained" (gauge at 0) by gauge presence rather than value. Nil-safe
// on the receiver.
func (m *StreamMetrics) ObserveRetryBudgetAvailable(provider, host string, budget *RetryBudget) {
	if m == nil || budget == nil {
		return
	}
	m.streamRetryBudgetAvailable.WithLabelValues(provider, host).Set(budget.Available())
}

// PipelineStageElementInc increments the element counter for a pipeline
// stage. Called by the pipeline runner after each element flows through
// a stage's output channel. Nil-safe.
func (m *StreamMetrics) PipelineStageElementInc(stage string) {
	if m == nil {
		return
	}
	m.pipelineStageElements.WithLabelValues(stage).Inc()
}

// PipelineStageAudioBytesAdd adds to the audio byte counter for a
// pipeline stage. Called with the raw PCM byte count of each audio
// element that flows through the stage. Nil-safe.
func (m *StreamMetrics) PipelineStageAudioBytesAdd(stage string, bytes int) {
	if m == nil {
		return
	}
	m.pipelineStageAudioBytes.WithLabelValues(stage).Add(float64(bytes))
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
