package mcp

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func streamableClientTestServer(t *testing.T, handle func(req JSONRPCMessage) JSONRPCMessage) (string, func()) {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("/mcp", func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		var req JSONRPCMessage
		_ = json.Unmarshal(body, &req)
		resp := handle(req)
		if resp.JSONRPC == "" {
			resp.JSONRPC = "2.0"
		}
		resp.ID = req.ID
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(resp)
	})
	srv := httptest.NewServer(mux)
	return srv.URL + "/mcp", srv.Close
}

func TestStreamableClient_Initialize_Succeeds(t *testing.T) {
	url, cleanup := streamableClientTestServer(t, func(req JSONRPCMessage) JSONRPCMessage {
		if req.Method == "initialize" {
			return JSONRPCMessage{Result: json.RawMessage(`{
                "protocolVersion": "2025-06-18",
                "capabilities": {"tools": {}},
                "serverInfo": {"name": "fake", "version": "0.1"}
            }`)}
		}
		return JSONRPCMessage{Error: &JSONRPCError{Code: -32601, Message: "not found"}}
	})
	defer cleanup()

	c := NewStreamableClient(ServerConfig{Name: "x", URL: url, TransportName: TransportStreamableHTTP})
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	resp, err := c.Initialize(ctx)
	require.NoError(t, err)
	assert.Equal(t, "fake", resp.ServerInfo.Name)
	assert.True(t, c.IsAlive())
	require.NoError(t, c.Close())
	assert.False(t, c.IsAlive())
}

func TestStreamableClient_ListTools(t *testing.T) {
	url, cleanup := streamableClientTestServer(t, func(req JSONRPCMessage) JSONRPCMessage {
		switch req.Method {
		case "initialize":
			return JSONRPCMessage{Result: json.RawMessage(`{
                "protocolVersion": "2025-06-18","capabilities": {"tools": {}},
                "serverInfo": {"name": "fake","version": "0.1"}}`)}
		case "tools/list":
			return JSONRPCMessage{Result: json.RawMessage(`{"tools":[{"name":"Read","inputSchema":{}}]}`)}
		}
		return JSONRPCMessage{Error: &JSONRPCError{Code: -32601, Message: "not found"}}
	})
	defer cleanup()

	c := NewStreamableClient(ServerConfig{Name: "x", URL: url, TransportName: TransportStreamableHTTP})
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

func TestStreamableClient_CallTool(t *testing.T) {
	url, cleanup := streamableClientTestServer(t, func(req JSONRPCMessage) JSONRPCMessage {
		switch req.Method {
		case "initialize":
			return JSONRPCMessage{Result: json.RawMessage(`{
                "protocolVersion": "2025-06-18","capabilities": {"tools": {}},
                "serverInfo": {"name": "fake","version": "0.1"}}`)}
		case "tools/call":
			return JSONRPCMessage{Result: json.RawMessage(
				`{"content":[{"type":"text","text":"hello"}],"isError":false}`,
			)}
		}
		return JSONRPCMessage{Error: &JSONRPCError{Code: -32601, Message: "not found"}}
	})
	defer cleanup()

	c := NewStreamableClient(ServerConfig{Name: "x", URL: url, TransportName: TransportStreamableHTTP})
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	_, err := c.Initialize(ctx)
	require.NoError(t, err)

	resp, err := c.CallTool(ctx, "Read", json.RawMessage(`{"path":"/foo"}`))
	require.NoError(t, err)
	require.Len(t, resp.Content, 1)
	assert.Equal(t, "hello", resp.Content[0].Text)
	require.NoError(t, c.Close())
}

func TestStreamableClient_NotInitialized(t *testing.T) {
	c := NewStreamableClient(ServerConfig{Name: "x", URL: "http://localhost:0", TransportName: TransportStreamableHTTP})
	_, err := c.ListTools(context.Background())
	assert.ErrorIs(t, err, ErrClientNotInitialized)
	_, err = c.CallTool(context.Background(), "x", json.RawMessage(`{}`))
	assert.ErrorIs(t, err, ErrClientNotInitialized)
}

func TestStreamableClient_Initialize_AfterClose(t *testing.T) {
	c := NewStreamableClient(ServerConfig{Name: "x", URL: "http://x", TransportName: TransportStreamableHTTP})
	require.NoError(t, c.Close())
	_, err := c.Initialize(context.Background())
	assert.ErrorIs(t, err, ErrClientClosed)
}

func TestStreamableClient_Initialize_Idempotent(t *testing.T) {
	url, cleanup := streamableClientTestServer(t, func(_ JSONRPCMessage) JSONRPCMessage {
		return JSONRPCMessage{Result: json.RawMessage(`{
            "protocolVersion":"2025-06-18","capabilities":{},
            "serverInfo":{"name":"fake","version":"0.1"}}`)}
	})
	defer cleanup()

	c := NewStreamableClient(ServerConfig{Name: "x", URL: url, TransportName: TransportStreamableHTTP})
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	r1, err := c.Initialize(ctx)
	require.NoError(t, err)
	r2, err := c.Initialize(ctx)
	require.NoError(t, err)
	assert.Same(t, r1, r2)
	require.NoError(t, c.Close())
}

func TestStreamableClient_Close_Idempotent(t *testing.T) {
	c := NewStreamableClient(ServerConfig{Name: "x", URL: "http://x", TransportName: TransportStreamableHTTP})
	require.NoError(t, c.Close())
	require.NoError(t, c.Close())
	assert.False(t, c.IsAlive())
}
