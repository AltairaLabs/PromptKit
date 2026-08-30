//go:build integration

package openai

import (
	"context"
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/AltairaLabs/PromptKit/runtime/providers"
	"github.com/AltairaLabs/PromptKit/runtime/types"
)

// TestOpenAI_ResponsesWithSeed_Live is the live guard for #1870.
//
// The Responses API rejects the parameter — verified directly against the API:
// sending {"model":"gpt-5","input":"say OK","seed":42} returns HTTP 400
// "Unknown parameter: 'seed'", while the identical request without it returns
// 200. So a caller who sets a Seed must still get a completed turn, which is
// only true if the builder drops it.
//
// Unit tests assert the key is absent from the request map; only this asserts
// that the resulting request is one the API actually accepts. Goes through
// CreateProviderFromSpec rather than the constructor, because config-reached
// behavior is what breaks.
func TestOpenAI_ResponsesWithSeed_Live(t *testing.T) {
	if os.Getenv("OPENAI_API_KEY") == "" {
		t.Skip("OPENAI_API_KEY not set")
	}
	model := os.Getenv("OPENAI_RESPONSES_MODEL")
	if model == "" {
		model = "gpt-5"
	}

	p, err := providers.CreateProviderFromSpec(providers.ProviderSpec{
		ID: "openai-seed-live", Type: "openai", Model: model,
		BaseURL:          "https://api.openai.com/v1",
		Defaults:         providers.ProviderDefaults{MaxTokens: 2048},
		AdditionalConfig: map[string]interface{}{"api_mode": "responses"},
	})
	require.NoError(t, err)
	defer func() { _ = p.Close() }()

	seed := 42
	resp, err := p.Predict(context.Background(), providers.PredictionRequest{
		Messages: []types.Message{{Role: "user", Content: "Reply with the single word OK"}},
		Seed:     &seed,
	})

	require.NoError(t, err,
		"a Seed on the Responses path must not 400 the call — the builder has to drop it")
	assert.NotEmpty(t, resp.Content, "the turn must actually complete")
}
