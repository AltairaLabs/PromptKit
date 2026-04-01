// Package integration contains black-box integration tests for the PromptKit SDK.
//
// These tests exercise the full SDK path — Open → Send → pipeline → events → metrics —
// using mock providers. They run in CI with no API keys or build tags required.
package integration

import (
	"encoding/json"
	"os"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	io_prometheus_client "github.com/prometheus/client_model/go"
	"github.com/stretchr/testify/require"

	"github.com/AltairaLabs/PromptKit/runtime/events"
	"github.com/AltairaLabs/PromptKit/runtime/metrics"
	"github.com/AltairaLabs/PromptKit/runtime/providers/mock"
	"github.com/AltairaLabs/PromptKit/sdk"
)

// -----------------------------------------------------------------------------
// Pack builders
// -----------------------------------------------------------------------------

// minimalPackJSON is the smallest valid pack that sdk.Open can load.
const minimalPackJSON = `{
	"id": "integration-test",
	"version": "1.0.0",
	"description": "Pack for SDK integration tests",
	"prompts": {
		"chat": {
			"id": "chat",
			"name": "Chat",
			"system_template": "You are a helpful assistant."
		}
	}
}`

// toolsPackJSON adds tool definitions to the minimal pack.
const toolsPackJSON = `{
	"id": "integration-test-tools",
	"version": "1.0.0",
	"description": "Pack with tools for SDK integration tests",
	"prompts": {
		"chat": {
			"id": "chat",
			"name": "Chat",
			"system_template": "You are a helpful assistant with tools."
		}
	},
	"tools": {
		"get_weather": {
			"name": "get_weather",
			"description": "Get weather for a city",
			"parameters": {
				"type": "object",
				"properties": {
					"city": {"type": "string"}
				},
				"required": ["city"]
			}
		}
	}
}`

// writePackFile writes pack JSON to a temp file and returns its path.
func writePackFile(t *testing.T, packJSON string) string {
	t.Helper()
	dir := t.TempDir()
	path := dir + "/test.pack.json"
	err := os.WriteFile(path, []byte(packJSON), 0o644)
	require.NoError(t, err)
	return path
}

// -----------------------------------------------------------------------------
// Conversation helpers
// -----------------------------------------------------------------------------

// openTestConv opens a conversation with a mock provider and common defaults.
// Extra options are appended after the defaults.
func openTestConv(t *testing.T, opts ...sdk.Option) *sdk.Conversation {
	t.Helper()
	return openTestConvWithPack(t, minimalPackJSON, "chat", opts...)
}

// openTestConvWithPack opens a conversation from custom pack JSON.
func openTestConvWithPack(t *testing.T, packJSON, promptName string, opts ...sdk.Option) *sdk.Conversation {
	t.Helper()
	packPath := writePackFile(t, packJSON)
	provider := mock.NewProvider("mock-test", "mock-model", false)

	defaults := []sdk.Option{
		sdk.WithProvider(provider),
		sdk.WithSkipSchemaValidation(),
	}
	allOpts := append(defaults, opts...)

	conv, err := sdk.Open(packPath, promptName, allOpts...)
	require.NoError(t, err)
	t.Cleanup(func() { _ = conv.Close() })
	return conv
}

// -----------------------------------------------------------------------------
// Event collector
// -----------------------------------------------------------------------------

// eventCollector subscribes to an EventBus and records every event in order.
type eventCollector struct {
	mu     sync.Mutex
	events []*events.Event
}

// newEventCollector creates a collector and subscribes it to bus.
func newEventCollector(bus *events.EventBus) *eventCollector {
	ec := &eventCollector{}
	bus.SubscribeAll(func(e *events.Event) {
		ec.mu.Lock()
		defer ec.mu.Unlock()
		ec.events = append(ec.events, e)
	})
	return ec
}

// all returns a snapshot of collected events.
func (ec *eventCollector) all() []*events.Event {
	ec.mu.Lock()
	defer ec.mu.Unlock()
	out := make([]*events.Event, len(ec.events))
	copy(out, ec.events)
	return out
}

// ofType returns events matching the given type.
func (ec *eventCollector) ofType(et events.EventType) []*events.Event {
	ec.mu.Lock()
	defer ec.mu.Unlock()
	var out []*events.Event
	for _, e := range ec.events {
		if e.Type == et {
			out = append(out, e)
		}
	}
	return out
}

// waitForType polls until at least one event of the given type is collected,
// or the timeout expires. Returns the matching events.
func (ec *eventCollector) waitForType(et events.EventType, timeout time.Duration) []*events.Event {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if evts := ec.ofType(et); len(evts) > 0 {
			return evts
		}
		time.Sleep(5 * time.Millisecond)
	}
	return ec.ofType(et)
}

// hasType returns true if at least one event of the given type was collected.
func (ec *eventCollector) hasType(et events.EventType) bool {
	return len(ec.ofType(et)) > 0
}

// typeSequence returns event types ordered by timestamp.
// Events are sorted by timestamp rather than listener invocation order
// because the event bus dispatches to listeners via goroutines, which
// can reorder delivery relative to publish order.
func (ec *eventCollector) typeSequence() []events.EventType {
	ec.mu.Lock()
	defer ec.mu.Unlock()
	sorted := make([]*events.Event, len(ec.events))
	copy(sorted, ec.events)
	slices.SortStableFunc(sorted, func(a, b *events.Event) int {
		return a.Timestamp.Compare(b.Timestamp)
	})
	seq := make([]events.EventType, len(sorted))
	for i, e := range sorted {
		seq[i] = e.Type
	}
	return seq
}

// waitForEvent polls until an event of the given type appears or timeout expires.
func (ec *eventCollector) waitForEvent(et events.EventType, timeout time.Duration) bool {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if ec.hasType(et) {
			return true
		}
		time.Sleep(5 * time.Millisecond)
	}
	return ec.hasType(et)
}

// waitForEvents polls until ALL specified event types have been collected or timeout expires.
func (ec *eventCollector) waitForEvents(types []events.EventType, timeout time.Duration) bool {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		allFound := true
		for _, et := range types {
			if !ec.hasType(et) {
				allFound = false
				break
			}
		}
		if allFound {
			return true
		}
		time.Sleep(5 * time.Millisecond)
	}
	return false
}

// -----------------------------------------------------------------------------
// Metrics helpers
// -----------------------------------------------------------------------------

// testMetricsSetup creates a fresh Prometheus registry, Collector, and EventBus
// wired together. Returns the bus (to pass as WithEventBus), the collector
// (to pass as WithMetrics), and the registry (to scrape assertions from).
func testMetricsSetup(t *testing.T) (*events.EventBus, *metrics.Collector, *prometheus.Registry) {
	t.Helper()
	reg := prometheus.NewRegistry()
	collector := metrics.NewCollector(metrics.CollectorOpts{
		Registerer: reg,
		Namespace:  "test",
	})
	bus := events.NewEventBus()
	t.Cleanup(func() {
		bus.Close()
	})

	// NOTE: Do NOT manually wire mc.OnEvent here — the SDK does it
	// internally when WithMetrics is passed to Open(). Double-wiring
	// causes double-counted metrics.

	return bus, collector, reg
}

// getMetricFamily retrieves a metric family by name from the registry.
func getMetricFamily(t *testing.T, reg *prometheus.Registry, name string) *io_prometheus_client.MetricFamily {
	t.Helper()
	families, err := reg.Gather()
	require.NoError(t, err)
	for _, f := range families {
		if f.GetName() == name {
			return f
		}
	}
	return nil
}

// mustGetMetricFamily retrieves a metric family, failing if not found.
func mustGetMetricFamily(t *testing.T, reg *prometheus.Registry, name string) *io_prometheus_client.MetricFamily {
	t.Helper()
	f := getMetricFamily(t, reg, name)
	require.NotNilf(t, f, "metric %q not found in registry", name)
	return f
}

// counterValue returns the sum of all samples for a counter metric.
func counterValue(family *io_prometheus_client.MetricFamily) float64 {
	var total float64
	for _, m := range family.GetMetric() {
		if c := m.GetCounter(); c != nil {
			total += c.GetValue()
		}
	}
	return total
}

// histogramCount returns the total observation count across all label sets.
func histogramCount(family *io_prometheus_client.MetricFamily) uint64 {
	var total uint64
	for _, m := range family.GetMetric() {
		if h := m.GetHistogram(); h != nil {
			total += h.GetSampleCount()
		}
	}
	return total
}

// counterValueWithLabels finds a counter metric matching the given label pairs.
func counterValueWithLabels(family *io_prometheus_client.MetricFamily, labels map[string]string) float64 {
	for _, m := range family.GetMetric() {
		if matchLabels(m, labels) {
			if c := m.GetCounter(); c != nil {
				return c.GetValue()
			}
		}
	}
	return 0
}

// matchLabels checks if a metric has all the given label key=value pairs.
func matchLabels(m *io_prometheus_client.Metric, labels map[string]string) bool {
	metricLabels := make(map[string]string)
	for _, lp := range m.GetLabel() {
		metricLabels[lp.GetName()] = lp.GetValue()
	}
	for k, v := range labels {
		if metricLabels[k] != v {
			return false
		}
	}
	return true
}

// gaugeValue returns the value of the first gauge metric in the family.
func gaugeValue(family *io_prometheus_client.MetricFamily) float64 {
	for _, m := range family.GetMetric() {
		if g := m.GetGauge(); g != nil {
			return g.GetValue()
		}
	}
	return 0
}

// dumpRegistry prints all metrics in the registry (useful for debugging).
func dumpRegistry(t *testing.T, reg *prometheus.Registry) {
	t.Helper()
	families, err := reg.Gather()
	require.NoError(t, err)
	for _, f := range families {
		data, _ := json.MarshalIndent(f, "", "  ")
		t.Logf("metric: %s\n%s", f.GetName(), string(data))
	}
}
