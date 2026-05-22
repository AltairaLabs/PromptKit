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

// TestStreamableHTTP_EndToEnd_ViaRegistry verifies that a ServerConfig with
// TransportName=streamable_http drives the registry to dispatch to a
// StreamableClient that can complete the full Initialize/ListTools/CallTool
// cycle against a Streamable HTTP server.
func TestStreamableHTTP_EndToEnd_ViaRegistry(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		var req JSONRPCMessage
		_ = json.Unmarshal(body, &req)

		var resp JSONRPCMessage
		resp.JSONRPC = "2.0"
		resp.ID = req.ID
		switch req.Method {
		case "initialize":
			resp.Result = json.RawMessage(
				`{"protocolVersion":"2025-06-18","capabilities":{"tools":{}},` +
					`"serverInfo":{"name":"fake","version":"0.1"}}`,
			)
		case "tools/list":
			resp.Result = json.RawMessage(`{"tools":[{"name":"weather","inputSchema":{}}]}`)
		case "tools/call":
			resp.Result = json.RawMessage(`{"content":[{"type":"text","text":"sunny"}],"isError":false}`)
		default:
			resp.Error = &JSONRPCError{Code: -32601, Message: "not found"}
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer srv.Close()

	reg := NewRegistry()
	defer func() { _ = reg.Close() }()

	require.NoError(t, reg.RegisterServer(ServerConfig{
		Name:          "weather",
		URL:           srv.URL,
		TransportName: TransportStreamableHTTP,
	}))

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	client, err := reg.GetClient(ctx, "weather")
	require.NoError(t, err)
	_, ok := client.(*StreamableClient)
	require.True(t, ok, "registry dispatched to %T, expected *StreamableClient", client)

	tools, err := client.ListTools(ctx)
	require.NoError(t, err)
	require.Len(t, tools, 1)
	assert.Equal(t, "weather", tools[0].Name)

	result, err := client.CallTool(ctx, "weather", json.RawMessage(`{"latitude":51.5,"longitude":-0.1}`))
	require.NoError(t, err)
	require.Len(t, result.Content, 1)
	assert.Equal(t, "sunny", result.Content[0].Text)
}
