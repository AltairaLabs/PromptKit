package engine

import (
	"testing"

	"github.com/AltairaLabs/PromptKit/pkg/config"
	"github.com/AltairaLabs/PromptKit/runtime/mcp"
	"github.com/AltairaLabs/PromptKit/runtime/providers"
	"github.com/AltairaLabs/PromptKit/runtime/providers/mock"
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

	providerReg, promptReg, mcpReg, convExec, err := BuildEngineComponents(cfg)
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

	// Create provider registry with the mock provider
	providerRegistry := providers.NewRegistry()
	mockProvider := mock.NewProvider("mock-assistant", "mock-model", false)
	providerRegistry.Register(mockProvider)

	registry, executor, err := buildSelfPlayComponents(cfg, nil, providerRegistry)
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

	// Empty provider registry
	providerRegistry := providers.NewRegistry()

	registry, executor, err := buildSelfPlayComponents(cfg, nil, providerRegistry)
	require.Error(t, err)
	require.Contains(t, err.Error(), "references unknown provider")
	require.Nil(t, registry)
	require.Nil(t, executor)
}

func TestBuildSelfPlayComponents_ProviderNotInRegistry(t *testing.T) {
	// Provider exists in config but not registered in the provider registry
	cfg := &config.Config{
		LoadedProviders: map[string]*config.Provider{
			"missing-provider": {
				ID:   "missing-provider",
				Type: "mock",
			},
		},
		SelfPlay: &config.SelfPlayConfig{
			Enabled: true,
			Roles: []config.SelfPlayRoleGroup{
				{
					ID:       "user-role",
					Provider: "missing-provider",
				},
			},
		},
	}

	// Empty provider registry - provider exists in config but not registered
	providerRegistry := providers.NewRegistry()

	registry, executor, err := buildSelfPlayComponents(cfg, nil, providerRegistry)
	require.Error(t, err)
	require.Contains(t, err.Error(), "not found in main registry")
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

	// Create provider registry with both mock providers
	providerRegistry := providers.NewRegistry()
	providerRegistry.Register(mock.NewProvider("mock-assistant", "mock-model", false))
	providerRegistry.Register(mock.NewProvider("mock-user", "mock-model-2", false))

	registry, executor, err := buildSelfPlayComponents(cfg, nil, providerRegistry)
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

	// Create provider registry with the mock provider
	providerRegistry := providers.NewRegistry()
	providerRegistry.Register(mock.NewProvider("mock-assistant", "mock-model", false))

	executor, err := newConversationExecutor(cfg, nil, nil, nil, providerRegistry)
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

	// Empty provider registry (no self-play, so not used)
	providerRegistry := providers.NewRegistry()

	executor, err := newConversationExecutor(cfg, nil, nil, nil, providerRegistry)
	require.NoError(t, err)
	require.NotNil(t, executor)

	// Should be a CompositeConversationExecutor with nil self-play components
	composite, ok := executor.(*CompositeConversationExecutor)
	require.True(t, ok)
	require.NotNil(t, composite.GetDefaultExecutor())
	require.NotNil(t, composite.GetDuplexExecutor())
}
