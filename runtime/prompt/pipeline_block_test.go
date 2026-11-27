package prompt_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/AltairaLabs/PromptKit/runtime/v2/persistence/memory"
	"github.com/AltairaLabs/PromptKit/runtime/v2/prompt"
)

// A compiled prompt carries no pipeline block. PromptKit set the same default
// block on every prompt, which no runtime read and no author could change, and
// authors took it for live config: a pack edited to raise its provider timeout
// changed nothing (#2929).
func TestCompiledPackHasNoPipelineBlock(t *testing.T) {
	repo := memory.NewPromptRepository()
	repo.RegisterPrompt("chat", &prompt.Config{
		APIVersion: "promptkit.altairalabs.ai/v1alpha1",
		Kind:       "PromptConfig",
		Spec: prompt.Spec{
			TaskType:       "chat",
			Version:        "1.0.0",
			SystemTemplate: "You are helpful.",
		},
	})

	pack, err := prompt.NewPackCompiler(prompt.NewRegistryWithRepository(repo)).
		CompileFromRegistry("pipeline-free", "test")
	require.NoError(t, err)
	data, err := prompt.NewPackCompiler(nil).MarshalPack(pack)
	require.NoError(t, err)

	var doc struct {
		Prompts map[string]map[string]json.RawMessage `json:"prompts"`
	}
	require.NoError(t, json.Unmarshal(data, &doc))
	require.Contains(t, doc.Prompts, "chat", "precondition: the prompt was compiled")
	assert.Equal(t, `"You are helpful."`, string(doc.Prompts["chat"]["system_template"]))
	assert.NotContains(t, doc.Prompts["chat"], "pipeline")
}

// The spec allows a pipeline block, and packs compiled by earlier releases
// carry one, so a pack that has it still loads, validates and keeps it.
func TestPackWithLegacyPipelineBlockStillLoads(t *testing.T) {
	const legacy = `{
  "id": "legacy",
  "name": "Legacy",
  "version": "1.0.0",
  "template_engine": {"version": "v1", "syntax": "{{variable}}"},
  "compilation": {"compiled_with": "packc-v1.0.0", "created_at": "2026-01-01T00:00:00Z", "schema": "v1"},
  "prompts": {
    "chat": {
      "id": "chat",
      "name": "Chat",
      "version": "1.0.0",
      "system_template": "You are helpful.",
      "pipeline": {
        "stages": ["template", "provider", "validator"],
        "middleware": [{"type": "provider", "config": {"retry_attempts": 3, "timeout": 30}}]
      }
    }
  }
}`
	path := filepath.Join(t.TempDir(), "legacy.pack.json")
	require.NoError(t, os.WriteFile(path, []byte(legacy), 0o600))

	pack, err := prompt.LoadPack(path)
	require.NoError(t, err)

	assert.Empty(t, pack.Validate())
	chat := pack.Prompts["chat"]
	require.NotNil(t, chat)
	assert.Equal(t, "You are helpful.", chat.SystemTemplate)
	require.NotNil(t, chat.Pipeline)
	assert.Equal(t, []string{"template", "provider", "validator"}, chat.Pipeline.Stages)
	require.Len(t, chat.Pipeline.Middleware, 1)
	assert.Equal(t, "provider", chat.Pipeline.Middleware[0].Type)
}
