package providers

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/testutil"
)

// --- NewPooledTransportWithOptions ---

func TestNewPooledTransportWithOptions_Defaults(t *testing.T) {
	t.Parallel()
	// Zero-valued options must yield a transport with the package
	// defaults applied. This is the path NewPooledTransport() takes.
	tr := NewPooledTransportWithOptions(HTTPTransportOptions{})
	if tr.MaxConnsPerHost != DefaultMaxConnsPerHost {
		t.Errorf("MaxConnsPerHost = %d, want %d", tr.MaxConnsPerHost, DefaultMaxConnsPerHost)
	}
	if tr.MaxIdleConnsPerHost != DefaultMaxIdleConnsPerHost {
		t.Errorf("MaxIdleConnsPerHost = %d, want %d", tr.MaxIdleConnsPerHost, DefaultMaxIdleConnsPerHost)
	}
	if tr.IdleConnTimeout != DefaultIdleConnTimeout {
		t.Errorf("IdleConnTimeout = %v, want %v", tr.IdleConnTimeout, DefaultIdleConnTimeout)
	}
	// Non-pool defaults must still be set so the returned transport is
	// ready-to-use and identical in every other way to NewPooledTransport().
	if !tr.ForceAttemptHTTP2 {
		t.Error("ForceAttemptHTTP2 should be true")
	}
	if tr.TLSHandshakeTimeout != DefaultTLSHandshakeTimeout {
		t.Errorf("TLSHandshakeTimeout = %v, want %v", tr.TLSHandshakeTimeout, DefaultTLSHandshakeTimeout)
	}
}

func TestNewPooledTransportWithOptions_Overrides(t *testing.T) {
	t.Parallel()
	tr := NewPooledTransportWithOptions(HTTPTransportOptions{
		MaxConnsPerHost:     500,
		MaxIdleConnsPerHost: 250,
		IdleConnTimeout:     5 * time.Minute,
	})
	if tr.MaxConnsPerHost != 500 {
		t.Errorf("MaxConnsPerHost = %d, want 500", tr.MaxConnsPerHost)
	}
	if tr.MaxIdleConnsPerHost != 250 {
		t.Errorf("MaxIdleConnsPerHost = %d, want 250", tr.MaxIdleConnsPerHost)
	}
	if tr.IdleConnTimeout != 5*time.Minute {
		t.Errorf("IdleConnTimeout = %v, want 5m", tr.IdleConnTimeout)
	}
}

func TestNewPooledTransportWithOptions_ZeroMaxConnsPerHostMeansUnlimited(t *testing.T) {
	t.Parallel()
	// MaxConnsPerHost=0 must be passed through as-is (unlimited),
	// matching Go's http.Transport behavior. This is the default.
	tr := NewPooledTransportWithOptions(HTTPTransportOptions{
		MaxConnsPerHost: 0,
	})
	if tr.MaxConnsPerHost != 0 {
		t.Errorf("MaxConnsPerHost = %d, want 0 (unlimited)", tr.MaxConnsPerHost)
	}
	// MaxIdleConnsPerHost=0 still falls back to default (0 idle conns
	// would disable connection reuse, which is never useful).
	if tr.MaxIdleConnsPerHost != DefaultMaxIdleConnsPerHost {
		t.Errorf("MaxIdleConnsPerHost = %d, want %d", tr.MaxIdleConnsPerHost, DefaultMaxIdleConnsPerHost)
	}
}

func TestNewPooledTransportWithOptions_NegativeValuesFallBack(t *testing.T) {
	t.Parallel()
	// Arena config parsers clamp to zero on malformed input, but a
	// library caller could pass negatives directly. Negatives must
	// fall back to defaults rather than being stored literally (Go's
	// http.Transport treats negative as "zero idle" which would break
	// pooling silently).
	tr := NewPooledTransportWithOptions(HTTPTransportOptions{
		MaxConnsPerHost:     -5,
		MaxIdleConnsPerHost: -5,
		IdleConnTimeout:     -time.Second,
	})
	if tr.MaxConnsPerHost != DefaultMaxConnsPerHost {
		t.Errorf("negative MaxConnsPerHost should fall back to default, got %d", tr.MaxConnsPerHost)
	}
	if tr.MaxIdleConnsPerHost != DefaultMaxIdleConnsPerHost {
		t.Errorf("negative MaxIdleConnsPerHost should fall back to default, got %d", tr.MaxIdleConnsPerHost)
	}
	if tr.IdleConnTimeout != DefaultIdleConnTimeout {
		t.Errorf("negative IdleConnTimeout should fall back to default, got %v", tr.IdleConnTimeout)
	}
}

// --- BaseProvider.SetHTTPTransport ---

func TestBaseProvider_SetHTTPTransport_UpdatesBothClients(t *testing.T) {
	t.Parallel()
	// Construct a BaseProvider with the standard factory path so both
	// clients are populated. Use a canary transport so we can observe
	// the swap.
	original := &canaryRoundTripper{name: "original"}
	b := NewBaseProvider("p", false, &http.Client{Transport: original})

	replacement := &canaryRoundTripper{name: "replacement"}
	b.SetHTTPTransport(replacement)

	if b.client.Transport != replacement {
		t.Errorf("regular client transport = %v, want replacement", b.client.Transport)
	}
	if b.streamingClient == nil {
		t.Fatal("streaming client unexpectedly nil")
	}
	if b.streamingClient.Transport != replacement {
		t.Errorf("streaming client transport = %v, want replacement", b.streamingClient.Transport)
	}
}

func TestBaseProvider_SetHTTPTransport_MaterializesStreamingClient(t *testing.T) {
	t.Parallel()
	// Corner case: a BaseProvider constructed with a nil client (which
	// newStreamingClient returns nil for) must get a streaming client
	// when a non-nil transport is installed, so streaming callers
	// aren't left with a nil client.
	var b BaseProvider
	b.client = &http.Client{} // no transport, no streaming client

	rt := &canaryRoundTripper{name: "fresh"}
	b.SetHTTPTransport(rt)

	if b.streamingClient == nil {
		t.Fatal("streaming client should have been materialised")
	}
	if b.streamingClient.Timeout != 0 {
		t.Errorf("streaming client timeout = %v, want 0 (unbounded for SSE)", b.streamingClient.Timeout)
	}
	if b.streamingClient.Transport != rt {
		t.Errorf("streaming client transport not set from replacement")
	}
}

// canaryRoundTripper is a no-op RoundTripper used to verify transport
// identity after SetHTTPTransport. The name field makes test failures
// more readable.
type canaryRoundTripper struct {
	name string
}

func (c *canaryRoundTripper) RoundTrip(_ *http.Request) (*http.Response, error) {
	return nil, http.ErrNotSupported
}

// --- connTrackingTransport ---

func TestConnTrackingTransport_IncrementsAndDecrements(t *testing.T) {
	t.Parallel()
	// End-to-end: wrap a real httptest server with the tracking
	// transport, make one request, verify the gauge goes up during
	// the body read and returns to zero after Close.
	reg := prometheus.NewRegistry()
	metrics := NewStreamMetrics(reg, "test", nil)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, "hello")
	}))
	defer server.Close()

	rt := newConnTrackingTransport(http.DefaultTransport, metrics)
	client := &http.Client{Transport: rt}

	resp, err := client.Get(server.URL)
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	host := strings.TrimPrefix(server.URL, "http://")

	// Before closing the body the gauge must be 1 — this is the
	// "request is holding a connection slot" state.
	if got := testutil.ToFloat64(metrics.httpConnsInUse.WithLabelValues(host)); got != 1 {
		t.Errorf("gauge during active request = %v, want 1", got)
	}

	// Reading the body does not decrement — only Close does, because
	// the connection slot is released back to the pool on body close.
	_, _ = io.ReadAll(resp.Body)
	if got := testutil.ToFloat64(metrics.httpConnsInUse.WithLabelValues(host)); got != 1 {
		t.Errorf("gauge after ReadAll but before Close = %v, want 1", got)
	}

	// Close must decrement.
	_ = resp.Body.Close()
	if got := testutil.ToFloat64(metrics.httpConnsInUse.WithLabelValues(host)); got != 0 {
		t.Errorf("gauge after Close = %v, want 0", got)
	}
}

func TestConnTrackingTransport_DoubleCloseIsIdempotent(t *testing.T) {
	t.Parallel()
	// A buggy caller that calls Close() twice must not double-
	// decrement the gauge. This is the exact class of bug that
	// negative gauge values hide until alerts fire on "<0".
	reg := prometheus.NewRegistry()
	metrics := NewStreamMetrics(reg, "test", nil)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, "hi")
	}))
	defer server.Close()

	client := &http.Client{Transport: newConnTrackingTransport(http.DefaultTransport, metrics)}
	resp, err := client.Get(server.URL)
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	host := strings.TrimPrefix(server.URL, "http://")

	_ = resp.Body.Close()
	_ = resp.Body.Close() // second close must be a no-op for the gauge

	if got := testutil.ToFloat64(metrics.httpConnsInUse.WithLabelValues(host)); got != 0 {
		t.Errorf("gauge after double close = %v, want 0 (double-close must not double-decrement)", got)
	}
}

func TestConnTrackingTransport_TransportErrorDecrements(t *testing.T) {
	t.Parallel()
	// When the underlying transport errors before producing a body,
	// the wrapper must still balance the gauge — otherwise a brief
	// network blip would leak gauge counts forever.
	reg := prometheus.NewRegistry()
	metrics := NewStreamMetrics(reg, "test", nil)

	rt := newConnTrackingTransport(&errorRoundTripper{}, metrics)
	client := &http.Client{Transport: rt}

	// Use a URL that will never be dialed (errorRoundTripper short-
	// circuits) but still parses cleanly.
	_, err := client.Get("http://127.0.0.1:1/x")
	if err == nil {
		t.Fatal("expected transport error")
	}

	if got := testutil.ToFloat64(metrics.httpConnsInUse.WithLabelValues("127.0.0.1:1")); got != 0 {
		t.Errorf("gauge after transport error = %v, want 0", got)
	}
}

// errorRoundTripper always returns an error without contacting any
// network. Used to exercise connTrackingTransport's error decrement
// path without relying on a real failed dial.
type errorRoundTripper struct{}

func (errorRoundTripper) RoundTrip(_ *http.Request) (*http.Response, error) {
	return nil, io.ErrUnexpectedEOF
}

func TestConnTrackingTransport_NilMetricsSafe(t *testing.T) {
	t.Parallel()
	// The metrics instance is allowed to be nil (DefaultStreamMetrics()
	// returns nil when no host has registered metrics). The wrapper
	// must remain functional in that case — provider code installed
	// through CreateProviderFromSpec on a host without metrics still
	// needs HTTP to work.
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, "ok")
	}))
	defer server.Close()

	rt := newConnTrackingTransport(http.DefaultTransport, nil)
	client := &http.Client{Transport: rt}
	resp, err := client.Get(server.URL)
	if err != nil {
		t.Fatalf("GET with nil metrics: %v", err)
	}
	_, _ = io.ReadAll(resp.Body)
	_ = resp.Body.Close()
}

func TestConnTrackingTransport_NilBaseFallsBackToDefault(t *testing.T) {
	t.Parallel()
	// A caller wrapping with a nil base must get a working transport
	// via http.DefaultTransport rather than a nil-deref panic.
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, "ok")
	}))
	defer server.Close()

	rt := newConnTrackingTransport(nil, nil)
	client := &http.Client{Transport: rt}
	resp, err := client.Get(server.URL)
	if err != nil {
		t.Fatalf("GET with nil base: %v", err)
	}
	_, _ = io.ReadAll(resp.Body)
	_ = resp.Body.Close()
}

// --- StreamMetrics http_conns_in_use nil-safety ---

func TestStreamMetrics_HTTPConnsInUse_NilSafe(t *testing.T) {
	t.Parallel()
	var m *StreamMetrics
	m.HTTPConnsInUseInc("example.com")
	m.HTTPConnsInUseDec("example.com")
}
