package sdk

import (
	"testing"

	"github.com/AltairaLabs/PromptKit/runtime/tools"
	"github.com/AltairaLabs/PromptKit/sdk/internal/pack"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestA2ACapability_Name(t *testing.T) {
	cap := NewA2ACapability()
	assert.Equal(t, "a2a", cap.Name())
}

func TestA2ACapability_Init_NilAgents(t *testing.T) {
	cap := NewA2ACapability()
	p := &pack.Pack{
		ID:      "test",
		Prompts: map[string]*pack.Prompt{"chat": {ID: "chat"}},
	}
	err := cap.Init(CapabilityContext{Pack: p, PromptName: "chat"})
	require.NoError(t, err)
	assert.Nil(t, cap.agentResolver)
	assert.NotNil(t, cap.prompt)
}

func TestA2ACapability_Init_WithAgents(t *testing.T) {
	cap := NewA2ACapability()
	p := &pack.Pack{
		ID: "test",
		Prompts: map[string]*pack.Prompt{
			"orchestrator": {
				ID:    "orchestrator",
				Tools: []string{"helper"},
			},
		},
		Agents: &pack.AgentsConfig{
			Entry: "orchestrator",
			Members: map[string]*pack.AgentDef{
				"helper": {
					Description: "A helper agent",
				},
			},
		},
	}
	err := cap.Init(CapabilityContext{Pack: p, PromptName: "orchestrator"})
	require.NoError(t, err)
	assert.NotNil(t, cap.agentResolver)
	assert.NotNil(t, cap.prompt)
}

func TestA2ACapability_RegisterTools_BridgePath(t *testing.T) {
	srv := a2aTestServer(t, "BridgeAgent", "do_stuff", "result")
	defer srv.Close()

	bridge := setupA2ABridge(t, srv.URL)
	cap := NewA2ACapability()
	cap.bridge = bridge

	registry := tools.NewRegistry()
	cap.RegisterTools(registry)

	tool, err := registry.GetTool("a2a__bridgeagent__do_stuff")
	require.NoError(t, err)
	assert.Equal(t, "a2a", tool.Mode)
}

func TestA2ACapability_RegisterTools_PackPath(t *testing.T) {
	cap := NewA2ACapability()
	p := &pack.Pack{
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
	}
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

	p := &pack.Pack{
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
	}

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

func TestA2ACapability_Close(t *testing.T) {
	cap := NewA2ACapability()
	assert.NoError(t, cap.Close())
}
