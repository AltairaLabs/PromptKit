package prompt

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestPack_GetToolAndListTools(t *testing.T) {
	p := &Pack{
		Tools: map[string]*PackTool{
			"search": {Name: "search", Description: "Search"},
			"lookup": {Name: "lookup", Description: "Look up"},
		},
	}
	require.NotNil(t, p.GetTool("search"))
	assert.Equal(t, "search", p.GetTool("search").Name)
	assert.Nil(t, p.GetTool("missing"))
	assert.ElementsMatch(t, []string{"search", "lookup"}, p.ListTools())

	empty := &Pack{}
	assert.Nil(t, empty.GetTool("x"))
	assert.Nil(t, empty.ListTools())
}

func TestPackPrompt_ToPromptConfig(t *testing.T) {
	pr := &PackPrompt{
		Version:        "1.2.3",
		Description:    "desc",
		SystemTemplate: "Hello {{name}}",
		Tools:          []string{"a", "b"},
		Variables: []Variable{
			{Name: "name", Type: "string", Required: true, Default: "world", Description: "the name"},
		},
	}

	cfg := pr.ToPromptConfig("chat")
	require.NotNil(t, cfg)
	assert.Equal(t, "promptkit.io/v1alpha1", cfg.APIVersion)
	assert.Equal(t, "Prompt", cfg.Kind)
	assert.Equal(t, "chat", cfg.Spec.TaskType)
	assert.Equal(t, "1.2.3", cfg.Spec.Version)
	assert.Equal(t, "desc", cfg.Spec.Description)
	assert.Equal(t, "Hello {{name}}", cfg.Spec.SystemTemplate)
	assert.Equal(t, []string{"a", "b"}, cfg.Spec.AllowedTools)

	// toMetadata carries the spec-exact fields and leaves Binding nil (binding
	// is a runtime concern, not part of the pack).
	require.Len(t, cfg.Spec.Variables, 1)
	v := cfg.Spec.Variables[0]
	assert.Equal(t, "name", v.Name)
	assert.Equal(t, "string", v.Type)
	assert.True(t, v.Required)
	assert.Equal(t, "world", v.Default)
	assert.Nil(t, v.Binding)

	// Empty-variables path.
	empty := (&PackPrompt{Version: "1"}).ToPromptConfig("t")
	assert.Empty(t, empty.Spec.Variables)
}
