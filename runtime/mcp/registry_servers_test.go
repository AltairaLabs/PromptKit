package mcp

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	gosdk "github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The tests in this file run the registry against real MCP servers (the
// official go-sdk over Streamable HTTP, and this test binary as a stdio
// server) so the registry's routing and process accounting are checked
// against the client it really creates, not a stand-in.

type mulIn struct {
	A int `json:"a"`
	B int `json:"b"`
}

type mulOut struct {
	Product int `json:"product"`
}

// serveMul is a Streamable HTTP server with a single "mul" tool, so a
// registry holding it and goSDKTestServer has tools that only one server owns.
func serveMul(t *testing.T) (string, *gosdk.Server) {
	t.Helper()
	s := gosdk.NewServer(&gosdk.Implementation{Name: "mul", Version: "1"}, nil)
	gosdk.AddTool(s, &gosdk.Tool{Name: "mul"},
		func(_ context.Context, _ *gosdk.CallToolRequest, in mulIn) (*gosdk.CallToolResult, mulOut, error) {
			return nil, mulOut{Product: in.A * in.B}, nil
		})
	srv := httptest.NewServer(gosdk.NewStreamableHTTPHandler(
		func(*http.Request) *gosdk.Server { return s }, &gosdk.StreamableHTTPOptions{Stateless: true}))
	t.Cleanup(srv.Close)
	return srv.URL, s
}

// TestRegistry_RoutesEachToolToTheServerThatOwnsIt registers two real servers
// and checks that ListAllTools reports each server's own tools, that
// GetClientForTool hands back a client that actually serves the tool (both
// before any listing, through the index refresh, and after), and that an
// unknown tool is refused.
func TestRegistry_RoutesEachToolToTheServerThatOwnsIt(t *testing.T) {
	ctx := context.Background()
	newReg := func(t *testing.T) *RegistryImpl {
		reg := NewRegistry()
		require.NoError(t, reg.RegisterServer(ServerConfig{Name: "ref", URL: serveStreamable(t, true)}))
		mulURL, _ := serveMul(t)
		require.NoError(t, reg.RegisterServer(ServerConfig{Name: "mul", URL: mulURL}))
		t.Cleanup(func() { _ = reg.Close() })
		return reg
	}

	t.Run("lookup before any listing refreshes the index", func(t *testing.T) {
		reg := newReg(t)
		client, err := reg.GetClientForTool(ctx, "mul")
		require.NoError(t, err)
		res, err := client.CallTool(ctx, "mul", json.RawMessage(`{"a":3,"b":4}`))
		require.NoError(t, err)
		assert.JSONEq(t, `{"product":12}`, string(res.StructuredContent))

		schema, err := reg.GetToolSchema(ctx, "add")
		require.NoError(t, err)
		assert.Equal(t, "add", schema.Name)
		assert.Contains(t, string(schema.OutputSchema), "total")
	})

	t.Run("ListAllTools reports each server's tools and indexes them", func(t *testing.T) {
		reg := newReg(t)
		all, err := reg.ListAllTools(ctx)
		require.NoError(t, err)
		require.Len(t, all, 2)
		assert.Equal(t, []string{"mul"}, toolNames(all["mul"]))
		assert.Equal(t, []string{"add", "ask", "boom", "exit", "slow"}, toolNames(all["ref"]))

		client, err := reg.GetClientForTool(ctx, "add")
		require.NoError(t, err)
		res, err := client.CallTool(ctx, "add", json.RawMessage(`{"a":2,"b":3}`))
		require.NoError(t, err)
		assert.JSONEq(t, `{"total":5}`, string(res.StructuredContent))
	})

	t.Run("ListAllTools indexes a tool added after the client connected", func(t *testing.T) {
		// A stateless server cannot notify the client of the change, so only
		// a fresh listing can route the new tool.
		mulURL, srv := serveMul(t)
		reg := NewRegistry()
		t.Cleanup(func() { _ = reg.Close() })
		require.NoError(t, reg.RegisterServer(ServerConfig{Name: "mul", URL: mulURL}))
		_, err := reg.GetClient(ctx, "mul")
		require.NoError(t, err)

		gosdk.AddTool(srv, &gosdk.Tool{Name: "square"},
			func(_ context.Context, _ *gosdk.CallToolRequest, in mulIn) (*gosdk.CallToolResult, mulOut, error) {
				return nil, mulOut{Product: in.A * in.A}, nil
			})
		all, err := reg.ListAllTools(ctx)
		require.NoError(t, err)
		assert.Equal(t, []string{"mul", "square"}, toolNames(all["mul"]))

		client, err := reg.GetClientForTool(ctx, "square")
		require.NoError(t, err)
		res, err := client.CallTool(ctx, "square", json.RawMessage(`{"a":5,"b":0}`))
		require.NoError(t, err)
		assert.JSONEq(t, `{"product":25}`, string(res.StructuredContent))
	})

	t.Run("an unknown tool is refused", func(t *testing.T) {
		reg := newReg(t)
		_, err := reg.GetClientForTool(ctx, "divide")
		require.EqualError(t, err, "tool divide not found in any MCP server")
	})

	t.Run("an empty registry finds no tool", func(t *testing.T) {
		reg := NewRegistry()
		all, err := reg.ListAllTools(ctx)
		require.NoError(t, err)
		assert.Empty(t, all)
		_, err = reg.GetClientForTool(ctx, "add")
		require.EqualError(t, err, "tool add not found in any MCP server")
	})

	t.Run("unregistering a server removes its tools from routing", func(t *testing.T) {
		reg := newReg(t)
		_, err := reg.ListAllTools(ctx)
		require.NoError(t, err)
		require.NoError(t, reg.UnregisterServer("mul"))
		_, err = reg.GetClientForTool(ctx, "mul")
		require.EqualError(t, err, "tool mul not found in any MCP server")
	})
}

// TestRegistry_ProcessLimitWithRealStdioServers starts real stdio server
// processes and checks the MaxProcesses limit as a caller sees it: a server
// beyond the limit is refused with ErrMaxProcessesReached, unregistering a
// running server frees its slot, and Close frees every slot.
func TestRegistry_ProcessLimitWithRealStdioServers(t *testing.T) {
	ctx := context.Background()
	server := func(name string) ServerConfig {
		cfg := stdioConfig(t, "gosdk")
		cfg.Name = name
		return cfg
	}

	t.Run("limit of one", func(t *testing.T) {
		reg := NewRegistryWithOptions(RegistryOptions{MaxProcesses: 1})
		t.Cleanup(func() { _ = reg.Close() })
		require.NoError(t, reg.RegisterServer(server("a")))
		require.NoError(t, reg.RegisterServer(server("b")))
		require.NoError(t, reg.RegisterServer(server("never-started")))

		_, err := reg.GetClient(ctx, "a")
		require.NoError(t, err)
		assert.Equal(t, 1, reg.ActiveProcessCount())

		_, err = reg.GetClient(ctx, "b")
		require.ErrorIs(t, err, ErrMaxProcessesReached)

		// A server that never started holds no slot, so removing it frees none.
		require.NoError(t, reg.UnregisterServer("never-started"))
		_, err = reg.GetClient(ctx, "b")
		require.ErrorIs(t, err, ErrMaxProcessesReached)

		require.NoError(t, reg.UnregisterServer("a"))
		assert.Equal(t, 0, reg.ActiveProcessCount())
		b, err := reg.GetClient(ctx, "b")
		require.NoError(t, err)
		res, err := b.CallTool(ctx, "add", json.RawMessage(`{"a":1,"b":1}`))
		require.NoError(t, err)
		assert.JSONEq(t, `{"total":2}`, string(res.StructuredContent))

		require.NoError(t, reg.Close())
		assert.Equal(t, 0, reg.ActiveProcessCount())
	})

	t.Run("no limit by default", func(t *testing.T) {
		reg := NewRegistry()
		t.Cleanup(func() { _ = reg.Close() })
		for _, name := range []string{"a", "b", "c"} {
			require.NoError(t, reg.RegisterServer(server(name)))
			_, err := reg.GetClient(ctx, name)
			require.NoError(t, err, name)
		}
		assert.Equal(t, 3, reg.ActiveProcessCount())
	})
}
