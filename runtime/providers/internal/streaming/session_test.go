package streaming

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/AltairaLabs/PromptKit/runtime/providers"
)

// serverThatSends creates a test server that sends the given messages then waits.
func serverThatSends(t *testing.T, messages []string) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := wsUpgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer conn.Close()

		for _, msg := range messages {
			if err := conn.WriteMessage(websocket.TextMessage, []byte(msg)); err != nil {
				return
			}
		}
		// Keep the connection alive until the client disconnects.
		for {
			if _, _, err := conn.ReadMessage(); err != nil {
				return
			}
		}
	}))
}

// serverThatCloses creates a test server that closes immediately after upgrade.
func serverThatCloses(t *testing.T) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := wsUpgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		// Close immediately with normal closure
		closeMsg := websocket.FormatCloseMessage(websocket.CloseNormalClosure, "bye")
		_ = conn.WriteMessage(websocket.CloseMessage, closeMsg)
		conn.Close()
	}))
}

// simpleHandler parses a JSON message and returns a StreamChunk with the content.
func simpleHandler(data []byte) ([]providers.StreamChunk, error) {
	var msg map[string]string
	if err := json.Unmarshal(data, &msg); err != nil {
		return nil, fmt.Errorf("bad json: %w", err)
	}
	text := msg["text"]
	return []providers.StreamChunk{{Delta: text}}, nil
}

func TestSession_ReceivesMessages(t *testing.T) {
	messages := []string{
		`{"text":"hello"}`,
		`{"text":"world"}`,
	}
	srv := serverThatSends(t, messages)
	defer srv.Close()

	conn := NewConn(&ConnConfig{URL: wsURL(srv)})
	require.NoError(t, conn.ConnectWithRetry(context.Background()))

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	session, err := NewSession(ctx, SessionConfig{
		Conn:      conn,
		OnMessage: simpleHandler,
	})
	require.NoError(t, err)
	defer session.Close()

	var received []string
	for chunk := range session.Response() {
		received = append(received, chunk.Delta)
		if len(received) >= 2 {
			cancel()
		}
	}

	assert.Contains(t, received, "hello")
	assert.Contains(t, received, "world")
}

func TestSession_Send(t *testing.T) {
	srv := echoServer(t)
	defer srv.Close()

	conn := NewConn(&ConnConfig{URL: wsURL(srv)})
	require.NoError(t, conn.ConnectWithRetry(context.Background()))

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	session, err := NewSession(ctx, SessionConfig{
		Conn:      conn,
		OnMessage: simpleHandler,
	})
	require.NoError(t, err)
	defer session.Close()

	// Send a message that the echo server will return
	require.NoError(t, session.Send(map[string]string{"text": "echo test"}))

	select {
	case chunk := <-session.Response():
		assert.Equal(t, "echo test", chunk.Delta)
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for echo response")
	}
}

func TestSession_SendRaw(t *testing.T) {
	srv := echoServer(t)
	defer srv.Close()

	conn := NewConn(&ConnConfig{URL: wsURL(srv)})
	require.NoError(t, conn.ConnectWithRetry(context.Background()))

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	session, err := NewSession(ctx, SessionConfig{
		Conn:      conn,
		OnMessage: simpleHandler,
	})
	require.NoError(t, err)
	defer session.Close()

	require.NoError(t, session.SendRaw([]byte(`{"text":"raw send"}`)))

	select {
	case chunk := <-session.Response():
		assert.Equal(t, "raw send", chunk.Delta)
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for response")
	}
}

func TestSession_Close(t *testing.T) {
	srv := echoServer(t)
	defer srv.Close()

	conn := NewConn(&ConnConfig{URL: wsURL(srv)})
	require.NoError(t, conn.ConnectWithRetry(context.Background()))

	session, err := NewSession(context.Background(), SessionConfig{
		Conn:      conn,
		OnMessage: simpleHandler,
	})
	require.NoError(t, err)

	require.NoError(t, session.Close())
	assert.True(t, session.Closed())

	// Second close should be idempotent
	require.NoError(t, session.Close())

	// Send after close should error
	err = session.Send(map[string]string{"test": "value"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "closed")
}

func TestSession_SendRawAfterClose(t *testing.T) {
	srv := echoServer(t)
	defer srv.Close()

	conn := NewConn(&ConnConfig{URL: wsURL(srv)})
	require.NoError(t, conn.ConnectWithRetry(context.Background()))

	session, err := NewSession(context.Background(), SessionConfig{
		Conn:      conn,
		OnMessage: simpleHandler,
	})
	require.NoError(t, err)
	require.NoError(t, session.Close())

	err = session.SendRaw([]byte("test"))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "closed")
}

func TestSession_Done(t *testing.T) {
	srv := echoServer(t)
	defer srv.Close()

	conn := NewConn(&ConnConfig{URL: wsURL(srv)})
	require.NoError(t, conn.ConnectWithRetry(context.Background()))

	session, err := NewSession(context.Background(), SessionConfig{
		Conn:      conn,
		OnMessage: simpleHandler,
	})
	require.NoError(t, err)

	// Done should not be closed yet
	select {
	case <-session.Done():
		t.Fatal("Done should not be closed before session Close")
	default:
	}

	require.NoError(t, session.Close())

	// Done should be closed after close
	select {
	case <-session.Done():
		// Success
	case <-time.After(time.Second):
		t.Fatal("Done channel should be closed after Close")
	}
}

func TestSession_ErrorOnFatalMessage(t *testing.T) {
	messages := []string{`not valid json`}
	srv := serverThatSends(t, messages)
	defer srv.Close()

	conn := NewConn(&ConnConfig{URL: wsURL(srv)})
	require.NoError(t, conn.ConnectWithRetry(context.Background()))

	handler := func(data []byte) ([]providers.StreamChunk, error) {
		var msg map[string]string
		if err := json.Unmarshal(data, &msg); err != nil {
			return nil, fmt.Errorf("fatal parse error: %w", err)
		}
		return []providers.StreamChunk{{Delta: msg["text"]}}, nil
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	session, err := NewSession(ctx, SessionConfig{
		Conn:      conn,
		OnMessage: handler,
	})
	require.NoError(t, err)
	defer session.Close()

	// Wait for the response channel to drain/close
	for range session.Response() {
		// drain
	}

	// Error should be available
	sessionErr := session.Error()
	require.Error(t, sessionErr)
	assert.Contains(t, sessionErr.Error(), "fatal parse error")
}

func TestSession_MultipleChunksFromOneMessage(t *testing.T) {
	messages := []string{`{"parts":["a","b","c"]}`}
	srv := serverThatSends(t, messages)
	defer srv.Close()

	conn := NewConn(&ConnConfig{URL: wsURL(srv)})
	require.NoError(t, conn.ConnectWithRetry(context.Background()))

	handler := func(data []byte) ([]providers.StreamChunk, error) {
		var msg struct {
			Parts []string `json:"parts"`
		}
		if err := json.Unmarshal(data, &msg); err != nil {
			return nil, err
		}
		chunks := make([]providers.StreamChunk, len(msg.Parts))
		for i, p := range msg.Parts {
			chunks[i] = providers.StreamChunk{Delta: p}
		}
		return chunks, nil
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	session, err := NewSession(ctx, SessionConfig{
		Conn:      conn,
		OnMessage: handler,
	})
	require.NoError(t, err)
	defer session.Close()

	var parts []string
	for chunk := range session.Response() {
		parts = append(parts, chunk.Delta)
		if len(parts) >= 3 {
			cancel()
		}
	}

	assert.Equal(t, []string{"a", "b", "c"}, parts)
}

func TestSession_NilConn(t *testing.T) {
	_, err := NewSession(context.Background(), SessionConfig{
		Conn:      nil,
		OnMessage: simpleHandler,
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "Conn is required")
}

func TestSession_NilOnMessage(t *testing.T) {
	conn := NewConn(&ConnConfig{URL: "ws://localhost:1"})
	_, err := NewSession(context.Background(), SessionConfig{
		Conn:      conn,
		OnMessage: nil,
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "OnMessage is required")
}

func TestSession_Reconnect(t *testing.T) {
	// Track connection count
	var connCount int
	var mu sync.Mutex

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := wsUpgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer conn.Close()

		mu.Lock()
		connCount++
		count := connCount
		mu.Unlock()

		if count == 1 {
			// First connection: send one message then close abruptly
			_ = conn.WriteMessage(websocket.TextMessage, []byte(`{"text":"first"}`))
			time.Sleep(50 * time.Millisecond)
			conn.Close()
			return
		}

		// Second connection: send a message then keep alive
		_ = conn.WriteMessage(websocket.TextMessage, []byte(`{"text":"after_reconnect"}`))
		for {
			if _, _, err := conn.ReadMessage(); err != nil {
				return
			}
		}
	}))
	defer srv.Close()

	wsConn := NewConn(&ConnConfig{
		URL:              wsURL(srv),
		RetryBackoffBase: 10 * time.Millisecond,
	})
	require.NoError(t, wsConn.ConnectWithRetry(context.Background()))

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	session, err := NewSession(ctx, SessionConfig{
		Conn:      wsConn,
		OnMessage: simpleHandler,
		OnError: func(_ error) bool {
			return true // Always reconnect
		},
		OnReconnect: func(_ context.Context, _ *Conn) error {
			return nil // No special setup needed
		},
		MaxReconnectAttempts: 3,
	})
	require.NoError(t, err)
	defer session.Close()

	var received []string
	for chunk := range session.Response() {
		received = append(received, chunk.Delta)
		if len(received) >= 2 {
			cancel()
		}
	}

	assert.Contains(t, received, "first")
	assert.Contains(t, received, "after_reconnect")
}

func TestSession_ReconnectDisabled(t *testing.T) {
	srv := serverThatCloses(t)
	defer srv.Close()

	wsConn := NewConn(&ConnConfig{URL: wsURL(srv)})
	require.NoError(t, wsConn.ConnectWithRetry(context.Background()))

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	session, err := NewSession(ctx, SessionConfig{
		Conn:      wsConn,
		OnMessage: simpleHandler,
		// No OnReconnect, so reconnection is disabled
	})
	require.NoError(t, err)
	defer session.Close()

	// Should close the response channel without reconnecting
	for range session.Response() {
		// drain
	}
}

func TestSession_ReconnectRejectedByClassifier(t *testing.T) {
	srv := serverThatCloses(t)
	defer srv.Close()

	wsConn := NewConn(&ConnConfig{URL: wsURL(srv)})
	require.NoError(t, wsConn.ConnectWithRetry(context.Background()))

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	session, err := NewSession(ctx, SessionConfig{
		Conn:      wsConn,
		OnMessage: simpleHandler,
		OnError: func(_ error) bool {
			return false // Never reconnect
		},
		OnReconnect: func(_ context.Context, _ *Conn) error {
			return nil
		},
		MaxReconnectAttempts: 3,
	})
	require.NoError(t, err)
	defer session.Close()

	for range session.Response() {
		// drain
	}
}

func TestSession_Conn(t *testing.T) {
	srv := echoServer(t)
	defer srv.Close()

	wsConn := NewConn(&ConnConfig{URL: wsURL(srv)})
	require.NoError(t, wsConn.ConnectWithRetry(context.Background()))

	session, err := NewSession(context.Background(), SessionConfig{
		Conn:      wsConn,
		OnMessage: simpleHandler,
	})
	require.NoError(t, err)
	defer session.Close()

	assert.Equal(t, wsConn, session.Conn())
}

func TestSession_ErrorWhenNoError(t *testing.T) {
	srv := echoServer(t)
	defer srv.Close()

	wsConn := NewConn(&ConnConfig{URL: wsURL(srv)})
	require.NoError(t, wsConn.ConnectWithRetry(context.Background()))

	session, err := NewSession(context.Background(), SessionConfig{
		Conn:      wsConn,
		OnMessage: simpleHandler,
	})
	require.NoError(t, err)
	defer session.Close()

	// Error should be nil when no error has occurred
	assert.NoError(t, session.Error())
}

func TestSession_DefaultResponseChannelSize(t *testing.T) {
	srv := echoServer(t)
	defer srv.Close()

	wsConn := NewConn(&ConnConfig{URL: wsURL(srv)})
	require.NoError(t, wsConn.ConnectWithRetry(context.Background()))

	session, err := NewSession(context.Background(), SessionConfig{
		Conn:                wsConn,
		OnMessage:           simpleHandler,
		ResponseChannelSize: 0, // Should default
	})
	require.NoError(t, err)
	defer session.Close()

	assert.Equal(t, DefaultResponseChannelSize, cap(session.responseCh))
}

func TestSession_CustomResponseChannelSize(t *testing.T) {
	srv := echoServer(t)
	defer srv.Close()

	wsConn := NewConn(&ConnConfig{URL: wsURL(srv)})
	require.NoError(t, wsConn.ConnectWithRetry(context.Background()))

	session, err := NewSession(context.Background(), SessionConfig{
		Conn:                wsConn,
		OnMessage:           simpleHandler,
		ResponseChannelSize: 42,
	})
	require.NoError(t, err)
	defer session.Close()

	assert.Equal(t, 42, cap(session.responseCh))
}

func TestSession_WithLogger(t *testing.T) {
	srv := echoServer(t)
	defer srv.Close()

	log := &testLogger{}
	wsConn := NewConn(&ConnConfig{URL: wsURL(srv)})
	require.NoError(t, wsConn.ConnectWithRetry(context.Background()))

	session, err := NewSession(context.Background(), SessionConfig{
		Conn:      wsConn,
		OnMessage: simpleHandler,
		Logger:    log,
	})
	require.NoError(t, err)

	// Give the receive loop time to start
	time.Sleep(50 * time.Millisecond)
	session.Close()

	log.mu.Lock()
	defer log.mu.Unlock()
	assert.True(t, len(log.messages) > 0, "logger should have received messages")
}

func TestSession_HandlerReturnsEmptySlice(t *testing.T) {
	messages := []string{`{"text":"skip"}`, `{"text":"keep"}`}
	srv := serverThatSends(t, messages)
	defer srv.Close()

	wsConn := NewConn(&ConnConfig{URL: wsURL(srv)})
	require.NoError(t, wsConn.ConnectWithRetry(context.Background()))

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	handler := func(data []byte) ([]providers.StreamChunk, error) {
		var msg map[string]string
		if err := json.Unmarshal(data, &msg); err != nil {
			return nil, err
		}
		if msg["text"] == "skip" {
			return nil, nil // Return nothing for "skip"
		}
		return []providers.StreamChunk{{Delta: msg["text"]}}, nil
	}

	session, err := NewSession(ctx, SessionConfig{
		Conn:      wsConn,
		OnMessage: handler,
	})
	require.NoError(t, err)
	defer session.Close()

	var received []string
	for chunk := range session.Response() {
		received = append(received, chunk.Delta)
		if len(received) >= 1 {
			cancel()
		}
	}

	assert.Equal(t, []string{"keep"}, received)
}
