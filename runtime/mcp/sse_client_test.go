package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// sseTestServer spins up a minimal SSE-protocol MCP server with a single
// pluggable handler for JSON-RPC methods. Returns url + cleanup.
func sseTestServer(t *testing.T, handle func(req JSONRPCMessage) JSONRPCMessage) (string, func()) {
	t.Helper()

	type session struct {
		events chan []byte
	}
	var mu sync.Mutex
	sessions := map[string]*session{}

	mux := http.NewServeMux()
	mux.HandleFunc("/sse", func(w http.ResponseWriter, r *http.Request) {
		id := "s1"
		s := &session{events: make(chan []byte, 32)}
		mu.Lock()
		sessions[id] = s
		mu.Unlock()

		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		_, _ = fmt.Fprintf(w, "event: endpoint\ndata: /message?sessionID=%s\n\n", id)
		w.(http.Flusher).Flush()

		for {
			select {
			case <-r.Context().Done():
				return
			case b := <-s.events:
				_, _ = fmt.Fprintf(w, "event: message\ndata: %s\n\n", b)
				w.(http.Flusher).Flush()
			}
		}
	})
	mux.HandleFunc("/message", func(w http.ResponseWriter, r *http.Request) {
		id := r.URL.Query().Get("sessionID")
		mu.Lock()
		s, ok := sessions[id]
		mu.Unlock()
		if !ok {
			http.Error(w, "unknown session", http.StatusBadRequest)
			return
		}
		var req JSONRPCMessage
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		w.WriteHeader(http.StatusAccepted)
		go func() {
			resp := handle(req)
			if resp.JSONRPC == "" {
				resp.JSONRPC = "2.0"
			}
			resp.ID = req.ID
			b, _ := json.Marshal(resp)
			s.events <- b
		}()
	})

	srv := httptest.NewServer(mux)
	return srv.URL, srv.Close
}

func TestSSEClient_Initialize_Succeeds(t *testing.T) {
	url, cleanup := sseTestServer(t, func(req JSONRPCMessage) JSONRPCMessage {
		if req.Method == "initialize" {
			return JSONRPCMessage{Result: json.RawMessage(`{
                "protocolVersion": "2025-06-18",
                "capabilities": {"tools": {}},
                "serverInfo": {"name": "fake", "version": "0.1"}
            }`)}
		}
		return JSONRPCMessage{Error: &JSONRPCError{Code: -32601, Message: "method not found"}}
	})
	defer cleanup()

	c := NewSSEClient(ServerConfig{Name: "x", URL: url})
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	resp, err := c.Initialize(ctx)
	require.NoError(t, err)
	require.NotNil(t, resp)
	assert.Equal(t, "fake", resp.ServerInfo.Name)
	assert.True(t, c.IsAlive())
	require.NoError(t, c.Close())
	assert.False(t, c.IsAlive())
}

func TestSSEClient_ListTools(t *testing.T) {
	url, cleanup := sseTestServer(t, func(req JSONRPCMessage) JSONRPCMessage {
		switch req.Method {
		case "initialize":
			return JSONRPCMessage{Result: json.RawMessage(`{
                "protocolVersion": "2025-06-18",
                "capabilities": {"tools": {}},
                "serverInfo": {"name": "fake", "version": "0.1"}
            }`)}
		case "tools/list":
			return JSONRPCMessage{Result: json.RawMessage(`{
                "tools": [{"name": "Read", "description": "read a file", "inputSchema": {}}]
            }`)}
		}
		return JSONRPCMessage{Error: &JSONRPCError{Code: -32601, Message: "not found"}}
	})
	defer cleanup()

	c := NewSSEClient(ServerConfig{Name: "x", URL: url})
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	_, err := c.Initialize(ctx)
	require.NoError(t, err)

	tools, err := c.ListTools(ctx)
	require.NoError(t, err)
	require.Len(t, tools, 1)
	assert.Equal(t, "Read", tools[0].Name)

	require.NoError(t, c.Close())
}

func TestSSEClient_CallTool(t *testing.T) {
	url, cleanup := sseTestServer(t, func(req JSONRPCMessage) JSONRPCMessage {
		switch req.Method {
		case "initialize":
			return JSONRPCMessage{Result: json.RawMessage(`{
                "protocolVersion": "2025-06-18",
                "capabilities": {"tools": {}},
                "serverInfo": {"name": "fake", "version": "0.1"}
            }`)}
		case "tools/call":
			return JSONRPCMessage{Result: json.RawMessage(`{
                "content": [{"type": "text", "text": "hello"}],
                "isError": false
            }`)}
		}
		return JSONRPCMessage{Error: &JSONRPCError{Code: -32601, Message: "not found"}}
	})
	defer cleanup()

	c := NewSSEClient(ServerConfig{Name: "x", URL: url})
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	_, err := c.Initialize(ctx)
	require.NoError(t, err)

	resp, err := c.CallTool(ctx, "Read", json.RawMessage(`{"path":"/foo"}`))
	require.NoError(t, err)
	require.Len(t, resp.Content, 1)
	assert.Equal(t, "hello", resp.Content[0].Text)
	assert.False(t, resp.IsError)

	require.NoError(t, c.Close())
}

func TestSSEClient_Close_Idempotent(t *testing.T) {
	url, cleanup := sseTestServer(t, func(req JSONRPCMessage) JSONRPCMessage {
		return JSONRPCMessage{Result: json.RawMessage(`{
            "protocolVersion":"2025-06-18","capabilities":{},
            "serverInfo":{"name":"x","version":"0.1"}
        }`)}
	})
	defer cleanup()

	c := NewSSEClient(ServerConfig{Name: "x", URL: url})
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	_, err := c.Initialize(ctx)
	require.NoError(t, err)

	require.NoError(t, c.Close())
	require.NoError(t, c.Close())
	assert.False(t, c.IsAlive())
}

func TestSSEClient_ListTools_NotInitialized(t *testing.T) {
	c := NewSSEClient(ServerConfig{Name: "x", URL: "http://localhost:0"})
	_, err := c.ListTools(context.Background())
	assert.ErrorIs(t, err, ErrClientNotInitialized)
}

func TestSSEClient_CallTool_NotInitialized(t *testing.T) {
	c := NewSSEClient(ServerConfig{Name: "x", URL: "http://localhost:0"})
	_, err := c.CallTool(context.Background(), "x", json.RawMessage(`{}`))
	assert.ErrorIs(t, err, ErrClientNotInitialized)
}

func TestSSEClient_Initialize_AfterClose(t *testing.T) {
	c := NewSSEClient(ServerConfig{Name: "x", URL: "http://localhost:0"})
	require.NoError(t, c.Close())
	_, err := c.Initialize(context.Background())
	assert.ErrorIs(t, err, ErrClientClosed)
}

func TestSSEClient_Initialize_Idempotent(t *testing.T) {
	url, cleanup := sseTestServer(t, func(req JSONRPCMessage) JSONRPCMessage {
		return JSONRPCMessage{Result: json.RawMessage(`{
            "protocolVersion":"2025-06-18","capabilities":{},
            "serverInfo":{"name":"fake","version":"0.1"}
        }`)}
	})
	defer cleanup()

	c := NewSSEClient(ServerConfig{Name: "x", URL: url})
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	r1, err := c.Initialize(ctx)
	require.NoError(t, err)
	r2, err := c.Initialize(ctx)
	require.NoError(t, err)
	assert.Same(t, r1, r2)
	require.NoError(t, c.Close())
}
