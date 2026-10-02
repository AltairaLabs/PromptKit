package sdk

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"

	"github.com/AltairaLabs/PromptKit/runtime/v2/mcp"
	"github.com/AltairaLabs/PromptKit/runtime/v2/packspec"
	"github.com/AltairaLabs/PromptKit/runtime/v2/tools"
	"github.com/AltairaLabs/PromptKit/sdk/v2/internal/pack"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// mockMCPRegistry is a test mock for mcp.Registry
type mockMCPRegistry struct {
	servers  []mcp.ServerConfig
	tools    map[string][]mcp.Tool
	callFunc func(name string, args json.RawMessage) (*mcp.ToolCallResponse, error)
	closed   bool
	// listErr, when set, makes ListAllTools fail. listCalls counts how many
	// times it was asked, so a test can tell a retry from a latched guard.
	listErr   error
	listCalls int
}

func newMockMCPRegistry() *mockMCPRegistry {
	return &mockMCPRegistry{
		servers: []mcp.ServerConfig{},
		tools:   make(map[string][]mcp.Tool),
	}
}

func (m *mockMCPRegistry) RegisterServer(config mcp.ServerConfig) error {
	m.servers = append(m.servers, config)
	return nil
}

func (m *mockMCPRegistry) UnregisterServer(name string) error {
	return nil
}

func (m *mockMCPRegistry) GetClient(ctx context.Context, serverName string) (mcp.Client, error) {
	return &mockMCPClient{registry: m}, nil
}

func (m *mockMCPRegistry) GetClientForTool(ctx context.Context, toolName string) (mcp.Client, error) {
	return &mockMCPClient{registry: m}, nil
}

func (m *mockMCPRegistry) ListServers() []string {
	names := make([]string, len(m.servers))
	for i, s := range m.servers {
		names[i] = s.Name
	}
	return names
}

func (m *mockMCPRegistry) ListAllTools(ctx context.Context) (map[string][]mcp.Tool, error) {
	m.listCalls++
	if m.listErr != nil {
		return nil, m.listErr
	}
	return m.tools, nil
}

func (m *mockMCPRegistry) GetServerConfig(serverName string) (mcp.ServerConfig, bool) {
	for _, s := range m.servers {
		if s.Name == serverName {
			return s, true
		}
	}
	return mcp.ServerConfig{}, false
}

func (m *mockMCPRegistry) Close() error {
	m.closed = true
	return nil
}

// mockMCPClient is a test mock for mcp.Client
type mockMCPClient struct {
	registry *mockMCPRegistry
}

func (c *mockMCPClient) Initialize(ctx context.Context) (*mcp.InitializeResponse, error) {
	return &mcp.InitializeResponse{
		ServerInfo: mcp.Implementation{Name: "mock", Version: "1.0"},
	}, nil
}

func (c *mockMCPClient) ListTools(ctx context.Context) ([]mcp.Tool, error) {
	var allTools []mcp.Tool
	for _, tools := range c.registry.tools {
		allTools = append(allTools, tools...)
	}
	return allTools, nil
}

func (c *mockMCPClient) CallTool(ctx context.Context, name string, arguments json.RawMessage) (*mcp.ToolCallResponse, error) {
	if c.registry.callFunc != nil {
		return c.registry.callFunc(name, arguments)
	}
	return &mcp.ToolCallResponse{
		Content: []mcp.Content{{Type: "text", Text: "mock result"}},
	}, nil
}

func (c *mockMCPClient) Close() error {
	return nil
}

func (c *mockMCPClient) IsAlive() bool {
	return true
}

func TestWithMCP(t *testing.T) {
	t.Run("adds server config", func(t *testing.T) {
		cfg := &config{}
		opt := WithMCP("test-server", "node", "server.js", "--arg1")
		err := opt(cfg)

		require.NoError(t, err)
		require.Len(t, cfg.mcpServers, 1)
		assert.Equal(t, "test-server", cfg.mcpServers[0].Name)
		assert.Equal(t, "node", cfg.mcpServers[0].Command)
		assert.Equal(t, []string{"server.js", "--arg1"}, cfg.mcpServers[0].Args)
	})

	t.Run("multiple servers", func(t *testing.T) {
		cfg := &config{}
		WithMCP("server1", "cmd1")(cfg)
		WithMCP("server2", "cmd2", "arg")(cfg)

		require.Len(t, cfg.mcpServers, 2)
		assert.Equal(t, "server1", cfg.mcpServers[0].Name)
		assert.Equal(t, "server2", cfg.mcpServers[1].Name)
	})
}

func TestMCPServerBuilder(t *testing.T) {
	t.Run("basic creation", func(t *testing.T) {
		builder := NewMCPServer("test", "node", "server.js")
		cfg := builder.Build()

		assert.Equal(t, "test", cfg.Name)
		assert.Equal(t, "node", cfg.Command)
		assert.Equal(t, []string{"server.js"}, cfg.Args)
	})

	t.Run("with env", func(t *testing.T) {
		builder := NewMCPServer("test", "node").
			WithEnv("API_KEY", "secret").
			WithEnv("DEBUG", "true")
		cfg := builder.Build()

		assert.Equal(t, "secret", cfg.Env["API_KEY"])
		assert.Equal(t, "true", cfg.Env["DEBUG"])
	})

	t.Run("with multiple env", func(t *testing.T) {
		builder := NewMCPServer("test", "node").
			WithEnv("KEY1", "val1").
			WithEnv("KEY2", "val2")
		cfg := builder.Build()

		assert.Equal(t, "val1", cfg.Env["KEY1"])
		assert.Equal(t, "val2", cfg.Env["KEY2"])
	})
}

func TestWithMCPServer(t *testing.T) {
	t.Run("adds builder config", func(t *testing.T) {
		cfg := &config{}
		builder := NewMCPServer("github", "npx", "@mcp/github").
			WithEnv("GITHUB_TOKEN", "token123")

		opt := WithMCPServer(builder)
		err := opt(cfg)

		require.NoError(t, err)
		require.Len(t, cfg.mcpServers, 1)
		assert.Equal(t, "github", cfg.mcpServers[0].Name)
		assert.Equal(t, "token123", cfg.mcpServers[0].Env["GITHUB_TOKEN"])
	})
}

func TestBuildToolRegistryWithMCP(t *testing.T) {
	t.Run("includes MCP tools", func(t *testing.T) {
		conv := newTestConversation()

		// Add mock MCP registry with tools
		mockRegistry := newMockMCPRegistry()
		mockRegistry.tools["server1"] = []mcp.Tool{
			{
				Name:        "mcp_tool",
				Description: "An MCP tool",
				InputSchema: json.RawMessage(`{"type":"object"}`),
			},
		}
		conv.mcpRegistry = mockRegistry

		// Call registerMCPExecutors to register MCP tools
		conv.handlersMu.Lock()
		localExec := &localExecutor{handlers: conv.handlers}
		conv.toolRegistry.RegisterExecutor(localExec)
		conv.registerMCPExecutors()
		conv.handlersMu.Unlock()
		registry := conv.ToolRegistry()

		assert.NotNil(t, registry)
		// MCP tool should be registered with qualified name
		tool, err := registry.GetTool("mcp__server1__mcp_tool")
		assert.NoError(t, err)
		assert.Equal(t, "mcp__server1__mcp_tool", tool.Name)
		assert.Equal(t, "mcp", tool.Mode)
	})

	t.Run("combines local and MCP tools", func(t *testing.T) {
		conv := newTestConversation()

		// Add a local tool
		conv.pack.Tools = map[string]*pack.Tool{
			"local_tool": {
				Name:        "local_tool",
				Description: "A local tool",
				Parameters:  &packspec.ToolParameters{Type: "object"},
			},
		}
		// Reinitialize toolRegistry with the local tool
		conv.toolRegistry = tools.NewRegistryWithRepository(pack.ToToolRepository(conv.pack))

		conv.OnTool("local_tool", func(args map[string]any) (any, error) {
			return "result", nil
		})

		// Add MCP registry
		mockRegistry := newMockMCPRegistry()
		mockRegistry.tools["server1"] = []mcp.Tool{
			{
				Name:        "mcp_tool",
				Description: "An MCP tool",
				InputSchema: json.RawMessage(`{"type":"object"}`),
			},
		}
		conv.mcpRegistry = mockRegistry

		// Call registerMCPExecutors to register MCP tools
		conv.handlersMu.Lock()
		localExec := &localExecutor{handlers: conv.handlers}
		conv.toolRegistry.RegisterExecutor(localExec)
		conv.registerMCPExecutors()
		conv.handlersMu.Unlock()
		registry := conv.ToolRegistry()

		assert.NotNil(t, registry)

		// Verify both tools are in registry
		localTool, err := registry.GetTool("local_tool")
		assert.NoError(t, err)
		assert.Equal(t, "local_tool", localTool.Name)

		mcpTool, err := registry.GetTool("mcp__server1__mcp_tool")
		assert.NoError(t, err)
		assert.Equal(t, "mcp__server1__mcp_tool", mcpTool.Name)
	})
}

func TestConversationCloseWithMCP(t *testing.T) {
	t.Run("closes MCP registry", func(t *testing.T) {
		conv := newTestConversation()
		mockRegistry := newMockMCPRegistry()
		conv.mcpRegistry = mockRegistry

		err := conv.Close()
		require.NoError(t, err)
		assert.True(t, mockRegistry.closed)
	})
}

type stubMCPAuthorizer struct{ name string }

func (stubMCPAuthorizer) Authorize(context.Context, *http.Request) error      { return nil }
func (stubMCPAuthorizer) Challenge(context.Context, *mcp.AuthChallenge) error { return nil }

func TestMCPClientConfigurer(t *testing.T) {
	plain := mcpClientConfigurer(&config{})
	require.NotNil(t, plain)
	opts := mcp.DefaultClientOptions()
	plain(mcp.ServerConfig{Name: "s"}, &opts)
	assert.False(t, opts.EnableGracefulDegradation,
		"a failed tools/list must reach the conversation as a failure, not as a server with no tools")
	assert.Nil(t, opts.Authorizer)
	assert.Nil(t, opts.ElicitationHandler)

	handler := func(context.Context, string, mcp.ElicitRequest) (mcp.ElicitResult, error) {
		return mcp.ElicitResult{Action: mcp.ElicitActionDecline}, nil
	}
	c := &config{}
	require.NoError(t, WithMCPAuthorizer(func(server string) mcp.Authorizer {
		if server == "secure" {
			return stubMCPAuthorizer{name: server}
		}
		return nil
	})(c))
	require.NoError(t, WithMCPElicitation(handler)(c))

	configure := mcpClientConfigurer(c)
	require.NotNil(t, configure)
	var secure, open mcp.ClientOptions
	configure(mcp.ServerConfig{Name: "secure"}, &secure)
	configure(mcp.ServerConfig{Name: "open"}, &open)
	assert.Equal(t, stubMCPAuthorizer{name: "secure"}, secure.Authorizer)
	assert.Nil(t, open.Authorizer)
	require.NotNil(t, secure.ElicitationHandler)
	res, err := open.ElicitationHandler(context.Background(), "open", mcp.ElicitRequest{})
	require.NoError(t, err)
	assert.Equal(t, mcp.ElicitActionDecline, res.Action)
}
