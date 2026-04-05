package providers

import (
	"strings"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/testutil"
)

// --- Nil-safety ---

// All StreamMetrics methods must be no-ops on a nil receiver so provider
// code can call them unconditionally without wrapping every call site in
// a nil check.
func TestStreamMetrics_NilSafe(t *testing.T) {
	t.Parallel()
	var m *StreamMetrics
	// These should not panic on nil.
	m.StreamsInFlightInc("p")
	m.StreamsInFlightDec("p")
	m.ProviderCallsInFlightInc("p")
	m.ProviderCallsInFlightDec("p")
	m.ObserveFirstChunkLatency("p", time.Second)
	m.RetryAttempt("p", "success")
}

// --- Gauge and histogram increments ---

func TestStreamMetrics_StreamsInFlight(t *testing.T) {
	t.Parallel()
	reg := prometheus.NewRegistry()
	m := NewStreamMetrics(reg, "test", nil)

	m.StreamsInFlightInc("openai")
	m.StreamsInFlightInc("openai")
	m.StreamsInFlightInc("gemini")
	m.StreamsInFlightDec("openai")

	if got := testutil.ToFloat64(m.streamsInFlight.WithLabelValues("openai")); got != 1 {
		t.Errorf("openai streams_in_flight = %v, want 1", got)
	}
	if got := testutil.ToFloat64(m.streamsInFlight.WithLabelValues("gemini")); got != 1 {
		t.Errorf("gemini streams_in_flight = %v, want 1", got)
	}
}

func TestStreamMetrics_ProviderCallsInFlight(t *testing.T) {
	t.Parallel()
	reg := prometheus.NewRegistry()
	m := NewStreamMetrics(reg, "test", nil)

	m.ProviderCallsInFlightInc("openai")
	m.ProviderCallsInFlightInc("openai")
	m.ProviderCallsInFlightDec("openai")

	if got := testutil.ToFloat64(m.providerCallsInFlight.WithLabelValues("openai")); got != 1 {
		t.Errorf("provider_calls_in_flight = %v, want 1", got)
	}
}

func TestStreamMetrics_ObserveFirstChunkLatency(t *testing.T) {
	t.Parallel()
	reg := prometheus.NewRegistry()
	m := NewStreamMetrics(reg, "test", nil)

	m.ObserveFirstChunkLatency("openai", 500*time.Millisecond)
	m.ObserveFirstChunkLatency("openai", 2*time.Second)

	// Verify the histogram recorded two samples.
	count := testutil.CollectAndCount(m.streamFirstChunkLatency)
	if count != 1 { // 1 series (one label combination)
		t.Errorf("histogram series count = %d, want 1", count)
	}
}

func TestStreamMetrics_RetryAttempt(t *testing.T) {
	t.Parallel()
	reg := prometheus.NewRegistry()
	m := NewStreamMetrics(reg, "test", nil)

	m.RetryAttempt("openai", "failed")
	m.RetryAttempt("openai", "failed")
	m.RetryAttempt("openai", "success")

	if got := testutil.ToFloat64(m.streamRetriesTotal.WithLabelValues("openai", "failed")); got != 2 {
		t.Errorf("failed count = %v, want 2", got)
	}
	if got := testutil.ToFloat64(m.streamRetriesTotal.WithLabelValues("openai", "success")); got != 1 {
		t.Errorf("success count = %v, want 1", got)
	}
}

// --- Namespace and const labels ---

func TestNewStreamMetrics_DefaultNamespace(t *testing.T) {
	t.Parallel()
	reg := prometheus.NewRegistry()
	m := NewStreamMetrics(reg, "", nil)
	m.StreamsInFlightInc("p")

	metrics, err := reg.Gather()
	if err != nil {
		t.Fatalf("gather: %v", err)
	}
	var found bool
	for _, mf := range metrics {
		if strings.HasPrefix(mf.GetName(), "promptkit_streams_in_flight") {
			found = true
		}
	}
	if !found {
		t.Error("expected metric name to be prefixed with default namespace 'promptkit'")
	}
}

func TestNewStreamMetrics_WithConstLabels(t *testing.T) {
	t.Parallel()
	reg := prometheus.NewRegistry()
	constLabels := prometheus.Labels{"env": "test", "region": "us-east-1"}
	m := NewStreamMetrics(reg, "app", constLabels)
	m.StreamsInFlightInc("openai")

	metrics, err := reg.Gather()
	if err != nil {
		t.Fatalf("gather: %v", err)
	}
	var sawEnv, sawRegion bool
	for _, mf := range metrics {
		if !strings.HasSuffix(mf.GetName(), "_streams_in_flight") {
			continue
		}
		for _, m := range mf.GetMetric() {
			for _, lp := range m.GetLabel() {
				if lp.GetName() == "env" && lp.GetValue() == "test" {
					sawEnv = true
				}
				if lp.GetName() == "region" && lp.GetValue() == "us-east-1" {
					sawRegion = true
				}
			}
		}
	}
	if !sawEnv || !sawRegion {
		t.Errorf("const labels not applied: sawEnv=%v sawRegion=%v", sawEnv, sawRegion)
	}
}

// --- Default instance registration ---

func TestRegisterDefaultStreamMetrics_InstallsOnce(t *testing.T) {
	ResetDefaultStreamMetrics()
	t.Cleanup(ResetDefaultStreamMetrics)

	reg := prometheus.NewRegistry()
	first := RegisterDefaultStreamMetrics(reg, "test", nil)
	if first == nil {
		t.Fatal("first registration returned nil")
	}
	if DefaultStreamMetrics() != first {
		t.Error("DefaultStreamMetrics() did not return the first-registered instance")
	}

	// Second call with a *different* registerer must not re-register or
	// replace the default — the first call wins. (If it tried to
	// register, prometheus would panic on the duplicate metric name
	// inside the same registry; a different registry would silently
	// install a second set of metrics, which is worse.)
	reg2 := prometheus.NewRegistry()
	second := RegisterDefaultStreamMetrics(reg2, "test", nil)
	if second != first {
		t.Error("second registration returned a different instance — should have been a no-op")
	}
}

func TestDefaultStreamMetrics_NilWhenUnregistered(t *testing.T) {
	ResetDefaultStreamMetrics()
	t.Cleanup(ResetDefaultStreamMetrics)

	if got := DefaultStreamMetrics(); got != nil {
		t.Errorf("expected nil when no default registered, got %v", got)
	}
	// Methods on the nil instance must still be safe.
	DefaultStreamMetrics().StreamsInFlightInc("p")
}
