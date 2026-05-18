package config_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/AltairaLabs/PromptKit/pkg/config"
	"github.com/AltairaLabs/PromptKit/runtime/providers/base"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"
)

func TestCompat_LegacyChatYAML_TranslatesToUnifiedSpec(t *testing.T) {
	data := []byte(`
apiVersion: promptkit.altairalabs.ai/v1alpha1
kind: Provider
metadata:
  name: openai-gpt-4o-mini
spec:
  id: openai-gpt-4o-mini
  type: openai
  model: gpt-4o-mini
  include_raw_output: false
  pricing:
    input_cost_per_1k: 0.00015
    output_cost_per_1k: 0.0006
  defaults:
    temperature: 0.7
    max_tokens: 1500
`)
	spec, err := config.LoadProviderSpec(data)
	require.NoError(t, err)

	assert.Equal(t, "openai-gpt-4o-mini", spec.Name)
	assert.Equal(t, "openai", spec.Impl)
	require.Len(t, spec.Capabilities, 1)

	cap := spec.Capabilities[0]
	assert.Equal(t, base.ProviderTypeInference, cap.Type)
	assert.Equal(t, "gpt-4o-mini", cap.Model)
	require.NotNil(t, cap.Pricing)
	assert.Equal(t, base.PricingSourceInline, cap.Pricing.Source)

	rates := map[string]float64{}
	for _, it := range cap.Pricing.Items {
		rates[it.Unit] = it.Rate
	}
	assert.InDelta(t, 0.00015/1000, rates["input_token"], 1e-12)
	assert.InDelta(t, 0.0006/1000, rates["output_token"], 1e-12)
}

func TestCompat_LegacyChatYAML_NoPricing(t *testing.T) {
	data := []byte(`
apiVersion: promptkit.altairalabs.ai/v1alpha1
kind: Provider
metadata:
  name: claude-3-5-haiku
spec:
  id: claude-3-5-haiku
  type: claude
  model: claude-3-5-haiku-20241022
  defaults:
    temperature: 0.7
    max_tokens: 1500
    top_p: 1.0
`)
	spec, err := config.LoadProviderSpec(data)
	require.NoError(t, err)

	assert.Equal(t, "claude-3-5-haiku", spec.Name)
	assert.Equal(t, "claude", spec.Impl)
	require.Len(t, spec.Capabilities, 1)
	assert.Equal(t, base.ProviderTypeInference, spec.Capabilities[0].Type)
	assert.Nil(t, spec.Capabilities[0].Pricing)
}

func TestCompat_LegacyChatYAML_DecorativeCapabilitiesIgnored(t *testing.T) {
	// legacy YAML with decorative capabilities: []string — must not error
	data := []byte(`
apiVersion: promptkit.altairalabs.ai/v1alpha1
kind: Provider
metadata:
  name: openai-legacy
spec:
  type: openai
  model: gpt-4
  capabilities:
    - text
    - streaming
    - tools
`)
	spec, err := config.LoadProviderSpec(data)
	require.NoError(t, err)
	assert.Equal(t, "openai", spec.Impl)
	require.Len(t, spec.Capabilities, 1)
	assert.Equal(t, base.ProviderTypeInference, spec.Capabilities[0].Type)
}

// The "unified" multi-role provider YAML shape (spec.impl + spec.capabilities
// as a list of {type, model, pricing}) was removed 2026-05-18. These tests
// pin the rejection contract so a future migration accidentally re-enabling
// the parser would fail.

func TestCompat_RejectsRemovedImplField(t *testing.T) {
	data := []byte(`
apiVersion: promptkit.altairalabs.ai/v1alpha1
kind: Provider
metadata:
  name: openai
spec:
  impl: openai
  capabilities:
    - type: tts
      model: tts-1
`)
	_, err := config.LoadProviderSpec(data)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "spec.impl")
	assert.Contains(t, err.Error(), "spec.type")
}

func TestCompat_RejectsMultiRoleCapabilitiesList(t *testing.T) {
	// capabilities as a list of mappings (the old unified shape) is rejected.
	data := []byte(`
apiVersion: promptkit.altairalabs.ai/v1alpha1
kind: Provider
metadata:
  name: acme-multi
spec:
  type: acme
  capabilities:
    - type: inference
      model: acme-chat
    - type: embedding
      model: acme-embed
`)
	_, err := config.LoadProviderSpec(data)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "spec.capabilities")
	assert.Contains(t, err.Error(), "spec.role")
}

func TestCompat_RejectsLegacyCapabilityFieldWithRenameMessage(t *testing.T) {
	// The pre-2026-05-18 field name was `capability` (singular). New name is
	// `role`. The loader rejects the old name with a rename hint rather than
	// silently dropping it. (Note the literal "capability: tts" in the YAML
	// here — this test fixture deliberately uses the OLD field name.)
	data := []byte(`
apiVersion: promptkit.altairalabs.ai/v1alpha1
kind: Provider
metadata:
  name: openai-tts
spec:
  type: openai
  capability: tts
`)
	_, err := config.LoadProviderSpec(data)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "spec.capability")
	assert.Contains(t, err.Error(), "spec.role")
}

func TestCompat_PreservesFeatureFlagsCapabilities(t *testing.T) {
	// capabilities as a list of strings (per-model feature flags) must still
	// load — that's distinct from the multi-role list and is unchanged.
	data := []byte(`
apiVersion: promptkit.altairalabs.ai/v1alpha1
kind: Provider
metadata:
  name: openai
spec:
  type: openai
  model: gpt-4o
  capabilities: [text, streaming, vision, tools]
`)
	spec, err := config.LoadProviderSpec(data)
	require.NoError(t, err)
	assert.Equal(t, "openai", spec.Impl)
	require.Len(t, spec.Capabilities, 1)
	assert.Equal(t, base.ProviderTypeInference, spec.Capabilities[0].Type)
}

func TestCompat_UnknownKind_ReturnsError(t *testing.T) {
	data := []byte(`
apiVersion: promptkit.altairalabs.ai/v1alpha1
kind: Scenario
metadata:
  name: x
spec: {}
`)
	_, err := config.LoadProviderSpec(data)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "Provider")
}

// TestCompat_RemotePricingSource and TestCompat_UnknownCapabilityType
// were removed 2026-05-18 along with the multi-role "unified" shape they
// exercised. The replacement coverage is in
// TestCompat_RejectsRemovedImplField + TestCompat_RejectsMultiRoleCapabilitiesList.

func TestCompat_AllExistingExampleYAMLs_Load(t *testing.T) {
	// Walk examples/*/providers/*.yaml — every file with `kind: Provider`
	// should parse without error via the compat shim.
	// Files in providers/ dirs that are NOT Provider configs (e.g. mock
	// response definitions) are skipped.
	pattern := "../../examples/*/providers/*.yaml"
	files, err := filepath.Glob(pattern)
	require.NoError(t, err)
	require.NotEmpty(t, files, "no provider YAML files found at %s", pattern)

	for _, f := range files {
		f := f
		t.Run(filepath.Base(f), func(t *testing.T) {
			data, err := os.ReadFile(f)
			require.NoError(t, err)
			// Skip files that aren't Provider documents.
			var probe struct {
				Kind string `yaml:"kind"`
			}
			_ = yaml.Unmarshal(data, &probe)
			if probe.Kind != "Provider" {
				t.Skipf("not a Provider document (kind=%q)", probe.Kind)
			}
			_, err = config.LoadProviderSpec(data)
			assert.NoError(t, err, "loading %s", f)
		})
	}
}
