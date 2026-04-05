package providers

import (
	"io"
	"net/http"
	"sync"
)

// connTrackingTransport wraps an http.RoundTripper and maintains the
// httpConnsInUse gauge on DefaultStreamMetrics. It counts per-request
// "connection holding" (from RoundTrip start to response body close),
// which is an upper bound on physical TCP connections in use because
// HTTP/2 may multiplex multiple requests on one connection.
//
// The gauge semantic is operationally useful for tuning
// MaxConnsPerHost: when it saturates the configured pool, new streams
// will serialize behind in-use slots. See AltairaLabs/PromptKit#873.
//
// The wrapper is constructed in CreateProviderFromSpec and installed on
// every provider via SetHTTPTransport so the gauge is always present
// regardless of whether operators have overridden pool config.
type connTrackingTransport struct {
	base    http.RoundTripper
	metrics *StreamMetrics
}

// newConnTrackingTransport wraps base with conn-lifecycle tracking
// against the given metrics. A nil base is treated as
// http.DefaultTransport so callers can chain wrappers without
// pre-checking.
func newConnTrackingTransport(base http.RoundTripper, metrics *StreamMetrics) http.RoundTripper {
	if base == nil {
		base = http.DefaultTransport
	}
	return &connTrackingTransport{base: base, metrics: metrics}
}

// RoundTrip increments the in-use gauge before delegating to the base
// transport and arranges for the decrement to fire when the response
// body is closed (or, on error, when the base returns). The decrement
// is guarded by sync.Once so double-close on the body is safe.
func (t *connTrackingTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	host := req.URL.Host
	t.metrics.HTTPConnsInUseInc(host)

	resp, err := t.base.RoundTrip(req)
	if err != nil {
		// No response body to wrap; fire the decrement inline so the
		// gauge balances even when RoundTrip never produced a body.
		t.metrics.HTTPConnsInUseDec(host)
		return nil, err
	}

	// A successful RoundTrip always yields a non-nil Body per
	// net/http contract, but defend against bizarre custom transports
	// returning nil to keep the decrement balanced.
	if resp.Body == nil {
		t.metrics.HTTPConnsInUseDec(host)
		return resp, nil
	}

	resp.Body = &trackedResponseBody{
		ReadCloser: resp.Body,
		onClose: func() {
			t.metrics.HTTPConnsInUseDec(host)
		},
	}
	return resp, nil
}

// trackedResponseBody wraps a response body so the first Close() fires
// an onClose callback. Subsequent closes are no-ops on the callback but
// still propagate to the underlying body so callers that call Close
// more than once (notably io.ReadAll on error paths) do not double-
// decrement the gauge.
type trackedResponseBody struct {
	io.ReadCloser
	once    sync.Once
	onClose func()
}

// Close closes the underlying body and fires the onClose callback
// exactly once. The underlying Close error is returned; the callback
// is invoked regardless of whether Close succeeded so the gauge cannot
// leak on failed closes.
func (t *trackedResponseBody) Close() error {
	err := t.ReadCloser.Close()
	t.once.Do(t.onClose)
	return err
}
