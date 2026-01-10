// Package prometheus provides Prometheus metrics exporters for PromptKit pipelines.
package prometheus

import (
	"github.com/prometheus/client_golang/prometheus"
)

const namespace = "promptkit"

var (
	// stageDuration is a histogram of stage processing duration in seconds.
	stageDuration = prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Namespace: namespace,
			Name:      "stage_duration_seconds",
			Help:      "Histogram of stage processing duration in seconds",
			Buckets:   prometheus.DefBuckets, // .005, .01, .025, .05, .1, .25, .5, 1, 2.5, 5, 10
		},
		[]string{"stage", "stage_type"},
	)

	// stageElementsTotal is a counter of elements processed by stage.
	stageElementsTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Namespace: namespace,
			Name:      "stage_elements_total",
			Help:      "Total number of elements processed by stage",
		},
		[]string{"stage", "status"}, // status: success, error
	)

	// pipelinesActive is a gauge of currently active pipelines.
	pipelinesActive = prometheus.NewGauge(
		prometheus.GaugeOpts{
			Namespace: namespace,
			Name:      "pipelines_active",
			Help:      "Number of currently active pipelines",
		},
	)

	// pipelineDuration is a histogram of total pipeline execution duration.
	pipelineDuration = prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Namespace: namespace,
			Name:      "pipeline_duration_seconds",
			Help:      "Histogram of total pipeline execution duration in seconds",
			Buckets:   []float64{.1, .25, .5, 1, 2.5, 5, 10, 30, 60, 120},
		},
		[]string{"status"}, // status: success, error
	)

	// providerRequestDuration is a histogram of LLM provider API call duration.
	providerRequestDuration = prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Namespace: namespace,
			Name:      "provider_request_duration_seconds",
			Help:      "Duration of LLM provider API calls in seconds",
			Buckets:   []float64{.1, .25, .5, 1, 2.5, 5, 10, 30, 60},
		},
		[]string{"provider", "model"},
	)

	// providerRequestsTotal is a counter of provider API calls.
	providerRequestsTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Namespace: namespace,
			Name:      "provider_requests_total",
			Help:      "Total number of provider API calls",
		},
		[]string{"provider", "model", "status"}, // status: success, error
	)

	// providerTokensTotal is a counter of tokens consumed by provider calls.
	providerTokensTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Namespace: namespace,
			Name:      "provider_tokens_total",
			Help:      "Total tokens consumed by provider calls",
		},
		[]string{"provider", "model", "type"}, // type: input, output, cached
	)

	// providerCostTotal is a counter of total cost from provider calls.
	providerCostTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Namespace: namespace,
			Name:      "provider_cost_total",
			Help:      "Total cost in USD from provider calls",
		},
		[]string{"provider", "model"},
	)

	// toolCallDuration is a histogram of tool call duration.
	toolCallDuration = prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Namespace: namespace,
			Name:      "tool_call_duration_seconds",
			Help:      "Duration of tool calls in seconds",
			Buckets:   []float64{.01, .025, .05, .1, .25, .5, 1, 2.5, 5, 10},
		},
		[]string{"tool"},
	)

	// toolCallsTotal is a counter of tool calls.
	toolCallsTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Namespace: namespace,
			Name:      "tool_calls_total",
			Help:      "Total number of tool calls",
		},
		[]string{"tool", "status"}, // status: success, error
	)

	// validationDuration is a histogram of validation duration.
	validationDuration = prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Namespace: namespace,
			Name:      "validation_duration_seconds",
			Help:      "Duration of validation checks in seconds",
			Buckets:   []float64{.001, .005, .01, .025, .05, .1, .25, .5, 1},
		},
		[]string{"validator", "validator_type"},
	)

	// validationsTotal is a counter of validation checks.
	validationsTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Namespace: namespace,
			Name:      "validations_total",
			Help:      "Total number of validation checks",
		},
		[]string{"validator", "validator_type", "status"}, // status: passed, failed
	)

	// allMetrics is a list of all metrics for registration.
	allMetrics = []prometheus.Collector{
		stageDuration,
		stageElementsTotal,
		pipelinesActive,
		pipelineDuration,
		providerRequestDuration,
		providerRequestsTotal,
		providerTokensTotal,
		providerCostTotal,
		toolCallDuration,
		toolCallsTotal,
		validationDuration,
		validationsTotal,
	}
)

// RecordStageDuration records the duration of a stage.
func RecordStageDuration(stageName, stageType string, durationSeconds float64) {
	stageDuration.WithLabelValues(stageName, stageType).Observe(durationSeconds)
}

// RecordStageElement records a processed element.
func RecordStageElement(stageName, status string) {
	stageElementsTotal.WithLabelValues(stageName, status).Inc()
}

// RecordPipelineStart records a pipeline start.
func RecordPipelineStart() {
	pipelinesActive.Inc()
}

// RecordPipelineEnd records a pipeline completion.
func RecordPipelineEnd(status string, durationSeconds float64) {
	pipelinesActive.Dec()
	pipelineDuration.WithLabelValues(status).Observe(durationSeconds)
}

// RecordProviderRequest records a provider API call.
func RecordProviderRequest(provider, model, status string, durationSeconds float64) {
	providerRequestDuration.WithLabelValues(provider, model).Observe(durationSeconds)
	providerRequestsTotal.WithLabelValues(provider, model, status).Inc()
}

// RecordProviderTokens records token consumption.
func RecordProviderTokens(provider, model string, inputTokens, outputTokens, cachedTokens int) {
	if inputTokens > 0 {
		providerTokensTotal.WithLabelValues(provider, model, "input").Add(float64(inputTokens))
	}
	if outputTokens > 0 {
		providerTokensTotal.WithLabelValues(provider, model, "output").Add(float64(outputTokens))
	}
	if cachedTokens > 0 {
		providerTokensTotal.WithLabelValues(provider, model, "cached").Add(float64(cachedTokens))
	}
}

// RecordProviderCost records cost from a provider call.
func RecordProviderCost(provider, model string, cost float64) {
	if cost > 0 {
		providerCostTotal.WithLabelValues(provider, model).Add(cost)
	}
}

// RecordToolCall records a tool call.
func RecordToolCall(toolName, status string, durationSeconds float64) {
	toolCallDuration.WithLabelValues(toolName).Observe(durationSeconds)
	toolCallsTotal.WithLabelValues(toolName, status).Inc()
}

// RecordValidation records a validation check.
func RecordValidation(validator, validatorType, status string, durationSeconds float64) {
	validationDuration.WithLabelValues(validator, validatorType).Observe(durationSeconds)
	validationsTotal.WithLabelValues(validator, validatorType, status).Inc()
}
