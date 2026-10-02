package claude

import (
	"strings"
	"testing"

	"github.com/AltairaLabs/PromptKit/runtime/v2/providers"
)

// The Messages API requires max_tokens, so Claude never resolves to "no limit":
// with nothing set it falls back to defaultMaxTokens.
func TestApplyDefaults_MaxTokensFallback(t *testing.T) {
	tests := []struct {
		name      string
		defaults  int
		requested int
		want      int
	}{
		{"nothing set falls back", 0, 0, defaultMaxTokens},
		{"unlimited via direct constructor falls back", providers.MaxTokensUnlimited, 0, defaultMaxTokens},
		{"provider default applies", 400, 0, 400},
		{"request wins", 400, 123, 123},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := NewProvider("test", "claude-sonnet-4-5", "http://localhost",
				providers.ProviderDefaults{MaxTokens: tt.defaults}, false)
			req := p.buildBaseRequest(providers.PredictionRequest{MaxTokens: tt.requested}, nil)
			if req.MaxTokens != tt.want {
				t.Errorf("max_tokens = %d, want %d", req.MaxTokens, tt.want)
			}
		})
	}
}

func TestClaudeFactory_RejectsUnlimitedMaxTokens(t *testing.T) {
	_, err := providers.CreateProviderFromSpec(providers.ProviderSpec{
		ID: "c", Type: "claude", Model: "claude-sonnet-4-5", BaseURL: "http://localhost",
		Defaults: providers.ProviderDefaults{MaxTokens: providers.MaxTokensUnlimited},
	})
	if err == nil || !strings.Contains(err.Error(), "max_tokens") {
		t.Fatalf("expected a max_tokens error, got %v", err)
	}

	p, err := providers.CreateProviderFromSpec(providers.ProviderSpec{
		ID: "c", Type: "claude", Model: "claude-sonnet-4-5", BaseURL: "http://localhost",
		Defaults: providers.ProviderDefaults{MaxTokens: 400},
	})
	if err != nil || p == nil {
		t.Fatalf("a positive limit must be accepted, got %v", err)
	}
}
