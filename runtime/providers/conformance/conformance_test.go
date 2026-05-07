// Package conformance verifies that every inference provider implementation
// satisfies the base.Provider interface via the embedded base.Implementation
// in providers.BaseProvider.
package conformance_test

import (
	"context"
	"testing"

	"github.com/AltairaLabs/PromptKit/runtime/providers"
	"github.com/AltairaLabs/PromptKit/runtime/providers/base"
	"github.com/AltairaLabs/PromptKit/runtime/providers/claude"
	"github.com/AltairaLabs/PromptKit/runtime/providers/gemini"
	"github.com/AltairaLabs/PromptKit/runtime/providers/ollama"
	"github.com/AltairaLabs/PromptKit/runtime/providers/openai"
	"github.com/AltairaLabs/PromptKit/runtime/providers/vllm"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestInferenceProviders_SatisfyBaseProvider asserts every inference impl
// satisfies base.Provider via the embedded base.Implementation in BaseProvider.
//
// Excluded from this test:
//   - mock: does not embed providers.BaseProvider (custom struct, migrated separately)
//   - replay: does not embed providers.BaseProvider (custom struct)
//   - imagen: being migrated to base.ImageProvider (Type=image) in Task 15
func TestInferenceProviders_SatisfyBaseProvider(t *testing.T) {
	impls := []struct {
		name string
		p    base.Provider
	}{
		{"openai", mustOpenAI(t)},
		{"claude", mustClaude(t)},
		{"gemini", mustGemini(t)},
		{"ollama", mustOllama(t)},
		{"vllm", mustVLLM(t)},
	}

	for _, tc := range impls {
		t.Run(tc.name, func(t *testing.T) {
			require.NotEmpty(t, tc.p.Name(), "Name() must return non-empty")
			assert.Equal(t, base.ProviderTypeInference, tc.p.Type())
			// Pricing is nil for now — Task 12 wires it from config.
			_ = tc.p.Pricing()
			assert.NoError(t, tc.p.Validate())
			assert.NoError(t, tc.p.Init(context.Background()))
			assert.NoError(t, tc.p.HealthCheck(context.Background()))
			assert.NoError(t, tc.p.Close())
		})
	}
}

func mustOpenAI(t *testing.T) base.Provider {
	t.Helper()
	return openai.NewProvider("openai-test", "gpt-4o-mini", "https://api.openai.com/v1", providers.ProviderDefaults{}, false)
}

func mustClaude(t *testing.T) base.Provider {
	t.Helper()
	return claude.NewProvider("claude-test", "claude-3-5-haiku", "https://api.anthropic.com", providers.ProviderDefaults{}, false)
}

func mustGemini(t *testing.T) base.Provider {
	t.Helper()
	return gemini.NewProvider("gemini-test", "gemini-2.0-flash", "https://generativelanguage.googleapis.com", providers.ProviderDefaults{}, false)
}

func mustOllama(t *testing.T) base.Provider {
	t.Helper()
	return ollama.NewProvider("ollama-test", "llama3", "http://localhost:11434", providers.ProviderDefaults{}, false, nil)
}

func mustVLLM(t *testing.T) base.Provider {
	t.Helper()
	return vllm.NewProvider("vllm-test", "test-model", "http://localhost:8000", providers.ProviderDefaults{}, false, nil)
}
