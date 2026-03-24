package metrics

import (
	"context"
	"testing"

	"github.com/prometheus/client_golang/prometheus"
	"go.opentelemetry.io/otel/trace"
)

func TestTraceExemplar_NilContext(t *testing.T) {
	//nolint:staticcheck // intentional nil context for testing
	if got := traceExemplar(nil); got != nil {
		t.Errorf("expected nil, got %v", got)
	}
}

func TestTraceExemplar_NoSpan(t *testing.T) {
	if got := traceExemplar(context.Background()); got != nil {
		t.Errorf("expected nil for context without span, got %v", got)
	}
}

func TestTraceExemplar_ValidSpan(t *testing.T) {
	traceID, _ := trace.TraceIDFromHex("0102030405060708090a0b0c0d0e0f10")
	sc := trace.NewSpanContext(trace.SpanContextConfig{
		TraceID:    traceID,
		SpanID:     trace.SpanID{1},
		TraceFlags: trace.FlagsSampled,
	})
	ctx := trace.ContextWithSpanContext(context.Background(), sc)

	got := traceExemplar(ctx)
	if got == nil {
		t.Fatal("expected non-nil exemplar")
	}
	if got["trace_id"] != "0102030405060708090a0b0c0d0e0f10" {
		t.Errorf("expected trace_id=0102030405060708090a0b0c0d0e0f10, got %q", got["trace_id"])
	}
}

func TestObserveWithExemplar_WithExemplar(t *testing.T) {
	hist := prometheus.NewHistogram(prometheus.HistogramOpts{
		Name:    "test_exemplar_hist",
		Buckets: []float64{1, 5, 10},
	})
	exemplar := prometheus.Labels{"trace_id": "abc123"}

	// Should not panic, and should record the observation.
	observeWithExemplar(hist, 2.5, exemplar)
}

func TestObserveWithExemplar_NilExemplar(t *testing.T) {
	hist := prometheus.NewHistogram(prometheus.HistogramOpts{
		Name:    "test_exemplar_hist_nil",
		Buckets: []float64{1, 5, 10},
	})

	// Should fall back to Observe without panic.
	observeWithExemplar(hist, 2.5, nil)
}

func TestIncWithExemplar_WithExemplar(t *testing.T) {
	counter := prometheus.NewCounter(prometheus.CounterOpts{
		Name: "test_exemplar_counter",
	})
	exemplar := prometheus.Labels{"trace_id": "abc123"}

	// Should not panic.
	incWithExemplar(counter, exemplar)
}

func TestIncWithExemplar_NilExemplar(t *testing.T) {
	counter := prometheus.NewCounter(prometheus.CounterOpts{
		Name: "test_exemplar_counter_nil",
	})

	// Should fall back to Inc without panic.
	incWithExemplar(counter, nil)
}
