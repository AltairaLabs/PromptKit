package base

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// gateWaiter is a TokenWaiter that holds each request until the test releases
// it, logging when each wait ends. No real time passes.
type gateWaiter struct {
	entered chan struct{}
	release chan struct{}
	log     *eventLog
}

func newGateWaiter(log *eventLog) *gateWaiter {
	return &gateWaiter{entered: make(chan struct{}, 1), release: make(chan struct{}), log: log}
}

func (g *gateWaiter) Wait(ctx context.Context) error {
	g.entered <- struct{}{}
	select {
	case <-g.release:
		g.log.add("wait-end")
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

type eventLog struct {
	mu     sync.Mutex
	events []string
}

func (l *eventLog) add(e string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.events = append(l.events, e)
}

func (l *eventLog) snapshot() []string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return append([]string(nil), l.events...)
}

// Authorize runs after the wait ends, on a copy of the request, and what it
// sets is what the upstream receives.
func TestRateLimitTransport_AuthorizesAfterTheWait(t *testing.T) {
	gotAuth := make(chan string, 1)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth <- r.Header.Get("Authorization")
	}))
	t.Cleanup(srv.Close)

	log := &eventLog{}
	gate := newGateWaiter(log)
	applied := 0
	rlt := &RateLimitTransport{
		Next:    http.DefaultTransport,
		Limiter: gate,
		Authorize: func(_ context.Context, r *http.Request) error {
			applied++
			log.add(fmt.Sprintf("auth-%d", applied))
			r.Header.Set("Authorization", fmt.Sprintf("token-%d", applied))
			return nil
		},
	}
	req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, srv.URL, http.NoBody)
	require.NoError(t, err)
	req.Header.Set("Authorization", "token-built-before-the-wait")

	done := make(chan error, 1)
	go func() {
		resp, rtErr := rlt.RoundTrip(req)
		if rtErr == nil {
			rtErr = resp.Body.Close()
		}
		done <- rtErr
	}()
	<-gate.entered
	assert.Empty(t, log.snapshot(), "nothing is authorized while the request waits")
	close(gate.release)
	require.NoError(t, <-done)

	assert.Equal(t, []string{"wait-end", "auth-1"}, log.snapshot())
	assert.Equal(t, "token-1", <-gotAuth, "the upstream gets the credential applied after the wait")
	assert.Equal(t, "token-built-before-the-wait", req.Header.Get("Authorization"),
		"the caller's request is not modified")
}

// A credential that fails after the wait fails the request without sending it.
func TestRateLimitTransport_AuthorizeErrorIsNotSent(t *testing.T) {
	srv, hits := countingServer(t)
	authErr := errors.New("token refresh failed")
	rlt := &RateLimitTransport{
		Next:      http.DefaultTransport,
		Limiter:   NewRequestLimiter(1000, 1),
		Authorize: func(context.Context, *http.Request) error { return authErr },
	}
	err := get(t.Context(), &http.Client{Transport: rlt}, srv.URL)
	require.ErrorIs(t, err, authErr)
	require.ErrorIs(t, err, ErrRequestAuthorization, "marked so retry loops leave it alone")
	assert.Zero(t, hits.Load())
}

// NewRequestLimiter returns a nil *rate.Limiter for a zero rate. Held in the
// TokenWaiter field it still means unlimited, and Authorize still runs.
func TestRateLimitTransport_NilRateLimiterInWaiterIsUnlimited(t *testing.T) {
	srv, hits := countingServer(t)
	authorized := 0
	rlt := &RateLimitTransport{
		Next:    http.DefaultTransport,
		Limiter: NewRequestLimiter(0, 0),
		Authorize: func(context.Context, *http.Request) error {
			authorized++
			return nil
		},
	}
	for range 3 {
		require.NoError(t, get(t.Context(), &http.Client{Transport: rlt}, srv.URL))
	}
	assert.Equal(t, int32(3), hits.Load())
	assert.Equal(t, 3, authorized)
}

// A redirect back to the same host is a new request with its own wait, and is
// authorized after that wait: http.Client builds the hop's headers from the
// original request, which the transport never authorized.
func TestRateLimitTransport_ReauthorizesSameHostRedirect(t *testing.T) {
	var mu sync.Mutex
	var hops []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		hops = append(hops, r.Header.Get("Authorization"))
		first := len(hops) == 1
		mu.Unlock()
		if first {
			http.Redirect(w, r, r.URL.RequestURI(), http.StatusTemporaryRedirect)
		}
	}))
	t.Cleanup(srv.Close)

	log := &eventLog{}
	applied := 0
	rlt := &RateLimitTransport{
		Next:    http.DefaultTransport,
		Limiter: countingWaiter{log: log},
		Authorize: func(_ context.Context, r *http.Request) error {
			applied++
			log.add(fmt.Sprintf("auth-%d", applied))
			r.Header.Set("Authorization", fmt.Sprintf("token-%d", applied))
			return nil
		},
	}
	require.NoError(t, get(t.Context(), &http.Client{Transport: rlt}, srv.URL))

	mu.Lock()
	defer mu.Unlock()
	assert.Equal(t, []string{"token-1", "token-2"}, hops, "each hop carries its own post-wait credential")
	assert.Equal(t, []string{"wait-end", "auth-1", "wait-end", "auth-2"}, log.snapshot())
}

// countingWaiter never blocks; it logs each wait.
type countingWaiter struct{ log *eventLog }

func (w countingWaiter) Wait(context.Context) error {
	w.log.add("wait-end")
	return nil
}

// A redirect to another scheme or host keeps none of the credential: the
// original request never carried it, and the hop is not authorized.
func TestSameOriginAsFirstHop(t *testing.T) {
	first, err := http.NewRequest(http.MethodGet, "https://api.example.com/v1", http.NoBody)
	require.NoError(t, err)
	hop := func(target string) *http.Request {
		r, hopErr := http.NewRequest(http.MethodGet, target, http.NoBody)
		require.NoError(t, hopErr)
		r.Response = &http.Response{Request: first}
		return r
	}
	assert.True(t, sameOriginAsFirstHop(first), "a request that is not a hop")
	assert.True(t, sameOriginAsFirstHop(hop("https://API.example.com/v2")), "same host, any path")
	assert.False(t, sameOriginAsFirstHop(hop("http://api.example.com/v1")), "scheme downgrade")
	assert.False(t, sameOriginAsFirstHop(hop("https://api.example.com:8443/v1")), "another port")
	assert.True(t, sameOriginAsFirstHop(hop("https://api.example.com:443/v1")), "the default port spelled out")
	assert.False(t, sameOriginAsFirstHop(hop("https://api.example.com:80/v1")), "another port, not https's default")
	assert.False(t, sameOriginAsFirstHop(hop("https://evil.example.com/v1")), "another host")
	// Another host that redirects to itself is still another host.
	second := hop("https://evil.example.com/a")
	third, err := http.NewRequest(http.MethodGet, "https://evil.example.com/b", http.NoBody)
	require.NoError(t, err)
	third.Response = &http.Response{Request: second}
	assert.False(t, sameOriginAsFirstHop(third), "compared with the first request, not the previous hop")
}

// http.Client strips Authorization on a redirect to another host. The
// redirect hop is not re-authorized, so the credential stays stripped.
func TestRateLimitTransport_DoesNotReauthorizeCrossHostRedirect(t *testing.T) {
	gotAuth := make(chan string, 1)
	target := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		gotAuth <- r.Header.Get("Authorization")
	}))
	t.Cleanup(target.Close)
	// 127.0.0.1 and localhost are different hosts to http.Client.
	targetURL := fmt.Sprintf("http://localhost:%d", target.Listener.Addr().(*net.TCPAddr).Port)
	origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, targetURL, http.StatusTemporaryRedirect)
	}))
	t.Cleanup(origin.Close)

	authorized := 0
	rlt := &RateLimitTransport{
		Next:    http.DefaultTransport,
		Limiter: NewRequestLimiter(1000, 10),
		Authorize: func(_ context.Context, r *http.Request) error {
			authorized++
			r.Header.Set("Authorization", "secret")
			return nil
		},
	}
	require.NoError(t, get(t.Context(), &http.Client{Transport: rlt}, origin.URL))
	assert.Empty(t, <-gotAuth, "the credential must not follow a cross-host redirect")
	assert.Equal(t, 1, authorized, "only the original request is authorized")
}
