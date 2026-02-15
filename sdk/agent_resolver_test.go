package sdk

import (
	"testing"

	"github.com/AltairaLabs/PromptKit/runtime/prompt"
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
