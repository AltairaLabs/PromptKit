package sdk

import (
	"context"
	"log/slog"
	"os"
	"path/filepath"
	"testing"

	pkgconfig "github.com/AltairaLabs/PromptKit/pkg/config"
	"github.com/AltairaLabs/PromptKit/runtime/mcp"
	"github.com/AltairaLabs/PromptKit/runtime/providers"
	"github.com/AltairaLabs/PromptKit/runtime/statestore"
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

// rtcMockStore is a minimal statestore.Store for runtime_config tests.
type rtcMockStore struct{}

func (m *rtcMockStore) Load(_ context.Context, _ string) (*statestore.ConversationState, error) {
	return nil, nil
}
func (m *rtcMockStore) Save(_ context.Context, _ *statestore.ConversationState) error { return nil }
func (m *rtcMockStore) Fork(_ context.Context, _, _ string) error                     { return nil }

// rtcMockProvider is a minimal providers.Provider for runtime_config tests.
type rtcMockProvider struct{ providers.Provider }
