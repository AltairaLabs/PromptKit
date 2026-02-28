package engine

import (
	"testing"

	"github.com/AltairaLabs/PromptKit/pkg/config"
	"github.com/AltairaLabs/PromptKit/runtime/evals"
	"github.com/AltairaLabs/PromptKit/runtime/mcp"
	"github.com/AltairaLabs/PromptKit/runtime/prompt"
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

	provider, err := createProviderImpl("", providerCfg)
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

	providerReg, promptReg, mcpReg, convExec, adapterReg, a2aCleanup, _, err := BuildEngineComponents(cfg)
	require.NoError(t, err)
	require.NotNil(t, providerReg)
	require.Nil(t, promptReg)
	require.Nil(t, mcpReg)
	require.NotNil(t, convExec)
	require.NotNil(t, adapterReg)
	require.Nil(t, a2aCleanup)
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

	executor, adapterReg, err := newConversationExecutor(cfg, nil, nil, nil, providerRegistry, nil)
	require.NoError(t, err)
	require.NotNil(t, executor)
	require.NotNil(t, adapterReg)

	// Should be a CompositeConversationExecutor
	composite, ok := executor.(*CompositeConversationExecutor)
	require.True(t, ok)
	require.NotNil(t, composite.GetDefaultExecutor())
	require.NotNil(t, composite.GetDuplexExecutor())
}

func TestBuildPackEvalHook_UnknownEvalType(t *testing.T) {
	cfg := &config.Config{
		LoadedPack: &prompt.Pack{
			ID: "test-pack",
			Evals: []evals.EvalDef{
				{ID: "good", Type: "contains", Trigger: evals.TriggerEveryTurn},
				{ID: "bad", Type: "contians", Trigger: evals.TriggerEveryTurn},
			},
		},
	}

	hook, err := buildPackEvalHook(cfg, false, nil)
	require.Error(t, err)
	require.Nil(t, hook)
	require.Contains(t, err.Error(), "unknown eval types")
	require.Contains(t, err.Error(), `"contians"`)
}

func TestBuildPackEvalHook_AllKnownTypes(t *testing.T) {
	cfg := &config.Config{
		LoadedPack: &prompt.Pack{
			ID: "test-pack",
			Evals: []evals.EvalDef{
				{ID: "check", Type: "contains", Trigger: evals.TriggerEveryTurn},
			},
		},
	}

	hook, err := buildPackEvalHook(cfg, false, nil)
	require.NoError(t, err)
	require.NotNil(t, hook)
}

func TestBuildPackEvalHook_EmptyEvalsReturnsNil(t *testing.T) {
	cfg := &config.Config{
		LoadedPack: &prompt.Pack{
			ID: "test-pack",
		},
	}

	hook, err := buildPackEvalHook(cfg, false, nil)
	require.NoError(t, err)
	require.Nil(t, hook)
}

func TestNewEngineFromConfig_UnknownEvalTypeError(t *testing.T) {
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
		LoadedPack: &prompt.Pack{
			ID: "test-pack",
			Evals: []evals.EvalDef{
				{ID: "bad", Type: "nonexistent_type", Trigger: evals.TriggerEveryTurn},
			},
		},
	}

	eng, err := NewEngineFromConfig(cfg)
	require.Error(t, err)
	require.Nil(t, eng)
	require.Contains(t, err.Error(), "failed to build pack eval hook")
	require.Contains(t, err.Error(), "unknown eval types")
}

func TestBuildEngineComponents_UnknownEvalTypeError(t *testing.T) {
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
		LoadedPack: &prompt.Pack{
			ID: "test-pack",
			Evals: []evals.EvalDef{
				{ID: "bad", Type: "nonexistent_type", Trigger: evals.TriggerEveryTurn},
			},
		},
	}

	_, _, _, _, _, _, _, err := BuildEngineComponents(cfg)
	require.Error(t, err)
	require.Contains(t, err.Error(), "failed to build pack eval hook")
	require.Contains(t, err.Error(), "unknown eval types")
}

func TestNewConversationExecutor_WithoutSelfPlay(t *testing.T) {
	cfg := &config.Config{
		LoadedProviders: map[string]*config.Provider{},
	}

	// Empty provider registry (no self-play, so not used)
	providerRegistry := providers.NewRegistry()

	executor, adapterReg, err := newConversationExecutor(cfg, nil, nil, nil, providerRegistry, nil)
	require.NoError(t, err)
	require.NotNil(t, executor)
	require.NotNil(t, adapterReg)

	// Should be a CompositeConversationExecutor with nil self-play components
	composite, ok := executor.(*CompositeConversationExecutor)
	require.True(t, ok)
	require.NotNil(t, composite.GetDefaultExecutor())
	require.NotNil(t, composite.GetDuplexExecutor())
}
