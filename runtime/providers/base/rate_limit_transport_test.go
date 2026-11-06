package base

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/time/rate"
)

// slowRate refills one token every 1000s, so a test never waits for a refill:
// a request past the burst either fails at once on its deadline or waits until
// the test cancels it.
const slowRate = 0.001

func countingServer(t *testing.T) (*httptest.Server, *atomic.Int32) {
	t.Helper()
	var hits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		hits.Add(1)
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(srv.Close)
	return srv, &hits
}

func get(ctx context.Context, c *http.Client, url string) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, http.NoBody)
	if err != nil {
		return err
	}
	resp, err := c.Do(req)
	if err != nil {
		return err
	}
	return resp.Body.Close()
}

func TestNewRequestLimiter(t *testing.T) {
	assert.Nil(t, NewRequestLimiter(0, 5), "zero rate is unlimited")
	assert.Nil(t, NewRequestLimiter(-1, 5), "negative rate is unlimited")

	l := NewRequestLimiter(10, 4)
	require.NotNil(t, l)
	assert.Equal(t, rate.Limit(10), l.Limit())
	assert.Equal(t, 4, l.Burst())

	assert.Equal(t, 1, NewRequestLimiter(10, 0).Burst(), "a missing burst counts as 1, not 0")
	assert.Equal(t, 1, NewRequestLimiter(10, -3).Burst())
}

func TestRateLimitTransport_BurstPassesThenNextWaits(t *testing.T) {
	srv, hits := countingServer(t)
	client := &http.Client{Transport: &RateLimitTransport{Limiter: NewRequestLimiter(slowRate, 2)}}

	for i := 0; i < 2; i++ {
		require.NoError(t, get(t.Context(), client, srv.URL), "request %d is inside the burst", i)
	}

	// The third token is 1000s away; with a 1-minute deadline the limiter
	// refuses at once rather than sleeping.
	ctx, cancel := context.WithTimeout(t.Context(), time.Minute)
	defer cancel()
	err := get(ctx, client, srv.URL)
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrRateLimitPastDeadline)
	assert.ErrorIs(t, err, context.DeadlineExceeded, "retry loops must see a deadline, not a network error")
	assert.Equal(t, int32(2), hits.Load(), "the request past the burst must not be sent")
}

func TestRateLimitTransport_CancelUnblocksWaitingRequest(t *testing.T) {
	srv, hits := countingServer(t)
	limiter := NewRequestLimiter(slowRate, 1)
	client := &http.Client{Transport: &RateLimitTransport{Limiter: limiter}}
	require.NoError(t, get(t.Context(), client, srv.URL))

	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- get(ctx, client, srv.URL) }()

	// The waiting request has reserved the next token, which drives the
	// bucket negative; only then is it known to be blocked in the wait.
	require.Eventually(t, func() bool { return limiter.Tokens() < 0 }, 5*time.Second, time.Millisecond)
	select {
	case err := <-done:
		t.Fatalf("request returned before cancel: %v", err)
	default:
	}

	cancel()
	select {
	case err := <-done:
		assert.ErrorIs(t, err, context.Canceled)
	case <-time.After(5 * time.Second):
		t.Fatal("cancel did not unblock the waiting request")
	}
	assert.Equal(t, int32(1), hits.Load(), "the canceled request must not be sent")
}

func TestRateLimitTransport_NilLimiterIsUnlimited(t *testing.T) {
	srv, hits := countingServer(t)
	client := &http.Client{Transport: &RateLimitTransport{}}
	for i := 0; i < 20; i++ {
		require.NoError(t, get(t.Context(), client, srv.URL))
	}
	assert.Equal(t, int32(20), hits.Load())
}

func TestApplyRateLimit_ReplacesRatherThanStacksAndKeepsTimeout(t *testing.T) {
	inner := http.DefaultTransport
	first := NewRequestLimiter(1, 1)
	second := NewRequestLimiter(2, 2)
	c := &http.Client{Transport: inner, Timeout: 7 * time.Second}

	ApplyRateLimit(c, first)
	ApplyRateLimit(c, second)
	rlt, ok := c.Transport.(*RateLimitTransport)
	require.True(t, ok)
	assert.Same(t, second, rlt.Limiter)
	assert.Equal(t, inner, rlt.Next, "the old limit must be unwrapped, not nested")
	assert.Equal(t, 7*time.Second, rlt.Timeout, "the client timeout moves behind the wait")
	assert.Zero(t, c.Timeout, "a client timeout would start before the wait")
	assert.Equal(t, 7*time.Second, ClientTimeout(c))
	assert.Equal(t, inner, ClientTransport(c))

	ApplyRateLimit(c, nil)
	assert.Equal(t, inner, c.Transport, "a nil limiter removes the limit")
	assert.Equal(t, 7*time.Second, c.Timeout, "and gives the timeout back")

	ApplyRateLimit(nil, first) // no-op, no panic
	assert.Zero(t, ClientTimeout(nil))
	assert.Nil(t, ClientTransport(nil))
}

// http.Client.Timeout puts a deadline on the request before the transport
// runs. The wait must not see it: a call queued longer than the timeout waits
// for its turn rather than failing at once.
func TestApplyRateLimit_WaitDoesNotUseUpRequestTimeout(t *testing.T) {
	srv, hits := countingServer(t)
	limiter := NewRequestLimiter(slowRate, 1)
	client := &http.Client{Timeout: time.Second}
	ApplyRateLimit(client, limiter)
	require.NoError(t, get(t.Context(), client, srv.URL))

	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- get(ctx, client, srv.URL) }()

	require.Eventually(t, func() bool {
		select {
		case err := <-done:
			t.Fatalf("queued call failed instead of waiting: %v", err)
		default:
		}
		return limiter.Tokens() < 0
	}, 5*time.Second, time.Millisecond)
	cancel()
	assert.ErrorIs(t, <-done, context.Canceled)
	assert.Equal(t, int32(1), hits.Load())
}

// The moved timeout still bounds the upstream call, with http.Client's own
// timeout error (a net.Error), which retry loops treat as transient.
func TestApplyRateLimit_TimeoutStillBoundsTheUpstreamCall(t *testing.T) {
	release := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-release:
		case <-r.Context().Done():
		}
	}))
	t.Cleanup(func() { close(release); srv.Close() })

	client := &http.Client{Timeout: 50 * time.Millisecond}
	ApplyRateLimit(client, NewRequestLimiter(1000, 10))

	// A 5s guard, so a timeout that never applies fails here instead of hanging.
	guard, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	start := time.Now()
	err := get(guard, client, srv.URL)
	require.Error(t, err)
	assert.Less(t, time.Since(start), 2*time.Second, "the 50ms timeout must end the call, not the 5s guard")
	var netErr interface{ Timeout() bool }
	require.ErrorAs(t, err, &netErr)
	assert.True(t, netErr.Timeout(), "err = %v", err)
	assert.NotErrorIs(t, err, ErrRateLimitPastDeadline)
	assert.Equal(t, 1, strings.Count(err.Error(), srv.URL), "one url.Error layer, not two: %v", err)
}

func TestHTTPTuning_RateLimiterWrapsOutermost(t *testing.T) {
	limiter := NewRequestLimiter(slowRate, 1)
	tuning := HTTPTuning{RateLimiter: limiter, Headers: map[string]string{"X-A": "1"}}
	assert.False(t, tuning.IsZero())

	srv, hits := countingServer(t)
	client := tuning.Client(&http.Client{})
	rlt, ok := client.Transport.(*RateLimitTransport)
	require.True(t, ok, "transport is %T", client.Transport)
	assert.Same(t, limiter, rlt.Limiter)

	require.NoError(t, get(t.Context(), client, srv.URL))
	ctx, cancel := context.WithTimeout(t.Context(), time.Minute)
	defer cancel()
	err := get(ctx, client, srv.URL)
	assert.True(t, errors.Is(err, ErrRateLimitPastDeadline), "err = %v", err)
	assert.Equal(t, int32(1), hits.Load())
}
