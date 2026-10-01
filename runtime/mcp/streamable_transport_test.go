package mcp

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// streamableTestServer spins up a single-endpoint MCP server that responds
// with application/json. handle is invoked once per POST.
func streamableTestServer(t *testing.T, handle func(req JSONRPCMessage) JSONRPCMessage) (string, func()) {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("/mcp", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		body, err := io.ReadAll(r.Body)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		var req JSONRPCMessage
		if err := json.Unmarshal(body, &req); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
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

func TestStreamableTransport_SendRequest_JSONResponse(t *testing.T) {
	url, cleanup := streamableTestServer(t, func(req JSONRPCMessage) JSONRPCMessage {
		if req.Method == "tools/list" {
			return JSONRPCMessage{Result: json.RawMessage(`{"tools":[{"name":"Read","inputSchema":{}}]}`)}
		}
		return JSONRPCMessage{Error: &JSONRPCError{Code: -32601, Message: "not found"}}
	})
	defer cleanup()

	tr := newStreamableTransport(
		ServerConfig{Name: "x", URL: url, TransportName: TransportStreamableHTTP},
		DefaultClientOptions(), nil,
	)
	defer tr.close()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	var out ToolsListResponse
	err := tr.sendRequest(ctx, "tools/list", nil, &out)
	require.NoError(t, err)
	require.Len(t, out.Tools, 1)
	assert.Equal(t, "Read", out.Tools[0].Name)
}

// streamableSSETestServer responds to the matching method with an SSE body
// containing exactly one JSON-RPC response message keyed to the request id.
func streamableSSETestServer(t *testing.T, method string, result json.RawMessage) (string, func()) {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("/mcp", func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		var req JSONRPCMessage
		_ = json.Unmarshal(body, &req)
		if req.Method != method {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			_ = json.NewEncoder(w).Encode(JSONRPCMessage{
				JSONRPC: "2.0", ID: req.ID,
				Error: &JSONRPCError{Code: -32601, Message: "not found"},
			})
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		resp := JSONRPCMessage{JSONRPC: "2.0", ID: req.ID, Result: result}
		payload, _ := json.Marshal(resp)
		_, _ = w.Write([]byte("event: message\ndata: "))
		_, _ = w.Write(payload)
		_, _ = w.Write([]byte("\n\n"))
		w.(http.Flusher).Flush()
	})
	srv := httptest.NewServer(mux)
	return srv.URL + "/mcp", srv.Close
}

func TestStreamableTransport_SendRequest_SSEResponse(t *testing.T) {
	url, cleanup := streamableSSETestServer(t, "tools/call",
		json.RawMessage(`{"content":[{"type":"text","text":"hi"}],"isError":false}`))
	defer cleanup()

	tr := newStreamableTransport(
		ServerConfig{Name: "x", URL: url, TransportName: TransportStreamableHTTP},
		DefaultClientOptions(), nil,
	)
	defer tr.close()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	var out ToolCallResponse
	err := tr.sendRequest(ctx, "tools/call", ToolCallRequest{Name: "Read"}, &out)
	require.NoError(t, err)
	require.Len(t, out.Content, 1)
	assert.Equal(t, "hi", out.Content[0].Text)
}

func TestStreamableTransport_SendRequest_SSESkipsUnrelatedMessages(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/mcp", func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		var req JSONRPCMessage
		_ = json.Unmarshal(body, &req)
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		// Notification (no id) — should be skipped.
		_, _ = w.Write([]byte(
			"event: message\ndata: {\"jsonrpc\":\"2.0\",\"method\":\"notifications/progress\"}\n\n",
		))
		// Response for the request.
		resp := JSONRPCMessage{JSONRPC: "2.0", ID: req.ID, Result: json.RawMessage(`{"tools":[]}`)}
		payload, _ := json.Marshal(resp)
		_, _ = w.Write([]byte("event: message\ndata: "))
		_, _ = w.Write(payload)
		_, _ = w.Write([]byte("\n\n"))
		w.(http.Flusher).Flush()
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	tr := newStreamableTransport(
		ServerConfig{Name: "x", URL: srv.URL + "/mcp", TransportName: TransportStreamableHTTP},
		DefaultClientOptions(), nil,
	)
	defer tr.close()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	var out ToolsListResponse
	err := tr.sendRequest(ctx, "tools/list", nil, &out)
	require.NoError(t, err)
	assert.Empty(t, out.Tools)
}

func TestStreamableTransport_SessionIDRoundTrip(t *testing.T) {
	var mu sync.Mutex
	var sawSessionOn2 string
	calls := 0
	mux := http.NewServeMux()
	mux.HandleFunc("/mcp", func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		calls++
		n := calls
		mu.Unlock()

		body, _ := io.ReadAll(r.Body)
		var req JSONRPCMessage
		_ = json.Unmarshal(body, &req)

		if n == 1 {
			w.Header().Set("Mcp-Session-Id", "sess-abc")
		} else {
			mu.Lock()
			sawSessionOn2 = r.Header.Get("Mcp-Session-Id")
			mu.Unlock()
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(JSONRPCMessage{
			JSONRPC: "2.0", ID: req.ID, Result: json.RawMessage(`{}`),
		})
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	tr := newStreamableTransport(
		ServerConfig{Name: "x", URL: srv.URL + "/mcp", TransportName: TransportStreamableHTTP},
		DefaultClientOptions(), nil,
	)
	defer tr.close()
	ctx := context.Background()

	require.NoError(t, tr.sendRequest(ctx, "initialize", nil, nil))
	require.NoError(t, tr.sendRequest(ctx, "tools/list", nil, nil))

	mu.Lock()
	assert.Equal(t, "sess-abc", sawSessionOn2)
	mu.Unlock()
}

func TestStreamableTransport_CustomHeadersAndProtocolVersion(t *testing.T) {
	// The server negotiates an older revision than the client offered. Every
	// request after initialize carries the NEGOTIATED version (2025-06-18
	// basic/transports: MCP-Protocol-Version), and initialize carries none.
	type seen struct{ method, auth, version, accept string }
	var mu sync.Mutex
	var got []seen
	mux := http.NewServeMux()
	mux.HandleFunc("/mcp", func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		var req JSONRPCMessage
		_ = json.Unmarshal(body, &req)
		mu.Lock()
		got = append(got, seen{req.Method, r.Header.Get("Authorization"),
			r.Header.Get("MCP-Protocol-Version"), r.Header.Get("Accept")})
		mu.Unlock()
		if req.ID == nil {
			w.WriteHeader(http.StatusAccepted)
			return
		}
		result := `{"tools":[]}`
		if req.Method == "initialize" {
			result = `{"protocolVersion":"2025-03-26","capabilities":{},"serverInfo":{"name":"s","version":"1"}}`
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(JSONRPCMessage{JSONRPC: "2.0", ID: req.ID, Result: json.RawMessage(result)})
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	c := NewStreamableClient(ServerConfig{
		Name:          "x",
		URL:           srv.URL + "/mcp",
		TransportName: TransportStreamableHTTP,
		Headers:       map[string]string{"Authorization": "Bearer tok"},
	})
	defer c.Close()
	_, err := c.Initialize(context.Background())
	require.NoError(t, err)
	_, err = c.ListTools(context.Background())
	require.NoError(t, err)

	mu.Lock()
	defer mu.Unlock()
	require.Len(t, got, 3)
	assert.Equal(t, seen{"initialize", "Bearer tok", "", "application/json, text/event-stream"}, got[0])
	assert.Equal(t, "2025-03-26", got[1].version, "notifications/initialized carries the negotiated version")
	assert.Equal(t, seen{"tools/list", "Bearer tok", "2025-03-26", "application/json, text/event-stream"}, got[2])
}

func TestStreamableTransport_NonOKStatus(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "boom", http.StatusInternalServerError)
	}))
	defer srv.Close()

	tr := newStreamableTransport(
		ServerConfig{Name: "x", URL: srv.URL, TransportName: TransportStreamableHTTP},
		DefaultClientOptions(), nil,
	)
	defer tr.close()
	err := tr.sendRequest(context.Background(), "initialize", nil, nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "status 500")
}

func TestStreamableTransport_UnexpectedContentType(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("nope"))
	}))
	defer srv.Close()

	tr := newStreamableTransport(
		ServerConfig{Name: "x", URL: srv.URL, TransportName: TransportStreamableHTTP},
		DefaultClientOptions(), nil,
	)
	defer tr.close()
	err := tr.sendRequest(context.Background(), "initialize", nil, nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unexpected content type")
}

func TestStreamableTransport_JSONRPCError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		var req JSONRPCMessage
		_ = json.Unmarshal(body, &req)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(JSONRPCMessage{
			JSONRPC: "2.0", ID: req.ID,
			Error: &JSONRPCError{Code: -32601, Message: "method not found"},
		})
	}))
	defer srv.Close()

	tr := newStreamableTransport(
		ServerConfig{Name: "x", URL: srv.URL, TransportName: TransportStreamableHTTP},
		DefaultClientOptions(), nil,
	)
	defer tr.close()
	err := tr.sendRequest(context.Background(), "tools/list", nil, nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "method not found")
}

func TestStreamableTransport_AcceptedStatus(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusAccepted)
	}))
	defer srv.Close()

	tr := newStreamableTransport(
		ServerConfig{Name: "x", URL: srv.URL, TransportName: TransportStreamableHTTP},
		DefaultClientOptions(), nil,
	)
	defer tr.close()
	// A notification is acknowledged with 202 and no body.
	require.NoError(t, tr.sendNotification(context.Background(), "notifications/initialized", nil))
	// A request must get a response; a 202 leaves it unanswered.
	err := tr.sendRequest(context.Background(), "tools/list", nil, nil)
	require.ErrorIs(t, err, errNoResponse)
}

func TestStreamableTransport_Close_Idempotent(t *testing.T) {
	tr := newStreamableTransport(
		ServerConfig{Name: "x", URL: "http://x", TransportName: TransportStreamableHTTP},
		DefaultClientOptions(), nil,
	)
	tr.close()
	tr.close() // must not panic
	assert.False(t, tr.alive.Load())
}

func TestStreamableTransport_SendAfterClose(t *testing.T) {
	tr := newStreamableTransport(
		ServerConfig{Name: "x", URL: "http://x", TransportName: TransportStreamableHTTP},
		DefaultClientOptions(), nil,
	)
	tr.close()
	err := tr.sendRequest(context.Background(), "x", nil, nil)
	assert.ErrorIs(t, err, ErrClientClosed)
}
