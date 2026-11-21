package mcp

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/AltairaLabs/PromptKit/runtime/v2/internal/retry"
)

// unavailableServer answers every request 503 and counts them; onHit runs on
// each.
func unavailableServer(t *testing.T, onHit func()) (string, *atomic.Int32) {
	t.Helper()
	var hits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		hits.Add(1)
		if onHit != nil {
			onHit()
		}
		http.Error(w, "down", http.StatusServiceUnavailable)
	}))
	t.Cleanup(srv.Close)
	return srv.URL, &hits
}

// connectHits connects to url with the given retries and returns the error and
// how many requests the server saw.
func connectHits(t *testing.T, ctx context.Context, url string, hits *atomic.Int32, retries int, delay time.Duration) (int32, error) {
	t.Helper()
	hits.Store(0)
	opts := testOptions()
	opts.MaxRetries = retries
	opts.RetryDelay = delay
	_, err := NewStreamableClientWithOptions(ServerConfig{Name: "s", URL: url}, opts).Initialize(ctx)
	return hits.Load(), err
}

// A connect that keeps failing is made MaxRetries+1 times in all.
func TestClient_ConnectIsTriedMaxRetriesPlusOneTimes(t *testing.T) {
	url, hits := unavailableServer(t, nil)
	perConnect, err := connectHits(t, context.Background(), url, hits, 0, time.Millisecond)
	require.Error(t, err)
	require.Positive(t, perConnect)

	total, err := connectHits(t, context.Background(), url, hits, 2, time.Millisecond)
	require.Error(t, err)
	assert.Equal(t, 3*perConnect, total, "MaxRetries=2 is three connects")
}

// A context canceled while waiting to retry ends the connect at once with the
// context's error, without waiting out the backoff.
func TestClient_CancelDuringConnectBackoffStopsRetrying(t *testing.T) {
	url, hits := unavailableServer(t, nil)
	perConnect, _ := connectHits(t, context.Background(), url, hits, 0, time.Millisecond)

	var mu sync.Mutex
	var outcomes []retry.Outcome
	retry.SetObserver(func(name string, o retry.Outcome) {
		if name == "mcp" {
			mu.Lock()
			outcomes = append(outcomes, o)
			mu.Unlock()
		}
	})
	t.Cleanup(func() { retry.SetObserver(nil) })

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	cancelURL, cancelHits := unavailableServer(t, cancel)
	start := time.Now()
	got, err := connectHits(t, ctx, cancelURL, cancelHits, 5, time.Hour)
	assert.True(t, errors.Is(err, context.Canceled), "err = %v, want context.Canceled", err)
	assert.LessOrEqual(t, got, perConnect, "one connect at most, no retry")
	assert.Less(t, time.Since(start), time.Second, "the backoff was waited out")
	mu.Lock()
	defer mu.Unlock()
	assert.Empty(t, outcomes, "a canceled connect is not a retry")
}
