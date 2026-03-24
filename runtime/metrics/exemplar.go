package metrics

import (
	"context"

	"github.com/prometheus/client_golang/prometheus"
	"go.opentelemetry.io/otel/trace"
)

// traceExemplar extracts a trace_id exemplar from the context's span.
// Returns nil if there is no valid trace ID (no span, or invalid trace ID).
func traceExemplar(ctx context.Context) prometheus.Labels {
	if ctx == nil {
		return nil
	}
	sc := trace.SpanContextFromContext(ctx)
	if !sc.TraceID().IsValid() {
		return nil
	}
	return prometheus.Labels{"trace_id": sc.TraceID().String()}
}

// observeWithExemplar observes a histogram value, attaching an exemplar if labels is non-nil.
func observeWithExemplar(observer prometheus.Observer, value float64, exemplar prometheus.Labels) {
	if exemplar != nil {
		if eo, ok := observer.(prometheus.ExemplarObserver); ok {
			eo.ObserveWithExemplar(value, exemplar)
			return
		}
	}
	observer.Observe(value)
}

// incWithExemplar increments a counter by 1, attaching an exemplar if labels is non-nil.
func incWithExemplar(counter prometheus.Counter, exemplar prometheus.Labels) {
	if exemplar != nil {
		if ea, ok := counter.(prometheus.ExemplarAdder); ok {
			ea.AddWithExemplar(1, exemplar)
			return
		}
	}
	counter.Inc()
}
