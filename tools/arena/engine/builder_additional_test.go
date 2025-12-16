package engine

import (
	"testing"

	"github.com/AltairaLabs/PromptKit/pkg/config"
	"github.com/AltairaLabs/PromptKit/runtime/mcp"
	"github.com/AltairaLabs/PromptKit/runtime/tools"
	"github.com/stretchr/testify/require"
)

func TestCreateProviderImpl_MockProvider(t *testing.T) {
	providerCfg := &config.Provider{
		ID:    "mock-assistant",
		Type:  "mock",
		Model: "mock-model",
		Defaults: config.ProviderDefaults{
			Temperature: 0.1,
			TopP:        1.0,
			MaxTokens:   128,
		},
		AdditionalConfig: map[string]interface{}{
			"mock_config": "providers/responses/mock-assistant.yaml",
		},
	}

	provider, err := createProviderImpl(providerCfg)
	require.NoError(t, err)
	require.NotNil(t, provider)
	require.Equal(t, "mock-assistant", provider.ID())
}

func TestBuildEngineComponents_MinimalConfig(t *testing.T) {
	cfg := &config.Config{
		LoadedProviders: map[string]*config.Provider{
			"mock-assistant": {
				ID:    "mock-assistant",
				Type:  "mock",
				Model: "mock-model",
				Defaults: config.ProviderDefaults{
					Temperature: 0.1,
					MaxTokens:   128,
					TopP:        1.0,
				},
			},
		},
	}

	providerReg, promptReg, mcpReg, convExec, err := buildEngineComponents(cfg)
	require.NoError(t, err)
	require.NotNil(t, providerReg)
	require.Nil(t, promptReg)
	require.Nil(t, mcpReg)
	require.NotNil(t, convExec)
}

func TestDiscoverAndRegisterMCPTools_EmptyRegistry(t *testing.T) {
	mcpRegistry := mcp.NewRegistry() // No servers registered
	toolRegistry := tools.NewRegistry()

	err := discoverAndRegisterMCPTools(mcpRegistry, toolRegistry)
	require.NoError(t, err)
}

func TestBuildMCPRegistry_WithServer(t *testing.T) {
	cfg := &config.Config{
		MCPServers: []config.MCPServerConfig{
			{
				Name:    "demo-server",
				Command: "echo",
				Args:    []string{"demo"},
			},
		},
	}

	registry, err := buildMCPRegistry(cfg)
	require.NoError(t, err)
	require.NotNil(t, registry)
}

func TestBuildSelfPlayComponents_Success(t *testing.T) {
	cfg := &config.Config{
		LoadedProviders: map[string]*config.Provider{
			"mock-assistant": {
				ID:    "mock-assistant",
				Type:  "mock",
				Model: "mock-model",
				Defaults: config.ProviderDefaults{
					Temperature: 0.1,
					MaxTokens:   128,
				},
			},
		},
		SelfPlay: &config.SelfPlayConfig{
			Enabled: true,
			Roles: []config.SelfPlayRoleGroup{
				{
					ID:       "user-role",
					Provider: "mock-assistant",
				},
			},
		},
	}

	registry, executor, err := buildSelfPlayComponents(cfg, nil)
	require.NoError(t, err)
	require.NotNil(t, registry)
	require.NotNil(t, executor)
}

func TestBuildSelfPlayComponents_UnknownProvider(t *testing.T) {
	cfg := &config.Config{
		LoadedProviders: map[string]*config.Provider{},
		SelfPlay: &config.SelfPlayConfig{
			Enabled: true,
			Roles: []config.SelfPlayRoleGroup{
				{
					ID:       "user-role",
					Provider: "nonexistent-provider",
				},
			},
		},
	}

	registry, executor, err := buildSelfPlayComponents(cfg, nil)
	require.Error(t, err)
	require.Contains(t, err.Error(), "references unknown provider")
	require.Nil(t, registry)
	require.Nil(t, executor)
}

func TestBuildSelfPlayComponents_InvalidProviderType(t *testing.T) {
	cfg := &config.Config{
		LoadedProviders: map[string]*config.Provider{
			"invalid-provider": {
				ID:   "invalid-provider",
				Type: "invalid-type-that-does-not-exist",
			},
		},
		SelfPlay: &config.SelfPlayConfig{
			Enabled: true,
			Roles: []config.SelfPlayRoleGroup{
				{
					ID:       "user-role",
					Provider: "invalid-provider",
				},
			},
		},
	}

	registry, executor, err := buildSelfPlayComponents(cfg, nil)
	require.Error(t, err)
	require.Contains(t, err.Error(), "failed to create provider")
	require.Nil(t, registry)
	require.Nil(t, executor)
}

func TestBuildSelfPlayComponents_MultipleRoles(t *testing.T) {
	cfg := &config.Config{
		LoadedProviders: map[string]*config.Provider{
			"mock-assistant": {
				ID:    "mock-assistant",
				Type:  "mock",
				Model: "mock-model",
			},
			"mock-user": {
				ID:    "mock-user",
				Type:  "mock",
				Model: "mock-model-2",
			},
		},
		SelfPlay: &config.SelfPlayConfig{
			Enabled: true,
			Roles: []config.SelfPlayRoleGroup{
				{
					ID:       "assistant-role",
					Provider: "mock-assistant",
				},
				{
					ID:       "user-role",
					Provider: "mock-user",
				},
			},
		},
		LoadedPersonas: map[string]*config.UserPersonaPack{
			"test-persona": {
				ID:          "test-persona",
				Description: "A test persona",
			},
		},
	}

	registry, executor, err := buildSelfPlayComponents(cfg, nil)
	require.NoError(t, err)
	require.NotNil(t, registry)
	require.NotNil(t, executor)
}

func TestNewConversationExecutor_WithSelfPlay(t *testing.T) {
	cfg := &config.Config{
		LoadedProviders: map[string]*config.Provider{
			"mock-assistant": {
				ID:    "mock-assistant",
				Type:  "mock",
				Model: "mock-model",
			},
		},
		SelfPlay: &config.SelfPlayConfig{
			Enabled: true,
			Roles: []config.SelfPlayRoleGroup{
				{
					ID:       "user-role",
					Provider: "mock-assistant",
				},
			},
		},
	}

	executor, err := newConversationExecutor(cfg, nil, nil, nil)
	require.NoError(t, err)
	require.NotNil(t, executor)

	// Should be a CompositeConversationExecutor
	composite, ok := executor.(*CompositeConversationExecutor)
	require.True(t, ok)
	require.NotNil(t, composite.GetDefaultExecutor())
	require.NotNil(t, composite.GetDuplexExecutor())
}

func TestNewConversationExecutor_WithoutSelfPlay(t *testing.T) {
	cfg := &config.Config{
		LoadedProviders: map[string]*config.Provider{},
	}

	executor, err := newConversationExecutor(cfg, nil, nil, nil)
	require.NoError(t, err)
	require.NotNil(t, executor)

	// Should be a CompositeConversationExecutor with nil self-play components
	composite, ok := executor.(*CompositeConversationExecutor)
	require.True(t, ok)
	require.NotNil(t, composite.GetDefaultExecutor())
	require.NotNil(t, composite.GetDuplexExecutor())
}
