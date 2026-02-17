package telemetry

import (
	"context"
	"net/http"
	"regexp"
)

// traceContextKey is a private type for the trace context key to avoid collisions.
type traceContextKey struct{}

// traceparentRe validates the W3C Trace Context traceparent header format:
// version-trace_id-parent_id-trace_flags (e.g., 00-<32 hex>-<16 hex>-<2 hex>).
var traceparentRe = regexp.MustCompile(`^[0-9a-f]{2}-[0-9a-f]{32}-[0-9a-f]{16}-[0-9a-f]{2}$`)

// TraceContext holds distributed trace headers extracted from an inbound HTTP request.
type TraceContext struct {
	Traceparent string // W3C traceparent header
	Tracestate  string // W3C tracestate header
	XRayTraceID string // AWS X-Ray X-Amzn-Trace-Id header
}

// IsEmpty returns true when no trace data is present.
func (tc TraceContext) IsEmpty() bool {
	return tc.Traceparent == "" && tc.Tracestate == "" && tc.XRayTraceID == ""
}

// ExtractTraceContext reads trace headers from an inbound HTTP request.
// Invalid traceparent values are silently discarded.
func ExtractTraceContext(r *http.Request) TraceContext {
	tc := TraceContext{
		Tracestate:  r.Header.Get("tracestate"),
		XRayTraceID: r.Header.Get("X-Amzn-Trace-Id"),
	}
	if tp := r.Header.Get("traceparent"); traceparentRe.MatchString(tp) {
		tc.Traceparent = tp
	}
	return tc
}

// ContextWithTrace stores a TraceContext in a Go context.
func ContextWithTrace(ctx context.Context, tc TraceContext) context.Context {
	return context.WithValue(ctx, traceContextKey{}, tc)
}

// TraceContextFromContext retrieves a TraceContext from a Go context.
// Returns an empty TraceContext if none is stored.
func TraceContextFromContext(ctx context.Context) TraceContext {
	tc, _ := ctx.Value(traceContextKey{}).(TraceContext)
	return tc
}

// TraceMiddleware extracts distributed trace headers from inbound requests
// and stores them in the request context for downstream propagation.
func TraceMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		tc := ExtractTraceContext(r)
		if !tc.IsEmpty() {
			r = r.WithContext(ContextWithTrace(r.Context(), tc))
		}
		next.ServeHTTP(w, r)
	})
}

// InjectTraceHeaders writes trace headers from the context onto an outbound
// HTTP request. It is a no-op if the context contains no trace data.
func InjectTraceHeaders(ctx context.Context, req *http.Request) {
	tc := TraceContextFromContext(ctx)
	if tc.IsEmpty() {
		return
	}
	if tc.Traceparent != "" {
		req.Header.Set("traceparent", tc.Traceparent)
	}
	if tc.Tracestate != "" {
		req.Header.Set("tracestate", tc.Tracestate)
	}
	if tc.XRayTraceID != "" {
		req.Header.Set("X-Amzn-Trace-Id", tc.XRayTraceID)
	}
}
