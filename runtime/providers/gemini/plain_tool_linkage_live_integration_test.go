//go:build integration

package gemini

import (
	"context"
	"os"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/AltairaLabs/PromptKit/runtime/v2/providers"
	"github.com/AltairaLabs/PromptKit/runtime/v2/types"
)

// TestPredict_PlainPathToolLinkage_Live proves generateContent accepts a
// request without tools whose history holds a functionCall and its
// functionResponse, and answers from the response.
func TestPredict_PlainPathToolLinkage_Live(t *testing.T) {
	if os.Getenv("GEMINI_API_KEY") == "" {
		t.Skip("GEMINI_API_KEY not set")
	}
	p := NewProvider("live", "gemini-2.5-flash", "https://generativelanguage.googleapis.com/v1beta",
		providers.ProviderDefaults{MaxTokens: 1024}, false)
	hist := linkageHistory()
	hist[2].ToolResult.Parts = []types.ContentPart{types.NewTextPart("21.7C and overcast.")}
	hist[3].Content = "What temperature did the tool report?"
	resp, err := p.Predict(context.Background(), providers.PredictionRequest{Messages: hist})
	require.NoError(t, err)
	assert.True(t, strings.Contains(resp.Content, "21") || strings.Contains(resp.Content, "22"),
		"answer does not reflect the tool result: %q", resp.Content)
}
