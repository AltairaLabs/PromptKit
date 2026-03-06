package mcp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDefaultClientOptions(t *testing.T) {
	opts := DefaultClientOptions()

	assert.Equal(t, 30*time.Second, opts.RequestTimeout)
	assert.Equal(t, 10*time.Second, opts.InitTimeout)
	assert.Equal(t, 3, opts.MaxRetries)
	assert.Equal(t, 100*time.Millisecond, opts.RetryDelay)
	assert.True(t, opts.EnableGracefulDegradation)
	assert.Equal(t, 3, opts.MaxReconnectAttempts)
}

func TestClientErrors(t *testing.T) {
	// Test that error constants are defined
	assert.NotNil(t, ErrClientNotInitialized)
	assert.NotNil(t, ErrClientClosed)
	assert.NotNil(t, ErrServerUnresponsive)
	assert.NotNil(t, ErrProcessDied)

	// Test error messages are meaningful
	assert.Contains(t, ErrClientNotInitialized.Error(), "not initialized")
	assert.Contains(t, ErrClientClosed.Error(), "closed")
	assert.Contains(t, ErrServerUnresponsive.Error(), "unresponsive")
	assert.Contains(t, ErrProcessDied.Error(), "died")
}

func TestNewStdioClient(t *testing.T) {
	config := ServerConfig{
		Name:    "test-server",
		Command: "echo",
		Args:    []string{"hello"},
	}

	client := NewStdioClient(config)

	assert.NotNil(t, client)
	assert.Equal(t, config.Name, client.config.Name)
	assert.Equal(t, config.Command, client.config.Command)
	assert.Equal(t, DefaultClientOptions(), client.options)
}

func TestNewStdioClientWithOptions(t *testing.T) {
	config := ServerConfig{
		Name:    "test-server",
		Command: "echo",
	}

	customOpts := ClientOptions{
		RequestTimeout:            5 * time.Second,
		InitTimeout:               2 * time.Second,
		MaxRetries:                5,
		RetryDelay:                50 * time.Millisecond,
		EnableGracefulDegradation: false,
	}

	client := NewStdioClientWithOptions(config, customOpts)

	assert.NotNil(t, client)
	assert.Equal(t, config.Name, client.config.Name)
	assert.Equal(t, customOpts.RequestTimeout, client.options.RequestTimeout)
	assert.Equal(t, customOpts.InitTimeout, client.options.InitTimeout)
	assert.Equal(t, customOpts.MaxRetries, client.options.MaxRetries)
	assert.Equal(t, customOpts.RetryDelay, client.options.RetryDelay)
	assert.False(t, client.options.EnableGracefulDegradation)
}

func TestCheckHealth(t *testing.T) {
	config := ServerConfig{
		Name:    "test-server",
		Command: "echo",
	}

	t.Run("not initialized", func(t *testing.T) {
		client := NewStdioClient(config)
		err := client.checkHealth()
		assert.Equal(t, ErrClientNotInitialized, err)
	})

	t.Run("closed client", func(t *testing.T) {
		client := NewStdioClient(config)
		client.closed = true
		err := client.checkHealth()
		assert.Equal(t, ErrClientClosed, err)
	})

	t.Run("started but no process with reconnect disabled", func(t *testing.T) {
		opts := DefaultClientOptions()
		opts.MaxReconnectAttempts = 0
		client := NewStdioClientWithOptions(config, opts)
		client.started = true
		err := client.checkHealth()
		assert.Equal(t, ErrProcessDied, err)
	})

	t.Run("started but no process with reconnect enabled fails", func(t *testing.T) {
		opts := DefaultClientOptions()
		opts.MaxReconnectAttempts = 1
		opts.RetryDelay = 10 * time.Millisecond
		opts.InitTimeout = 100 * time.Millisecond
		client := NewStdioClientWithOptions(config, opts)
		client.started = true
		// cmd is nil and command is "echo" which won't work as MCP server
		err := client.checkHealth()
		assert.Error(t, err)
		// Should attempt reconnection and fail
		assert.Contains(t, err.Error(), "reconnection failed")
	})
}

func TestIsAlive(t *testing.T) {
	config := ServerConfig{
		Name:    "test-server",
		Command: "echo",
	}

	t.Run("not started", func(t *testing.T) {
		client := NewStdioClient(config)
		assert.False(t, client.IsAlive())
	})

	t.Run("closed", func(t *testing.T) {
		client := NewStdioClient(config)
		client.started = true
		client.closed = true
		assert.False(t, client.IsAlive())
	})
}

func TestHandleNotification(t *testing.T) {
	config := ServerConfig{
		Name:    "test-server",
		Command: "echo",
	}
	client := NewStdioClient(config)

	// Test various notification types - these just log, so we're testing they don't panic
	t.Run("tools list changed", func(t *testing.T) {
		msg := &JSONRPCMessage{Method: "notifications/tools/list_changed"}
		client.handleNotification(msg) // Should not panic
	})

	t.Run("resources list changed", func(t *testing.T) {
		msg := &JSONRPCMessage{Method: "notifications/resources/list_changed"}
		client.handleNotification(msg) // Should not panic
	})

	t.Run("unknown notification", func(t *testing.T) {
		msg := &JSONRPCMessage{Method: "some/unknown/notification"}
		client.handleNotification(msg) // Should not panic
	})
}

func TestHandleMessage(t *testing.T) {
	config := ServerConfig{
		Name:    "test-server",
		Command: "echo",
	}
	client := NewStdioClient(config)

	t.Run("invalid ID type", func(t *testing.T) {
		// ID is a string instead of a number - should log warning and return
		msg := &JSONRPCMessage{ID: "not-a-number"}
		client.handleMessage(msg) // Should not panic
	})

	t.Run("notification message", func(t *testing.T) {
		msg := &JSONRPCMessage{Method: "some/notification"}
		client.handleMessage(msg) // Should route to handleNotification
	})

	t.Run("response with valid ID but no pending request", func(t *testing.T) {
		msg := &JSONRPCMessage{ID: float64(123)}
		client.handleMessage(msg) // Should handle gracefully
	})
}

func TestClose(t *testing.T) {
	config := ServerConfig{
		Name:    "test-server",
		Command: "echo",
	}

	t.Run("close without start", func(t *testing.T) {
		client := NewStdioClient(config)
		err := client.Close()
		assert.NoError(t, err)
		assert.True(t, client.closed)
	})

	t.Run("double close is idempotent", func(t *testing.T) {
		client := NewStdioClient(config)
		err := client.Close()
		assert.NoError(t, err)
		err = client.Close()
		assert.NoError(t, err)
	})
}

func TestUpdateActivity(t *testing.T) {
	config := ServerConfig{
		Name:    "test-server",
		Command: "echo",
	}
	client := NewStdioClient(config)

	assert.Equal(t, int64(0), client.lastActivity.Load())
	client.updateActivity()
	assert.NotEqual(t, int64(0), client.lastActivity.Load())
}

func TestWriteMessage(t *testing.T) {
	config := ServerConfig{
		Name:    "test-server",
		Command: "echo",
	}

	t.Run("nil stdin returns error", func(t *testing.T) {
		client := NewStdioClient(config)
		msg := &JSONRPCMessage{JSONRPC: "2.0", Method: "test"}
		err := client.writeMessage(msg)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "stdin not available")
	})
}

func TestSendNotification_WithPipe(t *testing.T) {
	opts := DefaultClientOptions()
	client, serverReader, serverWriter := newTestClientWithPipes(t, opts)
	defer serverWriter.Close()

	// Drain reads so Write doesn't block
	go func() {
		buf := make([]byte, 4096)
		for {
			if _, err := serverReader.Read(buf); err != nil {
				return
			}
		}
	}()

	t.Run("nil params", func(t *testing.T) {
		err := client.sendNotification("test/notify", nil)
		assert.NoError(t, err)
	})

	t.Run("with params", func(t *testing.T) {
		err := client.sendNotification("test/notify", map[string]string{"key": "val"})
		assert.NoError(t, err)
	})

	serverReader.Close()
}

func TestSendNotification(t *testing.T) {
	config := ServerConfig{
		Name:    "test-server",
		Command: "echo",
	}

	t.Run("nil stdin returns error", func(t *testing.T) {
		client := NewStdioClient(config)
		err := client.sendNotification("test/method", nil)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "stdin not available")
	})

	t.Run("with params nil stdin returns error", func(t *testing.T) {
		client := NewStdioClient(config)
		err := client.sendNotification("test/method", map[string]string{"key": "value"})
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "stdin not available")
	})
}

func TestListTools_Errors(t *testing.T) {
	config := ServerConfig{
		Name:    "test-server",
		Command: "echo",
	}

	t.Run("not initialized", func(t *testing.T) {
		client := NewStdioClient(config)
		tools, err := client.ListTools(context.Background())
		assert.Nil(t, tools)
		assert.ErrorIs(t, err, ErrClientNotInitialized)
	})

	t.Run("closed", func(t *testing.T) {
		client := NewStdioClient(config)
		client.closed = true
		tools, err := client.ListTools(context.Background())
		assert.Nil(t, tools)
		assert.ErrorIs(t, err, ErrClientClosed)
	})
}

func TestCallTool_Errors(t *testing.T) {
	config := ServerConfig{
		Name:    "test-server",
		Command: "echo",
	}

	t.Run("not initialized", func(t *testing.T) {
		client := NewStdioClient(config)
		resp, err := client.CallTool(context.Background(), "test-tool", nil)
		assert.Nil(t, resp)
		assert.ErrorIs(t, err, ErrClientNotInitialized)
	})

	t.Run("closed", func(t *testing.T) {
		client := NewStdioClient(config)
		client.closed = true
		resp, err := client.CallTool(context.Background(), "test-tool", nil)
		assert.Nil(t, resp)
		assert.ErrorIs(t, err, ErrClientClosed)
	})
}

func TestInitialize_AlreadyStarted(t *testing.T) {
	config := ServerConfig{
		Name:    "test-server",
		Command: "echo",
	}

	client := NewStdioClient(config)
	expectedResp := &InitializeResponse{
		ProtocolVersion: "2025-03-26",
	}
	client.started = true
	client.serverInfo = expectedResp

	resp, err := client.Initialize(context.Background())
	assert.NoError(t, err)
	assert.Equal(t, expectedResp, resp)
}

func TestInitialize_Closed(t *testing.T) {
	config := ServerConfig{
		Name:    "test-server",
		Command: "echo",
	}

	client := NewStdioClient(config)
	client.closed = true

	resp, err := client.Initialize(context.Background())
	assert.Nil(t, resp)
	assert.ErrorIs(t, err, ErrClientClosed)
}

func TestInitializeRetryReleasesMutex(t *testing.T) {
	// Use a command that will fail to start, triggering retries
	config := ServerConfig{
		Name:    "test-server",
		Command: "/nonexistent/command/that/does/not/exist",
	}

	opts := DefaultClientOptions()
	opts.MaxRetries = 2
	opts.RetryDelay = 200 * time.Millisecond

	client := NewStdioClientWithOptions(config, opts)

	// Start Initialize in a goroutine - it will retry and eventually fail
	var initWg sync.WaitGroup
	initWg.Add(1)
	go func() {
		defer initWg.Done()
		_, _ = client.Initialize(context.Background())
	}()

	// Give Initialize a moment to start and hit the first failure
	time.Sleep(50 * time.Millisecond)

	// Try to acquire RLock during the retry sleep window.
	// If the mutex is held during sleep, this would block for the full retry delay.
	// We use a tight deadline to detect whether the lock is available.
	lockAcquired := make(chan struct{})
	go func() {
		client.mu.RLock()
		close(lockAcquired)
		client.mu.RUnlock()
	}()

	select {
	case <-lockAcquired:
		// Success: the mutex was released during the retry sleep
	case <-time.After(2 * time.Second):
		t.Fatal("mutex was not released during retry sleep - other operations would be blocked")
	}

	// Wait for Initialize to finish
	initWg.Wait()
}

func TestInitializeRetryCancelledByContext(t *testing.T) {
	// Use a command that will fail to start, triggering retries
	config := ServerConfig{
		Name:    "test-server",
		Command: "/nonexistent/command/that/does/not/exist",
	}

	opts := DefaultClientOptions()
	opts.MaxRetries = 5
	opts.RetryDelay = 5 * time.Second // Long delay to ensure cancellation happens during sleep

	client := NewStdioClientWithOptions(config, opts)

	ctx, cancel := context.WithCancel(context.Background())

	var result error
	done := make(chan struct{})
	go func() {
		_, result = client.Initialize(ctx)
		close(done)
	}()

	// Wait for the first attempt to fail, then cancel during the retry sleep
	time.Sleep(100 * time.Millisecond)
	cancel()

	select {
	case <-done:
		require.Error(t, result)
		assert.ErrorIs(t, result, context.Canceled)
	case <-time.After(2 * time.Second):
		t.Fatal("Initialize did not return promptly after context cancellation")
	}
}

func TestInitializeRetryReturnsClosedAfterReacquire(t *testing.T) {
	// Test that if the client is closed during the retry sleep, Initialize detects it
	config := ServerConfig{
		Name:    "test-server",
		Command: "/nonexistent/command/that/does/not/exist",
	}

	opts := DefaultClientOptions()
	opts.MaxRetries = 3
	opts.RetryDelay = 200 * time.Millisecond

	client := NewStdioClientWithOptions(config, opts)

	var result error
	done := make(chan struct{})
	go func() {
		_, result = client.Initialize(context.Background())
		close(done)
	}()

	// Wait for first attempt to fail, then close the client during retry sleep
	time.Sleep(50 * time.Millisecond)
	client.mu.Lock()
	client.closed = true
	client.mu.Unlock()

	select {
	case <-done:
		require.Error(t, result)
		assert.ErrorIs(t, result, ErrClientClosed)
	case <-time.After(2 * time.Second):
		t.Fatal("Initialize did not return after client was closed during retry")
	}
}

// newTestClientWithPipes creates a StdioClient with io.Pipe-based stdin/stdout for testing.
// Returns the client and the server-side reader/writer to simulate the server.
// A long-running sleep process is started so checkHealth passes; the caller should
// call client.Close() or kill the process when done.
func newTestClientWithPipes(t *testing.T, opts ClientOptions) (*StdioClient, io.ReadCloser, io.WriteCloser) {
	t.Helper()
	config := ServerConfig{
		Name:    "test-server",
		Command: "sleep",
	}
	client := NewStdioClientWithOptions(config, opts)

	// Use pipes instead of a real process
	stdinReader, stdinWriter := io.Pipe()
	stdoutReader, stdoutWriter := io.Pipe()

	client.stdin = stdinWriter
	client.stdout = stdoutReader
	client.started = true

	// Start a real long-running process so checkHealth sees cmd.Process != nil
	client.cmd = exec.Command("sleep", "60")
	require.NoError(t, client.cmd.Start())
	t.Cleanup(func() {
		if client.cmd.Process != nil {
			_ = client.cmd.Process.Kill()
			_ = client.cmd.Wait()
		}
	})

	return client, stdinReader, stdoutWriter
}

func TestWriteMessage_WithPipe(t *testing.T) {
	opts := DefaultClientOptions()
	client, serverReader, serverWriter := newTestClientWithPipes(t, opts)
	defer serverWriter.Close()

	msg := &JSONRPCMessage{
		JSONRPC: "2.0",
		ID:      int64(1),
		Method:  "test/method",
	}

	// Write in a goroutine since pipe is synchronous
	done := make(chan error, 1)
	go func() {
		done <- client.writeMessage(msg)
	}()

	// Read what was written
	buf := make([]byte, 4096)
	n, err := serverReader.Read(buf)
	require.NoError(t, err)

	// Verify JSON + newline was written
	assert.Contains(t, string(buf[:n]), `"jsonrpc":"2.0"`)
	assert.Equal(t, byte('\n'), buf[n-1])

	err = <-done
	assert.NoError(t, err)

	serverReader.Close()
}

func TestSendRequest_Success(t *testing.T) {
	opts := DefaultClientOptions()
	opts.RequestTimeout = 2 * time.Second
	client, serverReader, serverWriter := newTestClientWithPipes(t, opts)

	// Start the read loop so responses get routed
	client.wg.Add(1)
	go client.readLoop()

	// Simulate server: read request, send response
	go func() {
		buf := make([]byte, 4096)
		n, err := serverReader.Read(buf)
		if err != nil {
			return
		}
		var req JSONRPCMessage
		if err := json.Unmarshal(buf[:n-1], &req); err != nil { // -1 for newline
			return
		}
		resp := JSONRPCMessage{
			JSONRPC: "2.0",
			ID:      req.ID,
			Result:  json.RawMessage(`{"status":"ok"}`),
		}
		data, _ := json.Marshal(resp)
		data = append(data, '\n')
		_, _ = serverWriter.Write(data)
	}()

	var result map[string]string
	err := client.sendRequest(context.Background(), "test/method", nil, &result)
	assert.NoError(t, err)
	assert.Equal(t, "ok", result["status"])

	serverReader.Close()
	serverWriter.Close()
	client.wg.Wait()
}

func TestSendRequest_JSONRPCError(t *testing.T) {
	opts := DefaultClientOptions()
	opts.RequestTimeout = 2 * time.Second
	client, serverReader, serverWriter := newTestClientWithPipes(t, opts)

	client.wg.Add(1)
	go client.readLoop()

	go func() {
		buf := make([]byte, 4096)
		n, err := serverReader.Read(buf)
		if err != nil {
			return
		}
		var req JSONRPCMessage
		if err := json.Unmarshal(buf[:n-1], &req); err != nil {
			return
		}
		resp := JSONRPCMessage{
			JSONRPC: "2.0",
			ID:      req.ID,
			Error:   &JSONRPCError{Code: -32600, Message: "Invalid Request"},
		}
		data, _ := json.Marshal(resp)
		data = append(data, '\n')
		_, _ = serverWriter.Write(data)
	}()

	err := client.sendRequest(context.Background(), "test/method", nil, nil)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "Invalid Request")

	serverReader.Close()
	serverWriter.Close()
	client.wg.Wait()
}

func TestSendRequest_Timeout(t *testing.T) {
	opts := DefaultClientOptions()
	opts.RequestTimeout = 50 * time.Millisecond
	client, serverReader, serverWriter := newTestClientWithPipes(t, opts)

	// Read loop but server never responds
	client.wg.Add(1)
	go client.readLoop()

	// Drain stdin so writeMessage doesn't block
	go func() {
		buf := make([]byte, 4096)
		for {
			if _, err := serverReader.Read(buf); err != nil {
				return
			}
		}
	}()

	err := client.sendRequest(context.Background(), "test/method", nil, nil)
	assert.Error(t, err)
	assert.ErrorIs(t, err, ErrServerUnresponsive)

	serverReader.Close()
	serverWriter.Close()
	client.wg.Wait()
}

func TestSendRequest_ContextCancelled(t *testing.T) {
	opts := DefaultClientOptions()
	opts.RequestTimeout = 5 * time.Second
	client, serverReader, serverWriter := newTestClientWithPipes(t, opts)

	client.wg.Add(1)
	go client.readLoop()

	go func() {
		buf := make([]byte, 4096)
		for {
			if _, err := serverReader.Read(buf); err != nil {
				return
			}
		}
	}()

	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		time.Sleep(50 * time.Millisecond)
		cancel()
	}()

	err := client.sendRequest(ctx, "test/method", nil, nil)
	assert.ErrorIs(t, err, context.Canceled)

	serverReader.Close()
	serverWriter.Close()
	client.wg.Wait()
}

func TestSendRequest_WithParams(t *testing.T) {
	opts := DefaultClientOptions()
	opts.RequestTimeout = 2 * time.Second
	client, serverReader, serverWriter := newTestClientWithPipes(t, opts)

	client.wg.Add(1)
	go client.readLoop()

	go func() {
		buf := make([]byte, 4096)
		n, err := serverReader.Read(buf)
		if err != nil {
			return
		}
		var req JSONRPCMessage
		if err := json.Unmarshal(buf[:n-1], &req); err != nil {
			return
		}
		// Verify params were sent
		assert.NotNil(t, req.Params)
		resp := JSONRPCMessage{
			JSONRPC: "2.0",
			ID:      req.ID,
			Result:  json.RawMessage(`{}`),
		}
		data, _ := json.Marshal(resp)
		data = append(data, '\n')
		_, _ = serverWriter.Write(data)
	}()

	params := map[string]string{"key": "value"}
	var result map[string]interface{}
	err := client.sendRequest(context.Background(), "test/method", params, &result)
	assert.NoError(t, err)

	serverReader.Close()
	serverWriter.Close()
	client.wg.Wait()
}

func TestSendRequestWithRetry_Success(t *testing.T) {
	opts := DefaultClientOptions()
	opts.RequestTimeout = 2 * time.Second
	opts.MaxRetries = 2
	opts.RetryDelay = 10 * time.Millisecond
	client, serverReader, serverWriter := newTestClientWithPipes(t, opts)

	client.wg.Add(1)
	go client.readLoop()

	// Server responds successfully on first try
	go func() {
		buf := make([]byte, 4096)
		n, err := serverReader.Read(buf)
		if err != nil {
			return
		}
		var req JSONRPCMessage
		if err := json.Unmarshal(buf[:n-1], &req); err != nil {
			return
		}
		resp := JSONRPCMessage{
			JSONRPC: "2.0",
			ID:      req.ID,
			Result:  json.RawMessage(`{"ok":true}`),
		}
		data, _ := json.Marshal(resp)
		data = append(data, '\n')
		_, _ = serverWriter.Write(data)
	}()

	var result map[string]bool
	err := client.sendRequestWithRetry(context.Background(), "test/method", nil, &result)
	assert.NoError(t, err)
	assert.True(t, result["ok"])

	serverReader.Close()
	serverWriter.Close()
	client.wg.Wait()
}

func TestSendRequestWithRetry_ContextCancelled(t *testing.T) {
	opts := DefaultClientOptions()
	opts.RequestTimeout = 100 * time.Millisecond
	opts.MaxRetries = 3
	opts.RetryDelay = 5 * time.Second // Long delay
	client, serverReader, serverWriter := newTestClientWithPipes(t, opts)

	client.wg.Add(1)
	go client.readLoop()

	// Drain stdin but never respond — first attempt times out, then retry delay starts
	go func() {
		buf := make([]byte, 4096)
		for {
			if _, err := serverReader.Read(buf); err != nil {
				return
			}
		}
	}()

	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		time.Sleep(300 * time.Millisecond) // After first timeout
		cancel()
	}()

	err := client.sendRequestWithRetry(ctx, "test/method", nil, nil)
	assert.Error(t, err)
	// Should get context.Canceled either from sendRequest or from retry delay select
	assert.True(t, err == context.Canceled || err == context.DeadlineExceeded ||
		assert.ObjectsAreEqual(context.Canceled, err))

	serverReader.Close()
	serverWriter.Close()
	client.wg.Wait()
}

func TestReadLoop(t *testing.T) {
	opts := DefaultClientOptions()
	client, _, serverWriter := newTestClientWithPipes(t, opts)

	// Register a pending request
	respChan := make(chan *JSONRPCMessage, 1)
	client.pendingReqs.Store(int64(42), respChan)

	client.wg.Add(1)
	go client.readLoop()

	// Send a response from the server
	resp := JSONRPCMessage{
		JSONRPC: "2.0",
		ID:      float64(42), // JSON numbers are float64
		Result:  json.RawMessage(`{"tools":[]}`),
	}
	data, _ := json.Marshal(resp)
	data = append(data, '\n')
	_, err := serverWriter.Write(data)
	require.NoError(t, err)

	// Wait for the response to be routed
	select {
	case msg := <-respChan:
		assert.NotNil(t, msg)
		assert.Equal(t, float64(42), msg.ID)
	case <-time.After(2 * time.Second):
		t.Fatal("readLoop did not route response to pending request")
	}

	serverWriter.Close()
	client.wg.Wait()
}

func TestReadLoop_InvalidJSON(t *testing.T) {
	opts := DefaultClientOptions()
	client, _, serverWriter := newTestClientWithPipes(t, opts)

	client.wg.Add(1)
	go client.readLoop()

	// Send invalid JSON — readLoop should log error but continue
	_, err := serverWriter.Write([]byte("not valid json\n"))
	require.NoError(t, err)

	// Send valid message after invalid one to prove loop continued
	respChan := make(chan *JSONRPCMessage, 1)
	client.pendingReqs.Store(int64(1), respChan)

	resp := JSONRPCMessage{
		JSONRPC: "2.0",
		ID:      float64(1),
		Result:  json.RawMessage(`{}`),
	}
	data, _ := json.Marshal(resp)
	data = append(data, '\n')
	_, err = serverWriter.Write(data)
	require.NoError(t, err)

	select {
	case msg := <-respChan:
		assert.NotNil(t, msg)
	case <-time.After(2 * time.Second):
		t.Fatal("readLoop did not continue after invalid JSON")
	}

	serverWriter.Close()
	client.wg.Wait()
}

func TestClose_WithPipes(t *testing.T) {
	opts := DefaultClientOptions()
	client, _, serverWriter := newTestClientWithPipes(t, opts)

	client.wg.Add(1)
	go client.readLoop()

	// Closing should clean up everything
	err := client.Close()
	assert.NoError(t, err)
	assert.True(t, client.closed)

	serverWriter.Close()
}

func TestSendRequest_WriteFailure(t *testing.T) {
	opts := DefaultClientOptions()
	opts.RequestTimeout = 1 * time.Second
	client, serverReader, serverWriter := newTestClientWithPipes(t, opts)

	// Close stdin so writes fail
	client.stdin.Close()
	serverReader.Close()
	serverWriter.Close()

	err := client.sendRequest(context.Background(), "test/method", nil, nil)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "failed to write request")
}

func TestSendRequestWithRetry_AllAttemptsFail(t *testing.T) {
	opts := DefaultClientOptions()
	opts.RequestTimeout = 50 * time.Millisecond
	opts.MaxRetries = 1
	opts.RetryDelay = 10 * time.Millisecond
	client, serverReader, serverWriter := newTestClientWithPipes(t, opts)

	client.wg.Add(1)
	go client.readLoop()

	// Drain stdin but never respond — all attempts timeout
	go func() {
		buf := make([]byte, 4096)
		for {
			if _, err := serverReader.Read(buf); err != nil {
				return
			}
		}
	}()

	err := client.sendRequestWithRetry(context.Background(), "test/method", nil, nil)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), fmt.Sprintf("request failed after %d attempts", opts.MaxRetries+1))

	serverReader.Close()
	serverWriter.Close()
	client.wg.Wait()
}

func TestListTools_WithPipe(t *testing.T) {
	opts := DefaultClientOptions()
	opts.RequestTimeout = 2 * time.Second
	opts.MaxRetries = 0
	client, serverReader, serverWriter := newTestClientWithPipes(t, opts)

	client.wg.Add(1)
	go client.readLoop()

	t.Run("success", func(t *testing.T) {
		go func() {
			buf := make([]byte, 4096)
			n, err := serverReader.Read(buf)
			if err != nil {
				return
			}
			var req JSONRPCMessage
			if err := json.Unmarshal(buf[:n-1], &req); err != nil {
				return
			}
			toolsJSON := `{"tools":[{"name":"test-tool","description":"a tool"}]}`
			resp := JSONRPCMessage{
				JSONRPC: "2.0",
				ID:      req.ID,
				Result:  json.RawMessage(toolsJSON),
			}
			data, _ := json.Marshal(resp)
			data = append(data, '\n')
			_, _ = serverWriter.Write(data)
		}()

		tools, err := client.ListTools(context.Background())
		require.NoError(t, err)
		assert.Len(t, tools, 1)
		assert.Equal(t, "test-tool", tools[0].Name)
	})

	serverReader.Close()
	serverWriter.Close()
	client.wg.Wait()
}

func TestListTools_GracefulDegradation(t *testing.T) {
	opts := DefaultClientOptions()
	opts.RequestTimeout = 50 * time.Millisecond
	opts.MaxRetries = 0
	opts.EnableGracefulDegradation = true
	client, serverReader, serverWriter := newTestClientWithPipes(t, opts)

	client.wg.Add(1)
	go client.readLoop()

	// Drain stdin but never respond — triggers timeout
	go func() {
		buf := make([]byte, 4096)
		for {
			if _, err := serverReader.Read(buf); err != nil {
				return
			}
		}
	}()

	tools, err := client.ListTools(context.Background())
	assert.NoError(t, err) // graceful degradation returns empty, not error
	assert.Empty(t, tools)

	serverReader.Close()
	serverWriter.Close()
	client.wg.Wait()
}

func TestListTools_NoGracefulDegradation(t *testing.T) {
	opts := DefaultClientOptions()
	opts.RequestTimeout = 50 * time.Millisecond
	opts.MaxRetries = 0
	opts.EnableGracefulDegradation = false
	client, serverReader, serverWriter := newTestClientWithPipes(t, opts)

	client.wg.Add(1)
	go client.readLoop()

	go func() {
		buf := make([]byte, 4096)
		for {
			if _, err := serverReader.Read(buf); err != nil {
				return
			}
		}
	}()

	tools, err := client.ListTools(context.Background())
	assert.Error(t, err)
	assert.Nil(t, tools)
	assert.Contains(t, err.Error(), "tools/list request failed")

	serverReader.Close()
	serverWriter.Close()
	client.wg.Wait()
}

func TestCallTool_WithPipe(t *testing.T) {
	opts := DefaultClientOptions()
	opts.RequestTimeout = 2 * time.Second
	opts.MaxRetries = 0
	client, serverReader, serverWriter := newTestClientWithPipes(t, opts)

	client.wg.Add(1)
	go client.readLoop()

	go func() {
		buf := make([]byte, 4096)
		n, err := serverReader.Read(buf)
		if err != nil {
			return
		}
		var req JSONRPCMessage
		if err := json.Unmarshal(buf[:n-1], &req); err != nil {
			return
		}
		resultJSON := `{"content":[{"type":"text","text":"result"}]}`
		resp := JSONRPCMessage{
			JSONRPC: "2.0",
			ID:      req.ID,
			Result:  json.RawMessage(resultJSON),
		}
		data, _ := json.Marshal(resp)
		data = append(data, '\n')
		_, _ = serverWriter.Write(data)
	}()

	args := json.RawMessage(`{"input":"test"}`)
	result, err := client.CallTool(context.Background(), "test-tool", args)
	require.NoError(t, err)
	assert.NotNil(t, result)

	serverReader.Close()
	serverWriter.Close()
	client.wg.Wait()
}

func TestCallTool_Error(t *testing.T) {
	opts := DefaultClientOptions()
	opts.RequestTimeout = 50 * time.Millisecond
	opts.MaxRetries = 0
	client, serverReader, serverWriter := newTestClientWithPipes(t, opts)

	client.wg.Add(1)
	go client.readLoop()

	go func() {
		buf := make([]byte, 4096)
		for {
			if _, err := serverReader.Read(buf); err != nil {
				return
			}
		}
	}()

	result, err := client.CallTool(context.Background(), "test-tool", nil)
	assert.Error(t, err)
	assert.Nil(t, result)
	assert.Contains(t, err.Error(), "tools/call request failed")

	serverReader.Close()
	serverWriter.Close()
	client.wg.Wait()
}

func TestLogStderr(t *testing.T) {
	config := ServerConfig{
		Name:    "test-server",
		Command: "echo",
	}
	client := NewStdioClient(config)

	_, stderrWriter := io.Pipe()
	stderrReader, stderrWriterForTest := io.Pipe()
	client.stderr = stderrReader

	client.wg.Add(1)
	go client.logStderr()

	// Write some stderr output
	_, err := stderrWriterForTest.Write([]byte("test stderr output\n"))
	require.NoError(t, err)

	// Close to end the goroutine
	stderrWriterForTest.Close()
	stderrWriter.Close()
	client.wg.Wait()
}

func TestInitialize_ProcessStartsButHandshakeFails(t *testing.T) {
	// Use "cat" which starts successfully but won't respond with valid JSON-RPC.
	// With equal InitTimeout and RequestTimeout, either timeout may fire first,
	// producing either "initialization timeout" or "initialize request failed".
	config := ServerConfig{
		Name:    "test-server",
		Command: "cat",
	}

	opts := DefaultClientOptions()
	opts.MaxRetries = 0
	opts.InitTimeout = 200 * time.Millisecond
	opts.RequestTimeout = 200 * time.Millisecond

	client := NewStdioClientWithOptions(config, opts)

	resp, err := client.Initialize(context.Background())
	assert.Nil(t, resp)
	assert.Error(t, err)
	errMsg := err.Error()
	assert.True(t,
		strings.Contains(errMsg, "initialize request failed") ||
			strings.Contains(errMsg, "initialization timeout"),
		"expected init failure error, got: %s", errMsg)
}

func TestInitialize_InitTimeout(t *testing.T) {
	// Test the DeadlineExceeded path in Initialize where InitTimeout is shorter than RequestTimeout
	config := ServerConfig{
		Name:    "test-server",
		Command: "cat",
	}

	opts := DefaultClientOptions()
	opts.MaxRetries = 0
	opts.InitTimeout = 100 * time.Millisecond
	opts.RequestTimeout = 5 * time.Second // Longer than InitTimeout

	client := NewStdioClientWithOptions(config, opts)

	resp, err := client.Initialize(context.Background())
	assert.Nil(t, resp)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "initialization timeout")
}

func TestInitialize_ProcessStartsHandshakeSucceeds(t *testing.T) {
	// Use "cat" which echoes stdin to stdout — we'll use it as a mock MCP server
	config := ServerConfig{
		Name:    "test-server",
		Command: "cat",
	}

	opts := DefaultClientOptions()
	opts.MaxRetries = 0
	opts.InitTimeout = 2 * time.Second
	opts.RequestTimeout = 2 * time.Second

	client := NewStdioClientWithOptions(config, opts)

	// Start process manually to get stdin/stdout pipes
	client.mu.Lock()
	err := client.startProcess()
	require.NoError(t, err)

	client.wg.Add(1)
	go client.readLoop()
	client.started = true
	client.mu.Unlock()

	// Now simulate the server by reading from the process's stdin pipe
	// and writing responses. But "cat" echoes stdin back to stdout, so
	// if we send a valid JSON-RPC response after reading the request, cat
	// would echo our input. Instead, let's use the pipe directly.
	// Actually with "cat", whatever we write to stdin comes back on stdout.
	// The initialize request gets written to stdin -> cat echoes it to stdout.
	// But the echoed message is a request not a response (has Method field set),
	// so handleMessage won't route it. We need a different approach.

	// Let's test the other Initialize paths instead - close it and test
	// that the error path through sendRequestWithRetry works
	client.Close()

	// Instead, test Initialize end-to-end with a mock that responds properly
	// Use pipes to simulate a server that responds to the initialize request
	client2 := NewStdioClientWithOptions(config, opts)

	stdinReader, stdinWriter := io.Pipe()
	stdoutReader, stdoutWriter := io.Pipe()

	client2.mu.Lock()
	client2.stdin = stdinWriter
	client2.stdout = stdoutReader

	client2.wg.Add(1)
	go client2.readLoop()
	client2.started = true
	client2.cmd = exec.Command("sleep", "60")
	require.NoError(t, client2.cmd.Start())
	client2.mu.Unlock()

	// Simulate server: read requests and respond
	go func() {
		buf := make([]byte, 8192)
		for {
			n, err := stdinReader.Read(buf)
			if err != nil {
				return
			}
			var req JSONRPCMessage
			if err := json.Unmarshal(buf[:n-1], &req); err != nil {
				continue
			}
			if req.Method == "initialize" {
				respBody := InitializeResponse{
					ProtocolVersion: ProtocolVersion,
					ServerInfo:      Implementation{Name: "test", Version: "1.0"},
				}
				resultJSON, _ := json.Marshal(respBody)
				resp := JSONRPCMessage{
					JSONRPC: "2.0",
					ID:      req.ID,
					Result:  resultJSON,
				}
				data, _ := json.Marshal(resp)
				data = append(data, '\n')
				_, _ = stdoutWriter.Write(data)
			}
			// Swallow notifications silently
		}
	}()

	// Call the second half of Initialize manually (sendRequestWithRetry + notification)
	initCtx, cancel := context.WithTimeout(context.Background(), opts.InitTimeout)
	defer cancel()

	initReq := InitializeRequest{
		ProtocolVersion: ProtocolVersion,
		ClientInfo:      Implementation{Name: "promptkit", Version: "0.1.0"},
	}

	var resp InitializeResponse
	err = client2.sendRequestWithRetry(initCtx, "initialize", initReq, &resp)
	require.NoError(t, err)
	assert.Equal(t, ProtocolVersion, resp.ProtocolVersion)
	assert.Equal(t, "test", resp.ServerInfo.Name)

	// Test the notification path (will fail due to stdin but that's non-fatal)
	_ = client2.sendNotification("notifications/initialized", nil)

	stdinReader.Close()
	stdoutWriter.Close()
	_ = client2.cmd.Process.Kill()
	_ = client2.cmd.Wait()
	client2.wg.Wait()
}

func TestCheckHealth_ReconnectionDisabled(t *testing.T) {
	config := ServerConfig{
		Name:    "test-server",
		Command: "echo",
	}

	opts := DefaultClientOptions()
	opts.MaxReconnectAttempts = 0 // Disable auto-reconnection
	client := NewStdioClientWithOptions(config, opts)
	client.started = true
	// cmd is nil — process is dead

	err := client.checkHealth()
	assert.Equal(t, ErrProcessDied, err)
}

func TestCheckHealth_HealthyProcess(t *testing.T) {
	config := ServerConfig{
		Name:    "test-server",
		Command: "sleep",
	}
	client := NewStdioClient(config)
	client.started = true
	client.cmd = exec.Command("sleep", "60")
	require.NoError(t, client.cmd.Start())
	t.Cleanup(func() {
		if client.cmd.Process != nil {
			_ = client.cmd.Process.Kill()
			_ = client.cmd.Wait()
		}
	})

	err := client.checkHealth()
	assert.NoError(t, err)
}

func TestFailPendingRequests(t *testing.T) {
	config := ServerConfig{
		Name:    "test-server",
		Command: "echo",
	}
	client := NewStdioClient(config)

	// Add some pending requests
	ch1 := make(chan *JSONRPCMessage, 1)
	ch2 := make(chan *JSONRPCMessage, 1)
	ch3 := make(chan *JSONRPCMessage, 1)
	client.pendingReqs.Store(int64(1), ch1)
	client.pendingReqs.Store(int64(2), ch2)
	client.pendingReqs.Store(int64(3), ch3)

	client.failPendingRequests()

	// All channels should have received error messages
	for i, ch := range []chan *JSONRPCMessage{ch1, ch2, ch3} {
		select {
		case msg := <-ch:
			require.NotNil(t, msg, "channel %d should have a message", i+1)
			assert.NotNil(t, msg.Error, "channel %d should have an error", i+1)
			assert.Equal(t, -32000, msg.Error.Code)
			assert.Contains(t, msg.Error.Message, "process died")
		default:
			t.Fatalf("channel %d should have received an error message", i+1)
		}
	}

	// Pending requests should be cleared
	count := 0
	client.pendingReqs.Range(func(_, _ interface{}) bool {
		count++
		return true
	})
	assert.Equal(t, 0, count, "pending requests should be cleared")
}

func TestFailPendingRequests_FullChannel(t *testing.T) {
	config := ServerConfig{
		Name:    "test-server",
		Command: "echo",
	}
	client := NewStdioClient(config)

	// Add a pending request with a channel that's already full
	ch := make(chan *JSONRPCMessage, 1)
	ch <- &JSONRPCMessage{} // Fill the channel
	client.pendingReqs.Store(int64(1), ch)

	// Should not panic when channel is full
	client.failPendingRequests()

	count := 0
	client.pendingReqs.Range(func(_, _ interface{}) bool {
		count++
		return true
	})
	assert.Equal(t, 0, count, "pending requests should be cleared even if channel was full")
}

func TestCleanupDeadProcess(t *testing.T) {
	config := ServerConfig{
		Name:    "test-server",
		Command: "sleep",
	}
	client := NewStdioClient(config)

	// Start a real process
	client.cmd = exec.Command("sleep", "60")
	var err error
	client.stdin, err = client.cmd.StdinPipe()
	require.NoError(t, err)
	client.stdout, err = client.cmd.StdoutPipe()
	require.NoError(t, err)
	client.stderr, err = client.cmd.StderrPipe()
	require.NoError(t, err)
	require.NoError(t, client.cmd.Start())

	// cleanupDeadProcess should not panic
	client.cleanupDeadProcess()
}

func TestReconnect_ClientClosed(t *testing.T) {
	config := ServerConfig{
		Name:    "test-server",
		Command: "echo",
	}
	client := NewStdioClient(config)
	client.started = true
	client.closed = true

	err := client.reconnect()
	assert.ErrorIs(t, err, ErrClientClosed)
}

func TestReconnect_AlreadyAlive(t *testing.T) {
	config := ServerConfig{
		Name:    "test-server",
		Command: "sleep",
	}
	client := NewStdioClient(config)
	client.started = true
	client.cmd = exec.Command("sleep", "60")
	require.NoError(t, client.cmd.Start())
	t.Cleanup(func() {
		if client.cmd.Process != nil {
			_ = client.cmd.Process.Kill()
			_ = client.cmd.Wait()
		}
	})

	// reconnect should return nil since process is alive
	err := client.reconnect()
	assert.NoError(t, err)
}

func TestReconnect_FailsWithBadCommand(t *testing.T) {
	config := ServerConfig{
		Name:    "test-server",
		Command: "/nonexistent/command",
	}

	opts := DefaultClientOptions()
	opts.MaxReconnectAttempts = 2
	opts.RetryDelay = 10 * time.Millisecond
	opts.InitTimeout = 100 * time.Millisecond
	client := NewStdioClientWithOptions(config, opts)
	client.started = true
	// cmd is nil — process is dead

	err := client.reconnect()
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "reconnection failed after 2 attempts")
}

func TestReconnect_ClosedDuringAttempt(t *testing.T) {
	config := ServerConfig{
		Name:    "test-server",
		Command: "/nonexistent/command",
	}

	opts := DefaultClientOptions()
	opts.MaxReconnectAttempts = 5
	opts.RetryDelay = 200 * time.Millisecond
	opts.InitTimeout = 100 * time.Millisecond
	client := NewStdioClientWithOptions(config, opts)
	client.started = true

	// Close the client during reconnection
	go func() {
		time.Sleep(50 * time.Millisecond)
		client.mu.Lock()
		client.closed = true
		client.mu.Unlock()
	}()

	err := client.reconnect()
	assert.Error(t, err)
	// Should detect closed state
	assert.True(t,
		errors.Is(err, ErrClientClosed) ||
			strings.Contains(err.Error(), "reconnection failed"),
		"expected closed or failed error, got: %s", err.Error())
}

func TestWaitForReconnect_Success(t *testing.T) {
	config := ServerConfig{
		Name:    "test-server",
		Command: "sleep",
	}
	client := NewStdioClient(config)
	client.started = true
	client.reconnecting = true
	doneCh := make(chan struct{})

	// Simulate reconnection completing
	go func() {
		time.Sleep(50 * time.Millisecond)
		client.mu.Lock()
		client.reconnecting = false
		client.cmd = exec.Command("sleep", "60")
		_ = client.cmd.Start()
		client.mu.Unlock()
		close(doneCh)
	}()
	t.Cleanup(func() {
		if client.cmd != nil && client.cmd.Process != nil {
			_ = client.cmd.Process.Kill()
			_ = client.cmd.Wait()
		}
	})

	err := client.waitForReconnect(doneCh)
	assert.NoError(t, err)
}

func TestWaitForReconnect_ClosedDuringWait(t *testing.T) {
	config := ServerConfig{
		Name:    "test-server",
		Command: "echo",
	}
	client := NewStdioClient(config)
	client.reconnecting = true
	doneCh := make(chan struct{})

	go func() {
		time.Sleep(50 * time.Millisecond)
		client.mu.Lock()
		client.closed = true
		client.reconnecting = false
		client.mu.Unlock()
		close(doneCh)
	}()

	err := client.waitForReconnect(doneCh)
	assert.ErrorIs(t, err, ErrClientClosed)
}

func TestWaitForReconnect_ReconnectFailed(t *testing.T) {
	config := ServerConfig{
		Name:    "test-server",
		Command: "echo",
	}
	client := NewStdioClient(config)
	client.reconnecting = true
	doneCh := make(chan struct{})

	go func() {
		time.Sleep(50 * time.Millisecond)
		client.mu.Lock()
		client.reconnecting = false
		// cmd stays nil — reconnect failed
		client.mu.Unlock()
		close(doneCh)
	}()

	err := client.waitForReconnect(doneCh)
	assert.Equal(t, ErrProcessDied, err)
}

func TestSendRequest_UnmarshalResultError(t *testing.T) {
	opts := DefaultClientOptions()
	opts.RequestTimeout = 2 * time.Second
	client, serverReader, serverWriter := newTestClientWithPipes(t, opts)

	client.wg.Add(1)
	go client.readLoop()

	go func() {
		buf := make([]byte, 4096)
		n, err := serverReader.Read(buf)
		if err != nil {
			return
		}
		var req JSONRPCMessage
		if err := json.Unmarshal(buf[:n-1], &req); err != nil {
			return
		}
		// Send result that can't be unmarshalled into target type
		resp := JSONRPCMessage{
			JSONRPC: "2.0",
			ID:      req.ID,
			Result:  json.RawMessage(`"not an object"`),
		}
		data, _ := json.Marshal(resp)
		data = append(data, '\n')
		_, _ = serverWriter.Write(data)
	}()

	var result struct{ Field int }
	err := client.sendRequest(context.Background(), "test/method", nil, &result)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "failed to unmarshal result")

	serverReader.Close()
	serverWriter.Close()
	client.wg.Wait()
}
