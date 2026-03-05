package providers

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSetRateLimit(t *testing.T) {
	base := NewBaseProvider("test", false, nil)

	// Default: no rate limiter
	assert.Nil(t, base.RateLimiter(), "default rate limiter should be nil")

	// Set a rate limit
	base.SetRateLimit(10.0, 5)
	require.NotNil(t, base.RateLimiter())
	assert.Equal(t, 5, base.RateLimiter().Burst())

	// Disable rate limiting with zero
	base.SetRateLimit(0, 1)
	assert.Nil(t, base.RateLimiter(), "zero rps should disable rate limiting")

	// Disable rate limiting with negative value
	base.SetRateLimit(10.0, 3)
	require.NotNil(t, base.RateLimiter())
	base.SetRateLimit(-1.0, 1)
	assert.Nil(t, base.RateLimiter(), "negative rps should disable rate limiting")
}

func TestWaitForRateLimit_NilLimiter(t *testing.T) {
	base := NewBaseProvider("test", false, nil)

	// Should return immediately with no error when no limiter is configured
	err := base.WaitForRateLimit(t.Context())
	assert.NoError(t, err)
}

func TestWaitForRateLimit_Allows(t *testing.T) {
	base := NewBaseProvider("test", false, nil)
	base.SetRateLimit(1000.0, 10) // generous limit

	err := base.WaitForRateLimit(t.Context())
	assert.NoError(t, err)
}

func TestWaitForRateLimit_CancelledContext(t *testing.T) {
	base := NewBaseProvider("test", false, nil)
	// Very low rate: 1 request per second, burst 1
	base.SetRateLimit(1.0, 1)

	// Consume the burst token
	err := base.WaitForRateLimit(t.Context())
	require.NoError(t, err)

	// Now cancel the context before the next token is available
	ctx, cancel := context.WithCancel(t.Context())
	cancel() // cancel immediately

	err = base.WaitForRateLimit(ctx)
	assert.Error(t, err, "should error when context is cancelled")
}

func TestWaitForRateLimit_Throttles(t *testing.T) {
	base := NewBaseProvider("test", false, nil)
	// 5 requests per second, burst of 2
	base.SetRateLimit(5.0, 2)

	// First two should return immediately (burst)
	start := time.Now()
	for i := 0; i < 2; i++ {
		err := base.WaitForRateLimit(t.Context())
		require.NoError(t, err)
	}

	// Third request should be throttled (need to wait ~200ms for next token)
	err := base.WaitForRateLimit(t.Context())
	require.NoError(t, err)
	elapsed := time.Since(start)

	// Should have taken at least 100ms (being generous with timing)
	assert.GreaterOrEqual(t, elapsed.Milliseconds(), int64(100),
		"third request should be throttled beyond burst")
}

func TestMakeRawRequest_WithRateLimit(t *testing.T) {
	var requestCount atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestCount.Add(1)
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer server.Close()

	client := &http.Client{Timeout: 5 * time.Second}
	base := NewBaseProvider("test", false, client)
	base.SetRateLimit(1000.0, 10) // generous limit, just verify it's wired in

	body := []byte(`{"test":true}`)
	headers := RequestHeaders{"Content-Type": "application/json"}

	resp, err := base.MakeRawRequest(t.Context(), server.URL, body, headers, "Test")
	require.NoError(t, err)
	assert.Equal(t, `{"ok":true}`, string(resp))
	assert.Equal(t, int32(1), requestCount.Load())
}

func TestMakeRawRequest_RateLimitCancelled(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer server.Close()

	client := &http.Client{Timeout: 5 * time.Second}
	base := NewBaseProvider("test", false, client)
	base.SetRateLimit(1.0, 1) // 1 rps, burst 1

	body := []byte(`{}`)
	headers := RequestHeaders{"Content-Type": "application/json"}

	// Consume the burst
	_, err := base.MakeRawRequest(t.Context(), server.URL, body, headers, "Test")
	require.NoError(t, err)

	// Cancel context before rate limiter allows next request
	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	_, err = base.MakeRawRequest(ctx, server.URL, body, headers, "Test")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "rate limit wait")
}

func TestMakeJSONRequest_WithRateLimit(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer server.Close()

	client := &http.Client{Timeout: 5 * time.Second}
	base := NewBaseProvider("test", false, client)
	base.SetRateLimit(1000.0, 10)

	headers := RequestHeaders{"Content-Type": "application/json"}
	resp, err := base.MakeJSONRequest(
		t.Context(), server.URL, map[string]string{"key": "val"}, headers, "Test",
	)
	require.NoError(t, err)
	assert.Equal(t, `{"ok":true}`, string(resp))
}

func TestRateLimit_ConcurrentRequests(t *testing.T) {
	var requestCount atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestCount.Add(1)
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer server.Close()

	client := &http.Client{Timeout: 5 * time.Second}
	base := NewBaseProvider("test", false, client)
	// 50 rps with burst of 5 -- tight enough to observe throttling
	base.SetRateLimit(50.0, 5)

	const numRequests = 10
	var wg sync.WaitGroup
	errs := make([]error, numRequests)

	start := time.Now()
	for i := 0; i < numRequests; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			body := []byte(`{}`)
			headers := RequestHeaders{"Content-Type": "application/json"}
			_, errs[idx] = base.MakeRawRequest(
				t.Context(), server.URL, body, headers, "Test",
			)
		}(i)
	}
	wg.Wait()
	elapsed := time.Since(start)

	// All requests should succeed
	for i, err := range errs {
		assert.NoError(t, err, "request %d should succeed", i)
	}
	assert.Equal(t, int32(numRequests), requestCount.Load())

	// With 50 rps and burst 5, 10 requests should take at least 80ms
	// (5 burst immediately, then 5 more at 20ms each = 100ms, minus some slack)
	assert.GreaterOrEqual(t, elapsed.Milliseconds(), int64(50),
		"concurrent requests should be throttled by rate limiter")
}

func TestRateLimit_NoLimitByDefault(t *testing.T) {
	var requestCount atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestCount.Add(1)
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer server.Close()

	client := &http.Client{Timeout: 5 * time.Second}
	base := NewBaseProvider("test", false, client)
	// No rate limit set -- all requests should go through immediately

	const numRequests = 20
	var wg sync.WaitGroup
	errs := make([]error, numRequests)

	for i := 0; i < numRequests; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			body := []byte(`{}`)
			headers := RequestHeaders{"Content-Type": "application/json"}
			_, errs[idx] = base.MakeRawRequest(
				t.Context(), server.URL, body, headers, "Test",
			)
		}(i)
	}
	wg.Wait()

	for i, err := range errs {
		assert.NoError(t, err, "request %d should succeed without rate limiting", i)
	}
	assert.Equal(t, int32(numRequests), requestCount.Load())
}

func TestRateLimiter_Getter(t *testing.T) {
	base := NewBaseProvider("test", false, nil)

	// No limiter by default
	assert.Nil(t, base.RateLimiter())

	// Set and retrieve
	base.SetRateLimit(100.0, 10)
	limiter := base.RateLimiter()
	require.NotNil(t, limiter)
	assert.Equal(t, 10, limiter.Burst())

	// Clear and verify
	base.SetRateLimit(0, 0)
	assert.Nil(t, base.RateLimiter())
}

func TestWaitForRateLimit_DeadlineExceeded(t *testing.T) {
	base := NewBaseProvider("test", false, nil)
	base.SetRateLimit(1.0, 1) // 1 rps, burst 1

	// Consume the burst
	err := base.WaitForRateLimit(t.Context())
	require.NoError(t, err)

	// Use a very short deadline
	ctx, cancel := context.WithTimeout(t.Context(), 1*time.Millisecond)
	defer cancel()

	// Allow the deadline to expire
	time.Sleep(2 * time.Millisecond)

	err = base.WaitForRateLimit(ctx)
	assert.Error(t, err, "should error when deadline is exceeded")
}
