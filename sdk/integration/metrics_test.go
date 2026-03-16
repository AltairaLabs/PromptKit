package integration

import (
	"context"
	"testing"
	"time"

	io_prometheus_client "github.com/prometheus/client_model/go"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/AltairaLabs/PromptKit/runtime/evals"
	_ "github.com/AltairaLabs/PromptKit/runtime/evals/handlers"
	"github.com/AltairaLabs/PromptKit/runtime/providers/mock"
	"github.com/AltairaLabs/PromptKit/sdk"
)

// ---------------------------------------------------------------------------
// 1.4 — Metrics collector integration
// ---------------------------------------------------------------------------

func TestMetrics_ProviderRequestsTotal(t *testing.T) {
	bus, collector, reg := testMetricsSetup(t)

	conv := openTestConv(t,
		sdk.WithEventBus(bus),
		sdk.WithMetrics(collector, nil),
	)

	_, err := conv.Send(context.Background(), "Hello")
	require.NoError(t, err)

	// Allow async event dispatch to metrics collector.
	waitForMetric(t, reg, "test_provider_requests_total", 2*time.Second)

	family := mustGetMetricFamily(t, reg, "test_provider_requests_total")
	val := counterValueWithLabels(family, map[string]string{"status": "success"})
	assert.Equal(t, 1.0, val, "provider_requests_total{status=success} should be 1")
}

func TestMetrics_ProviderRequestDuration(t *testing.T) {
	bus, collector, reg := testMetricsSetup(t)

	conv := openTestConv(t,
		sdk.WithEventBus(bus),
		sdk.WithMetrics(collector, nil),
	)

	_, err := conv.Send(context.Background(), "Hello")
	require.NoError(t, err)

	waitForMetric(t, reg, "test_provider_request_duration_seconds", 2*time.Second)

	family := mustGetMetricFamily(t, reg, "test_provider_request_duration_seconds")
	assert.Greater(t, histogramCount(family), uint64(0), "should have at least one observation")
}

func TestMetrics_TokenCounters(t *testing.T) {
	bus, collector, reg := testMetricsSetup(t)

	conv := openTestConv(t,
		sdk.WithEventBus(bus),
		sdk.WithMetrics(collector, nil),
	)

	_, err := conv.Send(context.Background(), "Hello")
	require.NoError(t, err)

	waitForMetric(t, reg, "test_provider_input_tokens_total", 2*time.Second)

	inputFamily := mustGetMetricFamily(t, reg, "test_provider_input_tokens_total")
	assert.Greater(t, counterValue(inputFamily), 0.0, "input tokens should be > 0")

	outputFamily := mustGetMetricFamily(t, reg, "test_provider_output_tokens_total")
	assert.Greater(t, counterValue(outputFamily), 0.0, "output tokens should be > 0")
}

func TestMetrics_PipelineDuration(t *testing.T) {
	bus, collector, reg := testMetricsSetup(t)

	conv := openTestConv(t,
		sdk.WithEventBus(bus),
		sdk.WithMetrics(collector, nil),
	)

	_, err := conv.Send(context.Background(), "Hello")
	require.NoError(t, err)

	waitForMetric(t, reg, "test_pipeline_duration_seconds", 2*time.Second)

	family := mustGetMetricFamily(t, reg, "test_pipeline_duration_seconds")
	assert.Greater(t, histogramCount(family), uint64(0), "should have at least one observation")
}

func TestMetrics_MultipleCallsAccumulate(t *testing.T) {
	bus, collector, reg := testMetricsSetup(t)

	conv := openTestConv(t,
		sdk.WithEventBus(bus),
		sdk.WithMetrics(collector, nil),
	)

	ctx := context.Background()
	_, err := conv.Send(ctx, "First")
	require.NoError(t, err)
	_, err = conv.Send(ctx, "Second")
	require.NoError(t, err)

	waitForMetric(t, reg, "test_provider_requests_total", 2*time.Second)

	family := mustGetMetricFamily(t, reg, "test_provider_requests_total")
	val := counterValueWithLabels(family, map[string]string{"status": "success"})
	assert.Equal(t, 2.0, val, "provider_requests_total should be 2 after two sends")
}

// ---------------------------------------------------------------------------
// 1.5 — Tool call metrics
// ---------------------------------------------------------------------------

func TestMetrics_ToolCallsTotal(t *testing.T) {
	bus, collector, reg := testMetricsSetup(t)

	repo := newTestTurnRepository()
	repo.addTurn("default", 1, mock.Turn{
		Type:    "tool_calls",
		Content: "Let me check the weather",
		ToolCalls: []mock.ToolCall{{
			Name:      "get_weather",
			Arguments: map[string]interface{}{"city": "London"},
		}},
	})
	repo.addTurn("default", 2, mock.Turn{
		Type:    "text",
		Content: "The weather in London is sunny.",
	})

	conv := openToolConv(t, repo, map[string]func(args map[string]any) (any, error){
		"get_weather": func(args map[string]any) (any, error) {
			return map[string]any{"temperature": 22}, nil
		},
	},
		sdk.WithEventBus(bus),
		sdk.WithMetrics(collector, nil),
	)

	_, err := conv.Send(context.Background(), "What is the weather?")
	require.NoError(t, err)

	waitForMetric(t, reg, "test_tool_calls_total", 2*time.Second)

	family := mustGetMetricFamily(t, reg, "test_tool_calls_total")
	val := counterValueWithLabels(family, map[string]string{"tool": "get_weather", "status": "success"})
	assert.Equal(t, 1.0, val, "tool_calls_total{tool=get_weather,status=success} should be 1")
}

func TestMetrics_ToolCallDuration(t *testing.T) {
	bus, collector, reg := testMetricsSetup(t)

	repo := newTestTurnRepository()
	repo.addTurn("default", 1, mock.Turn{
		Type:    "tool_calls",
		Content: "Let me check the weather",
		ToolCalls: []mock.ToolCall{{
			Name:      "get_weather",
			Arguments: map[string]interface{}{"city": "Paris"},
		}},
	})
	repo.addTurn("default", 2, mock.Turn{
		Type:    "text",
		Content: "Paris is cloudy.",
	})

	conv := openToolConv(t, repo, map[string]func(args map[string]any) (any, error){
		"get_weather": func(args map[string]any) (any, error) {
			return map[string]any{"temperature": 15}, nil
		},
	},
		sdk.WithEventBus(bus),
		sdk.WithMetrics(collector, nil),
	)

	_, err := conv.Send(context.Background(), "Weather in Paris?")
	require.NoError(t, err)

	waitForMetric(t, reg, "test_tool_call_duration_seconds", 2*time.Second)

	family := mustGetMetricFamily(t, reg, "test_tool_call_duration_seconds")
	assert.Greater(t, histogramCount(family), uint64(0), "should have at least one tool call duration observation")
}

// ---------------------------------------------------------------------------
// 1.6 — Eval result metrics through a real conversation
// ---------------------------------------------------------------------------

func TestMetrics_EvalResultMetrics(t *testing.T) {
	bus, collector, reg := testMetricsSetup(t)

	registry := evals.NewEvalTypeRegistry()
	runner := evals.NewEvalRunner(registry)

	conv := openTestConvWithPack(t, evalsPackWithMetricsJSON, "chat",
		sdk.WithEventBus(bus),
		sdk.WithMetrics(collector, nil),
		sdk.WithEvalRunner(runner),
	)

	_, err := conv.Send(context.Background(), "Hello")
	require.NoError(t, err)

	// Eval metrics are recorded asynchronously after pipeline completes.
	waitForMetric(t, reg, "test_eval_response_length", 3*time.Second)

	// Explicit metric: the pack defines response_length as a gauge.
	lengthFam := mustGetMetricFamily(t, reg, "test_eval_response_length")
	assert.Greater(t, gaugeValue(lengthFam), 0.0, "eval gauge metric should have a value > 0")
}

func TestMetrics_EvalResultMetrics_AutoGenerated(t *testing.T) {
	bus, collector, reg := testMetricsSetup(t)

	registry := evals.NewEvalTypeRegistry()
	runner := evals.NewEvalRunner(registry)

	// Use the evals pack WITHOUT explicit metrics — auto-generation should kick in.
	conv := openTestConvWithPack(t, evalsPackJSON, "chat",
		sdk.WithEventBus(bus),
		sdk.WithMetrics(collector, nil),
		sdk.WithEvalRunner(runner),
	)

	_, err := conv.Send(context.Background(), "Hello")
	require.NoError(t, err)

	// Auto-generated metrics use the eval ID as the metric name.
	waitForMetric(t, reg, "test_eval_check-response-length", 3*time.Second)

	family := mustGetMetricFamily(t, reg, "test_eval_check-response-length")
	assert.Greater(t, gaugeValue(family), 0.0,
		"auto-generated eval metric should have a value > 0")
}

// evalsPackWithMetricsJSON defines a pack with evals that have explicit metric definitions.
const evalsPackWithMetricsJSON = `{
	"id": "eval-metrics-test",
	"version": "1.0.0",
	"description": "Pack with evals and explicit metric definitions",
	"prompts": {
		"chat": {
			"id": "chat",
			"name": "Chat",
			"system_template": "You are a helpful assistant."
		}
	},
	"evals": [
		{
			"id": "check-response-length",
			"type": "min_length",
			"trigger": "every_turn",
			"params": { "min": 1 },
			"metric": {
				"name": "response_length",
				"type": "gauge"
			}
		}
	]
}`

// waitForMetric polls until the named metric appears in the registry or timeout expires.
func waitForMetric(t *testing.T, reg interface {
	Gather() ([]*io_prometheus_client.MetricFamily, error)
}, name string, timeout time.Duration) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		families, err := reg.Gather()
		if err == nil {
			for _, f := range families {
				if f.GetName() == name {
					return
				}
			}
		}
		time.Sleep(10 * time.Millisecond)
	}
}
