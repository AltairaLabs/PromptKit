package telemetry

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestExtractTraceContext_W3C(t *testing.T) {
	r := httptest.NewRequest(http.MethodGet, "/", http.NoBody)
	r.Header.Set("traceparent", "00-4bf92f3577b34da6a3ce929d0e0e4736-00f067aa0ba902b7-01")
	r.Header.Set("tracestate", "congo=t61rcWkgMzE")

	tc := ExtractTraceContext(r)

	if tc.Traceparent != "00-4bf92f3577b34da6a3ce929d0e0e4736-00f067aa0ba902b7-01" {
		t.Errorf("Traceparent = %q", tc.Traceparent)
	}
	if tc.Tracestate != "congo=t61rcWkgMzE" {
		t.Errorf("Tracestate = %q", tc.Tracestate)
	}
	if tc.XRayTraceID != "" {
		t.Errorf("XRayTraceID = %q, want empty", tc.XRayTraceID)
	}
	if tc.IsEmpty() {
		t.Error("expected non-empty TraceContext")
	}
}

func TestExtractTraceContext_XRay(t *testing.T) {
	r := httptest.NewRequest(http.MethodGet, "/", http.NoBody)
	r.Header.Set("X-Amzn-Trace-Id", "Root=1-5759e988-bd862e3fe1be46a994272793;Parent=53995c3f42cd8ad8;Sampled=1")

	tc := ExtractTraceContext(r)

	if tc.XRayTraceID != "Root=1-5759e988-bd862e3fe1be46a994272793;Parent=53995c3f42cd8ad8;Sampled=1" {
		t.Errorf("XRayTraceID = %q", tc.XRayTraceID)
	}
	if tc.Traceparent != "" {
		t.Errorf("Traceparent = %q, want empty", tc.Traceparent)
	}
	if tc.IsEmpty() {
		t.Error("expected non-empty TraceContext")
	}
}

func TestExtractTraceContext_Both(t *testing.T) {
	r := httptest.NewRequest(http.MethodGet, "/", http.NoBody)
	r.Header.Set("traceparent", "00-4bf92f3577b34da6a3ce929d0e0e4736-00f067aa0ba902b7-01")
	r.Header.Set("tracestate", "congo=t61rcWkgMzE")
	r.Header.Set("X-Amzn-Trace-Id", "Root=1-5759e988-bd862e3fe1be46a994272793")

	tc := ExtractTraceContext(r)

	if tc.Traceparent == "" {
		t.Error("expected Traceparent")
	}
	if tc.Tracestate == "" {
		t.Error("expected Tracestate")
	}
	if tc.XRayTraceID == "" {
		t.Error("expected XRayTraceID")
	}
}

func TestExtractTraceContext_None(t *testing.T) {
	r := httptest.NewRequest(http.MethodGet, "/", http.NoBody)

	tc := ExtractTraceContext(r)

	if !tc.IsEmpty() {
		t.Errorf("expected empty TraceContext, got %+v", tc)
	}
}

func TestExtractTraceContext_InvalidTraceparent(t *testing.T) {
	r := httptest.NewRequest(http.MethodGet, "/", http.NoBody)
	r.Header.Set("traceparent", "not-a-valid-traceparent")

	tc := ExtractTraceContext(r)

	if tc.Traceparent != "" {
		t.Errorf("Traceparent = %q, want empty for invalid input", tc.Traceparent)
	}
}

func TestContextRoundTrip(t *testing.T) {
	r := httptest.NewRequest(http.MethodGet, "/", http.NoBody)
	r.Header.Set("traceparent", "00-4bf92f3577b34da6a3ce929d0e0e4736-00f067aa0ba902b7-01")
	r.Header.Set("tracestate", "congo=t61rcWkgMzE")
	r.Header.Set("X-Amzn-Trace-Id", "Root=1-5759e988-bd862e3fe1be46a994272793")

	// Extract from inbound request.
	tc := ExtractTraceContext(r)

	// Store in context.
	ctx := ContextWithTrace(context.Background(), tc)

	// Inject into outbound request.
	outReq := httptest.NewRequest(http.MethodPost, "/downstream", http.NoBody)
	InjectTraceHeaders(ctx, outReq)

	if got := outReq.Header.Get("traceparent"); got != tc.Traceparent {
		t.Errorf("traceparent = %q, want %q", got, tc.Traceparent)
	}
	if got := outReq.Header.Get("tracestate"); got != tc.Tracestate {
		t.Errorf("tracestate = %q, want %q", got, tc.Tracestate)
	}
	if got := outReq.Header.Get("X-Amzn-Trace-Id"); got != tc.XRayTraceID {
		t.Errorf("X-Amzn-Trace-Id = %q, want %q", got, tc.XRayTraceID)
	}
}

func TestTraceMiddleware(t *testing.T) {
	wantTP := "00-4bf92f3577b34da6a3ce929d0e0e4736-00f067aa0ba902b7-01"

	var gotTC TraceContext
	inner := http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		gotTC = TraceContextFromContext(r.Context())
	})

	handler := TraceMiddleware(inner)
	r := httptest.NewRequest(http.MethodPost, "/a2a", http.NoBody)
	r.Header.Set("traceparent", wantTP)
	handler.ServeHTTP(httptest.NewRecorder(), r)

	if gotTC.Traceparent != wantTP {
		t.Errorf("Traceparent = %q, want %q", gotTC.Traceparent, wantTP)
	}
}

func TestTraceMiddleware_NoHeaders(t *testing.T) {
	var gotTC TraceContext
	inner := http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		gotTC = TraceContextFromContext(r.Context())
	})

	handler := TraceMiddleware(inner)
	r := httptest.NewRequest(http.MethodPost, "/a2a", http.NoBody)
	handler.ServeHTTP(httptest.NewRecorder(), r)

	if !gotTC.IsEmpty() {
		t.Errorf("expected empty TraceContext, got %+v", gotTC)
	}
}

func TestInjectTraceHeaders_NoOp(t *testing.T) {
	ctx := context.Background() // no trace context stored

	outReq := httptest.NewRequest(http.MethodPost, "/downstream", http.NoBody)
	InjectTraceHeaders(ctx, outReq)

	if got := outReq.Header.Get("traceparent"); got != "" {
		t.Errorf("traceparent = %q, want empty", got)
	}
	if got := outReq.Header.Get("tracestate"); got != "" {
		t.Errorf("tracestate = %q, want empty", got)
	}
	if got := outReq.Header.Get("X-Amzn-Trace-Id"); got != "" {
		t.Errorf("X-Amzn-Trace-Id = %q, want empty", got)
	}
}
