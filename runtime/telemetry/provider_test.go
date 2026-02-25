package telemetry

import (
	"testing"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/trace"
	"go.opentelemetry.io/otel/trace/noop"
)

func TestTracer_NilProvider(t *testing.T) {
	tracer := Tracer(nil)
	if tracer == nil {
		t.Fatal("expected non-nil tracer")
	}
}

func TestTracer_WithProvider(t *testing.T) {
	tp := noop.NewTracerProvider()
	tracer := Tracer(tp)
	if tracer == nil {
		t.Fatal("expected non-nil tracer")
	}
}

func TestSetupPropagation(t *testing.T) {
	// Store original propagator to restore after test.
	orig := otel.GetTextMapPropagator()
	defer otel.SetTextMapPropagator(orig)

	SetupPropagation()

	prop := otel.GetTextMapPropagator()
	if prop == nil {
		t.Fatal("expected propagator to be set")
	}

	// Verify it handles traceparent field (W3C TraceContext).
	fields := prop.Fields()
	found := false
	for _, f := range fields {
		if f == "traceparent" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected propagator to handle 'traceparent', got fields: %v", fields)
	}
}

func TestNewTracerProvider(t *testing.T) {
	// NewTracerProvider requires a real endpoint; skip if we can't connect.
	// We just verify it doesn't panic with an invalid endpoint.
	tp, err := NewTracerProvider(t.Context(), "http://localhost:0/v1/traces", "test-service")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer func() { _ = tp.Shutdown(t.Context()) }()

	// Verify it implements TracerProvider.
	var _ trace.TracerProvider = tp
}
