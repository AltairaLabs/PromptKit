package gemini

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/AltairaLabs/PromptKit/runtime/v2/providers"
	"github.com/AltairaLabs/PromptKit/runtime/v2/types"
)

// With no limit from the request or the defaults, maxOutputTokens is left out
// so the model's own maximum applies.
func TestGeminiRequest_NoMaxTokens(t *testing.T) {
	for _, defaults := range []int{0, providers.MaxTokensUnlimited} {
		p := NewToolProvider("test", "gemini-2.5-flash", "http://localhost",
			providers.ProviderDefaults{MaxTokens: defaults}, false)
		req := providers.PredictionRequest{Messages: []types.Message{{Role: "user", Content: "hi"}}}

		temperature, topP, maxTokens := p.applyRequestDefaults(req)
		body, err := json.Marshal(p.buildGeminiRequest(nil, nil, temperature, topP, maxTokens))
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(string(body), "maxOutputTokens") {
			t.Errorf("defaults=%d: plain request sent maxOutputTokens: %s", defaults, body)
		}

		genConfig, _ := p.buildToolRequest(context.Background(), req, nil, "")["generationConfig"].(map[string]any)
		if _, ok := genConfig["maxOutputTokens"]; ok {
			t.Errorf("defaults=%d: tool request sent maxOutputTokens: %v", defaults, genConfig)
		}
	}
}

func TestGeminiRequest_MaxTokensFromDefaults(t *testing.T) {
	p := NewToolProvider("test", "gemini-2.5-flash", "http://localhost",
		providers.ProviderDefaults{MaxTokens: 400}, false)
	req := providers.PredictionRequest{Messages: []types.Message{{Role: "user", Content: "hi"}}}
	genConfig, _ := p.buildToolRequest(context.Background(), req, nil, "")["generationConfig"].(map[string]any)
	if genConfig["maxOutputTokens"] != 400 {
		t.Errorf("maxOutputTokens = %v, want 400", genConfig["maxOutputTokens"])
	}
}
