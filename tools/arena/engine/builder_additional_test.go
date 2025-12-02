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
