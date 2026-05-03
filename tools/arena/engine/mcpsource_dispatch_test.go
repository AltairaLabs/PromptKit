package engine

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/AltairaLabs/PromptKit/pkg/config"
	"github.com/AltairaLabs/PromptKit/runtime/mcp"
	"github.com/AltairaLabs/PromptKit/runtime/tools"
	"github.com/AltairaLabs/PromptKit/tools/arena/mcpsource"
)

// fakeSSEMCPServer stands up an in-process SSE-MCP server speaking the
// JSON-RPC 2.0 wire protocol. It serves a single configurable tool and
// records every CallTool invocation for assertions.
//
// Lifted in spirit from runtime/mcp/sse_client_test.go's sseTestServer,
// kept inline here so this test stays self-contained.
type fakeSSEMCPServer struct {
	URL      string
	close    func()
	tool     mcp.Tool
	callsMu  sync.Mutex
	calls    []recordedCall
	callResp mcp.ToolCallResponse
}

type recordedCall struct {
	Name string
	Args json.RawMessage
}

func newFakeSSEMCPServer(t *testing.T, tool mcp.Tool, callResp mcp.ToolCallResponse) *fakeSSEMCPServer {
	t.Helper()

	srv := &fakeSSEMCPServer{tool: tool, callResp: callResp}

	type session struct {
		events chan []byte
	}
	var sessionsMu sync.Mutex
	sessions := map[string]*session{}
	var sessionCounter atomic.Int64

	mux := http.NewServeMux()

	mux.HandleFunc("/sse", func(w http.ResponseWriter, r *http.Request) {
		id := fmt.Sprintf("s%d", sessionCounter.Add(1))
		s := &session{events: make(chan []byte, 32)}
		sessionsMu.Lock()
		sessions[id] = s
		sessionsMu.Unlock()

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
		sessionsMu.Lock()
		s, ok := sessions[id]
		sessionsMu.Unlock()
		if !ok {
			http.Error(w, "unknown session", http.StatusBadRequest)
			return
		}
		var req mcp.JSONRPCMessage
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		w.WriteHeader(http.StatusAccepted)

		go func() {
			resp := srv.handle(req)
			if resp.JSONRPC == "" {
				resp.JSONRPC = "2.0"
			}
			resp.ID = req.ID
			b, _ := json.Marshal(resp)
			s.events <- b
		}()
	})

	httpSrv := httptest.NewServer(mux)
	srv.URL = httpSrv.URL
	srv.close = httpSrv.Close
	t.Cleanup(httpSrv.Close)
	return srv
}

func (s *fakeSSEMCPServer) handle(req mcp.JSONRPCMessage) mcp.JSONRPCMessage {
	switch req.Method {
	case "initialize":
		return mcp.JSONRPCMessage{Result: json.RawMessage(`{
            "protocolVersion": "2025-06-18",
            "capabilities": {"tools": {}},
            "serverInfo": {"name": "fake", "version": "0.1"}
        }`)}
	case "tools/list":
		toolsJSON, _ := json.Marshal(map[string]any{"tools": []mcp.Tool{s.tool}})
		return mcp.JSONRPCMessage{Result: toolsJSON}
	case "tools/call":
		var p struct {
			Name      string          `json:"name"`
			Arguments json.RawMessage `json:"arguments"`
		}
		_ = json.Unmarshal(req.Params, &p)
		s.callsMu.Lock()
		s.calls = append(s.calls, recordedCall{Name: p.Name, Args: p.Arguments})
		s.callsMu.Unlock()

		respJSON, _ := json.Marshal(s.callResp)
		return mcp.JSONRPCMessage{Result: respJSON}
	}
	return mcp.JSONRPCMessage{Error: &mcp.JSONRPCError{Code: -32601, Message: "method not found: " + req.Method}}
}

func (s *fakeSSEMCPServer) recordedCalls() []recordedCall {
	s.callsMu.Lock()
	defer s.callsMu.Unlock()
	out := make([]recordedCall, len(s.calls))
	copy(out, s.calls)
	return out
}

// fakeMCPSource is an MCPSource that returns a fixed URL — typically
// pointing at a fakeSSEMCPServer in tests.
type fakeMCPSource struct{ url string }

func (s fakeMCPSource) Open(_ context.Context, _ map[string]any) (mcpsource.MCPConn, io.Closer, error) {
	return mcpsource.MCPConn{URL: s.url}, io.NopCloser(nil), nil
}

// registerTestSource registers a fakeMCPSource under a unique name and
// returns the name. The mcpsource package panics on duplicate names, so we
// derive the name from the test name to avoid cross-test collisions.
var testSourceCounter atomic.Int64

func registerTestSource(t *testing.T, url string) string {
	t.Helper()
	name := fmt.Sprintf("test-source-%d", testSourceCounter.Add(1))
	mcpsource.RegisterMCPSource(name, fakeMCPSource{url: url})
	return name
}

// TestMCPSourceScope_DiscoversToolsAfterOpen is the key wiring assertion:
// after a session-scoped source is opened, its tools must become resolvable
// through the runtime tools.Registry. Currently this fails because
// mcpSourceScope.openOne registers the MCP server in the MCP registry but
// never triggers MCP tool discovery — so the tools.Registry never learns
// about them and downstream lookups (ProviderStage / Executor) miss.
func TestMCPSourceScope_DiscoversToolsAfterOpen(t *testing.T) {
	srv := newFakeSSEMCPServer(t,
		mcp.Tool{
			Name:        "Read",
			Description: "Read a file",
			InputSchema: json.RawMessage(`{"type":"object","properties":{"path":{"type":"string"}}}`),
		},
		mcp.ToolCallResponse{Content: []mcp.Content{{Type: "text", Text: "file-contents"}}},
	)

	sourceName := registerTestSource(t, srv.URL)

	mcpReg := mcp.NewRegistry()
	t.Cleanup(func() { _ = mcpReg.Close() })
	toolReg := tools.NewRegistry()
	toolReg.RegisterExecutor(tools.NewMCPExecutor(mcpReg))

	scope := newMCPSourceScopeWithTools(mcpReg, toolReg)

	cfg := []config.MCPServerConfig{{
		Name:   "sandbox",
		Source: sourceName,
		Scope:  string(mcpsource.ScopeSession),
	}}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	require.NoError(t, scope.OpenAll(ctx, mcpsource.ScopeSession, "session-1", nil, nil, cfg))
	t.Cleanup(func() {
		for _, err := range scope.CloseAll(mcpsource.ScopeSession, "session-1") {
			t.Logf("close error: %v", err)
		}
	})

	// The MCP server should be registered in the MCP registry.
	_, ok := mcpReg.GetServerConfig("sandbox")
	require.True(t, ok, "MCP server should be registered after Open")

	// Source-backed MCP servers register tools under their raw name —
	// the sandbox is "just another MCP server" from the pack author's
	// perspective.
	desc, lookupErr := toolReg.GetTool("Read")
	require.NoError(t, lookupErr, "tool descriptor should be registered after source open")
	assert.Equal(t, "Read", desc.Name)
	assert.Equal(t, "mcp", desc.Mode)
}

// TestMCPSourceScope_ToolDispatchEndToEnd verifies the full request path:
// once the session source is open, calling the MCP tool through the runtime
// tools.Registry round-trips to the SSE server and returns the response.
func TestMCPSourceScope_ToolDispatchEndToEnd(t *testing.T) {
	srv := newFakeSSEMCPServer(t,
		mcp.Tool{
			Name:        "Read",
			Description: "Read a file",
			InputSchema: json.RawMessage(`{"type":"object","properties":{"path":{"type":"string"}}}`),
		},
		mcp.ToolCallResponse{Content: []mcp.Content{{Type: "text", Text: "fake-file-contents"}}},
	)

	sourceName := registerTestSource(t, srv.URL)

	mcpReg := mcp.NewRegistry()
	t.Cleanup(func() { _ = mcpReg.Close() })
	toolReg := tools.NewRegistry()
	toolReg.RegisterExecutor(tools.NewMCPExecutor(mcpReg))

	scope := newMCPSourceScopeWithTools(mcpReg, toolReg)

	cfg := []config.MCPServerConfig{{
		Name:   "sandbox",
		Source: sourceName,
		Scope:  string(mcpsource.ScopeSession),
	}}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	require.NoError(t, scope.OpenAll(ctx, mcpsource.ScopeSession, "session-1", nil, nil, cfg))
	t.Cleanup(func() {
		for _, err := range scope.CloseAll(mcpsource.ScopeSession, "session-1") {
			t.Logf("close error: %v", err)
		}
	})

	result, err := toolReg.Execute(ctx, "Read", json.RawMessage(`{"path":"/etc/hostname"}`))
	require.NoError(t, err)
	require.NotNil(t, result)
	assert.Empty(t, result.Error, "tool execution should succeed")
	assert.Contains(t, string(result.Result), "fake-file-contents")

	calls := srv.recordedCalls()
	require.Len(t, calls, 1, "expected exactly one MCP tool call to reach the server")
	assert.Equal(t, "Read", calls[0].Name, "MCP server should see the raw tool name")
}
