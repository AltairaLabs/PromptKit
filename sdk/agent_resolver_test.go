package sdk

import (
	"encoding/json"
	"testing"

	"github.com/AltairaLabs/PromptKit/runtime/prompt"
	"github.com/AltairaLabs/PromptKit/sdk/internal/pack"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewAgentToolResolver_NilPack(t *testing.T) {
	r := NewAgentToolResolver(nil)
	assert.Nil(t, r)
}

func TestNewAgentToolResolver_NoAgents(t *testing.T) {
	pack := &prompt.Pack{
		ID:      "test-pack",
		Version: "1.0.0",
	}
	r := NewAgentToolResolver(pack)
	assert.Nil(t, r)
}

func TestAgentToolResolver_IsAgentTool(t *testing.T) {
	pack := &prompt.Pack{
		ID:      "test-pack",
		Version: "1.0.0",
		Prompts: map[string]*prompt.PackPrompt{
			"summarizer": {
				Name:        "Summarizer Agent",
				Description: "Summarizes text",
			},
		},
		Agents: &prompt.AgentsConfig{
			Entry: "summarizer",
			Members: map[string]*prompt.AgentDef{
				"summarizer": {
					Description: "Summarizes text",
				},
			},
		},
	}

	r := NewAgentToolResolver(pack)
	require.NotNil(t, r)

	assert.True(t, r.IsAgentTool("summarizer"))
	assert.False(t, r.IsAgentTool("nonexistent"))
	assert.False(t, r.IsAgentTool(""))
}

func TestAgentToolResolver_IsAgentTool_NilReceiver(t *testing.T) {
	var r *AgentToolResolver
	assert.False(t, r.IsAgentTool("anything"))
}

func TestAgentToolResolver_ResolveAgentTools(t *testing.T) {
	pack := &prompt.Pack{
		ID:      "test-pack",
		Version: "1.0.0",
		Prompts: map[string]*prompt.PackPrompt{
			"summarizer": {
				Name:        "Summarizer Agent",
				Description: "Summarizes text",
			},
			"translator": {
				Name:        "Translator Agent",
				Description: "Translates text",
			},
		},
		Agents: &prompt.AgentsConfig{
			Entry: "summarizer",
			Members: map[string]*prompt.AgentDef{
				"summarizer": {
					Description: "Summarizes text",
					Tags:        []string{"nlp"},
				},
				"translator": {
					Description: "Translates text",
					Tags:        []string{"nlp", "i18n"},
				},
			},
		},
	}

	r := NewAgentToolResolver(pack)
	require.NotNil(t, r)

	// Mixed list: agent refs + a regular tool name
	descriptors := r.ResolveAgentTools([]string{"summarizer", "get_weather", "translator"})
	require.Len(t, descriptors, 2)

	// Collect resolved names
	names := make(map[string]bool)
	for _, d := range descriptors {
		names[d.Name] = true
		assert.Equal(t, "a2a", d.Mode)
		assert.NotNil(t, d.A2AConfig)
		assert.NotEmpty(t, d.A2AConfig.SkillID)
	}
	assert.True(t, names["summarizer"])
	assert.True(t, names["translator"])
}

func TestAgentToolResolver_ResolveAgentTools_Empty(t *testing.T) {
	pack := &prompt.Pack{
		ID:      "test-pack",
		Version: "1.0.0",
		Agents: &prompt.AgentsConfig{
			Entry: "summarizer",
			Members: map[string]*prompt.AgentDef{
				"summarizer": {
					Description: "Summarizes text",
				},
			},
		},
	}

	r := NewAgentToolResolver(pack)
	require.NotNil(t, r)

	descriptors := r.ResolveAgentTools([]string{})
	assert.Nil(t, descriptors)
}

func TestAgentToolResolver_ResolveAgentTools_NilReceiver(t *testing.T) {
	var r *AgentToolResolver
	descriptors := r.ResolveAgentTools([]string{"anything"})
	assert.Nil(t, descriptors)
}

// --- New tests for endpoint resolution, schemas, and wiring ---

func TestAgentToolResolver_InputOutputSchemas(t *testing.T) {
	pack := &prompt.Pack{
		ID:      "test-pack",
		Version: "1.0.0",
		Prompts: map[string]*prompt.PackPrompt{
			"summarizer": {
				Name:        "Summarizer",
				Description: "Summarizes text",
			},
		},
		Agents: &prompt.AgentsConfig{
			Entry: "summarizer",
			Members: map[string]*prompt.AgentDef{
				"summarizer": {Description: "Summarizes text"},
			},
		},
	}

	r := NewAgentToolResolver(pack)
	require.NotNil(t, r)

	descriptors := r.ResolveAgentTools([]string{"summarizer"})
	require.Len(t, descriptors, 1)

	d := descriptors[0]

	// Verify input schema has "query" required field
	var inputSchema map[string]any
	require.NoError(t, json.Unmarshal(d.InputSchema, &inputSchema))
	assert.Equal(t, "object", inputSchema["type"])

	props, ok := inputSchema["properties"].(map[string]any)
	require.True(t, ok)
	queryProp, ok := props["query"].(map[string]any)
	require.True(t, ok)
	assert.Equal(t, "string", queryProp["type"])

	required, ok := inputSchema["required"].([]any)
	require.True(t, ok)
	assert.Contains(t, required, "query")

	// Verify output schema has "response" field
	var outputSchema map[string]any
	require.NoError(t, json.Unmarshal(d.OutputSchema, &outputSchema))
	assert.Equal(t, "object", outputSchema["type"])

	outProps, ok := outputSchema["properties"].(map[string]any)
	require.True(t, ok)
	_, ok = outProps["response"].(map[string]any)
	require.True(t, ok)
}

func TestStaticEndpointResolver(t *testing.T) {
	r := &StaticEndpointResolver{BaseURL: "http://localhost:9000"}
	assert.Equal(t, "http://localhost:9000", r.Resolve("any-agent"))
	assert.Equal(t, "http://localhost:9000", r.Resolve("another-agent"))
}

func TestMapEndpointResolver(t *testing.T) {
	r := &MapEndpointResolver{
		Endpoints: map[string]string{
			"summarizer": "http://summarizer:9001",
			"translator": "http://translator:9002",
		},
	}
	assert.Equal(t, "http://summarizer:9001", r.Resolve("summarizer"))
	assert.Equal(t, "http://translator:9002", r.Resolve("translator"))
	assert.Equal(t, "", r.Resolve("unknown"))
}

func TestAgentToolResolver_WithStaticEndpoint(t *testing.T) {
	pack := &prompt.Pack{
		ID:      "test-pack",
		Version: "1.0.0",
		Prompts: map[string]*prompt.PackPrompt{
			"summarizer": {
				Name:        "Summarizer",
				Description: "Summarizes text",
			},
		},
		Agents: &prompt.AgentsConfig{
			Entry: "summarizer",
			Members: map[string]*prompt.AgentDef{
				"summarizer": {Description: "Summarizes text"},
			},
		},
	}

	r := NewAgentToolResolver(pack)
	require.NotNil(t, r)

	r.SetEndpointResolver(&StaticEndpointResolver{BaseURL: "http://localhost:8080"})

	descriptors := r.ResolveAgentTools([]string{"summarizer"})
	require.Len(t, descriptors, 1)

	assert.Equal(t, "http://localhost:8080", descriptors[0].A2AConfig.AgentURL)
	assert.Equal(t, "summarizer", descriptors[0].A2AConfig.SkillID)
}

func TestAgentToolResolver_WithMapEndpoints(t *testing.T) {
	pack := &prompt.Pack{
		ID:      "test-pack",
		Version: "1.0.0",
		Prompts: map[string]*prompt.PackPrompt{
			"summarizer": {
				Name:        "Summarizer",
				Description: "Summarizes text",
			},
			"translator": {
				Name:        "Translator",
				Description: "Translates text",
			},
		},
		Agents: &prompt.AgentsConfig{
			Entry: "summarizer",
			Members: map[string]*prompt.AgentDef{
				"summarizer": {Description: "Summarizes text"},
				"translator": {Description: "Translates text"},
			},
		},
	}

	r := NewAgentToolResolver(pack)
	require.NotNil(t, r)

	r.SetEndpointResolver(&MapEndpointResolver{
		Endpoints: map[string]string{
			"summarizer": "http://summarizer:9001",
			"translator": "http://translator:9002",
		},
	})

	descriptors := r.ResolveAgentTools([]string{"summarizer", "translator"})
	require.Len(t, descriptors, 2)

	byName := make(map[string]string)
	for _, d := range descriptors {
		byName[d.Name] = d.A2AConfig.AgentURL
	}
	assert.Equal(t, "http://summarizer:9001", byName["summarizer"])
	assert.Equal(t, "http://translator:9002", byName["translator"])
}

func TestAgentToolResolver_NoEndpointResolver(t *testing.T) {
	pack := &prompt.Pack{
		ID:      "test-pack",
		Version: "1.0.0",
		Prompts: map[string]*prompt.PackPrompt{
			"summarizer": {
				Name:        "Summarizer",
				Description: "Summarizes text",
			},
		},
		Agents: &prompt.AgentsConfig{
			Entry: "summarizer",
			Members: map[string]*prompt.AgentDef{
				"summarizer": {Description: "Summarizes text"},
			},
		},
	}

	r := NewAgentToolResolver(pack)
	require.NotNil(t, r)

	// No endpoint resolver set - AgentURL should be empty
	descriptors := r.ResolveAgentTools([]string{"summarizer"})
	require.Len(t, descriptors, 1)
	assert.Equal(t, "", descriptors[0].A2AConfig.AgentURL)
}

func TestAgentToolResolver_SetEndpointResolver_NilReceiver(t *testing.T) {
	var r *AgentToolResolver
	// Should not panic
	r.SetEndpointResolver(&StaticEndpointResolver{BaseURL: "http://localhost"})
}

func TestAgentToolResolver_MemberNames(t *testing.T) {
	pack := &prompt.Pack{
		ID:      "test-pack",
		Version: "1.0.0",
		Prompts: map[string]*prompt.PackPrompt{
			"summarizer": {Name: "Summarizer"},
			"translator": {Name: "Translator"},
		},
		Agents: &prompt.AgentsConfig{
			Entry: "summarizer",
			Members: map[string]*prompt.AgentDef{
				"summarizer": {},
				"translator": {},
			},
		},
	}

	r := NewAgentToolResolver(pack)
	require.NotNil(t, r)

	names := r.MemberNames()
	assert.Len(t, names, 2)
	assert.ElementsMatch(t, []string{"summarizer", "translator"}, names)
}

func TestAgentToolResolver_MemberNames_NilReceiver(t *testing.T) {
	var r *AgentToolResolver
	assert.Nil(t, r.MemberNames())
}

func TestAgentToolResolver_NonAgentToolsUnaffected(t *testing.T) {
	// Verify that tool names NOT in agents.members are ignored by the resolver
	pack := &prompt.Pack{
		ID:      "test-pack",
		Version: "1.0.0",
		Prompts: map[string]*prompt.PackPrompt{
			"summarizer": {Name: "Summarizer"},
		},
		Agents: &prompt.AgentsConfig{
			Entry: "summarizer",
			Members: map[string]*prompt.AgentDef{
				"summarizer": {Description: "Summarizes text"},
			},
		},
	}

	r := NewAgentToolResolver(pack)
	require.NotNil(t, r)

	// These are regular tools, not agent members
	assert.False(t, r.IsAgentTool("get_weather"))
	assert.False(t, r.IsAgentTool("search_docs"))

	// Only agent tools should be resolved
	descriptors := r.ResolveAgentTools([]string{"get_weather", "search_docs"})
	assert.Nil(t, descriptors)
}

func TestPackToRuntimePack(t *testing.T) {
	// Test the internal conversion from sdk pack to runtime pack
	sdkPack := &pack.Pack{
		ID:      "test",
		Version: "1.0.0",
		Prompts: map[string]*pack.Prompt{
			"chat": {
				Name:        "Chat",
				Description: "A chat prompt",
			},
		},
		Agents: &pack.AgentsConfig{
			Entry: "chat",
			Members: map[string]*pack.AgentDef{
				"chat": {
					Description: "Chat agent",
					Tags:        []string{"chat"},
					InputModes:  []string{"text/plain"},
					OutputModes: []string{"text/plain"},
				},
			},
		},
	}

	rp := packToRuntimePack(sdkPack)
	require.NotNil(t, rp)
	assert.Equal(t, "test", rp.ID)
	assert.Equal(t, "1.0.0", rp.Version)

	require.Contains(t, rp.Prompts, "chat")
	assert.Equal(t, "Chat", rp.Prompts["chat"].Name)

	require.NotNil(t, rp.Agents)
	assert.Equal(t, "chat", rp.Agents.Entry)
	require.Contains(t, rp.Agents.Members, "chat")
	assert.Equal(t, "Chat agent", rp.Agents.Members["chat"].Description)
	assert.Equal(t, []string{"chat"}, rp.Agents.Members["chat"].Tags)
}
