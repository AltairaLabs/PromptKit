package integration

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	yamlrepo "github.com/AltairaLabs/PromptKit/runtime/v2/persistence/yaml"
	"github.com/AltairaLabs/PromptKit/runtime/v2/prompt"
	"github.com/AltairaLabs/PromptKit/sdk/v2"
)

// TestFragments_DynamicNameCompiledFromEnumResolvesFromSetVar compiles a prompt
// whose fragment a variable chooses, where the variable declares the values it
// takes (validation.enum). The pack carries every candidate, so the SDK
// resolves the fragment from the variable at render time, whichever source
// set it — here SetVar alone, with the variable required.
func TestFragments_DynamicNameCompiledFromEnumResolvesFromSetVar(t *testing.T) {
	base := t.TempDir()
	writeFile(t, filepath.Join(base, "fragments", "persona_us.yaml"),
		"fragment_type: text\nversion: v1\ncontent: \"Use US spelling.\"\n")
	writeFile(t, filepath.Join(base, "fragments", "persona_uk.yaml"),
		"fragment_type: text\nversion: v1\ncontent: \"Use UK spelling.\"\n")
	writeFile(t, filepath.Join(base, "prompts", "support.yaml"), `apiVersion: promptkit.altairalabs.ai/v1alpha1
kind: PromptConfig
metadata:
  name: support
spec:
  task_type: support
  version: 1.0.0
  description: Support prompt
  system_template: "Support. {{persona_{{region}}}}"
  fragments:
    - name: persona_{{region}}
      path: ../fragments/persona_{{region}}.yaml
      required: true
  variables:
    - name: region
      type: string
      required: true
      validation:
        enum: [us, uk]
`)
	reg := prompt.NewRegistryWithRepository(
		yamlrepo.NewYAMLPromptRepository(base, map[string]string{"support": "prompts/support.yaml"}))
	compiler := prompt.NewPackCompiler(reg)
	pack, err := compiler.Compile("support", "test")
	require.NoError(t, err)
	assert.Equal(t, "Use US spelling.", pack.Fragments["persona_us"])
	assert.Equal(t, "Use UK spelling.", pack.Fragments["persona_uk"])
	packPath := filepath.Join(t.TempDir(), "support.pack.json")
	require.NoError(t, compiler.WritePack(pack, packPath))

	for region, want := range map[string]string{"us": "Support. Use US spelling.", "uk": "Support. Use UK spelling."} {
		rec := newRecordingProvider()
		conv, err := sdk.Open(packPath, "support", sdk.WithProvider(rec))
		require.NoError(t, err)
		t.Cleanup(func() { _ = conv.Close() })
		conv.SetVar("region", region)
		_, err = conv.Send(context.Background(), "hi")
		require.NoError(t, err, "region %s", region)
		assert.Equal(t, want, rec.system())
		// The source registry renders the same.
		source := reg.LoadWithVars("support", map[string]string{"region": region}, "")
		require.NotNil(t, source)
		assert.Equal(t, source.SystemPrompt, rec.system())
	}
}
