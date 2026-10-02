package sdk

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sort"
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
