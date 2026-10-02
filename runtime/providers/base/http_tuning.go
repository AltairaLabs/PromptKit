package base

import (
	"fmt"
	"net/http"
	"time"
)

// HTTPTuning carries the request-tuning settings a provider file can declare
// for a capability provider (tts, stt, embedding, inference, rerank): custom
// headers, a per-request timeout and a pre-built HTTP transport. The zero
// value changes nothing.
type HTTPTuning struct {
	// Headers are added to every HTTP request the provider makes. A header
	// the provider itself sets (its credential, its content type) is not
	// overridden: the request fails with an error naming the header instead.
	Headers map[string]string
	// RequestTimeout replaces the provider's HTTP client timeout when
	// positive. Zero keeps the provider's own default.
	RequestTimeout time.Duration
	// Transport replaces the provider's HTTP transport when non-nil. A
	// transport that applies platform auth keeps doing so: only the
	// connection layer beneath it is replaced.
	Transport http.RoundTripper
}

// IsZero reports whether t carries no settings.
func (t HTTPTuning) IsZero() bool {
	return len(t.Headers) == 0 && t.RequestTimeout <= 0 && t.Transport == nil
}

// layeredTransport is a RoundTripper that wraps a base transport — typically
// one that applies a platform credential per request. Tuning replaces or
// wraps only the base, so the credential is still applied and is applied
// before custom headers are checked for collisions.
type layeredTransport interface {
	BaseTransport() http.RoundTripper
	WithBaseTransport(base http.RoundTripper) http.RoundTripper
}

// Client returns a tuned copy of c; c itself is not modified. A nil c is
// treated as an empty client.
func (t HTTPTuning) Client(c *http.Client) *http.Client {
	out := &http.Client{}
	if c != nil {
		*out = *c
	}
	if t.RequestTimeout > 0 {
		out.Timeout = t.RequestTimeout
	}
	if t.Transport != nil || len(t.Headers) > 0 {
		out.Transport = t.tuneTransport(out.Transport)
	}
	return out
}

// tuneTransport applies Transport and Headers to rt, reaching inside a
// layeredTransport so its auth layer stays outermost.
func (t HTTPTuning) tuneTransport(rt http.RoundTripper) http.RoundTripper {
	if lt, ok := rt.(layeredTransport); ok {
		return lt.WithBaseTransport(t.tuneTransport(lt.BaseTransport()))
	}
	if t.Transport != nil {
		rt = t.Transport
	}
	if rt == nil {
		rt = http.DefaultTransport
	}
	if len(t.Headers) > 0 {
		rt = NewHeaderTransport(rt, t.Headers)
	}
	return rt
}

// HTTPTunable is implemented by capability providers that accept HTTPTuning.
// The capability factories apply a non-zero tuning through it after
// construction and reject one for a provider that does not implement it.
type HTTPTunable interface {
	ApplyHTTPTuning(t HTTPTuning) error
}

// ApplyHTTPTuning applies t to the provider built for providerType. A zero t is
// a no-op for any provider; a non-zero t is an error for a provider that is not
// HTTPTunable, so a declared setting is never silently dropped.
func ApplyHTTPTuning(provider any, providerType string, t HTTPTuning) error {
	if t.IsZero() {
		return nil
	}
	tunable, ok := provider.(HTTPTunable)
	if !ok {
		return fmt.Errorf(
			"provider type %q does not support headers, request_timeout or http_transport for this role",
			providerType)
	}
	return tunable.ApplyHTTPTuning(t)
}

// ApplyHTTPTuning replaces f.Client with a tuned copy. Promoted to every
// service that embeds *HTTPServiceFields.
func (f *HTTPServiceFields) ApplyHTTPTuning(t HTTPTuning) error {
	f.Client = t.Client(f.Client)
	return nil
}

// headerTransport adds fixed headers to every request.
type headerTransport struct {
	next    http.RoundTripper
	headers map[string]string
}

// NewHeaderTransport returns a RoundTripper that adds headers to a clone of
// every request before delegating to next (http.DefaultTransport when nil).
// A header already present on the request — set by the provider, or by an
// auth transport wrapping this one — is not overwritten: the request fails
// with an error naming it, so a custom header can never silently replace a
// credential.
func NewHeaderTransport(next http.RoundTripper, headers map[string]string) http.RoundTripper {
	if next == nil {
		next = http.DefaultTransport
	}
	return &headerTransport{next: next, headers: headers}
}

// RoundTrip implements http.RoundTripper.
func (h *headerTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	for k := range h.headers {
		if req.Header.Get(k) != "" {
			if req.Body != nil {
				_ = req.Body.Close()
			}
			return nil, fmt.Errorf("custom header %q collides with built-in header set by provider", k)
		}
	}
	r := req.Clone(req.Context())
	for k, v := range h.headers {
		r.Header.Set(k, v)
	}
	return h.next.RoundTrip(r)
}
