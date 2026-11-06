package base

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
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

func TestWithRateLimit_ReplacesRatherThanStacks(t *testing.T) {
	inner := http.DefaultTransport
	first := NewRequestLimiter(1, 1)
	second := NewRequestLimiter(2, 2)

	rt := WithRateLimit(WithRateLimit(inner, first), second)
	rlt, ok := rt.(*RateLimitTransport)
	require.True(t, ok)
	assert.Same(t, second, rlt.Limiter)
	assert.Equal(t, inner, rlt.Next, "the old limit must be unwrapped, not nested")

	assert.Equal(t, inner, WithRateLimit(rt, nil), "a nil limiter removes the limit")
	assert.Equal(t, inner, WithRateLimit(inner, nil))
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
