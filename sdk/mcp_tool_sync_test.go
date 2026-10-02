package sdk

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"sort"
	"sync/atomic"
	"testing"
	"time"

	gosdk "github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/AltairaLabs/PromptKit/runtime/v2/mcp"
	"github.com/AltairaLabs/PromptKit/runtime/v2/tools"
)

func mcpNames(reg *tools.Registry) []string {
	var names []string
	reg.IterateTools(func(name string, _ *tools.ToolDescriptor) {
		if len(name) > 5 && name[:5] == "mcp__" {
			names = append(names, name)
		}
	})
	sort.Strings(names)
	return names
}

func TestRegisterMCPServerTools_ReplacesTheServersTools(t *testing.T) {
	servers := newMockMCPRegistry()
	_ = servers.RegisterServer(mcp.ServerConfig{Name: "s", ToolFilter: &mcp.ToolFilter{Blocklist: []string{"hidden"}}})
	_ = servers.RegisterServer(mcp.ServerConfig{Name: "other"})
	reg := tools.NewRegistry()

	registerMCPServerTools(reg, servers, "other", []mcp.Tool{{Name: "x"}})
	registerMCPServerTools(reg, servers, "s", []mcp.Tool{{Name: "a"}, {Name: "b"}})
	assert.Equal(t, []string{"mcp__other__x", "mcp__s__a", "mcp__s__b"}, mcpNames(reg))

	registerMCPServerTools(reg, servers, "s", []mcp.Tool{{Name: "b", Description: "new"}, {Name: "c"}, {Name: "hidden"}})
	assert.Equal(t, []string{"mcp__other__x", "mcp__s__b", "mcp__s__c"}, mcpNames(reg),
		"a dropped tool is removed, a new one added, the filter still applies, another server is untouched")
	b, err := reg.GetTool("mcp__s__b")
	require.NoError(t, err)
	assert.Equal(t, "new", b.Description, "a changed tool is replaced")
	assert.Equal(t, "mcp", b.Mode)
}

func TestMCPToolSync_UpdatesEveryTrackedRegistry(t *testing.T) {
	servers := newMockMCPRegistry()
	_ = servers.RegisterServer(mcp.ServerConfig{Name: "s"})
	sync := newMCPToolSync(servers)
	parent, fork, closed := tools.NewRegistry(), tools.NewRegistry(), tools.NewRegistry()
	sync.track(parent)
	sync.track(fork)
	sync.track(closed)
	sync.untrack(closed)

	sync.toolsChanged("s", []mcp.Tool{{Name: "a"}})
	assert.Equal(t, []string{"mcp__s__a"}, mcpNames(parent))
	assert.Equal(t, []string{"mcp__s__a"}, mcpNames(fork), "a fork shares the MCP registry, so it is updated too")
	assert.Empty(t, mcpNames(closed), "a closed conversation's registry is no longer updated")
}

func TestConversation_MCPToolListChangeReachesTheToolRegistry(t *testing.T) {
	// A live server adds and removes tools; the conversation's tool registry
	// follows, so the next turn offers the current set.
	server := gosdk.NewServer(&gosdk.Implementation{Name: "changing", Version: "1"}, nil)
	noop := func(context.Context, *gosdk.CallToolRequest, struct{}) (*gosdk.CallToolResult, any, error) {
		return &gosdk.CallToolResult{}, nil, nil
	}
	gosdk.AddTool(server, &gosdk.Tool{Name: "first"}, noop)
	srv := httptest.NewServer(gosdk.NewStreamableHTTPHandler(func(*http.Request) *gosdk.Server { return server }, nil))
	defer srv.Close()

	conv := newTestConversation()
	cfg := &config{mcpServers: []mcp.ServerConfig{{Name: "s", URL: srv.URL, TransportName: mcp.TransportStreamableHTTP}}}
	require.NoError(t, initMCPRegistry(conv, cfg))
	defer conv.mcpRegistry.Close()
	conv.registerMCPExecutors()
	require.Equal(t, []string{"mcp__s__first"}, mcpNames(conv.toolRegistry))

	gosdk.AddTool(server, &gosdk.Tool{Name: "second"}, noop)
	require.Eventually(t, func() bool {
		return assert.ObjectsAreEqual([]string{"mcp__s__first", "mcp__s__second"}, mcpNames(conv.toolRegistry))
	}, 5*time.Second, 10*time.Millisecond, "an added tool is registered")

	server.RemoveTools("first")
	require.Eventually(t, func() bool {
		return assert.ObjectsAreEqual([]string{"mcp__s__second"}, mcpNames(conv.toolRegistry))
	}, 5*time.Second, 10*time.Millisecond, "a removed tool is unregistered")
}

func TestConversation_MCPServerThatFailsToListIsRetried(t *testing.T) {
	// Two servers: "up" lists its tools at once; "late" connects but fails
	// tools/list until the test lets it through. The conversation opens with
	// up's tools, names late in its warning, and registers late's tools once
	// a background retry reaches it. Had the failed listing been degraded to
	// an empty list, late would count as listed and never be retried.
	buf := captureWarnLogs(t)
	noop := func(context.Context, *gosdk.CallToolRequest, struct{}) (*gosdk.CallToolResult, any, error) {
		return &gosdk.CallToolResult{}, nil, nil
	}
	var ready atomic.Bool
	newServer := func(tool string, gated bool) *httptest.Server {
		s := gosdk.NewServer(&gosdk.Implementation{Name: tool, Version: "1"}, nil)
		gosdk.AddTool(s, &gosdk.Tool{Name: tool}, noop)
		if gated {
			s.AddReceivingMiddleware(func(next gosdk.MethodHandler) gosdk.MethodHandler {
				return func(ctx context.Context, method string, req gosdk.Request) (gosdk.Result, error) {
					if method == "tools/list" && !ready.Load() {
						return nil, errors.New("tools not loaded yet")
					}
					return next(ctx, method, req)
				}
			})
		}
		return httptest.NewServer(gosdk.NewStreamableHTTPHandler(func(*http.Request) *gosdk.Server { return s }, nil))
	}
	up := newServer("a", false)
	defer up.Close()
	late := newServer("b", true)
	defer late.Close()

	conv := newTestConversation()
	cfg := &config{mcpServers: []mcp.ServerConfig{
		{Name: "up", URL: up.URL, TransportName: mcp.TransportStreamableHTTP},
		{Name: "late", URL: late.URL, TransportName: mcp.TransportStreamableHTTP},
	}}
	require.NoError(t, initMCPRegistry(conv, cfg))
	conv.mcpTools.retryBase = 10 * time.Millisecond
	conv.mcpTools.retryMax = 50 * time.Millisecond
	defer func() { _ = conv.Close() }()

	conv.registerMCPExecutors()
	assert.Equal(t, []string{"mcp__up__a"}, mcpNames(conv.toolRegistry))
	assert.Contains(t, buf.String(), `"servers":["late"]`, "the warning names the server whose tools are missing")

	ready.Store(true)
	require.Eventually(t, func() bool {
		return assert.ObjectsAreEqual([]string{"mcp__late__b", "mcp__up__a"}, mcpNames(conv.toolRegistry))
	}, 10*time.Second, 20*time.Millisecond, "the late server's tools are registered once it answers")
	_, err := conv.mcpRegistry.GetClientForTool(context.Background(), "b")
	assert.NoError(t, err, "and routed to it")
}

// unreachableMCPRegistry is a registry whose servers can never be reached.
type unreachableMCPRegistry struct {
	*mockMCPRegistry
	attempts atomic.Int32
}

func (r *unreachableMCPRegistry) GetClient(context.Context, string) (mcp.Client, error) {
	r.attempts.Add(1)
	return nil, errors.New("connection refused")
}

func TestMCPToolSync_CloseStopsRetries(t *testing.T) {
	servers := &unreachableMCPRegistry{mockMCPRegistry: newMockMCPRegistry()}
	sync := newMCPToolSync(servers)
	sync.retryBase, sync.retryMax = time.Millisecond, 5*time.Millisecond

	sync.retry("down")
	sync.retry("down")
	require.Eventually(t, func() bool { return servers.attempts.Load() >= 3 }, 5*time.Second, time.Millisecond,
		"an unreachable server keeps being retried")
	sync.mu.Lock()
	assert.Len(t, sync.retrying, 1, "a server is retried by one loop, however often it is asked")
	sync.mu.Unlock()

	sync.close()
	require.Eventually(t, func() bool {
		sync.mu.Lock()
		defer sync.mu.Unlock()
		return len(sync.retrying) == 0
	}, 5*time.Second, time.Millisecond, "closing stops the retry loop")
	sync.retry("down")
	sync.mu.Lock()
	assert.Empty(t, sync.retrying, "no retry starts after close")
	sync.mu.Unlock()
}
