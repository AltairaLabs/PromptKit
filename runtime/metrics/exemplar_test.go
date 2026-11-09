package metrics

import (
	"testing"

	"go.opentelemetry.io/otel/trace"
)

func TestTraceExemplar_ZeroSpanContext(t *testing.T) {
	var sc trace.SpanContext // zero value
	if got := traceExemplar(sc); got != nil {
		t.Errorf("expected nil for zero SpanContext, got %v", got)
	}
}

func TestTraceExemplar_ValidSpanContext(t *testing.T) {
	traceID, _ := trace.TraceIDFromHex("0102030405060708090a0b0c0d0e0f10")
	sc := trace.NewSpanContext(trace.SpanContextConfig{
		TraceID:    traceID,
		SpanID:     trace.SpanID{1},
		TraceFlags: trace.FlagsSampled,
	})

	got := traceExemplar(sc)
	if got == nil {
		t.Fatal("expected non-nil exemplar")
	}
	if got["trace_id"] != "0102030405060708090a0b0c0d0e0f10" {
		t.Errorf("expected trace_id=0102030405060708090a0b0c0d0e0f10, got %q", got["trace_id"])
	}
}
