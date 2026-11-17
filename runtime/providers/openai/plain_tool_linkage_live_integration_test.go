//go:build integration

package openai

import (
	"context"
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/AltairaLabs/PromptKit/runtime/v2/providers"
	"github.com/AltairaLabs/PromptKit/runtime/v2/types"
)

// TestPredict_PlainPathToolLinkage_Live proves OpenAI accepts the plain
// request with a tool call and its linked result, and answers.
func TestPredict_PlainPathToolLinkage_Live(t *testing.T) {
	key := os.Getenv("OPENAI_API_KEY")
	if key == "" {
		t.Skip("OPENAI_API_KEY not set")
	}
	p := NewProvider("live", "gpt-4o-mini", "https://api.openai.com/v1",
		providers.ProviderDefaults{MaxTokens: 100}, false)
	p.apiKey = key
	hist := linkageHistory()
	hist[2].ToolResult.Parts = []types.ContentPart{types.NewTextPart("21.7C and overcast.")}
	resp, err := p.Predict(context.Background(), providers.PredictionRequest{Messages: hist})
	require.NoError(t, err)
	assert.NotEmpty(t, resp.Content)
}
