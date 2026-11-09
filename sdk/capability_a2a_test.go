package sdk

import (
	"testing"

	"github.com/AltairaLabs/PromptKit/runtime/v2/a2a"
	"github.com/AltairaLabs/PromptKit/runtime/v2/tools"
	"github.com/AltairaLabs/PromptKit/sdk/v2/internal/pack"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/AltairaLabs/PromptKit/runtime/v2/packspec"
)

func TestA2ACapability_Name(t *testing.T) {
	cap := NewA2ACapability()
	assert.Equal(t, "a2a", cap.Name())
}

// A pack without agents gives the capability nothing to register.
func TestA2ACapability_Init_NilAgents(t *testing.T) {
	cap := NewA2ACapability()
	p := &pack.Pack{Pack: packspec.Pack{
		ID:      "test",
		Prompts: map[string]*pack.Prompt{"chat": {ID: "chat", Tools: []string{"helper"}}},
	}}
	require.NoError(t, cap.Init(CapabilityContext{Pack: p, PromptName: "chat"}))

	registry := tools.NewRegistry()
	cap.RegisterTools(registry)
	assert.Empty(t, registry.GetTools(), "a tool name with no agent member is not an agent tool")
}

// Init reads the prompt's tool list against the pack's agent members: each
// member the prompt lists becomes an a2a tool that calls that member's skill.
func TestA2ACapability_Init_WithAgents(t *testing.T) {
	cap := NewA2ACapability()
	p := &pack.Pack{Pack: packspec.Pack{
		ID: "test",
		Prompts: map[string]*pack.Prompt{
			"orchestrator": {ID: "orchestrator", Tools: []string{"helper", "get_weather"}},
			"helper":       {ID: "helper", Description: "Helps"},
			"unlisted":     {ID: "unlisted"},
		},
		Agents: &pack.AgentsConfig{
			Entry: "orchestrator",
			Members: map[string]*pack.AgentDef{
				"helper":   {Description: "A helper agent"},
				"unlisted": {Description: "Not in the prompt's tools"},
			},
		},
	}}
	require.NoError(t, cap.Init(CapabilityContext{Pack: p, PromptName: "orchestrator"}))

	registry := tools.NewRegistry()
	cap.RegisterTools(registry)
	got := registry.GetTools()
	require.Len(t, got, 1, "only the members the prompt lists become tools")
	tool := got["a2a__helper"]
	require.NotNil(t, tool)
	assert.Equal(t, "a2a", tool.Mode)
	require.NotNil(t, tool.A2AConfig)
	assert.Equal(t, "helper", tool.A2AConfig.SkillID)
}

func TestA2ACapability_RegisterTools_BridgePath(t *testing.T) {
	srv := a2aTestServer(t, "BridgeAgent", "do_stuff", "result")
	defer srv.Close()

	bridge := setupA2ABridge(t, srv.URL)
	cap := NewA2ACapability()
	cap.bridges = []*a2a.ToolBridge{bridge}

	registry := tools.NewRegistry()
	cap.RegisterTools(registry)

	tool, err := registry.GetTool("a2a__bridgeagent__do_stuff")
	require.NoError(t, err)
	assert.Equal(t, "a2a", tool.Mode)
}

func TestA2ACapability_RegisterTools_PackPath(t *testing.T) {
	cap := NewA2ACapability()
	p := &pack.Pack{Pack: packspec.Pack{
		ID: "test",
		Prompts: map[string]*pack.Prompt{
			"orchestrator": {
				ID:    "orchestrator",
				Tools: []string{"worker"},
			},
		},
		Agents: &pack.AgentsConfig{
			Entry: "orchestrator",
			Members: map[string]*pack.AgentDef{
				"worker": {
					Description: "A worker agent",
				},
			},
		},
	}}
	resolver := &StaticEndpointResolver{BaseURL: "http://localhost:9000"}
	cap.endpointResolver = resolver

	err := cap.Init(CapabilityContext{Pack: p, PromptName: "orchestrator"})
	require.NoError(t, err)

	registry := tools.NewRegistry()
	cap.RegisterTools(registry)

	tool, err := registry.GetTool("a2a__worker")
	require.NoError(t, err)
	assert.Equal(t, "a2a", tool.Mode)
	assert.Equal(t, "http://localhost:9000", tool.A2AConfig.AgentURL)
}

func TestA2ACapability_RegisterTools_LocalExecutor(t *testing.T) {
	cap := NewA2ACapability()
	localExec := NewLocalAgentExecutor(nil)
	cap.localExecutor = localExec

	p := &pack.Pack{Pack: packspec.Pack{
		ID: "test",
		Prompts: map[string]*pack.Prompt{
			"orchestrator": {
				ID:    "orchestrator",
				Tools: []string{"worker"},
			},
		},
		Agents: &pack.AgentsConfig{
			Entry: "orchestrator",
			Members: map[string]*pack.AgentDef{
				"worker": {
					Description: "A worker agent",
				},
			},
		},
	}}

	err := cap.Init(CapabilityContext{Pack: p, PromptName: "orchestrator"})
	require.NoError(t, err)

	registry := tools.NewRegistry()
	cap.RegisterTools(registry)

	tool, err := registry.GetTool("a2a__worker")
	require.NoError(t, err)
	assert.Equal(t, "a2a", tool.Mode)
}

func TestA2ACapability_RegisterTools_NoConfig(t *testing.T) {
	cap := NewA2ACapability()
	registry := tools.NewRegistry()

	// Should not panic
	cap.RegisterTools(registry)

	allTools := registry.GetTools()
	assert.Empty(t, allTools)
}
