package streaming

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

// refusingServer refuses every handshake with 503, counting them; onHit runs
// on each.
func refusingServer(t *testing.T, header http.Header, onHit func()) (*httptest.Server, *atomic.Int32) {
	t.Helper()
	var hits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		hits.Add(1)
		if onHit != nil {
			onHit()
		}
		for k, v := range header {
			w.Header()[k] = v
		}
		http.Error(w, "busy", http.StatusServiceUnavailable)
	}))
	t.Cleanup(srv.Close)
	return srv, &hits
}

// warnCounter is a Logger that counts Warn lines by message.
type warnCounter struct {
	noopLogger
	mu    sync.Mutex
	warns map[string]int
}

func (w *warnCounter) Warn(msg string, _ ...interface{}) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.warns[msg]++
}

// MaxRetries is the number of connection attempts in all, each failure is
// logged, and the error after the last one says how many there were.
func TestConn_ConnectWithRetry_MakesMaxRetriesAttempts(t *testing.T) {
	srv, hits := refusingServer(t, nil, nil)
	log := &warnCounter{warns: map[string]int{}}
	c := NewConn(&ConnConfig{
		URL: wsURL(srv), MaxRetries: 4, RetryBackoffBase: time.Millisecond, RetryBackoffMax: time.Millisecond, Logger: log,
	})

	err := c.ConnectWithRetry(context.Background())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to connect after 4 attempts: failed to connect: websocket: bad handshake")
	assert.Equal(t, int32(4), hits.Load())
	assert.Equal(t, map[string]int{"connection attempt failed": 4}, log.warns)
}

// A connect that succeeds logs no failed attempt.
func TestConn_ConnectWithRetry_SuccessLogsNoFailure(t *testing.T) {
	srv := echoServer(t)
	defer srv.Close()
	log := &warnCounter{warns: map[string]int{}}
	c := NewConn(&ConnConfig{URL: wsURL(srv), Logger: log})
	require.NoError(t, c.ConnectWithRetry(context.Background()))
	defer c.Close()
	assert.Empty(t, log.warns)
}

// A context canceled while waiting to reconnect ends the retries at once with
// the context's error.
func TestConn_ConnectWithRetry_CancelDuringBackoffStopsRetrying(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	srv, hits := refusingServer(t, nil, cancel)
	c := NewConn(&ConnConfig{URL: wsURL(srv), MaxRetries: 5, RetryBackoffBase: time.Hour, RetryBackoffMax: time.Hour})

	start := time.Now()
	err := c.ConnectWithRetry(ctx)
	assert.Equal(t, context.Canceled, err, "the context's own error, not wrapped as exhaustion")
	assert.Equal(t, int32(1), hits.Load())
	assert.Less(t, time.Since(start), time.Second, "the backoff was waited out")
}

// A handshake refused with Retry-After is retried after what the server asked
// for, not the computed backoff, while that is within RetryBackoffMax.
func TestConn_HandshakeRetryAfterReplacesTheBackoff(t *testing.T) {
	srv, _ := refusingServer(t, http.Header{"Retry-After": {"7"}}, nil)
	c := NewConn(&ConnConfig{URL: wsURL(srv), RetryBackoffBase: time.Hour, RetryBackoffMax: 10 * time.Second})

	err := c.Connect(context.Background())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to connect: websocket: bad handshake")

	p := connectPolicy(&c.cfg)
	retry, after := p.Classify(err)
	assert.True(t, retry)
	wait, ok := p.Wait(0, after)
	assert.True(t, ok)
	assert.Equal(t, 7*time.Second, wait)
}

// A handshake refused with a Retry-After past RetryBackoffMax is not waited
// out: ConnectWithRetry gives up after that one attempt, at once.
func TestConn_HandshakeRetryAfterPastTheMaxStopsRetrying(t *testing.T) {
	srv, hits := refusingServer(t, http.Header{"Retry-After": {"3600"}}, nil)
	c := NewConn(&ConnConfig{URL: wsURL(srv), MaxRetries: 5, RetryBackoffBase: time.Millisecond, RetryBackoffMax: 10 * time.Second})

	start := time.Now()
	err := c.ConnectWithRetry(context.Background())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to connect after 1 attempts")
	assert.Equal(t, int32(1), hits.Load())
	assert.Less(t, time.Since(start), time.Second, "the Retry-After was waited")
}
