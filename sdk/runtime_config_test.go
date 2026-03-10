package sdk

import (
	"context"
	"encoding/json"
	"log/slog"
	"os"
	"path/filepath"
	"testing"

	pkgconfig "github.com/AltairaLabs/PromptKit/pkg/config"
	"github.com/AltairaLabs/PromptKit/runtime/mcp"
	"github.com/AltairaLabs/PromptKit/runtime/providers"
	"github.com/AltairaLabs/PromptKit/runtime/statestore"
	"github.com/AltairaLabs/PromptKit/runtime/tools"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func writeRuntimeConfig(t *testing.T, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "runtime.yaml")
	require.NoError(t, os.WriteFile(path, []byte(content), 0o644))
	return path
}

func TestWithRuntimeConfig_InvalidPath(t *testing.T) {
	opt := WithRuntimeConfig("/nonexistent/runtime.yaml")
	err := opt(&config{})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "loading runtime config")
}

func TestWithRuntimeConfig_InvalidYAML(t *testing.T) {
	path := writeRuntimeConfig(t, "{{not yaml")
	opt := WithRuntimeConfig(path)
	err := opt(&config{})
	require.Error(t, err)
}

func TestApplyRuntimeConfig_MCPServers(t *testing.T) {
	spec := &pkgconfig.RuntimeConfigSpec{
		MCPServers: []pkgconfig.MCPServerConfig{
			{
				Name:    "filesystem",
				Command: "npx",
				Args:    []string{"-y", "@anthropic/mcp-filesystem"},
				Env:     map[string]string{"HOME": "/tmp"},
				ToolFilter: &pkgconfig.MCPToolFilter{
					Allowlist: []string{"read_file"},
				},
			},
			{
				Name:    "database",
				Command: "db-server",
			},
		},
	}

	c := &config{}
	require.NoError(t, applyRuntimeConfig(c, spec))

	assert.Len(t, c.mcpServers, 2)
	assert.Equal(t, "filesystem", c.mcpServers[0].Name)
	assert.Equal(t, "npx", c.mcpServers[0].Command)
	assert.Equal(t, []string{"-y", "@anthropic/mcp-filesystem"}, c.mcpServers[0].Args)
	assert.Equal(t, map[string]string{"HOME": "/tmp"}, c.mcpServers[0].Env)
	require.NotNil(t, c.mcpServers[0].ToolFilter)
	assert.Equal(t, []string{"read_file"}, c.mcpServers[0].ToolFilter.Allowlist)
	assert.Equal(t, "database", c.mcpServers[1].Name)
}

func TestApplyRuntimeConfig_MCPServers_AppendToExisting(t *testing.T) {
	spec := &pkgconfig.RuntimeConfigSpec{
		MCPServers: []pkgconfig.MCPServerConfig{
			{Name: "new-server", Command: "new-cmd"},
		},
	}

	c := &config{
		mcpServers: []mcp.ServerConfig{
			{Name: "existing", Command: "old-cmd"},
		},
	}
	require.NoError(t, applyRuntimeConfig(c, spec))
	assert.Len(t, c.mcpServers, 2)
	assert.Equal(t, "existing", c.mcpServers[0].Name)
	assert.Equal(t, "new-server", c.mcpServers[1].Name)
}

func TestApplyRuntimeConfig_StateStore_Memory(t *testing.T) {
	spec := &pkgconfig.RuntimeConfigSpec{
		StateStore: &pkgconfig.StateStoreConfig{Type: "memory"},
	}

	c := &config{}
	require.NoError(t, applyRuntimeConfig(c, spec))
	assert.NotNil(t, c.stateStore)
}

func TestApplyRuntimeConfig_StateStore_SkipsIfSet(t *testing.T) {
	spec := &pkgconfig.RuntimeConfigSpec{
		StateStore: &pkgconfig.StateStoreConfig{Type: "memory"},
	}

	existing := &rtcMockStore{}
	c := &config{stateStore: existing}
	require.NoError(t, applyRuntimeConfig(c, spec))
	assert.Equal(t, existing, c.stateStore, "should not override existing state store")
}

func TestApplyRuntimeConfig_StateStore_RedisRequiresConfig(t *testing.T) {
	spec := &pkgconfig.RuntimeConfigSpec{
		StateStore: &pkgconfig.StateStoreConfig{Type: "redis"},
	}

	c := &config{}
	err := applyRuntimeConfig(c, spec)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "redis configuration is required")
}

func TestApplyRuntimeConfig_StateStore_UnsupportedType(t *testing.T) {
	spec := &pkgconfig.RuntimeConfigSpec{
		StateStore: &pkgconfig.StateStoreConfig{Type: "dynamodb"},
	}

	c := &config{}
	err := applyRuntimeConfig(c, spec)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unsupported state store type")
}

func TestApplyRuntimeConfig_Logging(t *testing.T) {
	spec := &pkgconfig.RuntimeConfigSpec{
		Logging: &pkgconfig.LoggingConfigSpec{
			DefaultLevel: "debug",
			Format:       "json",
			CommonFields: map[string]string{"env": "test"},
		},
	}

	c := &config{}
	require.NoError(t, applyRuntimeConfig(c, spec))
	assert.NotNil(t, c.logger)
}

func TestApplyRuntimeConfig_Logging_SkipsIfSet(t *testing.T) {
	spec := &pkgconfig.RuntimeConfigSpec{
		Logging: &pkgconfig.LoggingConfigSpec{DefaultLevel: "debug"},
	}

	existing := slog.Default()
	c := &config{logger: existing}
	require.NoError(t, applyRuntimeConfig(c, spec))
	assert.Equal(t, existing, c.logger, "should not override existing logger")
}

func TestApplyRuntimeConfig_Provider_SkipsIfSet(t *testing.T) {
	spec := &pkgconfig.RuntimeConfigSpec{
		Providers: []pkgconfig.Provider{
			{Type: "openai", Model: "gpt-4o"},
		},
	}

	existing := &rtcMockProvider{}
	c := &config{provider: existing}
	require.NoError(t, applyRuntimeConfig(c, spec))
	assert.Equal(t, existing, c.provider, "should not override existing provider")
}

func TestApplyRuntimeConfig_EmptySpec(t *testing.T) {
	spec := &pkgconfig.RuntimeConfigSpec{}
	c := &config{}
	require.NoError(t, applyRuntimeConfig(c, spec))
	assert.Nil(t, c.provider)
	assert.Empty(t, c.mcpServers)
	assert.Nil(t, c.stateStore)
	assert.Nil(t, c.logger)
}

func TestBuildLoggerFromSpec_TextFormat(t *testing.T) {
	spec := &pkgconfig.LoggingConfigSpec{
		DefaultLevel: "warn",
		Format:       "text",
	}
	l := buildLoggerFromSpec(spec)
	assert.NotNil(t, l)
}

func TestBuildLoggerFromSpec_JSONFormat(t *testing.T) {
	spec := &pkgconfig.LoggingConfigSpec{
		DefaultLevel: "error",
		Format:       "json",
	}
	l := buildLoggerFromSpec(spec)
	assert.NotNil(t, l)
}

func TestBuildLoggerFromSpec_DefaultLevel(t *testing.T) {
	spec := &pkgconfig.LoggingConfigSpec{}
	l := buildLoggerFromSpec(spec)
	assert.NotNil(t, l)
}

func TestSlogLevel(t *testing.T) {
	tests := []struct {
		input    string
		expected slog.Level
	}{
		{"trace", slog.LevelDebug - slogTraceOffset},
		{"debug", slog.LevelDebug},
		{"info", slog.LevelInfo},
		{"warn", slog.LevelWarn},
		{"error", slog.LevelError},
		{"", slog.LevelInfo},
		{"unknown", slog.LevelInfo},
	}
	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			assert.Equal(t, tt.expected, slogLevel(tt.input))
		})
	}
}

func TestApplyRuntimeConfig_Provider_MockType(t *testing.T) {
	spec := &pkgconfig.RuntimeConfigSpec{
		Providers: []pkgconfig.Provider{
			{Type: "mock", Model: "mock-model"},
		},
	}

	c := &config{}
	err := applyRuntimeConfig(c, spec)
	require.NoError(t, err)
	assert.NotNil(t, c.provider)
}

func TestCreateProviderFromConfig_WithPlatform(t *testing.T) {
	// Mock provider type doesn't need real credentials
	p := &pkgconfig.Provider{
		Type:  "mock",
		Model: "mock-model",
	}
	prov, err := createProviderFromConfig(p)
	require.NoError(t, err)
	assert.NotNil(t, prov)
}

func TestCreateStateStoreFromConfig_Redis(t *testing.T) {
	cfg := &pkgconfig.StateStoreConfig{
		Type: "redis",
		Redis: &pkgconfig.RedisConfig{
			Address:  "localhost:6379",
			Password: "secret",
			Database: 1,
			TTL:      "1h",
			Prefix:   "test",
		},
	}
	store, err := createStateStoreFromConfig(cfg)
	require.NoError(t, err)
	assert.NotNil(t, store)
}

func TestCreateStateStoreFromConfig_EmptyType(t *testing.T) {
	cfg := &pkgconfig.StateStoreConfig{Type: ""}
	store, err := createStateStoreFromConfig(cfg)
	require.NoError(t, err)
	assert.NotNil(t, store)
}

func TestCreateStateStoreFromConfig_InvalidTTL(t *testing.T) {
	cfg := &pkgconfig.StateStoreConfig{
		Type: "redis",
		Redis: &pkgconfig.RedisConfig{
			Address: "localhost:6379",
			TTL:     "not-a-duration",
		},
	}
	_, err := createStateStoreFromConfig(cfg)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid TTL duration")
}

func TestApplyRuntimeConfig_ExecTools(t *testing.T) {
	spec := &pkgconfig.RuntimeConfigSpec{
		Tools: map[string]*pkgconfig.ToolSpec{
			"sentiment": {
				Mode: "exec",
				ExecConfig: &pkgconfig.ExecBinding{
					Command:   "/usr/bin/sentiment-check",
					Args:      []string{"--strict"},
					Env:       []string{"API_KEY"},
					TimeoutMs: 5000,
				},
			},
			"no_exec": {
				Mode: "live",
				// No ExecConfig — should be skipped
			},
		},
	}

	c := &config{}
	require.NoError(t, applyRuntimeConfig(c, spec))

	require.Len(t, c.execToolConfigs, 1)
	cfg, ok := c.execToolConfigs["sentiment"]
	require.True(t, ok)
	assert.Equal(t, "/usr/bin/sentiment-check", cfg.Command)
	assert.Equal(t, []string{"--strict"}, cfg.Args)
	assert.Equal(t, []string{"API_KEY"}, cfg.Env)
	assert.Equal(t, 5000, cfg.TimeoutMs)
}

func TestApplyRuntimeConfig_ExecEvals(t *testing.T) {
	spec := &pkgconfig.RuntimeConfigSpec{
		Evals: map[string]*pkgconfig.ExecBinding{
			"sentiment_check": {
				Command:   "/usr/bin/eval-sentiment",
				Args:      []string{"--lang", "en"},
				Env:       []string{"NLTK_DATA"},
				TimeoutMs: 10000,
			},
			"compliance_check": {
				Command: "/usr/bin/compliance",
			},
		},
	}

	c := &config{}
	require.NoError(t, applyRuntimeConfig(c, spec))

	assert.Len(t, c.execEvalHandlers, 2)
	require.NotNil(t, c.evalRegistry)

	// Verify handlers are registered in the registry
	has1 := c.evalRegistry.Has("sentiment_check")
	has2 := c.evalRegistry.Has("compliance_check")
	assert.True(t, has1, "sentiment_check should be registered")
	assert.True(t, has2, "compliance_check should be registered")
}

func TestApplyRuntimeConfig_ExecEvals_NilBinding(t *testing.T) {
	spec := &pkgconfig.RuntimeConfigSpec{
		Evals: map[string]*pkgconfig.ExecBinding{
			"should_skip": nil,
		},
	}

	c := &config{}
	require.NoError(t, applyRuntimeConfig(c, spec))
	assert.Empty(t, c.execEvalHandlers)
}

func TestApplyRuntimeConfig_ExecTools_NilSpec(t *testing.T) {
	spec := &pkgconfig.RuntimeConfigSpec{
		Tools: map[string]*pkgconfig.ToolSpec{
			"nil_tool": nil,
		},
	}

	c := &config{}
	require.NoError(t, applyRuntimeConfig(c, spec))
	assert.Empty(t, c.execToolConfigs)
}

func TestRegisterExecExecutor_NoConfigs(t *testing.T) {
	conv := &Conversation{
		toolRegistry: tools.NewRegistry(),
		config:       &config{},
	}
	// Should be a no-op when no exec tool configs
	conv.registerExecExecutor()
}

func TestRegisterExecExecutor_WithConfigs(t *testing.T) {
	registry := tools.NewRegistry()
	// Register a tool descriptor to be updated
	_ = registry.Register(&tools.ToolDescriptor{
		Name:        "my_tool",
		Description: "Test tool",
		InputSchema: json.RawMessage(`{"type":"object"}`),
		Mode:        "local",
	})

	execCfg := &tools.ExecConfig{
		Command: "/usr/bin/my-tool",
		Args:    []string{"--flag"},
	}

	conv := &Conversation{
		toolRegistry: registry,
		config: &config{
			execToolConfigs: map[string]*tools.ExecConfig{
				"my_tool":     execCfg,
				"nonexistent": {Command: "/bin/false"},
			},
		},
	}

	conv.registerExecExecutor()

	// Verify the tool descriptor was updated
	td := registry.Get("my_tool")
	require.NotNil(t, td)
	assert.Equal(t, "exec", td.Mode)
	require.NotNil(t, td.ExecConfig)
	assert.Equal(t, "/usr/bin/my-tool", td.ExecConfig.Command)
	assert.Equal(t, []string{"--flag"}, td.ExecConfig.Args)
}

func TestApplyExecHooks_ProviderHook(t *testing.T) {
	spec := &pkgconfig.RuntimeConfigSpec{
		Hooks: map[string]*pkgconfig.ExecHook{
			"pii_redactor": {
				ExecBinding: pkgconfig.ExecBinding{Command: "./hooks/pii-redactor"},
				Hook:        "provider",
				Phases:      []string{"before_call", "after_call"},
				Mode:        "filter",
			},
		},
	}

	c := &config{}
	require.NoError(t, applyRuntimeConfig(c, spec))
	assert.Len(t, c.providerHooks, 1)
	assert.Equal(t, "pii_redactor", c.providerHooks[0].Name())
}

func TestApplyExecHooks_ToolHook(t *testing.T) {
	spec := &pkgconfig.RuntimeConfigSpec{
		Hooks: map[string]*pkgconfig.ExecHook{
			"query_allowlist": {
				ExecBinding: pkgconfig.ExecBinding{Command: "./hooks/query-allowlist.py"},
				Hook:        "tool",
				Phases:      []string{"before_execution"},
			},
		},
	}

	c := &config{}
	require.NoError(t, applyRuntimeConfig(c, spec))
	assert.Len(t, c.toolHooks, 1)
	assert.Equal(t, "query_allowlist", c.toolHooks[0].Name())
}

func TestApplyExecHooks_SessionHook(t *testing.T) {
	spec := &pkgconfig.RuntimeConfigSpec{
		Hooks: map[string]*pkgconfig.ExecHook{
			"audit_logger": {
				ExecBinding: pkgconfig.ExecBinding{Command: "./hooks/audit-logger.py"},
				Hook:        "session",
				Phases:      []string{"session_start", "session_end"},
				Mode:        "observe",
			},
		},
	}

	c := &config{}
	require.NoError(t, applyRuntimeConfig(c, spec))
	assert.Len(t, c.sessionHooks, 1)
	assert.Equal(t, "audit_logger", c.sessionHooks[0].Name())
}

func TestApplyExecHooks_NilBinding(t *testing.T) {
	spec := &pkgconfig.RuntimeConfigSpec{
		Hooks: map[string]*pkgconfig.ExecHook{
			"should_skip": nil,
		},
	}

	c := &config{}
	require.NoError(t, applyRuntimeConfig(c, spec))
	assert.Empty(t, c.providerHooks)
	assert.Empty(t, c.toolHooks)
	assert.Empty(t, c.sessionHooks)
}

func TestApplyExecHooks_MultipleTypes(t *testing.T) {
	spec := &pkgconfig.RuntimeConfigSpec{
		Hooks: map[string]*pkgconfig.ExecHook{
			"pii": {
				ExecBinding: pkgconfig.ExecBinding{Command: "./pii"},
				Hook:        "provider",
				Phases:      []string{"before_call"},
			},
			"audit": {
				ExecBinding: pkgconfig.ExecBinding{Command: "./audit"},
				Hook:        "session",
				Phases:      []string{"session_start"},
			},
			"gate": {
				ExecBinding: pkgconfig.ExecBinding{Command: "./gate"},
				Hook:        "tool",
				Phases:      []string{"before_execution"},
			},
		},
	}

	c := &config{}
	require.NoError(t, applyRuntimeConfig(c, spec))
	assert.Len(t, c.providerHooks, 1)
	assert.Len(t, c.toolHooks, 1)
	assert.Len(t, c.sessionHooks, 1)
}

// rtcMockStore is a minimal statestore.Store for runtime_config tests.
type rtcMockStore struct{}

func (m *rtcMockStore) Load(_ context.Context, _ string) (*statestore.ConversationState, error) {
	return nil, nil
}
func (m *rtcMockStore) Save(_ context.Context, _ *statestore.ConversationState) error { return nil }
func (m *rtcMockStore) Fork(_ context.Context, _, _ string) error                     { return nil }

// rtcMockProvider is a minimal providers.Provider for runtime_config tests.
type rtcMockProvider struct{ providers.Provider }
