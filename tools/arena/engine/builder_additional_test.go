package engine

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/AltairaLabs/PromptKit/pkg/config"
	"github.com/AltairaLabs/PromptKit/runtime/evals"
	"github.com/AltairaLabs/PromptKit/runtime/mcp"
	"github.com/AltairaLabs/PromptKit/runtime/prompt"
	"github.com/AltairaLabs/PromptKit/runtime/providers"
	"github.com/AltairaLabs/PromptKit/runtime/providers/mock"
	"github.com/AltairaLabs/PromptKit/runtime/tools"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestParseProviderDuration covers the duration string parser used by the
// arena loader to translate config.Provider.RequestTimeout and
// config.Provider.StreamIdleTimeout into time.Duration values for
// providers.ProviderSpec. Invalid or non-positive input must log and yield
// zero so the provider falls back to its built-in default.
func TestParseProviderDuration(t *testing.T) {
	tests := []struct {
		name  string
		value string
		want  time.Duration
	}{
		{"empty string returns zero", "", 0},
		{"valid seconds", "90s", 90 * time.Second},
		{"valid minutes", "3m", 3 * time.Minute},
		{"valid compound", "1m30s", 90 * time.Second},
		{"valid hours", "2h", 2 * time.Hour},
		{"malformed", "garbage", 0},
		{"missing unit", "90", 0},
		{"zero duration", "0s", 0},
		{"negative duration", "-5s", 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := parseProviderDuration("test-provider", "request_timeout", tt.value)
			require.Equal(t, tt.want, got)
		})
	}
}

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

	providerReg, promptReg, mcpReg, convExec, adapterReg, a2aCleanup, _, _, err := BuildEngineComponents(cfg, nil)
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

func TestBuildMCPRegistry_PropagatesSSEFields(t *testing.T) {
	cfg := &config.Config{
		MCPServers: []config.MCPServerConfig{
			{
				Name:    "sandbox",
				URL:     "https://sandbox.local:8080",
				Headers: map[string]string{"Authorization": "Bearer abc"},
			},
		},
	}

	registry, err := buildMCPRegistry(cfg)
	require.NoError(t, err)
	require.NotNil(t, registry)

	got, ok := registry.GetServerConfig("sandbox")
	require.True(t, ok)
	require.Equal(t, "https://sandbox.local:8080", got.URL)
	require.Equal(t, "Bearer abc", got.Headers["Authorization"])
	require.Empty(t, got.Command)
}

// TestBuildMCPRegistry_SkipsSourceBackedEntries verifies that MCP servers
// with a Source field are NOT registered statically — they are opened
// dynamically by mcpSourceScope at scope boundaries (run/scenario/session).
func TestBuildMCPRegistry_SkipsSourceBackedEntries(t *testing.T) {
	cfg := &config.Config{
		MCPServers: []config.MCPServerConfig{
			{Name: "stdio-x", Command: "./foo"},
			{Name: "source-y", Source: "docker", Scope: "session"},
		},
	}
	registry, err := buildMCPRegistry(cfg)
	require.NoError(t, err)
	require.NotNil(t, registry)

	servers := registry.ListServers()
	assert.Contains(t, servers, "stdio-x")
	assert.NotContains(t, servers, "source-y")
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

	executor, adapterReg, err := newConversationExecutor(cfg, nil, nil, nil, providerRegistry, nil, "")
	require.NoError(t, err)
	require.NotNil(t, executor)
	require.NotNil(t, adapterReg)

	// Should be a CompositeConversationExecutor
	composite, ok := executor.(*CompositeConversationExecutor)
	require.True(t, ok)
	require.NotNil(t, composite.GetDefaultExecutor())
	require.NotNil(t, composite.GetDuplexExecutor())
}

func TestBuildEvalOrchestrator_UnknownEvalType(t *testing.T) {
	cfg := &config.Config{
		LoadedPack: &prompt.Pack{
			ID: "test-pack",
			Evals: []evals.EvalDef{
				{ID: "good", Type: "contains", Trigger: evals.TriggerEveryTurn},
				{ID: "bad", Type: "contians", Trigger: evals.TriggerEveryTurn},
			},
		},
	}

	hook, err := buildEvalOrchestrator(cfg, false, nil)
	require.Error(t, err)
	require.Nil(t, hook)
	require.Contains(t, err.Error(), "unknown eval types")
	require.Contains(t, err.Error(), `"contians"`)
}

func TestBuildEvalOrchestrator_AllKnownTypes(t *testing.T) {
	cfg := &config.Config{
		LoadedPack: &prompt.Pack{
			ID: "test-pack",
			Evals: []evals.EvalDef{
				{ID: "check", Type: "contains", Trigger: evals.TriggerEveryTurn},
			},
		},
	}

	hook, err := buildEvalOrchestrator(cfg, false, nil)
	require.NoError(t, err)
	require.NotNil(t, hook)
}

func TestBuildEvalOrchestrator_EmptyEvalsReturnsNil(t *testing.T) {
	cfg := &config.Config{
		LoadedPack: &prompt.Pack{
			ID: "test-pack",
		},
	}

	hook, err := buildEvalOrchestrator(cfg, false, nil)
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

	_, _, _, _, _, _, _, _, err := BuildEngineComponents(cfg, nil)
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

	executor, adapterReg, err := newConversationExecutor(cfg, nil, nil, nil, providerRegistry, nil, "")
	require.NoError(t, err)
	require.NotNil(t, executor)
	require.NotNil(t, adapterReg)

	// Should be a CompositeConversationExecutor with nil self-play components
	composite, ok := executor.(*CompositeConversationExecutor)
	require.True(t, ok)
	require.NotNil(t, composite.GetDefaultExecutor())
	require.NotNil(t, composite.GetDuplexExecutor())
}

func TestBuildEngineComponents_ProviderFilterSkipsCredentialResolution(t *testing.T) {
	cfg := &config.Config{
		LoadedProviders: map[string]*config.Provider{
			"mock-ok": {
				ID:    "mock-ok",
				Type:  "mock",
				Model: "mock-model",
				Defaults: config.ProviderDefaults{
					Temperature: 0.1,
					MaxTokens:   128,
				},
			},
			"azure-missing-creds": {
				ID:    "azure-missing-creds",
				Type:  "openai",
				Model: "gpt-4o",
				Credential: &config.CredentialConfig{
					CredentialEnv: "TOTALLY_NONEXISTENT_API_KEY_FOR_TEST_938",
				},
			},
		},
	}

	t.Run("without filter fails on missing credential", func(t *testing.T) {
		_, _, _, _, _, _, _, _, err := BuildEngineComponents(cfg, nil)
		require.Error(t, err)
		require.Contains(t, err.Error(), "TOTALLY_NONEXISTENT_API_KEY_FOR_TEST_938")
	})

	t.Run("filter to mock-ok skips azure credential resolution", func(t *testing.T) {
		providerReg, _, _, _, _, _, _, _, err := BuildEngineComponents(cfg, []string{"mock-ok"})
		require.NoError(t, err)
		require.NotNil(t, providerReg)

		// Only mock-ok should be registered
		providers := providerReg.List()
		require.Len(t, providers, 1)
		require.Contains(t, providers, "mock-ok")
	})

	t.Run("empty filter initializes all providers", func(t *testing.T) {
		_, _, _, _, _, _, _, _, err := BuildEngineComponents(cfg, []string{})
		require.Error(t, err, "empty filter should behave like no filter")
	})
}

func TestDiscoverAndRegisterSkillTools_FromConfig(t *testing.T) {
	dir := t.TempDir()
	skillDir := filepath.Join(dir, "skills", "test-skill")
	require.NoError(t, os.MkdirAll(skillDir, 0o755))
	require.NoError(t, os.WriteFile(
		filepath.Join(skillDir, "SKILL.md"),
		[]byte("---\nname: test-skill\ndescription: A test skill\n---\nTest instructions.\n"),
		0o600,
	))

	cfg := &config.Config{
		LoadedSkillSources: []prompt.SkillSourceConfig{
			{Path: filepath.Join(dir, "skills")},
		},
	}

	registry := tools.NewRegistry()
	exec, preloadedInstructions, err := discoverAndRegisterSkillTools(cfg, registry)
	require.NoError(t, err)
	require.NotNil(t, exec)

	allTools := registry.GetTools()
	assert.Contains(t, allTools, "skill__activate")
	assert.Contains(t, allTools, "skill__deactivate")

	// skill__activate descriptor must embed the available-skills index so the
	// LLM can discover which skills exist (issue #954).
	activateDesc := registry.Get("skill__activate")
	require.NotNil(t, activateDesc)
	assert.Contains(t, activateDesc.Description, "Available skills:",
		"skill__activate description should embed the skills index")
	assert.Contains(t, activateDesc.Description, "test-skill: A test skill")

	// Skill has no preload: true, so preloaded instructions should be empty.
	assert.Empty(t, preloadedInstructions)
}

func TestDiscoverAndRegisterSkillTools_PreloadedInstructions(t *testing.T) {
	dir := t.TempDir()
	skillDir := filepath.Join(dir, "skills", "memory-protocol")
	require.NoError(t, os.MkdirAll(skillDir, 0o755))
	require.NoError(t, os.WriteFile(
		filepath.Join(skillDir, "SKILL.md"),
		[]byte("---\nname: memory-protocol\ndescription: Memory rules\n---\nMUST call memory__recall first.\n"),
		0o600,
	))

	cfg := &config.Config{
		LoadedSkillSources: []prompt.SkillSourceConfig{
			{Path: filepath.Join(dir, "skills", "memory-protocol"), Preload: true},
		},
	}

	registry := tools.NewRegistry()
	_, preloadedInstructions, err := discoverAndRegisterSkillTools(cfg, registry)
	require.NoError(t, err)

	// Preloaded skill instructions must be returned so they can be injected
	// into the system prompt (issue #953).
	assert.Contains(t, preloadedInstructions, "# Active Skills")
	assert.Contains(t, preloadedInstructions, "memory-protocol")
	assert.Contains(t, preloadedInstructions, "MUST call memory__recall first.")
}

func TestDiscoverAndRegisterSkillTools_EmptyConfig(t *testing.T) {
	cfg := &config.Config{}
	registry := tools.NewRegistry()
	exec, preloadedInstructions, err := discoverAndRegisterSkillTools(cfg, registry)
	require.NoError(t, err)
	assert.Nil(t, exec)
	assert.Empty(t, preloadedInstructions)
	assert.Empty(t, registry.GetTools())
}
