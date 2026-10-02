package vllm

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/AltairaLabs/PromptKit/runtime/v2/providers"
)

// With no limit from the request or the defaults, max_tokens is left out so the
// model's own maximum applies.
func TestVLLMRequest_NoMaxTokens(t *testing.T) {
	for _, defaults := range []int{0, providers.MaxTokensUnlimited} {
		p := NewProvider("test", "model", "http://localhost",
			providers.ProviderDefaults{MaxTokens: defaults}, false, nil)
		req := providers.PredictionRequest{}
		temperature, topP, maxTokens := p.applyRequestDefaults(&req)

		body, err := json.Marshal(p.buildRequest(&req, nil, temperature, topP, maxTokens, false))
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(string(body), "max_tokens") {
			t.Errorf("defaults=%d: plain request sent max_tokens: %s", defaults, body)
		}

		params := toolRequestParams{temperature: temperature, topP: topP, maxTokens: maxTokens}
		if _, ok := p.buildToolRequest(&req, nil, params)["max_tokens"]; ok {
			t.Errorf("defaults=%d: tool request sent max_tokens", defaults)
		}
	}
}

func TestVLLMRequest_MaxTokensFromDefaults(t *testing.T) {
	p := NewProvider("test", "model", "http://localhost", providers.ProviderDefaults{MaxTokens: 400}, false, nil)
	req := providers.PredictionRequest{}
	_, _, maxTokens := p.applyRequestDefaults(&req)
	params := toolRequestParams{maxTokens: maxTokens}
	if got := p.buildToolRequest(&req, nil, params)["max_tokens"]; got != 400 {
		t.Errorf("max_tokens = %v, want 400", got)
	}
}
