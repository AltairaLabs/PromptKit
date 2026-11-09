// Package conformance verifies that every inference provider implementation
// satisfies the base.Provider interface via the embedded base.Implementation
// in providers.BaseProvider.
package conformance_test

import (
	"context"
	"testing"

	"github.com/AltairaLabs/PromptKit/runtime/v2/providers"
	"github.com/AltairaLabs/PromptKit/runtime/v2/providers/base"
	"github.com/AltairaLabs/PromptKit/runtime/v2/providers/claude"
	"github.com/AltairaLabs/PromptKit/runtime/v2/providers/gemini"
	"github.com/AltairaLabs/PromptKit/runtime/v2/providers/mock"
	"github.com/AltairaLabs/PromptKit/runtime/v2/providers/ollama"
	"github.com/AltairaLabs/PromptKit/runtime/v2/providers/openai"
	"github.com/AltairaLabs/PromptKit/runtime/v2/providers/vllm"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestInferenceProviders_SatisfyBaseProvider asserts every inference impl
// satisfies base.Provider via the embedded base.Implementation.
//
// Excluded from this test:
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
		{"mock", mustMock(t)},
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

// providers.Provider and providers.InferenceProvider are the same type.
var _ providers.InferenceProvider = providers.Provider(nil)

// TestAssertInferenceProvider_BaseImplIsNotInference verifies that a bare
// *base.Implementation (which satisfies base.Provider but not InferenceProvider)
// returns an error from AssertInferenceProvider.
func TestAssertInferenceProvider_BaseImplIsNotInference(t *testing.T) {
	var bp base.Provider = base.NewImplementation("p", base.ProviderTypeInference, nil)
	_, err := providers.AssertInferenceProvider(bp)
	require.Error(t, err)
}

// TestAssertInferenceProvider_ImageTypeIsNotInference verifies that an image-type
// base.Implementation is rejected by AssertInferenceProvider.
func TestAssertInferenceProvider_ImageTypeIsNotInference(t *testing.T) {
	var bp base.Provider = base.NewImplementation("p", base.ProviderTypeImage, nil)
	_, err := providers.AssertInferenceProvider(bp)
	require.Error(t, err)
}

// TestAssertInferenceProvider_RealImplSucceeds verifies that a real inference
// provider (openai) is correctly asserted as an InferenceProvider.
func TestAssertInferenceProvider_RealImplSucceeds(t *testing.T) {
	op := openai.NewProvider("openai-test", "gpt-4o-mini", "https://api.openai.com/v1", providers.ProviderDefaults{}, false)
	inf, err := providers.AssertInferenceProvider(op)
	require.NoError(t, err)
	assert.Same(t, op, inf.(*openai.Provider), "the assertion must return the provider it was given")
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

func mustMock(t *testing.T) base.Provider {
	t.Helper()
	return mock.NewProvider("mock-test", "mock-model", false)
}
