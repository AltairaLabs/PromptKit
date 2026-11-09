package prompt_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/AltairaLabs/PromptKit/runtime/v2/persistence/memory"
	"github.com/AltairaLabs/PromptKit/runtime/v2/prompt"
)

// packToRegistryJSON is a pack whose prompt uses every field the pack ->
// Config conversion (Pack.PromptConfig / ToConfig) carries to rendering:
// variables with a default and a required one, tools, a tool policy, a
// model override and a fragment.
const packToRegistryJSON = `{
  "$schema": "https://promptpack.org/schema/latest/promptpack.schema.json",
  "id": "support-pack",
  "name": "Support",
  "version": "1.0.0",
  "template_engine": {"version": "v1", "syntax": "{{variable}}"},
  "fragments": {"signoff": "Thanks for contacting us."},
  "prompts": {
    "support": {
      "id": "support",
      "name": "Support",
      "version": "2.1.0",
      "description": "Support agent",
      "system_template": "You help {{customer}} with {{topic}}. {{signoff}}",
      "variables": [
        {"name": "customer", "type": "string", "required": true},
        {"name": "topic", "type": "string", "required": false, "default": "billing"}
      ],
      "tools": ["lookup_order", "refund"],
      "tool_policy": {"max_rounds": 4, "blocklist": ["refund"]},
      "model_overrides": {"gpt-4o": {"system_template_suffix": " Be brief."}}
    }
  }
}`

// TestPack_PromptConfigRendersThroughTheRegistry drives a pack file through
// the path a pack consumer takes: LoadPack, Pack.PromptConfig, a repository,
// and a prompt.Registry, and asserts what the registry renders and hands the
// pipeline. A field ToConfig drops or mangles shows up here as a wrong prompt.
func TestPack_PromptConfigRendersThroughTheRegistry(t *testing.T) {
	reg := registryFromPackFile(t, packToRegistryJSON, "support")

	t.Run("default model", func(t *testing.T) {
		got := reg.LoadWithVars("support", map[string]string{"customer": "Ada"}, "")
		require.NotNil(t, got)
		assert.Equal(t, "You help Ada with billing. Thanks for contacting us.", got.SystemPrompt)
		assert.Equal(t, []string{"lookup_order", "refund"}, got.AllowedTools)
	})

	t.Run("overridden model and caller variables", func(t *testing.T) {
		got := reg.LoadWithVars("support", map[string]string{"customer": "Ada", "topic": "shipping"}, "gpt-4o")
		require.NotNil(t, got)
		assert.Equal(t, "You help Ada with shipping. Thanks for contacting us. Be brief.", got.SystemPrompt)
	})

	t.Run("template handed to the pipeline", func(t *testing.T) {
		tmpl, err := reg.LoadTemplate("support", map[string]string{"customer": "Ada"}, "")
		require.NoError(t, err)
		assert.Equal(t, []string{"customer"}, tmpl.RequiredVars)
		assert.Equal(t, "billing", tmpl.DefaultVars["topic"])
		assert.Equal(t, []string{"lookup_order", "refund"}, tmpl.AllowedTools)
		require.NotNil(t, tmpl.ToolPolicy)
		require.NotNil(t, tmpl.ToolPolicy.MaxRounds)
		assert.Equal(t, 4, *tmpl.ToolPolicy.MaxRounds)
		assert.Equal(t, []string{"refund"}, tmpl.ToolPolicy.Blocklist)
	})

	t.Run("a required variable left unset fails the load", func(t *testing.T) {
		_, err := reg.LoadTemplate("support", nil, "")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "customer")
	})
}

// registryFromPackFile writes packJSON to a file, loads it with LoadPack, and
// returns a registry over a memory repository holding the pack's prompt
// (via Pack.PromptConfig) and fragments.
func registryFromPackFile(t *testing.T, packJSON, taskType string) *prompt.Registry {
	t.Helper()
	path := filepath.Join(t.TempDir(), "pack.json")
	if err := os.WriteFile(path, []byte(packJSON), 0o600); err != nil {
		t.Fatal(err)
	}
	pack, err := prompt.LoadPack(path)
	if err != nil {
		t.Fatalf("LoadPack: %v", err)
	}
	repo := memory.NewPromptRepository()
	repo.RegisterPrompt(taskType, pack.PromptConfig(taskType))
	for name, content := range pack.Fragments {
		repo.RegisterFragment(name, &prompt.Fragment{Content: content})
	}
	return prompt.NewRegistryWithRepository(repo)
}
