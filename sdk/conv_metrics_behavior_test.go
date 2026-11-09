package sdk

import (
	"context"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/AltairaLabs/PromptKit/runtime/v2/metrics"
)

// convMetricValue sums the samples of family name whose labels include want.
func convMetricValue(t *testing.T, reg *prometheus.Registry, name string, want map[string]string) float64 {
	t.Helper()
	fams, err := reg.Gather()
	require.NoError(t, err)
	var total float64
	for _, f := range fams {
		if f.GetName() != name {
			continue
		}
		for _, m := range f.GetMetric() {
			labels := map[string]string{}
			for _, l := range m.GetLabel() {
				labels[l.GetName()] = l.GetValue()
			}
			match := true
			for k, v := range want {
				match = match && labels[k] == v
			}
			if match {
				total += m.GetCounter().GetValue()
			}
		}
	}
	return total
}

// WithMetrics: a turn is counted on the caller's collector, with the
// provider's reported token usage and the caller's instance labels.
func TestConv_WithMetricsCountsTheTurn(t *testing.T) {
	reg := prometheus.NewRegistry()
	collector := metrics.NewCollector(metrics.CollectorOpts{
		Registerer: reg, Namespace: "convtest", InstanceLabels: []string{"env"}})
	w := newConvWire(t) // usage: 3 prompt tokens, 2 completion tokens
	conv := openConv(t, w, WithMetrics(collector, map[string]string{"env": "test"}))

	_, err := conv.Send(context.Background(), "hi")
	require.NoError(t, err)

	labels := map[string]string{"env": "test", "provider": "agent", "model": "gpt-4o-mini"}
	require.Eventually(t, func() bool {
		return convMetricValue(t, reg, "convtest_provider_output_tokens_total", labels) == 2
	}, 2*time.Second, 5*time.Millisecond)
	assert.InDelta(t, 3, convMetricValue(t, reg, "convtest_provider_input_tokens_total", labels), 0)
	labels["status"] = "success"
	assert.InDelta(t, 1, convMetricValue(t, reg, "convtest_provider_requests_total", labels), 0)
}
