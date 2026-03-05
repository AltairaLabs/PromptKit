package providers

import (
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/AltairaLabs/PromptKit/runtime/pipeline"
)

func TestDefaultRetryPolicy(t *testing.T) {
	p := DefaultRetryPolicy()

	if p.MaxRetries != DefaultMaxRetries {
		t.Errorf("MaxRetries = %d, want %d", p.MaxRetries, DefaultMaxRetries)
	}
	if p.Backoff != DefaultBackoff {
		t.Errorf("Backoff = %q, want %q", p.Backoff, DefaultBackoff)
	}
	if p.InitialDelayMs != DefaultInitialDelayMs {
		t.Errorf(
			"InitialDelayMs = %d, want %d",
			p.InitialDelayMs, DefaultInitialDelayMs,
		)
	}
}

func TestIsRetryableStatusCode(t *testing.T) {
	tests := []struct {
		code     int
		expected bool
	}{
		{http.StatusOK, false},
		{http.StatusBadRequest, false},
		{http.StatusUnauthorized, false},
		{http.StatusForbidden, false},
		{http.StatusNotFound, false},
		{http.StatusInternalServerError, false},
		{http.StatusTooManyRequests, true},
		{http.StatusBadGateway, true},
		{http.StatusServiceUnavailable, true},
		{http.StatusGatewayTimeout, true},
	}

	for _, tt := range tests {
		got := isRetryableStatusCode(tt.code)
		if got != tt.expected {
			t.Errorf(
				"isRetryableStatusCode(%d) = %v, want %v",
				tt.code, got, tt.expected,
			)
		}
	}
}

func TestIsRetryableError(t *testing.T) {
	tests := []struct {
		name     string
		err      error
		expected bool
	}{
		{"nil error", nil, false},
		{"context canceled", context.Canceled, false},
		{"context deadline exceeded", context.DeadlineExceeded, false},
		{
			"net timeout error",
			&net.DNSError{IsTimeout: true},
			true,
		},
		{
			"DNS error",
			&net.DNSError{Err: "no such host", Name: "example.com"},
			true,
		},
		{
			"op error (connection refused)",
			&net.OpError{Op: "dial", Err: errors.New("connection refused")},
			true,
		},
		{
			"generic error",
			errors.New("some random error"),
			false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := isRetryableError(tt.err)
			if got != tt.expected {
				t.Errorf(
					"isRetryableError(%v) = %v, want %v",
					tt.err, got, tt.expected,
				)
			}
		})
	}
}

func TestParseRetryAfter(t *testing.T) {
	tests := []struct {
		name     string
		header   string
		resp     *http.Response
		minDelay time.Duration
		maxDelay time.Duration
	}{
		{
			name:     "nil response",
			resp:     nil,
			minDelay: 0,
			maxDelay: 0,
		},
		{
			name: "missing header",
			resp: &http.Response{
				Header: http.Header{},
			},
			minDelay: 0,
			maxDelay: 0,
		},
		{
			name: "delta seconds",
			resp: &http.Response{
				Header: http.Header{
					"Retry-After": []string{"5"},
				},
			},
			minDelay: 5 * time.Second,
			maxDelay: 5 * time.Second,
		},
		{
			name: "zero seconds",
			resp: &http.Response{
				Header: http.Header{
					"Retry-After": []string{"0"},
				},
			},
			minDelay: 0,
			maxDelay: 0,
		},
		{
			name: "invalid value",
			resp: &http.Response{
				Header: http.Header{
					"Retry-After": []string{"not-a-number"},
				},
			},
			minDelay: 0,
			maxDelay: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := parseRetryAfter(tt.resp)
			if got < tt.minDelay || got > tt.maxDelay {
				t.Errorf(
					"parseRetryAfter() = %v, want [%v, %v]",
					got, tt.minDelay, tt.maxDelay,
				)
			}
		})
	}
}

func TestParseRetryAfter_HTTPDate(t *testing.T) {
	future := time.Now().Add(10 * time.Second)
	resp := &http.Response{
		Header: http.Header{
			"Retry-After": []string{
				future.UTC().Format(http.TimeFormat),
			},
		},
	}
	got := parseRetryAfter(resp)
	if got <= 0 {
		t.Errorf("expected positive duration for future date, got %v", got)
	}
	if got > 11*time.Second {
		t.Errorf("expected delay <= 11s, got %v", got)
	}
}

func TestCalculateBackoff(t *testing.T) {
	tests := []struct {
		name       string
		policy     pipeline.RetryPolicy
		attempt    int
		retryAfter time.Duration
		minDelay   time.Duration
		maxDelay   time.Duration
	}{
		{
			name: "exponential attempt 0",
			policy: pipeline.RetryPolicy{
				Backoff:        "exponential",
				InitialDelayMs: 100,
			},
			attempt:  0,
			minDelay: 100 * time.Millisecond,
			// base 100ms + up to 50% jitter = 150ms max
			maxDelay: 151 * time.Millisecond,
		},
		{
			name: "exponential attempt 1",
			policy: pipeline.RetryPolicy{
				Backoff:        "exponential",
				InitialDelayMs: 100,
			},
			attempt:  1,
			minDelay: 200 * time.Millisecond,
			maxDelay: 301 * time.Millisecond,
		},
		{
			name: "exponential attempt 2",
			policy: pipeline.RetryPolicy{
				Backoff:        "exponential",
				InitialDelayMs: 100,
			},
			attempt:  2,
			minDelay: 400 * time.Millisecond,
			maxDelay: 601 * time.Millisecond,
		},
		{
			name: "fixed backoff",
			policy: pipeline.RetryPolicy{
				Backoff:        "fixed",
				InitialDelayMs: 200,
			},
			attempt:  3,
			minDelay: 200 * time.Millisecond,
			maxDelay: 301 * time.Millisecond,
		},
		{
			name: "retry-after overrides backoff",
			policy: pipeline.RetryPolicy{
				Backoff:        "exponential",
				InitialDelayMs: 100,
			},
			attempt:    0,
			retryAfter: 5 * time.Second,
			minDelay:   5 * time.Second,
			maxDelay:   5 * time.Second,
		},
		{
			name: "zero initial delay uses default",
			policy: pipeline.RetryPolicy{
				Backoff:        "exponential",
				InitialDelayMs: 0,
			},
			attempt:  0,
			minDelay: time.Duration(DefaultInitialDelayMs) * time.Millisecond,
			maxDelay: time.Duration(
				float64(DefaultInitialDelayMs)*1.5+1,
			) * time.Millisecond,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := calculateBackoff(tt.policy, tt.attempt, tt.retryAfter)
			if got < tt.minDelay || got > tt.maxDelay {
				t.Errorf(
					"calculateBackoff() = %v, want [%v, %v]",
					got, tt.minDelay, tt.maxDelay,
				)
			}
		})
	}
}

func TestCalculateBackoff_CapsAtMax(t *testing.T) {
	policy := pipeline.RetryPolicy{
		Backoff:        "exponential",
		InitialDelayMs: 10000, // 10s
	}
	// attempt 10 => 10s * 2^10 = 10240s, should be capped at 60s + jitter
	got := calculateBackoff(policy, 10, 0)
	// max = 60s + 50% jitter = 90s
	if got > 91*time.Second {
		t.Errorf("expected capped delay <= 91s, got %v", got)
	}
}

func TestDoWithRetry_SuccessOnFirstAttempt(t *testing.T) {
	policy := pipeline.RetryPolicy{
		MaxRetries:     3,
		Backoff:        "exponential",
		InitialDelayMs: 10,
	}

	var attempts int32
	server := httptest.NewServer(http.HandlerFunc(
		func(w http.ResponseWriter, r *http.Request) {
			atomic.AddInt32(&attempts, 1)
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"ok":true}`))
		},
	))
	defer server.Close()

	doFn := func() (*http.Response, error) {
		return http.Get(server.URL)
	}

	resp, err := DoWithRetry(t.Context(), policy, "test", doFn)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected 200, got %d", resp.StatusCode)
	}
	if atomic.LoadInt32(&attempts) != 1 {
		t.Errorf("expected 1 attempt, got %d", atomic.LoadInt32(&attempts))
	}
}

func TestDoWithRetry_RetriesOn429(t *testing.T) {
	policy := pipeline.RetryPolicy{
		MaxRetries:     2,
		Backoff:        "fixed",
		InitialDelayMs: 10, // Very short for testing
	}

	var attempts int32
	server := httptest.NewServer(http.HandlerFunc(
		func(w http.ResponseWriter, r *http.Request) {
			n := atomic.AddInt32(&attempts, 1)
			if n <= 2 {
				w.Header().Set("Retry-After", "0")
				w.WriteHeader(http.StatusTooManyRequests)
				_, _ = w.Write([]byte(`{"error":"rate limited"}`))
				return
			}
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"ok":true}`))
		},
	))
	defer server.Close()

	doFn := func() (*http.Response, error) {
		return http.Get(server.URL)
	}

	resp, err := DoWithRetry(t.Context(), policy, "test", doFn)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected 200, got %d", resp.StatusCode)
	}
	if atomic.LoadInt32(&attempts) != 3 {
		t.Errorf("expected 3 attempts, got %d", atomic.LoadInt32(&attempts))
	}
}

func TestDoWithRetry_RetriesOn503(t *testing.T) {
	policy := pipeline.RetryPolicy{
		MaxRetries:     1,
		Backoff:        "fixed",
		InitialDelayMs: 10,
	}

	var attempts int32
	server := httptest.NewServer(http.HandlerFunc(
		func(w http.ResponseWriter, r *http.Request) {
			n := atomic.AddInt32(&attempts, 1)
			if n == 1 {
				w.WriteHeader(http.StatusServiceUnavailable)
				_, _ = w.Write([]byte(`{"error":"overloaded"}`))
				return
			}
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"ok":true}`))
		},
	))
	defer server.Close()

	doFn := func() (*http.Response, error) {
		return http.Get(server.URL)
	}

	resp, err := DoWithRetry(t.Context(), policy, "test", doFn)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected 200, got %d", resp.StatusCode)
	}
	if atomic.LoadInt32(&attempts) != 2 {
		t.Errorf("expected 2 attempts, got %d", atomic.LoadInt32(&attempts))
	}
}

func TestDoWithRetry_RetriesOn502(t *testing.T) {
	policy := pipeline.RetryPolicy{
		MaxRetries:     1,
		Backoff:        "fixed",
		InitialDelayMs: 10,
	}

	var attempts int32
	server := httptest.NewServer(http.HandlerFunc(
		func(w http.ResponseWriter, r *http.Request) {
			n := atomic.AddInt32(&attempts, 1)
			if n == 1 {
				w.WriteHeader(http.StatusBadGateway)
				_, _ = w.Write([]byte(`bad gateway`))
				return
			}
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`ok`))
		},
	))
	defer server.Close()

	doFn := func() (*http.Response, error) {
		return http.Get(server.URL)
	}

	resp, err := DoWithRetry(t.Context(), policy, "test", doFn)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected 200, got %d", resp.StatusCode)
	}
}

func TestDoWithRetry_ExhaustsRetries(t *testing.T) {
	policy := pipeline.RetryPolicy{
		MaxRetries:     2,
		Backoff:        "fixed",
		InitialDelayMs: 10,
	}

	var attempts int32
	server := httptest.NewServer(http.HandlerFunc(
		func(w http.ResponseWriter, r *http.Request) {
			atomic.AddInt32(&attempts, 1)
			w.WriteHeader(http.StatusServiceUnavailable)
			_, _ = w.Write([]byte(`{"error":"overloaded"}`))
		},
	))
	defer server.Close()

	doFn := func() (*http.Response, error) {
		return http.Get(server.URL)
	}

	_, err := DoWithRetry(t.Context(), policy, "test", doFn)
	if err == nil {
		t.Fatal("expected error after exhausting retries")
	}

	var retryErr *RetryableHTTPError
	if !errors.As(err, &retryErr) {
		t.Fatalf("expected RetryableHTTPError, got %T: %v", err, err)
	}
	if retryErr.StatusCode != http.StatusServiceUnavailable {
		t.Errorf("expected status 503, got %d", retryErr.StatusCode)
	}

	// Should have made maxRetries + 1 attempts
	if atomic.LoadInt32(&attempts) != 3 {
		t.Errorf("expected 3 attempts, got %d", atomic.LoadInt32(&attempts))
	}
}

func TestDoWithRetry_NoRetryOn400(t *testing.T) {
	policy := pipeline.RetryPolicy{
		MaxRetries:     3,
		Backoff:        "fixed",
		InitialDelayMs: 10,
	}

	var attempts int32
	server := httptest.NewServer(http.HandlerFunc(
		func(w http.ResponseWriter, r *http.Request) {
			atomic.AddInt32(&attempts, 1)
			w.WriteHeader(http.StatusBadRequest)
			_, _ = w.Write([]byte(`{"error":"bad request"}`))
		},
	))
	defer server.Close()

	doFn := func() (*http.Response, error) {
		return http.Get(server.URL)
	}

	resp, err := DoWithRetry(t.Context(), policy, "test", doFn)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer resp.Body.Close()

	// Should return the 400 without retrying
	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", resp.StatusCode)
	}
	if atomic.LoadInt32(&attempts) != 1 {
		t.Errorf("expected 1 attempt (no retry), got %d",
			atomic.LoadInt32(&attempts))
	}
}

func TestDoWithRetry_RetriesNetworkError(t *testing.T) {
	policy := pipeline.RetryPolicy{
		MaxRetries:     1,
		Backoff:        "fixed",
		InitialDelayMs: 10,
	}

	var attempts int32
	server := httptest.NewServer(http.HandlerFunc(
		func(w http.ResponseWriter, r *http.Request) {
			atomic.AddInt32(&attempts, 1)
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`ok`))
		},
	))
	defer server.Close()

	callCount := int32(0)
	doFn := func() (*http.Response, error) {
		n := atomic.AddInt32(&callCount, 1)
		if n == 1 {
			// Simulate a network error on first call
			return nil, &net.OpError{
				Op:  "dial",
				Err: errors.New("connection refused"),
			}
		}
		return http.Get(server.URL)
	}

	resp, err := DoWithRetry(t.Context(), policy, "test", doFn)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected 200, got %d", resp.StatusCode)
	}
	if atomic.LoadInt32(&callCount) != 2 {
		t.Errorf("expected 2 calls, got %d", atomic.LoadInt32(&callCount))
	}
}

func TestDoWithRetry_ContextCanceled(t *testing.T) {
	policy := pipeline.RetryPolicy{
		MaxRetries:     5,
		Backoff:        "fixed",
		InitialDelayMs: 1000, // Long delay
	}

	ctx, cancel := context.WithCancel(t.Context())

	var attempts int32
	doFn := func() (*http.Response, error) {
		n := atomic.AddInt32(&attempts, 1)
		if n == 1 {
			// Cancel context after first attempt to abort retry wait
			cancel()
			return nil, &net.OpError{
				Op:  "dial",
				Err: errors.New("connection refused"),
			}
		}
		return nil, errors.New("should not reach here")
	}

	_, err := DoWithRetry(ctx, policy, "test", doFn)
	if err == nil {
		t.Fatal("expected error when context is canceled")
	}
	if !errors.Is(err, context.Canceled) {
		t.Errorf("expected context.Canceled, got: %v", err)
	}
}

func TestDoWithRetry_NonRetryableError(t *testing.T) {
	policy := pipeline.RetryPolicy{
		MaxRetries:     3,
		Backoff:        "fixed",
		InitialDelayMs: 10,
	}

	var attempts int32
	doFn := func() (*http.Response, error) {
		atomic.AddInt32(&attempts, 1)
		// Return a non-retryable error (not a network error)
		return nil, errors.New("invalid URL format")
	}

	_, err := DoWithRetry(t.Context(), policy, "test", doFn)
	if err == nil {
		t.Fatal("expected error")
	}
	if atomic.LoadInt32(&attempts) != 1 {
		t.Errorf("expected 1 attempt (no retry), got %d",
			atomic.LoadInt32(&attempts))
	}
}

func TestDoWithRetry_ZeroRetries(t *testing.T) {
	policy := pipeline.RetryPolicy{
		MaxRetries:     0,
		Backoff:        "fixed",
		InitialDelayMs: 10,
	}

	var attempts int32
	server := httptest.NewServer(http.HandlerFunc(
		func(w http.ResponseWriter, r *http.Request) {
			atomic.AddInt32(&attempts, 1)
			w.WriteHeader(http.StatusServiceUnavailable)
			_, _ = w.Write([]byte(`error`))
		},
	))
	defer server.Close()

	doFn := func() (*http.Response, error) {
		return http.Get(server.URL)
	}

	_, err := DoWithRetry(t.Context(), policy, "test", doFn)
	if err == nil {
		t.Fatal("expected error")
	}
	if atomic.LoadInt32(&attempts) != 1 {
		t.Errorf("expected 1 attempt (no retries), got %d",
			atomic.LoadInt32(&attempts))
	}
}

func TestDoWithRetry_RespectsRetryAfterHeader(t *testing.T) {
	policy := pipeline.RetryPolicy{
		MaxRetries:     1,
		Backoff:        "exponential",
		InitialDelayMs: 10,
	}

	var attempts int32
	start := time.Now()
	server := httptest.NewServer(http.HandlerFunc(
		func(w http.ResponseWriter, r *http.Request) {
			n := atomic.AddInt32(&attempts, 1)
			if n == 1 {
				w.Header().Set("Retry-After", "1")
				w.WriteHeader(http.StatusTooManyRequests)
				_, _ = w.Write([]byte(`rate limited`))
				return
			}
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`ok`))
		},
	))
	defer server.Close()

	doFn := func() (*http.Response, error) {
		return http.Get(server.URL)
	}

	resp, err := DoWithRetry(t.Context(), policy, "test", doFn)
	elapsed := time.Since(start)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer resp.Body.Close()

	// Should have waited at least ~1 second for Retry-After
	if elapsed < 900*time.Millisecond {
		t.Errorf(
			"expected at least ~1s delay from Retry-After, got %v",
			elapsed,
		)
	}
}

func TestDoWithRetry_RetriesOn504(t *testing.T) {
	policy := pipeline.RetryPolicy{
		MaxRetries:     1,
		Backoff:        "fixed",
		InitialDelayMs: 10,
	}

	var attempts int32
	server := httptest.NewServer(http.HandlerFunc(
		func(w http.ResponseWriter, r *http.Request) {
			n := atomic.AddInt32(&attempts, 1)
			if n == 1 {
				w.WriteHeader(http.StatusGatewayTimeout)
				_, _ = w.Write([]byte(`timeout`))
				return
			}
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`ok`))
		},
	))
	defer server.Close()

	doFn := func() (*http.Response, error) {
		return http.Get(server.URL)
	}

	resp, err := DoWithRetry(t.Context(), policy, "test", doFn)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected 200, got %d", resp.StatusCode)
	}
	if atomic.LoadInt32(&attempts) != 2 {
		t.Errorf("expected 2 attempts, got %d", atomic.LoadInt32(&attempts))
	}
}

func TestRetryableHTTPError_Error(t *testing.T) {
	err := &RetryableHTTPError{
		StatusCode: 503,
		Status:     "503 Service Unavailable",
	}
	msg := err.Error()
	if !strings.Contains(msg, "503 Service Unavailable") {
		t.Errorf("expected error to contain status, got %q", msg)
	}
	if !strings.Contains(msg, "request failed after retries") {
		t.Errorf(
			"expected error to contain retry message, got %q", msg,
		)
	}
}

func TestDoWithRetry_ContextAlreadyCanceled(t *testing.T) {
	policy := pipeline.RetryPolicy{
		MaxRetries:     3,
		Backoff:        "fixed",
		InitialDelayMs: 10,
	}

	ctx, cancel := context.WithCancel(t.Context())
	cancel() // Cancel before calling

	doFn := func() (*http.Response, error) {
		t.Fatal("doFn should not be called with canceled context")
		return nil, nil
	}

	_, err := DoWithRetry(ctx, policy, "test", doFn)
	if err == nil {
		t.Fatal("expected error for canceled context")
	}
	if !errors.Is(err, context.Canceled) {
		t.Errorf("expected context.Canceled, got: %v", err)
	}
}

func TestMakeRawRequest_RetriesOnTransientError(t *testing.T) {
	// Integration test: MakeRawRequest uses DoWithRetry internally
	var attempts int32
	server := httptest.NewServer(http.HandlerFunc(
		func(w http.ResponseWriter, r *http.Request) {
			n := atomic.AddInt32(&attempts, 1)
			if n == 1 {
				w.WriteHeader(http.StatusServiceUnavailable)
				_, _ = w.Write([]byte(`unavailable`))
				return
			}
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"ok":true}`))
		},
	))
	defer server.Close()

	client := &http.Client{Timeout: 5 * time.Second}
	base := NewBaseProvider("test-retry", false, client)
	base.SetRetryPolicy(pipeline.RetryPolicy{
		MaxRetries:     2,
		Backoff:        "fixed",
		InitialDelayMs: 10,
	})

	result, err := base.MakeRawRequest(
		t.Context(), server.URL, []byte(`{}`),
		RequestHeaders{"Content-Type": "application/json"},
		"TestProvider",
	)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if string(result) != `{"ok":true}` {
		t.Errorf("unexpected response: %s", string(result))
	}
	if atomic.LoadInt32(&attempts) != 2 {
		t.Errorf("expected 2 attempts, got %d", atomic.LoadInt32(&attempts))
	}
}

func TestBaseProvider_SetGetRetryPolicy(t *testing.T) {
	client := &http.Client{Timeout: 5 * time.Second}
	base := NewBaseProvider("test", false, client)

	// Verify default policy is set
	defaultPolicy := base.GetRetryPolicy()
	if defaultPolicy.MaxRetries != DefaultMaxRetries {
		t.Errorf(
			"default MaxRetries = %d, want %d",
			defaultPolicy.MaxRetries, DefaultMaxRetries,
		)
	}

	// Set custom policy
	custom := pipeline.RetryPolicy{
		MaxRetries:     5,
		Backoff:        "fixed",
		InitialDelayMs: 1000,
	}
	base.SetRetryPolicy(custom)

	got := base.GetRetryPolicy()
	if got.MaxRetries != 5 {
		t.Errorf("MaxRetries = %d, want 5", got.MaxRetries)
	}
	if got.Backoff != "fixed" {
		t.Errorf("Backoff = %q, want %q", got.Backoff, "fixed")
	}
	if got.InitialDelayMs != 1000 {
		t.Errorf("InitialDelayMs = %d, want 1000", got.InitialDelayMs)
	}
}

func TestDoWithRetry_NetworkErrorExhaustsRetries(t *testing.T) {
	policy := pipeline.RetryPolicy{
		MaxRetries:     2,
		Backoff:        "fixed",
		InitialDelayMs: 10,
	}

	var attempts int32
	doFn := func() (*http.Response, error) {
		atomic.AddInt32(&attempts, 1)
		return nil, &net.OpError{
			Op:  "dial",
			Err: errors.New("connection refused"),
		}
	}

	_, err := DoWithRetry(t.Context(), policy, "test", doFn)
	if err == nil {
		t.Fatal("expected error after exhausting retries")
	}
	if atomic.LoadInt32(&attempts) != 3 {
		t.Errorf("expected 3 attempts, got %d", atomic.LoadInt32(&attempts))
	}
}

func TestDoWithRetry_RetriesResponseBodyClosed(t *testing.T) {
	// Verify that response bodies are properly closed on retry
	policy := pipeline.RetryPolicy{
		MaxRetries:     1,
		Backoff:        "fixed",
		InitialDelayMs: 10,
	}

	var attempts int32
	server := httptest.NewServer(http.HandlerFunc(
		func(w http.ResponseWriter, r *http.Request) {
			n := atomic.AddInt32(&attempts, 1)
			if n == 1 {
				w.WriteHeader(http.StatusTooManyRequests)
				_, _ = w.Write([]byte(`rate limited`))
				return
			}
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`ok`))
		},
	))
	defer server.Close()

	doFn := func() (*http.Response, error) {
		return http.Get(server.URL)
	}

	resp, err := DoWithRetry(t.Context(), policy, "test", doFn)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Read and close final response
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()

	if string(body) != "ok" {
		t.Errorf("expected 'ok', got %q", string(body))
	}
}
