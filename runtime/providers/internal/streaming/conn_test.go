package streaming

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// wsUpgrader is the test WebSocket upgrader.
var wsUpgrader = websocket.Upgrader{
	CheckOrigin: func(_ *http.Request) bool { return true },
}

// echoServer returns a test server that echoes WebSocket messages back.
func echoServer(t *testing.T) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := wsUpgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer conn.Close()
		for {
			mt, data, err := conn.ReadMessage()
			if err != nil {
				return
			}
			if err := conn.WriteMessage(mt, data); err != nil {
				return
			}
		}
	}))
}

// wsURL converts an HTTP test server URL to a WebSocket URL.
func wsURL(server *httptest.Server) string {
	return "ws" + strings.TrimPrefix(server.URL, "http")
}

func TestConn_ConnectAndSendReceive(t *testing.T) {
	srv := echoServer(t)
	defer srv.Close()

	c := NewConn(&ConnConfig{URL: wsURL(srv)})
	ctx := context.Background()

	require.NoError(t, c.Connect(ctx))
	defer c.Close()

	// Send a JSON message
	msg := map[string]string{"hello": "world"}
	require.NoError(t, c.Send(msg))

	// Receive the echo
	data, err := c.Receive(ctx)
	require.NoError(t, err)

	var got map[string]string
	require.NoError(t, json.Unmarshal(data, &got))
	assert.Equal(t, "world", got["hello"])
}

func TestConn_SendRaw(t *testing.T) {
	srv := echoServer(t)
	defer srv.Close()

	c := NewConn(&ConnConfig{URL: wsURL(srv)})
	ctx := context.Background()
	require.NoError(t, c.Connect(ctx))
	defer c.Close()

	payload := []byte(`{"raw":"message"}`)
	require.NoError(t, c.SendRaw(payload))

	data, err := c.Receive(ctx)
	require.NoError(t, err)
	assert.Equal(t, payload, data)
}

func TestConn_ConnectWithRetry_Success(t *testing.T) {
	srv := echoServer(t)
	defer srv.Close()

	c := NewConn(&ConnConfig{
		URL:        wsURL(srv),
		MaxRetries: 3,
	})

	require.NoError(t, c.ConnectWithRetry(context.Background()))
	defer c.Close()
}

func TestConn_ConnectWithRetry_Failure(t *testing.T) {
	c := NewConn(&ConnConfig{
		URL:              "ws://localhost:1", // Nothing listening
		MaxRetries:       2,
		RetryBackoffBase: 10 * time.Millisecond,
		RetryBackoffMax:  50 * time.Millisecond,
	})

	err := c.ConnectWithRetry(context.Background())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to connect after 2 attempts")
}

func TestConn_ConnectWithRetry_ContextCancelled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel immediately

	c := NewConn(&ConnConfig{
		URL:        "ws://localhost:1",
		MaxRetries: 5,
	})

	err := c.ConnectWithRetry(ctx)
	require.Error(t, err)
	assert.ErrorIs(t, err, context.Canceled)
}

func TestConn_Close_Idempotent(t *testing.T) {
	srv := echoServer(t)
	defer srv.Close()

	c := NewConn(&ConnConfig{URL: wsURL(srv)})
	require.NoError(t, c.Connect(context.Background()))

	require.NoError(t, c.Close())
	require.NoError(t, c.Close()) // second close should succeed
	assert.True(t, c.IsClosed())
}

func TestConn_Close_WithoutConnect(t *testing.T) {
	c := NewConn(&ConnConfig{URL: "ws://localhost:1"})
	require.NoError(t, c.Close())
	assert.True(t, c.IsClosed())
}

func TestConn_SendOnClosed(t *testing.T) {
	srv := echoServer(t)
	defer srv.Close()

	c := NewConn(&ConnConfig{URL: wsURL(srv)})
	require.NoError(t, c.Connect(context.Background()))
	require.NoError(t, c.Close())

	err := c.Send(map[string]string{"test": "value"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not connected")
}

func TestConn_SendRawOnClosed(t *testing.T) {
	srv := echoServer(t)
	defer srv.Close()

	c := NewConn(&ConnConfig{URL: wsURL(srv)})
	require.NoError(t, c.Connect(context.Background()))
	require.NoError(t, c.Close())

	err := c.SendRaw([]byte("test"))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not connected")
}

func TestConn_ReceiveOnClosed(t *testing.T) {
	c := NewConn(&ConnConfig{URL: "ws://localhost:1"})
	_, err := c.Receive(context.Background())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not connected")
}

func TestConn_ReceiveContextCancel(t *testing.T) {
	// Server that never sends
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := wsUpgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer conn.Close()
		// Block until connection is closed by the client.
		select {}
	}))
	defer srv.Close()

	c := NewConn(&ConnConfig{URL: wsURL(srv)})
	require.NoError(t, c.Connect(context.Background()))
	defer c.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	_, err := c.Receive(ctx)
	require.Error(t, err)
	assert.ErrorIs(t, err, context.DeadlineExceeded)
}

func TestConn_ReceiveLoop(t *testing.T) {
	srv := echoServer(t)
	defer srv.Close()

	c := NewConn(&ConnConfig{URL: wsURL(srv)})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	require.NoError(t, c.Connect(ctx))
	defer c.Close()

	msgCh := make(chan []byte, 5)

	// Send 3 messages then cancel
	for i := 0; i < 3; i++ {
		require.NoError(t, c.Send(map[string]int{"n": i}))
	}

	go func() {
		// Give time for messages to echo back
		time.Sleep(100 * time.Millisecond)
		cancel()
	}()

	_ = c.ReceiveLoop(ctx, msgCh)

	// Should have received at least some messages
	close(msgCh)
	var count int
	for range msgCh {
		count++
	}
	assert.GreaterOrEqual(t, count, 1)
}

func TestConn_Heartbeat(t *testing.T) {
	var pingReceived sync.WaitGroup
	pingReceived.Add(1)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := wsUpgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer conn.Close()
		conn.SetPingHandler(func(string) error {
			pingReceived.Done()
			return nil
		})
		// Keep reading to process control frames
		for {
			if _, _, err := conn.ReadMessage(); err != nil {
				return
			}
		}
	}))
	defer srv.Close()

	c := NewConn(&ConnConfig{URL: wsURL(srv)})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	require.NoError(t, c.Connect(ctx))
	defer c.Close()

	c.StartHeartbeat(ctx, 50*time.Millisecond)

	// Wait for at least one ping
	done := make(chan struct{})
	go func() {
		pingReceived.Wait()
		close(done)
	}()

	select {
	case <-done:
		// Success
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for ping")
	}
}

func TestConn_ConnectWhenClosed(t *testing.T) {
	srv := echoServer(t)
	defer srv.Close()

	c := NewConn(&ConnConfig{URL: wsURL(srv)})
	require.NoError(t, c.Close())

	err := c.Connect(context.Background())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "closed")
}

func TestConn_Reset(t *testing.T) {
	srv := echoServer(t)
	defer srv.Close()

	c := NewConn(&ConnConfig{URL: wsURL(srv)})
	require.NoError(t, c.Connect(context.Background()))
	assert.False(t, c.IsClosed())

	c.Reset()
	assert.False(t, c.IsClosed()) // Reset should allow reuse

	// Should be able to connect again
	require.NoError(t, c.Connect(context.Background()))
	defer c.Close()

	// Verify the new connection works
	require.NoError(t, c.Send(map[string]string{"after": "reset"}))
	data, err := c.Receive(context.Background())
	require.NoError(t, err)
	assert.Contains(t, string(data), "reset")
}

func TestConn_SendMarshalError(t *testing.T) {
	srv := echoServer(t)
	defer srv.Close()

	c := NewConn(&ConnConfig{URL: wsURL(srv)})
	require.NoError(t, c.Connect(context.Background()))
	defer c.Close()

	// A channel cannot be JSON-marshaled
	err := c.Send(make(chan int))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to marshal")
}

func TestConnConfig_Defaults(t *testing.T) {
	cfg := &ConnConfig{}
	cfg.defaults()

	assert.Equal(t, DefaultDialTimeout, cfg.DialTimeout)
	assert.Equal(t, DefaultWriteWait, cfg.WriteWait)
	assert.Equal(t, int64(DefaultMaxMessageSize), cfg.MaxMessageSize)
	assert.Equal(t, DefaultMaxRetries, cfg.MaxRetries)
	assert.Equal(t, DefaultRetryBackoffBase, cfg.RetryBackoffBase)
	assert.Equal(t, DefaultRetryBackoffMax, cfg.RetryBackoffMax)
	assert.Equal(t, DefaultCloseGracePeriod, cfg.CloseGracePeriod)
	assert.NotNil(t, cfg.Logger)
}

func TestConnConfig_CustomValues(t *testing.T) {
	cfg := &ConnConfig{
		DialTimeout:      5 * time.Second,
		WriteWait:        3 * time.Second,
		MaxMessageSize:   1024,
		MaxRetries:       7,
		RetryBackoffBase: 500 * time.Millisecond,
		RetryBackoffMax:  10 * time.Second,
		CloseGracePeriod: 2 * time.Second,
	}
	cfg.defaults()

	assert.Equal(t, 5*time.Second, cfg.DialTimeout)
	assert.Equal(t, 3*time.Second, cfg.WriteWait)
	assert.Equal(t, int64(1024), cfg.MaxMessageSize)
	assert.Equal(t, 7, cfg.MaxRetries)
}

func TestCalculateBackoff(t *testing.T) {
	base := 100 * time.Millisecond
	max := 500 * time.Millisecond

	for i := 0; i < 100; i++ {
		d := calculateBackoff(base, max)
		// Should be within +-25% of base, and not exceed max
		assert.LessOrEqual(t, d, max)
		assert.GreaterOrEqual(t, d, time.Duration(0))
	}
}

func TestCalculateBackoff_CapAtMax(t *testing.T) {
	base := 10 * time.Second
	max := 1 * time.Second

	d := calculateBackoff(base, max)
	assert.LessOrEqual(t, d, max)
}

func TestConn_WithLogger(t *testing.T) {
	srv := echoServer(t)
	defer srv.Close()

	log := &testLogger{}
	c := NewConn(&ConnConfig{
		URL:    wsURL(srv),
		Logger: log,
	})

	require.NoError(t, c.Connect(context.Background()))
	defer c.Close()

	assert.True(t, len(log.messages) > 0, "logger should have received messages")
}

type testLogger struct {
	mu       sync.Mutex
	messages []string
}

func (l *testLogger) Debug(msg string, _ ...interface{}) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.messages = append(l.messages, "DEBUG: "+msg)
}

func (l *testLogger) Info(msg string, _ ...interface{}) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.messages = append(l.messages, "INFO: "+msg)
}

func (l *testLogger) Warn(msg string, _ ...interface{}) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.messages = append(l.messages, "WARN: "+msg)
}

func (l *testLogger) Error(msg string, _ ...interface{}) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.messages = append(l.messages, "ERROR: "+msg)
}
