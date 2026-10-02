package ollama

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/AltairaLabs/PromptKit/runtime/v2/providers"
	"github.com/AltairaLabs/PromptKit/runtime/v2/types"
)

// With no limit from the request or the defaults, max_tokens is left out so the
// model's own maximum applies.
func TestOllamaRequest_NoMaxTokens(t *testing.T) {
	for _, defaults := range []int{0, providers.MaxTokensUnlimited} {
		p := NewToolProvider("test", "llama3", "http://localhost",
			providers.ProviderDefaults{MaxTokens: defaults}, false, nil)
		req := providers.PredictionRequest{Messages: []types.Message{{Role: "user", Content: "hi"}}}

		if _, ok := p.buildToolRequest(context.Background(), req, nil, "")["max_tokens"]; ok {
			t.Errorf("defaults=%d: tool request sent max_tokens", defaults)
		}

		_, _, maxTokens := p.applyRequestDefaults(req)
		body, err := json.Marshal(ollamaRequest{MaxTokens: maxTokens})
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(string(body), "max_tokens") {
			t.Errorf("defaults=%d: plain request sent max_tokens: %s", defaults, body)
		}
	}
}

func TestOllamaRequest_MaxTokensFromDefaults(t *testing.T) {
	p := NewToolProvider("test", "llama3", "http://localhost",
		providers.ProviderDefaults{MaxTokens: 400}, false, nil)
	req := providers.PredictionRequest{Messages: []types.Message{{Role: "user", Content: "hi"}}}
	if got := p.buildToolRequest(context.Background(), req, nil, "")["max_tokens"]; got != 400 {
		t.Errorf("max_tokens = %v, want 400", got)
	}
}
