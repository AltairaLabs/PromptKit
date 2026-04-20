package openai

import (
	"context"
	"net/http"
	"strings"
	"testing"

	"github.com/AltairaLabs/PromptKit/runtime/providers"
)

// mockSigningCredential records whether Apply was invoked. Used to verify
// the Bedrock path attaches the credential (which in production performs
// SigV4 signing on the request).
type mockSigningCredential struct{ applied bool }

func (m *mockSigningCredential) Type() string { return "aws" }
func (m *mockSigningCredential) Apply(_ context.Context, _ *http.Request) error {
	m.applied = true
	return nil
}

func TestProvider_IsBedrock(t *testing.T) {
	tests := []struct {
		platform string
		want     bool
	}{
		{bedrockPlatform, true},
		{"azure", false},
		{"vertex", false},
		{"", false},
	}
	for _, tt := range tests {
		t.Run(tt.platform, func(t *testing.T) {
			p := &Provider{platform: tt.platform}
			if got := p.isBedrock(); got != tt.want {
				t.Errorf("isBedrock platform=%q got %v want %v", tt.platform, got, tt.want)
			}
		})
	}
}

func TestProvider_ChatCompletionsURL_Bedrock(t *testing.T) {
	p := &Provider{
		platform: bedrockPlatform,
		baseURL:  "https://bedrock-runtime.us-west-2.amazonaws.com",
		model:    "openai.gpt-oss-20b-1:0",
	}
	got := p.chatCompletionsURL()
	want := "https://bedrock-runtime.us-west-2.amazonaws.com/model/openai.gpt-oss-20b-1:0/invoke"
	if got != want {
		t.Errorf("Bedrock chatCompletionsURL = %q, want %q", got, want)
	}
	// Sanity: must not contain the standard /chat/completions or ?api-version=
	if strings.Contains(got, "/chat/completions") {
		t.Errorf("Bedrock URL must not include /chat/completions: %s", got)
	}
	if strings.Contains(got, "?api-version=") {
		t.Errorf("Bedrock URL must not include Azure ?api-version=: %s", got)
	}
}

func TestProvider_ResponsesURL_BedrockFallsBackToCompletions(t *testing.T) {
	p := &Provider{
		platform: bedrockPlatform,
		baseURL:  "https://bedrock-runtime.us-west-2.amazonaws.com",
		model:    "openai.gpt-oss-20b-1:0",
	}
	if got, want := p.responsesURL(), p.chatCompletionsURL(); got != want {
		t.Errorf("Bedrock responsesURL must equal chatCompletionsURL, got %q want %q", got, want)
	}
}

func TestNewProviderFromConfig_Bedrock(t *testing.T) {
	cred := &mockSigningCredential{}

	t.Run("derives baseURL from PlatformConfig.Region when empty", func(t *testing.T) {
		p := NewProviderFromConfig(&ProviderConfig{
			ID:       "test",
			Model:    "openai.gpt-oss-20b-1:0",
			Platform: bedrockPlatform,
			PlatformConfig: &providers.PlatformConfig{
				Type:   bedrockPlatform,
				Region: "us-west-2",
			},
			Credential: cred,
		})
		want := "https://bedrock-runtime.us-west-2.amazonaws.com"
		if p.baseURL != want {
			t.Errorf("baseURL = %q, want %q", p.baseURL, want)
		}
	})

	t.Run("explicit BaseURL is preserved", func(t *testing.T) {
		custom := "https://custom.bedrock.example"
		p := NewProviderFromConfig(&ProviderConfig{
			ID:       "test",
			Model:    "openai.gpt-oss-20b-1:0",
			BaseURL:  custom,
			Platform: bedrockPlatform,
			PlatformConfig: &providers.PlatformConfig{
				Type:   bedrockPlatform,
				Region: "us-west-2",
			},
			Credential: cred,
		})
		if p.baseURL != custom {
			t.Errorf("explicit baseURL must win, got %q", p.baseURL)
		}
	})

	t.Run("forces Chat Completions API mode (no Responses API on Bedrock)", func(t *testing.T) {
		p := NewProviderFromConfig(&ProviderConfig{
			ID:       "test",
			Model:    "openai.gpt-oss-20b-1:0",
			Platform: bedrockPlatform,
			PlatformConfig: &providers.PlatformConfig{
				Type: bedrockPlatform, Region: "us-west-2",
			},
			Credential: cred,
		})
		if p.apiMode != APIModeCompletions {
			t.Errorf("Bedrock provider must use APIModeCompletions, got %v", p.apiMode)
		}
	})

	t.Run("auto-adds max_tokens and top_p to unsupportedParams", func(t *testing.T) {
		// max_tokens — Bedrock OpenAI uses max_completion_tokens.
		// top_p    — gpt-oss rejects 0.0 and the framework default is 0;
		//            skipping leaves the model default in place.
		p := NewProviderFromConfig(&ProviderConfig{
			ID:       "test",
			Model:    "openai.gpt-oss-20b-1:0",
			Platform: bedrockPlatform,
			PlatformConfig: &providers.PlatformConfig{
				Type: bedrockPlatform, Region: "us-west-2",
			},
			Credential: cred,
		})
		for _, param := range []string{"max_tokens", "top_p"} {
			if !hasUnsupportedParam(p.unsupportedParams, param) {
				t.Errorf("Bedrock provider must mark %s unsupported, got %v",
					param, p.unsupportedParams)
			}
		}
	})

	t.Run("explicit UnsupportedParams overrides Bedrock auto-detection", func(t *testing.T) {
		p := NewProviderFromConfig(&ProviderConfig{
			ID:                "test",
			Model:             "openai.gpt-oss-20b-1:0",
			Platform:          bedrockPlatform,
			PlatformConfig:    &providers.PlatformConfig{Type: bedrockPlatform, Region: "us-west-2"},
			Credential:        cred,
			UnsupportedParams: []string{"top_p"},
		})
		if hasUnsupportedParam(p.unsupportedParams, "max_tokens") {
			t.Errorf("explicit UnsupportedParams must not be merged, got %v", p.unsupportedParams)
		}
		if !hasUnsupportedParam(p.unsupportedParams, "top_p") {
			t.Errorf("explicit top_p must be preserved, got %v", p.unsupportedParams)
		}
	})

	t.Run("non-Bedrock platforms unchanged", func(t *testing.T) {
		p := NewProviderFromConfig(&ProviderConfig{
			ID:    "test",
			Model: "gpt-4o",
		})
		if hasUnsupportedParam(p.unsupportedParams, "max_tokens") {
			t.Errorf("non-Bedrock provider should not mark max_tokens unsupported, got %v", p.unsupportedParams)
		}
	})
}
