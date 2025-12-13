package sdk

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/AltairaLabs/PromptKit/runtime/mcp"
	"github.com/AltairaLabs/PromptKit/runtime/tools"
	"github.com/AltairaLabs/PromptKit/sdk/internal/pack"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// mockMCPRegistry is a test mock for mcp.Registry
type mockMCPRegistry struct {
	servers  []mcp.ServerConfig
	tools    map[string][]mcp.Tool
	callFunc func(name string, args json.RawMessage) (*mcp.ToolCallResponse, error)
	closed   bool
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
	return m.tools, nil
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

func TestMCPHandlerAdapter(t *testing.T) {
	t.Run("name returns tool name", func(t *testing.T) {
		adapter := &mcpHandlerAdapter{
			name:     "read_file",
			registry: newMockMCPRegistry(),
		}
		assert.Equal(t, "read_file", adapter.Name())
	})

	t.Run("execute calls mcp tool", func(t *testing.T) {
		registry := newMockMCPRegistry()
		registry.callFunc = func(name string, args json.RawMessage) (*mcp.ToolCallResponse, error) {
			assert.Equal(t, "read_file", name)
			return &mcp.ToolCallResponse{
				Content: []mcp.Content{{Type: "text", Text: "file contents"}},
			}, nil
		}

		adapter := &mcpHandlerAdapter{
			name:     "read_file",
			registry: registry,
		}

		result, err := adapter.Execute(&tools.ToolDescriptor{}, json.RawMessage(`{"path":"/tmp/test"}`))
		require.NoError(t, err)
		assert.Contains(t, string(result), "file contents")
	})

	t.Run("execute handles error response", func(t *testing.T) {
		registry := newMockMCPRegistry()
		registry.callFunc = func(name string, args json.RawMessage) (*mcp.ToolCallResponse, error) {
			return &mcp.ToolCallResponse{
				Content: []mcp.Content{{Type: "text", Text: "file not found"}},
				IsError: true,
			}, nil
		}

		adapter := &mcpHandlerAdapter{
			name:     "read_file",
			registry: registry,
		}

		_, err := adapter.Execute(&tools.ToolDescriptor{}, json.RawMessage(`{}`))
		require.Error(t, err)
		assert.Contains(t, err.Error(), "file not found")
	})

	t.Run("execute handles multiple content items", func(t *testing.T) {
		registry := newMockMCPRegistry()
		registry.callFunc = func(name string, args json.RawMessage) (*mcp.ToolCallResponse, error) {
			return &mcp.ToolCallResponse{
				Content: []mcp.Content{
					{Type: "text", Text: "first"},
					{Type: "text", Text: "second"},
				},
			}, nil
		}

		adapter := &mcpHandlerAdapter{
			name:     "multi_output",
			registry: registry,
		}

		result, err := adapter.Execute(&tools.ToolDescriptor{}, json.RawMessage(`{}`))
		require.NoError(t, err)
		// Multiple content items are returned as array
		var content []mcp.Content
		err = json.Unmarshal(result, &content)
		require.NoError(t, err)
		assert.Len(t, content, 2)
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
		// MCP tool should be registered
		tool, err := registry.GetTool("mcp_tool")
		assert.NoError(t, err)
		assert.Equal(t, "mcp_tool", tool.Name)
		assert.Equal(t, "mcp", tool.Mode)
	})

	t.Run("combines local and MCP tools", func(t *testing.T) {
		conv := newTestConversation()

		// Add a local tool
		conv.pack.Tools = map[string]*pack.Tool{
			"local_tool": {
				Name:        "local_tool",
				Description: "A local tool",
				Parameters:  map[string]any{"type": "object"},
			},
		}
		// Reinitialize toolRegistry with the local tool
		conv.toolRegistry = tools.NewRegistryWithRepository(conv.pack.ToToolRepository())

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

		mcpTool, err := registry.GetTool("mcp_tool")
		assert.NoError(t, err)
		assert.Equal(t, "mcp_tool", mcpTool.Name)
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
