package openai

import (
	"testing"

	"github.com/AltairaLabs/PromptKit/runtime/v2/providers"
)

// A resolved limit of zero means no limit: neither token field is sent, so the
// model's own maximum applies.
func TestAddMaxTokensToRequest_NoLimit(t *testing.T) {
	for _, unsupported := range [][]string{nil, {"max_completion_tokens"}} {
		req := map[string]interface{}{}
		addMaxTokensToRequest(req, unsupported, 0)
		if len(req) != 0 {
			t.Errorf("unsupported=%v: expected no token field, got %v", unsupported, req)
		}
	}
}

func TestApplyRequestDefaults_UnlimitedMaxTokens(t *testing.T) {
	p := NewProvider("test", "gpt-4o", "http://localhost",
		providers.ProviderDefaults{MaxTokens: providers.MaxTokensUnlimited}, false)
	if _, _, maxTokens := p.applyRequestDefaults(providers.PredictionRequest{}); maxTokens != 0 {
		t.Errorf("maxTokens = %d, want 0 (no limit)", maxTokens)
	}
	if _, _, maxTokens := p.applyRequestDefaults(providers.PredictionRequest{MaxTokens: 77}); maxTokens != 77 {
		t.Errorf("maxTokens = %d, want the request's 77", maxTokens)
	}
}
