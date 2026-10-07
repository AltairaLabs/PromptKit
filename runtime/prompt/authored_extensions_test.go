package prompt

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/xeipuuv/gojsonschema"
	"gopkg.in/yaml.v3"

	"github.com/AltairaLabs/PromptKit/runtime/v2/packspec"
	"github.com/AltairaLabs/PromptKit/runtime/v2/prompt/schema"
)

const authoredExtensionsPromptYAML = `
apiVersion: promptkit.io/v1alpha1
kind: Prompt
metadata:
  name: billing
spec:
  task_type: billing
  version: 1.0.0
  description: Billing
  system_template: You bill.
  extensions:
    acme:tier: gold
  validators:
    - id: no-cards
      type: banned_words
      params:
        words: ["4111"]
      extensions:
        acme:control:
          framework: PCI
          ids: ["3.4"]
    - type: max_length
      params:
        max_characters: 500
`

// TestAuthoredRFC0016FieldsReachTheEmittedPack — Prompt.extensions and
// Validator id/extensions written in a prompt config reach the compiled pack
// unchanged, and the pack still validates against the embedded schema.
func TestAuthoredRFC0016FieldsReachTheEmittedPack(t *testing.T) {
	var cfg Config
	require.NoError(t, yaml.Unmarshal([]byte(authoredExtensionsPromptYAML), &cfg))

	pr := NewPackCompiler(nil).createPackPrompt(&cfg)

	require.Equal(t, map[string]any{"acme:tier": "gold"}, pr.Extensions)
	require.Len(t, pr.Validators, 2)
	require.Equal(t, "no-cards", pr.Validators[0].ID)
	require.Equal(t, map[string]any{
		"acme:control": map[string]any{"framework": "PCI", "ids": []any{"3.4"}},
	}, pr.Validators[0].Extensions)
	require.NotContains(t, pr.Validators[0].Params, "acme:control",
		"extensions are never merged into params")
	require.Equal(t, map[string]any{"words": []any{"4111"}}, pr.Validators[0].Params)
	require.Empty(t, pr.Validators[1].ID, "a validator without an id stays unidentified")
	require.Nil(t, pr.Validators[1].Extensions)

	pack := &Pack{Pack: packspec.Pack{
		ID: "p", Name: "P", Version: "1.0.0", Description: "d",
		TemplateEngine: &TemplateEngineInfo{Version: "v1", Syntax: "{{variable}}"},
		Prompts:        map[string]*PackPrompt{"billing": pr},
	}}
	out, err := NewPackCompiler(nil).MarshalPack(pack)
	require.NoError(t, err)
	result, err := schema.ValidateJSONAgainstLoader(out,
		gojsonschema.NewStringLoader(schema.GetEmbeddedSchema()))
	require.NoError(t, err)
	require.True(t, result.Valid, "emitted pack must validate: %v", result.Errors)
}

// TestPromptWithoutRFC0016FieldsCompilesUnchanged — a config that declares
// none of them emits no extensions or id keys at all.
func TestPromptWithoutRFC0016FieldsCompilesUnchanged(t *testing.T) {
	var cfg Config
	require.NoError(t, yaml.Unmarshal([]byte(`
apiVersion: promptkit.io/v1alpha1
kind: Prompt
metadata:
  name: plain
spec:
  task_type: plain
  version: 1.0.0
  system_template: Hi.
  validators:
    - type: max_length
      params:
        max_characters: 500
`), &cfg))

	out, err := json.Marshal(NewPackCompiler(nil).createPackPrompt(&cfg))
	require.NoError(t, err)

	var got map[string]any
	require.NoError(t, json.Unmarshal(out, &got))
	require.NotContains(t, got, "extensions")
	validator := got["validators"].([]any)[0].(map[string]any)
	require.NotContains(t, validator, "id")
	require.NotContains(t, validator, "extensions")
}

// TestCompileCarriesAuthoredRFC0016Fields — the single-prompt Compile path
// (CompileToFile) carries the same fields as CompileFromRegistry, and
// compiling never writes a folded message into the authoring config's params.
func TestCompileCarriesAuthoredRFC0016Fields(t *testing.T) {
	var cfg Config
	require.NoError(t, yaml.Unmarshal([]byte(authoredExtensionsPromptYAML), &cfg))
	cfg.Spec.Validators[0].Message = "Card numbers are not allowed."

	repo := newMockRepository()
	repo.prompts["billing"] = &cfg
	registry := NewRegistryWithRepository(repo)
	require.NoError(t, registry.RegisterConfig("billing", &cfg))

	pack, err := NewPackCompiler(registry).Compile("billing", "test")
	require.NoError(t, err)

	pr := pack.Prompts["billing"]
	require.NotNil(t, pr)
	require.Equal(t, map[string]any{"acme:tier": "gold"}, pr.Extensions)
	require.Equal(t, "no-cards", pr.Validators[0].ID)
	require.Contains(t, pr.Validators[0].Extensions, "acme:control")
	require.Equal(t, "Card numbers are not allowed.", pr.Validators[0].Params["message"])

	require.NotContains(t, cfg.Spec.Validators[0].Params, "message",
		"compiling must not mutate the authoring config's params")
}
