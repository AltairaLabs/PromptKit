package metrics

import (
	"github.com/prometheus/client_golang/prometheus"
	"go.opentelemetry.io/otel/trace"
)

// traceExemplar extracts a trace_id exemplar from a span context.
// Returns nil if the trace ID is invalid (zero value or no active trace).
func traceExemplar(sc trace.SpanContext) prometheus.Labels {
	tid := sc.TraceID()
	if !tid.IsValid() {
		return nil
	}
	return prometheus.Labels{"trace_id": tid.String()}
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
