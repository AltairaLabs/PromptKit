package providers

import (
	"context"
	"errors"
	"io"
	"net/http"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestStreamRetryPolicy_RetryDelay(t *testing.T) {
	p := StreamRetryPolicy{Enabled: true, InitialDelay: time.Millisecond, MaxDelay: 5 * time.Millisecond}

	t.Run("no Retry-After uses the jittered backoff", func(t *testing.T) {
		d, ok := p.RetryDelay(0, 0)
		require.True(t, ok)
		assert.LessOrEqual(t, d, time.Millisecond)
	})
	t.Run("Retry-After longer than the backoff wins", func(t *testing.T) {
		d, ok := p.RetryDelay(0, 3*time.Second)
		require.True(t, ok)
		assert.Equal(t, 3*time.Second, d, "MaxDelay caps the backoff, not the server's Retry-After")
	})
	t.Run("Retry-After past the default cap is not retried", func(t *testing.T) {
		_, ok := p.RetryDelay(0, DefaultStreamRetryMaxRetryAfter+time.Second)
		assert.False(t, ok)
	})
	t.Run("a configured cap applies", func(t *testing.T) {
		capped := p
		capped.MaxRetryAfter = 10 * time.Second
		_, ok := capped.RetryDelay(0, 11*time.Second)
		assert.False(t, ok)
		d, ok := capped.RetryDelay(0, 10*time.Second)
		require.True(t, ok)
		assert.Equal(t, 10*time.Second, d)
	})
}

// rateLimitedThenOK answers the first request with a 429 carrying retryAfter,
// and later ones with a stream.
func rateLimitedThenOK(t *testing.T, retryAfter string, hits *int32) func(http.ResponseWriter, *http.Request) {
	t.Helper()
	return func(w http.ResponseWriter, _ *http.Request) {
		if atomic.AddInt32(hits, 1) == 1 {
			w.Header().Set("Retry-After", retryAfter)
			w.WriteHeader(http.StatusTooManyRequests)
			_, _ = io.WriteString(w, "rate limited")
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, "data: ok\n\n")
	}
}

// The retry waits for the server's Retry-After rather than its own millisecond
// backoff. A deadline well short of Retry-After but far past the backoff
// shows it: honoring the header, the context expires before the second
// attempt; ignoring it (#2105), the second attempt would land and succeed.
func TestOpenStreamWithRetry_HonorsRetryAfter(t *testing.T) {
	t.Parallel()
	var hits int32
	srv := streamTestServer(t, rateLimitedThenOK(t, "1", &hits))
	defer srv.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	_, err := OpenStreamWithRetry(ctx,
		StreamRetryPolicy{Enabled: true, MaxAttempts: 3, InitialDelay: time.Millisecond, MaxDelay: 5 * time.Millisecond},
		"test", time.Second,
		func(ctx context.Context) (*http.Request, error) {
			return http.NewRequestWithContext(ctx, http.MethodGet, srv.URL, http.NoBody)
		},
		srv.Client(),
	)
	require.ErrorIs(t, err, context.DeadlineExceeded)
	assert.Equal(t, int32(1), atomic.LoadInt32(&hits), "no retry before Retry-After elapsed")
}

// A Retry-After past the policy's cap is not retried early; the rate-limit
// error comes back at once, carrying the delay the server asked for.
func TestOpenStreamWithRetry_RetryAfterPastCapFailsFast(t *testing.T) {
	t.Parallel()
	var hits int32
	srv := streamTestServer(t, rateLimitedThenOK(t, "120", &hits))
	defer srv.Close()

	start := time.Now()
	_, err := OpenStreamWithRetry(context.Background(),
		StreamRetryPolicy{Enabled: true, MaxAttempts: 3, InitialDelay: time.Millisecond, MaxDelay: 5 * time.Millisecond},
		"test", time.Second,
		func(ctx context.Context) (*http.Request, error) {
			return http.NewRequestWithContext(ctx, http.MethodGet, srv.URL, http.NoBody)
		},
		srv.Client(),
	)
	var httpErr *ProviderHTTPError
	require.True(t, errors.As(err, &httpErr), "got %v", err)
	assert.Equal(t, http.StatusTooManyRequests, httpErr.StatusCode)
	assert.Equal(t, 120*time.Second, httpErr.RetryAfter)
	assert.Equal(t, int32(1), atomic.LoadInt32(&hits))
	assert.Less(t, time.Since(start), 5*time.Second, "failed fast instead of waiting")
}
