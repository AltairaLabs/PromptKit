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

func TestCompat_UnifiedYAML_PassesThroughUnchanged(t *testing.T) {
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
      pricing:
        items:
          - { unit: character, rate: 0.000015 }
`)
	spec, err := config.LoadProviderSpec(data)
	require.NoError(t, err)

	assert.Equal(t, "openai", spec.Name)
	assert.Equal(t, "openai", spec.Impl)
	require.Len(t, spec.Capabilities, 1)
	assert.Equal(t, base.ProviderTypeTTS, spec.Capabilities[0].Type)
	assert.Equal(t, "tts-1", spec.Capabilities[0].Model)
	require.NotNil(t, spec.Capabilities[0].Pricing)
	require.Len(t, spec.Capabilities[0].Pricing.Items, 1)
	assert.Equal(t, "character", spec.Capabilities[0].Pricing.Items[0].Unit)
	assert.InDelta(t, 0.000015, spec.Capabilities[0].Pricing.Items[0].Rate, 1e-12)
}

func TestCompat_UnifiedYAML_MultipleCapabilities(t *testing.T) {
	data := []byte(`
apiVersion: promptkit.altairalabs.ai/v1alpha1
kind: Provider
metadata:
  name: acme-multi
spec:
  impl: acme
  endpoint: https://api.acme.example/v1
  auth:
    type: api_key
    env: ACME_API_KEY
  capabilities:
    - type: inference
      model: acme-chat
    - type: embedding
      model: acme-embed
    - type: image
      model: acme-image
`)
	spec, err := config.LoadProviderSpec(data)
	require.NoError(t, err)

	assert.Equal(t, "acme-multi", spec.Name)
	assert.Equal(t, "acme", spec.Impl)
	assert.Equal(t, "https://api.acme.example/v1", spec.Endpoint)
	assert.Equal(t, "api_key", spec.Auth.Type)
	assert.Equal(t, "ACME_API_KEY", spec.Auth.Env)
	require.Len(t, spec.Capabilities, 3)
	assert.Equal(t, base.ProviderTypeInference, spec.Capabilities[0].Type)
	assert.Equal(t, base.ProviderTypeEmbedding, spec.Capabilities[1].Type)
	assert.Equal(t, base.ProviderTypeImage, spec.Capabilities[2].Type)
}

func TestCompat_UnifiedYAML_PricingCorrectAt(t *testing.T) {
	data := []byte(`
apiVersion: promptkit.altairalabs.ai/v1alpha1
kind: Provider
metadata:
  name: dated-provider
spec:
  impl: openai
  capabilities:
    - type: inference
      model: gpt-4o
      pricing:
        correct_at: "2024-11-01"
        currency: usd
        items:
          - { unit: input_token, rate: 0.0000025 }
          - { unit: output_token, rate: 0.00001 }
`)
	spec, err := config.LoadProviderSpec(data)
	require.NoError(t, err)
	require.Len(t, spec.Capabilities, 1)
	p := spec.Capabilities[0].Pricing
	require.NotNil(t, p)
	assert.Equal(t, "usd", p.Currency)
	assert.Equal(t, 2024, p.PricingCorrectAt.Year())
	assert.Equal(t, 11, int(p.PricingCorrectAt.Month()))
}

func TestCompat_RemotePricingSource_RejectedWithClearError(t *testing.T) {
	data := []byte(`
apiVersion: promptkit.altairalabs.ai/v1alpha1
kind: Provider
metadata:
  name: x
spec:
  impl: openai
  capabilities:
    - type: inference
      model: m
      pricing:
        source: remote
`)
	_, err := config.LoadProviderSpec(data)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "remote")
	assert.Contains(t, err.Error(), "not yet supported")
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

func TestCompat_UnknownCapabilityType_ReturnsError(t *testing.T) {
	data := []byte(`
apiVersion: promptkit.altairalabs.ai/v1alpha1
kind: Provider
metadata:
  name: x
spec:
  impl: openai
  capabilities:
    - type: telepathy
      model: m
`)
	_, err := config.LoadProviderSpec(data)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "telepathy")
}

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
