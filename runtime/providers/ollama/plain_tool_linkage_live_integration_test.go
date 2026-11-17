//go:build integration

package ollama

import (
	"context"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/AltairaLabs/PromptKit/runtime/v2/providers"
	"github.com/AltairaLabs/PromptKit/runtime/v2/types"
)

// TestPredict_PlainPathToolLinkage_Live proves a local Ollama accepts a
// request without tools whose history holds a tool call and its linked
// result, and answers from the result. OLLAMA_MODEL picks the model.
func TestPredict_PlainPathToolLinkage_Live(t *testing.T) {
	base := os.Getenv("OLLAMA_BASE_URL")
	if base == "" {
		base = "http://localhost:11434"
	}
	client := &http.Client{Timeout: 2 * time.Second}
	resp, err := client.Get(base + "/api/tags")
	if err != nil {
		t.Skipf("no Ollama server at %s: %v", base, err)
	}
	_ = resp.Body.Close()
	model := os.Getenv("OLLAMA_MODEL")
	if model == "" {
		model = "llama3.2"
	}
	p := NewProvider("live", model, base, providers.ProviderDefaults{MaxTokens: 100}, false, nil)
	hist := linkageHistory()
	hist[2].ToolResult.Parts = []types.ContentPart{types.NewTextPart("21.7C and overcast.")}
	hist[3].Content = "What temperature did the tool report?"
	out, err := p.Predict(context.Background(), providers.PredictionRequest{Messages: hist})
	require.NoError(t, err)
	assert.True(t, strings.Contains(out.Content, "21") || strings.Contains(out.Content, "22"),
		"answer does not reflect the tool result: %q", out.Content)
}
