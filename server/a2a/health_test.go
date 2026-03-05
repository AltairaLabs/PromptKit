package a2aserver

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// failingChecker is a HealthChecker that always returns an error.
type failingChecker struct{ err error }

func (c *failingChecker) Check(context.Context) error { return c.err }

// passingChecker is a HealthChecker that always succeeds.
type passingChecker struct{}

func (*passingChecker) Check(context.Context) error { return nil }

func TestHealthz_ReturnsOK(t *testing.T) {
	t.Parallel()

	opener := func(string) (Conversation, error) { return nil, nil }
	srv := NewServer(opener)
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	resp, err := http.Get(ts.URL + "/healthz") //nolint:noctx // test helper
	require.NoError(t, err)
	defer resp.Body.Close()

	assert.Equal(t, http.StatusOK, resp.StatusCode)

	var body map[string]string
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&body))
	assert.Equal(t, "ok", body["status"])
}

func TestReadyz_ReadyAfterInit(t *testing.T) {
	t.Parallel()

	opener := func(string) (Conversation, error) { return nil, nil }
	srv := NewServer(opener)
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	resp, err := http.Get(ts.URL + "/readyz") //nolint:noctx // test helper
	require.NoError(t, err)
	defer resp.Body.Close()

	assert.Equal(t, http.StatusOK, resp.StatusCode)

	var body map[string]any
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&body))
	assert.Equal(t, "ready", body["status"])
}

func TestReadyz_NotReadyDuringShutdown(t *testing.T) {
	t.Parallel()

	opener := func(string) (Conversation, error) { return nil, nil }
	srv := NewServer(opener)
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	// Simulate shutdown: mark server not ready.
	require.NoError(t, srv.Shutdown(context.Background()))

	resp, err := http.Get(ts.URL + "/readyz") //nolint:noctx // test helper
	require.NoError(t, err)
	defer resp.Body.Close()

	assert.Equal(t, http.StatusServiceUnavailable, resp.StatusCode)

	var body map[string]string
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&body))
	assert.Equal(t, "not_ready", body["status"])
	assert.Contains(t, body["reason"], "shutting down")
}

func TestReadyz_FailingHealthCheck(t *testing.T) {
	t.Parallel()

	opener := func(string) (Conversation, error) { return nil, nil }
	srv := NewServer(opener, WithHealthCheck("db", &failingChecker{err: errors.New("connection refused")}))
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	resp, err := http.Get(ts.URL + "/readyz") //nolint:noctx // test helper
	require.NoError(t, err)
	defer resp.Body.Close()

	assert.Equal(t, http.StatusServiceUnavailable, resp.StatusCode)

	var body map[string]any
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&body))
	assert.Equal(t, "not_ready", body["status"])

	checks, ok := body["checks"].(map[string]any)
	require.True(t, ok, "expected checks map in response")
	dbCheck, ok := checks["db"].(map[string]any)
	require.True(t, ok, "expected db check in response")
	assert.Equal(t, "fail", dbCheck["status"])
	assert.Equal(t, "connection refused", dbCheck["error"])
}

func TestReadyz_PassingHealthCheck(t *testing.T) {
	t.Parallel()

	opener := func(string) (Conversation, error) { return nil, nil }
	srv := NewServer(opener, WithHealthCheck("cache", &passingChecker{}))
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	resp, err := http.Get(ts.URL + "/readyz") //nolint:noctx // test helper
	require.NoError(t, err)
	defer resp.Body.Close()

	assert.Equal(t, http.StatusOK, resp.StatusCode)

	var body map[string]any
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&body))
	assert.Equal(t, "ready", body["status"])

	checks, ok := body["checks"].(map[string]any)
	require.True(t, ok, "expected checks map in response")
	cacheCheck, ok := checks["cache"].(map[string]any)
	require.True(t, ok, "expected cache check in response")
	assert.Equal(t, "pass", cacheCheck["status"])
}

func TestReadyz_MultipleCheckers_MixedResults(t *testing.T) {
	t.Parallel()

	opener := func(string) (Conversation, error) { return nil, nil }
	srv := NewServer(opener,
		WithHealthCheck("ok-check", &passingChecker{}),
		WithHealthCheck("bad-check", &failingChecker{err: errors.New("timeout")}),
	)
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	resp, err := http.Get(ts.URL + "/readyz") //nolint:noctx // test helper
	require.NoError(t, err)
	defer resp.Body.Close()

	assert.Equal(t, http.StatusServiceUnavailable, resp.StatusCode)

	var body map[string]any
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&body))

	checks := body["checks"].(map[string]any)
	okCheck := checks["ok-check"].(map[string]any)
	assert.Equal(t, "pass", okCheck["status"])

	badCheck := checks["bad-check"].(map[string]any)
	assert.Equal(t, "fail", badCheck["status"])
	assert.Equal(t, "timeout", badCheck["error"])
}

func TestHealthCheckerFunc(t *testing.T) {
	t.Parallel()

	called := false
	f := HealthCheckerFunc(func(context.Context) error {
		called = true
		return nil
	})

	err := f.Check(context.Background())
	assert.NoError(t, err)
	assert.True(t, called)
}

func TestReadyz_NotReadyBeforeInit(t *testing.T) {
	t.Parallel()

	// Manually construct a server without calling NewServer to simulate
	// the isReady flag being false (zero value).
	s := &Server{
		convs:       make(map[string]Conversation),
		convLastUse: make(map[string]time.Time),
		cancels:     make(map[string]context.CancelFunc),
		subs:        make(map[string]*taskBroadcaster),
		stopCh:      make(chan struct{}),
	}
	ts := httptest.NewServer(s.Handler())
	defer ts.Close()

	resp, err := http.Get(ts.URL + "/readyz") //nolint:noctx // test helper
	require.NoError(t, err)
	defer resp.Body.Close()

	assert.Equal(t, http.StatusServiceUnavailable, resp.StatusCode)
}
