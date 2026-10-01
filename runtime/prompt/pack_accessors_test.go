package prompt

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/AltairaLabs/PromptKit/runtime/v2/packspec"
)

func TestPack_GetToolAndListTools(t *testing.T) {
	p := &Pack{Pack: packspec.Pack{
		Tools: map[string]*PackTool{
			"search": {Name: "search", Description: "Search"},
			"lookup": {Name: "lookup", Description: "Look up"},
		},
	}}
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
		Variables: []*Variable{
			{Name: "name", Type: "string", Required: true, Default: "world", Description: "the name"},
		},
	}

	cfg := ToConfig(pr, "chat")
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
	empty := ToConfig(&PackPrompt{Version: "1"}, "t")
	assert.Empty(t, empty.Spec.Variables)
}

// A pack prompt's tool_policy must survive every hop to the pipeline: pack →
// ToConfig (the SDK's load path) → registry → LoadTemplate, which is what
// PromptAssemblyStage puts on TurnState for the provider stage (#2104).
func TestToolPolicy_SurvivesPackToTemplate(t *testing.T) {
	policy := &ToolPolicyPack{MaxRounds: packspec.Ptr(200), Blocklist: []string{"delete_kit"}}
	cfg := ToConfig(&PackPrompt{Version: "1", SystemTemplate: "hi", ToolPolicy: policy}, "builder")
	assert.Equal(t, policy, cfg.Spec.ToolPolicy)

	repo := newMockRepository()
	repo.prompts["builder"] = cfg
	tmpl, err := NewRegistryWithRepository(repo).LoadTemplate("builder", nil, "")
	require.NoError(t, err)
	assert.Equal(t, policy, tmpl.ToolPolicy)
}

// Compile (packc's single-prompt path) writes the prompt's tool_policy into the pack.
func TestPackCompiler_CompileCarriesToolPolicy(t *testing.T) {
	policy := &ToolPolicyPack{MaxRounds: packspec.Ptr(200)}
	repo := newMockRepository()
	repo.prompts["builder"] = &Config{Spec: Spec{
		TaskType: "builder", Version: "1.0.0", SystemTemplate: "hi", ToolPolicy: policy,
	}}
	compiler := NewPackCompilerWithDeps(NewRegistryWithRepository(repo),
		mockTimeProvider{fixedTime: time.Unix(0, 0)}, newMockFileWriter())
	pack, err := compiler.Compile("builder", "test")
	require.NoError(t, err)
	assert.Equal(t, policy, pack.Prompts["builder"].ToolPolicy)
}
