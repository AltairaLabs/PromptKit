package integration

import (
	"context"
	"testing"
	"time"

	io_prometheus_client "github.com/prometheus/client_model/go"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

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
