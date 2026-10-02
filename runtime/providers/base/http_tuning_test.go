package base_test

import (
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/AltairaLabs/PromptKit/runtime/v2/providers/base"
)

// recordingRT records the last request it saw and answers 200.
type recordingRT struct{ last *http.Request }

func (r *recordingRT) RoundTrip(req *http.Request) (*http.Response, error) {
	r.last = req
	return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader("")), Request: req}, nil
}

// trackedBody reports whether Close was called.
type trackedBody struct {
	io.Reader
	closed bool
}

func (b *trackedBody) Close() error { b.closed = true; return nil }

// authLayer stands in for a platform-auth transport: it sets a credential
// header and delegates to a swappable base.
type authLayer struct{ base http.RoundTripper }

func (a *authLayer) RoundTrip(req *http.Request) (*http.Response, error) {
	r := req.Clone(req.Context())
	r.Header.Set("Authorization", "Bearer platform")
	return a.base.RoundTrip(r)
}

func (a *authLayer) BaseTransport() http.RoundTripper { return a.base }

func (a *authLayer) WithBaseTransport(next http.RoundTripper) http.RoundTripper {
	return &authLayer{base: next}
}

func TestHeaderTransport_AddsHeaderWithoutMutatingRequest(t *testing.T) {
	rec := &recordingRT{}
	rt := base.NewHeaderTransport(rec, map[string]string{"X-Gateway": "v"})
	req, err := http.NewRequest(http.MethodGet, "http://example.invalid/", http.NoBody)
	require.NoError(t, err)

	resp, err := rt.RoundTrip(req)
	require.NoError(t, err)
	_ = resp.Body.Close()

	require.NotNil(t, rec.last)
	assert.Equal(t, "v", rec.last.Header.Get("X-Gateway"))
	assert.Empty(t, req.Header.Get("X-Gateway"), "caller's request must not be mutated")
}

func TestHeaderTransport_CollisionErrorsAndClosesBody(t *testing.T) {
	rec := &recordingRT{}
	rt := base.NewHeaderTransport(rec, map[string]string{"authorization": "Bearer mine"})
	body := &trackedBody{Reader: strings.NewReader("x")}
	req, err := http.NewRequest(http.MethodPost, "http://example.invalid/", body)
	require.NoError(t, err)
	req.Header.Set("Authorization", "Bearer provider")

	resp, err := rt.RoundTrip(req)
	if resp != nil {
		_ = resp.Body.Close()
	}
	require.Error(t, err)
	assert.Contains(t, err.Error(), `custom header "authorization" collides with built-in header`)
	assert.True(t, body.closed, "request body must be closed on error")
	assert.Nil(t, rec.last, "request must not reach the next transport")
}

func TestHTTPTuningClient_TimeoutKeptWhenUnset(t *testing.T) {
	orig := &http.Client{Timeout: 42 * time.Second}
	got := base.HTTPTuning{Headers: map[string]string{"X-A": "1"}}.Client(orig)
	assert.Equal(t, 42*time.Second, got.Timeout)
	assert.NotSame(t, orig, got)
	assert.Nil(t, orig.Transport, "original client must not be modified")
}

func TestHTTPTuningClient_TimeoutOverriddenWhenSet(t *testing.T) {
	orig := &http.Client{Timeout: 42 * time.Second}
	got := base.HTTPTuning{RequestTimeout: 3 * time.Second}.Client(orig)
	assert.Equal(t, 3*time.Second, got.Timeout)
	assert.Equal(t, 42*time.Second, orig.Timeout)
}

func TestHTTPTuningClient_NilClient(t *testing.T) {
	got := base.HTTPTuning{RequestTimeout: time.Second}.Client(nil)
	require.NotNil(t, got)
	assert.Equal(t, time.Second, got.Timeout)
}

func TestHTTPTuningClient_ReplacesPlainTransport(t *testing.T) {
	rec := &recordingRT{}
	got := base.HTTPTuning{Transport: rec}.Client(&http.Client{Transport: &recordingRT{}})
	assert.Same(t, rec, got.Transport)
}

func TestHTTPTuningClient_SwapsBaseOfLayeredTransport(t *testing.T) {
	oldBase := &recordingRT{}
	newBase := &recordingRT{}
	orig := &http.Client{Transport: &authLayer{base: oldBase}}

	got := base.HTTPTuning{Transport: newBase}.Client(orig)

	layer, ok := got.Transport.(*authLayer)
	require.True(t, ok, "auth wrapper must stay outermost, got %T", got.Transport)
	assert.Same(t, newBase, layer.base)
	assert.Same(t, oldBase, orig.Transport.(*authLayer).base, "original transport must not be modified")
}

func TestHTTPTuningClient_HeadersGoInsideAuthLayer(t *testing.T) {
	rec := &recordingRT{}
	orig := &http.Client{Transport: &authLayer{base: rec}}

	ok := base.HTTPTuning{Headers: map[string]string{"X-Gateway": "v"}}.Client(orig)
	req, err := http.NewRequest(http.MethodGet, "http://example.invalid/", http.NoBody)
	require.NoError(t, err)
	resp, err := ok.Do(req)
	require.NoError(t, err)
	_ = resp.Body.Close()
	assert.Equal(t, "v", rec.last.Header.Get("X-Gateway"))
	assert.Equal(t, "Bearer platform", rec.last.Header.Get("Authorization"))

	// A header the auth layer applies is caught only if the header
	// transport sits beneath it.
	clash := base.HTTPTuning{Headers: map[string]string{"Authorization": "x"}}.Client(orig)
	req2, err := http.NewRequest(http.MethodGet, "http://example.invalid/", http.NoBody)
	require.NoError(t, err)
	resp2, err := clash.Do(req2)
	if resp2 != nil {
		_ = resp2.Body.Close()
	}
	require.Error(t, err)
	assert.Contains(t, err.Error(), "collides with built-in header")
}

func TestHTTPTuning_IsZero(t *testing.T) {
	assert.True(t, base.HTTPTuning{}.IsZero())
	assert.True(t, base.HTTPTuning{Headers: map[string]string{}}.IsZero())
	assert.False(t, base.HTTPTuning{Headers: map[string]string{"a": "b"}}.IsZero())
	assert.False(t, base.HTTPTuning{RequestTimeout: time.Second}.IsZero())
	assert.False(t, base.HTTPTuning{Transport: &recordingRT{}}.IsZero())
}

func TestHTTPServiceFields_ApplyHTTPTuning(t *testing.T) {
	_, f := base.NewHTTPService("k", base.HTTPServiceDefaults{Name: "x", Timeout: time.Minute})
	require.NoError(t, f.ApplyHTTPTuning(base.HTTPTuning{RequestTimeout: 2 * time.Second}))
	assert.Equal(t, 2*time.Second, f.Client.Timeout)
}
