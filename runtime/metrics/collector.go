// Package metrics provides the unified PromptKit metrics collector.
//
// The Collector records both pipeline operational metrics (from EventBus events)
// and eval result metrics (from pack-defined MetricDefs) into a Prometheus
// registry. It replaces the dead-code runtime/metrics/prometheus package and
// the hand-rolled Prometheus text writer in runtime/evals/metrics.go.
//
// Usage:
//
//	reg := prometheus.NewRegistry()
//	collector := metrics.NewCollector(metrics.CollectorOpts{
//	    Registerer: reg,
//	    Namespace:  "myapp",
//	    ConstLabels: prometheus.Labels{"env": "prod"},
//	    InstanceLabels: []string{"tenant"},
//	})
//
//	// Per-conversation binding:
//	ctx := collector.Bind(prometheus.Labels{"tenant": "acme"})
//	bus.SubscribeAll(ctx.OnEvent)     // pipeline metrics
//	recorder := ctx                    // evals.MetricRecorder
package metrics

import (
	"fmt"
	"sort"
	"sync"

	"github.com/prometheus/client_golang/prometheus"

	"github.com/AltairaLabs/PromptKit/runtime/evals"
	"github.com/AltairaLabs/PromptKit/runtime/events"
	"github.com/AltairaLabs/PromptKit/runtime/logger"
)

// Status constants for metric labels.
const (
	statusSuccess = "success"
	statusError   = "error"
	statusPassed  = "passed"
	statusFailed  = "failed"

	defaultNamespace = "promptkit"
)

// Default histogram buckets for specific metric types.
var (
	pipelineBuckets   = []float64{.1, .25, .5, 1, 2.5, 5, 10, 30, 60, 120}
	providerBuckets   = []float64{.1, .25, .5, 1, 2.5, 5, 10, 30, 60}
	toolCallBuckets   = []float64{.01, .025, .05, .1, .25, .5, 1, 2.5, 5, 10}
	validationBuckets = []float64{.001, .005, .01, .025, .05, .1, .25, .5, 1}
)

// CollectorOpts configures a Collector.
type CollectorOpts struct {
	// Registerer is the Prometheus registerer to register metrics into.
	// If nil, prometheus.DefaultRegisterer is used.
	// If the value is a *prometheus.Registry, it is accessible via
	// Collector.Registry() for use with promhttp.HandlerFor().
	Registerer prometheus.Registerer

	// Namespace overrides the metric name prefix (default: "promptkit").
	Namespace string

	// ConstLabels are added to every metric as constant label pairs.
	// These must be truly constant for the lifetime of the process —
	// they are baked into the metric descriptor at registration time.
	// Use for process-level dimensions: env, region, service_name.
	ConstLabels prometheus.Labels

	// InstanceLabels are label NAMES that vary per conversation.
	// They become additional dimensions on every Vec metric, allowing
	// multiple conversations to share one Collector without registration
	// conflicts. Values are provided per-conversation via Bind().
	// Use for: tenant, prompt_name, conversation_type.
	InstanceLabels []string

	// DisablePipelineMetrics disables operational metrics (provider, tool, pipeline,
	// validation). Set this to true when you only need eval result metrics — for
	// example, in standalone eval workers that use sdk.Evaluate() without a live
	// pipeline. See also NewEvalOnlyCollector() for a convenience constructor.
	DisablePipelineMetrics bool

	// DisableEvalMetrics disables eval result metrics.
	DisableEvalMetrics bool
}

// Collector records both pipeline and eval metrics into a Prometheus registry.
// Created once per process via NewCollector(). Shared across conversations.
//
// Pipeline events are recorded via MetricContext.OnEvent (implements events.Listener).
// Eval results are recorded via MetricContext.Record (implements evals.MetricRecorder).
//
// Instance labels (varying per conversation) are passed via a MetricContext
// that the SDK creates per conversation using Bind().
type Collector struct {
	// Pre-declared pipeline metrics (registered once at creation)
	pipelineDuration          *prometheus.HistogramVec
	providerRequestDuration   *prometheus.HistogramVec
	providerRequestsTotal     *prometheus.CounterVec
	providerInputTokensTotal  *prometheus.CounterVec
	providerOutputTokensTotal *prometheus.CounterVec
	providerCachedTokensTotal *prometheus.CounterVec
	providerCostTotal         *prometheus.CounterVec
	toolCallDuration          *prometheus.HistogramVec
	toolCallsTotal            *prometheus.CounterVec
	validationDuration        *prometheus.HistogramVec
	validationsTotal          *prometheus.CounterVec

	// Dynamic eval metrics (created on first observation)
	evalMetrics map[string]evalMetricEntry
	mu          sync.RWMutex

	// Config
	namespace      string
	instanceLabels []string // sorted label names that vary per conversation
	constLabels    prometheus.Labels
	registerer     prometheus.Registerer
	registry       *prometheus.Registry // non-nil only when registerer is a *Registry

	disablePipeline bool
	disableEval     bool
}

// evalMetricEntry holds a registered prometheus collector for a dynamic eval metric.
type evalMetricEntry struct {
	metricType evals.MetricType
	gauge      *prometheus.GaugeVec
	counter    *prometheus.CounterVec
	histogram  *prometheus.HistogramVec
}

// NewCollector creates a unified metrics collector and registers pipeline
// metrics into the provided Registerer (or prometheus.DefaultRegisterer).
func NewCollector(opts CollectorOpts) *Collector {
	registerer := opts.Registerer
	if registerer == nil {
		registerer = prometheus.DefaultRegisterer
	}

	ns := opts.Namespace
	if ns == "" {
		ns = defaultNamespace
	}

	// Sort instance labels for deterministic ordering.
	instanceLabels := make([]string, len(opts.InstanceLabels))
	copy(instanceLabels, opts.InstanceLabels)
	sort.Strings(instanceLabels)

	reg, _ := registerer.(*prometheus.Registry)

	c := &Collector{
		namespace:       ns,
		instanceLabels:  instanceLabels,
		constLabels:     opts.ConstLabels,
		registerer:      registerer,
		registry:        reg,
		evalMetrics:     make(map[string]evalMetricEntry),
		disablePipeline: opts.DisablePipelineMetrics,
		disableEval:     opts.DisableEvalMetrics,
	}

	if !opts.DisablePipelineMetrics {
		c.registerPipelineMetrics()
	}

	return c
}

// NewEvalOnlyCollector creates a Collector that only records eval result metrics,
// with pipeline operational metrics disabled. This is a convenience wrapper for:
//
//	metrics.NewCollector(metrics.CollectorOpts{
//	    Registerer:             reg,
//	    DisablePipelineMetrics: true,
//	    ...
//	})
//
// Use this for standalone eval workers (e.g. sdk.Evaluate()) that don't run a
// live pipeline and therefore don't need provider, tool, or pipeline metrics.
func NewEvalOnlyCollector(opts CollectorOpts) *Collector {
	opts.DisablePipelineMetrics = true
	return NewCollector(opts)
}

// Registry returns the underlying *prometheus.Registry if one was provided,
// or nil if a non-Registry Registerer was used.
func (c *Collector) Registry() *prometheus.Registry { return c.registry }

// Bind creates a MetricContext for a specific conversation, binding
// instance label values. The returned context implements both
// events.Listener (via OnEvent) and evals.MetricRecorder (via Record).
//
// Label key ordering in the map does not matter — the Collector sorts
// InstanceLabels internally for deterministic Prometheus label ordering.
// Only the keys need to match; values are looked up by key, not position.
//
// If the Collector has no InstanceLabels, pass nil.
func (c *Collector) Bind(instanceLabels map[string]string) *MetricContext {
	return &MetricContext{collector: c, labels: instanceLabels}
}

// registerPipelineMetrics creates and registers all pre-declared pipeline metrics.
func (c *Collector) registerPipelineMetrics() {
	c.pipelineDuration = c.mustRegisterHistogramVec(
		"pipeline_duration_seconds",
		"Histogram of total pipeline execution duration in seconds",
		pipelineBuckets,
		[]string{"status"},
	)

	c.providerRequestDuration = c.mustRegisterHistogramVec(
		"provider_request_duration_seconds",
		"Duration of LLM provider API calls in seconds",
		providerBuckets,
		[]string{"provider", "model"},
	)

	c.providerRequestsTotal = c.mustRegisterCounterVec(
		"provider_requests_total",
		"Total number of provider API calls",
		[]string{"provider", "model", "status"},
	)

	c.providerInputTokensTotal = c.mustRegisterCounterVec(
		"provider_input_tokens_total",
		"Total input tokens sent to provider calls",
		[]string{"provider", "model"},
	)

	c.providerOutputTokensTotal = c.mustRegisterCounterVec(
		"provider_output_tokens_total",
		"Total output tokens received from provider calls",
		[]string{"provider", "model"},
	)

	c.providerCachedTokensTotal = c.mustRegisterCounterVec(
		"provider_cached_tokens_total",
		"Total cached tokens in provider calls",
		[]string{"provider", "model"},
	)

	c.providerCostTotal = c.mustRegisterCounterVec(
		"provider_cost_total",
		"Total cost in USD from provider calls",
		[]string{"provider", "model"},
	)

	c.toolCallDuration = c.mustRegisterHistogramVec(
		"tool_call_duration_seconds",
		"Duration of tool calls in seconds",
		toolCallBuckets,
		[]string{"tool"},
	)

	c.toolCallsTotal = c.mustRegisterCounterVec(
		"tool_calls_total",
		"Total number of tool calls",
		[]string{"tool", "status"},
	)

	c.validationDuration = c.mustRegisterHistogramVec(
		"validation_duration_seconds",
		"Duration of validation checks in seconds",
		validationBuckets,
		[]string{"validator", "validator_type"},
	)

	c.validationsTotal = c.mustRegisterCounterVec(
		"validations_total",
		"Total number of validation checks",
		[]string{"validator", "validator_type", "status"},
	)
}

// allLabels returns instance labels + event-level labels as the full label set
// for a Vec metric. Instance labels come first (sorted), then event labels in order.
func (c *Collector) allLabels(eventLabels []string) []string {
	all := make([]string, 0, len(c.instanceLabels)+len(eventLabels))
	all = append(all, c.instanceLabels...)
	all = append(all, eventLabels...)
	return all
}

func (c *Collector) mustRegisterHistogramVec(
	name, help string, buckets []float64, eventLabels []string,
) *prometheus.HistogramVec {
	vec := prometheus.NewHistogramVec(prometheus.HistogramOpts{
		Namespace:   c.namespace,
		Name:        name,
		Help:        help,
		Buckets:     buckets,
		ConstLabels: c.constLabels,
	}, c.allLabels(eventLabels))
	c.registerer.MustRegister(vec)
	return vec
}

func (c *Collector) mustRegisterCounterVec(
	name, help string, eventLabels []string,
) *prometheus.CounterVec {
	vec := prometheus.NewCounterVec(prometheus.CounterOpts{
		Namespace:   c.namespace,
		Name:        name,
		Help:        help,
		ConstLabels: c.constLabels,
	}, c.allLabels(eventLabels))
	c.registerer.MustRegister(vec)
	return vec
}

// MetricContext is a per-conversation handle that carries instance label
// values. It implements events.Listener (via OnEvent) and evals.MetricRecorder
// (via Record), forwarding observations to the shared Collector with bound labels.
type MetricContext struct {
	collector *Collector
	labels    map[string]string // instance label values for this conversation
}

// Compile-time interface checks.
var _ evals.MetricRecorder = (*MetricContext)(nil)

// instanceLabelValues returns the instance label values in the same sorted
// order as Collector.instanceLabels.
func (mc *MetricContext) instanceLabelValues() []string {
	if len(mc.collector.instanceLabels) == 0 {
		return nil
	}
	vals := make([]string, len(mc.collector.instanceLabels))
	for i, name := range mc.collector.instanceLabels {
		vals[i] = mc.labels[name]
	}
	return vals
}

// labelValues returns instance label values + event label values as a single
// slice for WithLabelValues() calls.
func (mc *MetricContext) labelValues(eventValues ...string) []string {
	inst := mc.instanceLabelValues()
	if len(inst) == 0 {
		return eventValues
	}
	all := make([]string, 0, len(inst)+len(eventValues))
	all = append(all, inst...)
	all = append(all, eventValues...)
	return all
}

// OnEvent processes a pipeline event and records relevant metrics.
// This method is designed to be used with EventBus.SubscribeAll.
func (mc *MetricContext) OnEvent(event *events.Event) {
	if mc.collector.disablePipeline {
		return
	}

	//exhaustive:ignore
	switch event.Type {
	case events.EventPipelineCompleted:
		mc.handlePipelineCompleted(event)
	case events.EventPipelineFailed:
		mc.handlePipelineFailed(event)
	case events.EventProviderCallCompleted:
		mc.handleProviderCallCompleted(event)
	case events.EventProviderCallFailed:
		mc.handleProviderCallFailed(event)
	case events.EventToolCallCompleted:
		mc.handleToolCallCompleted(event)
	case events.EventToolCallFailed:
		mc.handleToolCallFailed(event)
	case events.EventValidationPassed:
		mc.handleValidationPassed(event)
	case events.EventValidationFailed:
		mc.handleValidationFailed(event)
	}
}

func (mc *MetricContext) handlePipelineCompleted(event *events.Event) {
	data, ok := event.Data.(*events.PipelineCompletedData)
	if !ok {
		return
	}
	mc.collector.pipelineDuration.WithLabelValues(
		mc.labelValues(statusSuccess)...,
	).Observe(data.Duration.Seconds())
}

func (mc *MetricContext) handlePipelineFailed(event *events.Event) {
	data, ok := event.Data.(*events.PipelineFailedData)
	if !ok {
		return
	}
	mc.collector.pipelineDuration.WithLabelValues(
		mc.labelValues(statusError)...,
	).Observe(data.Duration.Seconds())
}

func (mc *MetricContext) handleProviderCallCompleted(event *events.Event) {
	data, ok := event.Data.(*events.ProviderCallCompletedData)
	if !ok {
		return
	}
	mc.collector.providerRequestDuration.WithLabelValues(
		mc.labelValues(data.Provider, data.Model)...,
	).Observe(data.Duration.Seconds())

	mc.collector.providerRequestsTotal.WithLabelValues(
		mc.labelValues(data.Provider, data.Model, statusSuccess)...,
	).Inc()

	if data.InputTokens > 0 {
		mc.collector.providerInputTokensTotal.WithLabelValues(
			mc.labelValues(data.Provider, data.Model)...,
		).Add(float64(data.InputTokens))
	}
	if data.OutputTokens > 0 {
		mc.collector.providerOutputTokensTotal.WithLabelValues(
			mc.labelValues(data.Provider, data.Model)...,
		).Add(float64(data.OutputTokens))
	}
	if data.CachedTokens > 0 {
		mc.collector.providerCachedTokensTotal.WithLabelValues(
			mc.labelValues(data.Provider, data.Model)...,
		).Add(float64(data.CachedTokens))
	}

	if data.Cost > 0 {
		mc.collector.providerCostTotal.WithLabelValues(
			mc.labelValues(data.Provider, data.Model)...,
		).Add(data.Cost)
	}
}

func (mc *MetricContext) handleProviderCallFailed(event *events.Event) {
	data, ok := event.Data.(*events.ProviderCallFailedData)
	if !ok {
		return
	}
	mc.collector.providerRequestDuration.WithLabelValues(
		mc.labelValues(data.Provider, data.Model)...,
	).Observe(data.Duration.Seconds())

	mc.collector.providerRequestsTotal.WithLabelValues(
		mc.labelValues(data.Provider, data.Model, statusError)...,
	).Inc()
}

func (mc *MetricContext) handleToolCallCompleted(event *events.Event) {
	data, ok := event.Data.(*events.ToolCallCompletedData)
	if !ok {
		return
	}
	status := statusSuccess
	if data.Status == statusError {
		status = statusError
	}
	mc.collector.toolCallDuration.WithLabelValues(
		mc.labelValues(data.ToolName)...,
	).Observe(data.Duration.Seconds())

	mc.collector.toolCallsTotal.WithLabelValues(
		mc.labelValues(data.ToolName, status)...,
	).Inc()
}

func (mc *MetricContext) handleToolCallFailed(event *events.Event) {
	data, ok := event.Data.(*events.ToolCallFailedData)
	if !ok {
		return
	}
	mc.collector.toolCallDuration.WithLabelValues(
		mc.labelValues(data.ToolName)...,
	).Observe(data.Duration.Seconds())

	mc.collector.toolCallsTotal.WithLabelValues(
		mc.labelValues(data.ToolName, statusError)...,
	).Inc()
}

func (mc *MetricContext) handleValidationPassed(event *events.Event) {
	data, ok := event.Data.(*events.ValidationPassedData)
	if !ok {
		return
	}
	mc.collector.validationDuration.WithLabelValues(
		mc.labelValues(data.ValidatorName, data.ValidatorType)...,
	).Observe(data.Duration.Seconds())

	mc.collector.validationsTotal.WithLabelValues(
		mc.labelValues(data.ValidatorName, data.ValidatorType, statusPassed)...,
	).Inc()
}

func (mc *MetricContext) handleValidationFailed(event *events.Event) {
	data, ok := event.Data.(*events.ValidationFailedData)
	if !ok {
		return
	}
	mc.collector.validationDuration.WithLabelValues(
		mc.labelValues(data.ValidatorName, data.ValidatorType)...,
	).Observe(data.Duration.Seconds())

	mc.collector.validationsTotal.WithLabelValues(
		mc.labelValues(data.ValidatorName, data.ValidatorType, statusFailed)...,
	).Inc()
}

// Record implements evals.MetricRecorder. It records an eval result into the
// Prometheus registry using the metric definition from the pack.
//
//nolint:gocritic // EvalResult passed by value to satisfy MetricRecorder interface
func (mc *MetricContext) Record(result evals.EvalResult, metric *evals.MetricDef) error {
	if mc.collector.disableEval {
		return nil
	}
	if metric == nil {
		return fmt.Errorf("nil metric definition")
	}

	c := mc.collector
	name := prefixedName(c.namespace+"_eval", metric.Name)
	key := evalMetricKey(name, metric)

	c.mu.RLock()
	entry, ok := c.evalMetrics[key]
	c.mu.RUnlock()

	if !ok {
		var err error
		entry, err = c.registerEvalMetric(key, name, metric)
		if err != nil {
			return err
		}
	}

	value := evals.ExtractValue(result, metric)
	labelValues := mc.evalLabelValues(metric)

	switch metric.Type {
	case evals.MetricGauge:
		entry.gauge.WithLabelValues(labelValues...).Set(value)
	case evals.MetricCounter:
		entry.counter.WithLabelValues(labelValues...).Inc()
	case evals.MetricHistogram:
		entry.histogram.WithLabelValues(labelValues...).Observe(value)
	case evals.MetricBoolean:
		v := 0.0
		if result.Passed { //nolint:staticcheck // Passed is deprecated but still the source for boolean metrics
			v = 1.0
		}
		entry.gauge.WithLabelValues(labelValues...).Set(v)
	default:
		return fmt.Errorf("unknown metric type: %q", metric.Type)
	}

	return nil
}

// evalLabelValues returns instance label values + pack-author label values
// for an eval metric observation.
func (mc *MetricContext) evalLabelValues(metric *evals.MetricDef) []string {
	// Pack-author label names sorted for deterministic ordering.
	defLabelNames := sortedKeys(metric.Labels)

	vals := make([]string, 0, len(mc.collector.instanceLabels)+len(defLabelNames))
	// Instance labels first (sorted).
	for _, name := range mc.collector.instanceLabels {
		vals = append(vals, mc.labels[name])
	}
	// Pack-author labels.
	for _, name := range defLabelNames {
		vals = append(vals, metric.Labels[name])
	}
	return vals
}

// registerEvalMetric creates and registers a prometheus metric for a dynamic eval
// metric definition. Uses double-checked locking.
func (c *Collector) registerEvalMetric(
	key, name string, metric *evals.MetricDef,
) (evalMetricEntry, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	// Double-check after acquiring write lock.
	if entry, ok := c.evalMetrics[key]; ok {
		return entry, nil
	}

	defLabelNames := sortedKeys(metric.Labels)
	allLabelNames := c.allLabels(defLabelNames)

	var entry evalMetricEntry
	entry.metricType = metric.Type

	switch metric.Type {
	case evals.MetricGauge, evals.MetricBoolean:
		vec := prometheus.NewGaugeVec(prometheus.GaugeOpts{
			Name:        name,
			Help:        fmt.Sprintf("Eval metric: %s", metric.Name),
			ConstLabels: c.constLabels,
		}, allLabelNames)
		if err := c.registerer.Register(vec); err != nil {
			logger.Warn("failed to register eval gauge metric",
				"metric", name, "error", err)
			return evalMetricEntry{}, err
		}
		entry.gauge = vec

	case evals.MetricCounter:
		vec := prometheus.NewCounterVec(prometheus.CounterOpts{
			Name:        name,
			Help:        fmt.Sprintf("Eval metric: %s", metric.Name),
			ConstLabels: c.constLabels,
		}, allLabelNames)
		if err := c.registerer.Register(vec); err != nil {
			logger.Warn("failed to register eval counter metric",
				"metric", name, "error", err)
			return evalMetricEntry{}, err
		}
		entry.counter = vec

	case evals.MetricHistogram:
		buckets := prometheus.DefBuckets
		vec := prometheus.NewHistogramVec(prometheus.HistogramOpts{
			Name:        name,
			Help:        fmt.Sprintf("Eval metric: %s", metric.Name),
			Buckets:     buckets,
			ConstLabels: c.constLabels,
		}, allLabelNames)
		if err := c.registerer.Register(vec); err != nil {
			logger.Warn("failed to register eval histogram metric",
				"metric", name, "error", err)
			return evalMetricEntry{}, err
		}
		entry.histogram = vec

	default:
		return evalMetricEntry{}, fmt.Errorf("unknown metric type: %q", metric.Type)
	}

	c.evalMetrics[key] = entry
	return entry, nil
}

// prefixedName prepends the namespace if not already prefixed.
func prefixedName(namespace, name string) string {
	prefix := namespace + "_"
	if len(name) >= len(prefix) && name[:len(prefix)] == prefix {
		return name
	}
	return prefix + name
}

// evalMetricKey builds a map key for an eval metric from its name and label schema.
func evalMetricKey(name string, metric *evals.MetricDef) string {
	return name + "|" + string(metric.Type)
}

// sortedKeys returns the keys of a map in sorted order.
func sortedKeys(m map[string]string) []string {
	if len(m) == 0 {
		return nil
	}
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}
