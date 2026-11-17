//go:build integration

package claude

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

// TestPredict_PlainPathToolLinkage_Live proves the Messages API accepts a
// request without tools whose history holds a tool_use and its tool_result,
// and answers from the result.
func TestPredict_PlainPathToolLinkage_Live(t *testing.T) {
	if os.Getenv("ANTHROPIC_API_KEY") == "" && os.Getenv("CLAUDE_API_KEY") == "" {
		t.Skip("ANTHROPIC_API_KEY not set")
	}
	p := NewProvider("live", "claude-haiku-4-5", "https://api.anthropic.com/v1",
		providers.ProviderDefaults{MaxTokens: 100}, false)
	hist := linkageHistory()
	hist[2].ToolResult.Parts = []types.ContentPart{types.NewTextPart("21.7C and overcast.")}
	hist[3].Content = "What temperature did the tool report?"
	resp, err := p.Predict(context.Background(), providers.PredictionRequest{Messages: hist})
	require.NoError(t, err)
	assert.True(t, strings.Contains(resp.Content, "21") || strings.Contains(resp.Content, "22"),
		"answer does not reflect the tool result: %q", resp.Content)
}
