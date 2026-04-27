package claude

import (
	"testing"

	"github.com/AltairaLabs/PromptKit/runtime/providers"
)

// TestBedrockAnthropicEndpoint covers the Bedrock URL helper added for
// PromptKit#1029 — Bedrock-hosted Anthropic was previously routed to
// api.anthropic.com and 404ed.
func TestBedrockAnthropicEndpoint(t *testing.T) {
	tests := []struct {
		region string
		want   string
	}{
		{"us-east-1", "https://bedrock-runtime.us-east-1.amazonaws.com"},
		{"us-west-2", "https://bedrock-runtime.us-west-2.amazonaws.com"},
		{"eu-central-1", "https://bedrock-runtime.eu-central-1.amazonaws.com"},
		{"", ""}, // empty region → empty result so callers can fall back
	}
	for _, tt := range tests {
		t.Run(tt.region, func(t *testing.T) {
			got := bedrockAnthropicEndpoint(tt.region)
			if got != tt.want {
				t.Errorf("bedrockAnthropicEndpoint(%q) = %q, want %q",
					tt.region, got, tt.want)
			}
		})
	}
}

// TestNewProviderWithCredential_BedrockComputesBaseURL pins the regression
// for #1029 in the claude factory: when caller passes Platform=bedrock,
// non-empty Region, and an empty BaseURL, the factory must compute the
// Bedrock-Runtime URL rather than leave baseURL empty.
func TestNewProviderWithCredential_BedrockComputesBaseURL(t *testing.T) {
	p := NewProviderWithCredential(
		"id", "anthropic.claude-haiku-4-5-20251001-v1:0",
		"", // BaseURL intentionally empty — factory must compute from PlatformConfig
		providers.ProviderDefaults{},
		false, nil,
		bedrockPlatform,
		&providers.PlatformConfig{Type: "bedrock", Region: "us-west-2"},
	)
	if got, want := p.baseURL, "https://bedrock-runtime.us-west-2.amazonaws.com"; got != want {
		t.Errorf("p.baseURL = %q, want %q", got, want)
	}
}

// TestNewProviderWithCredential_BedrockExplicitBaseURLWins ensures an
// explicit BaseURL still takes precedence over the computed Bedrock URL,
// matching the documented "callers that pass an explicit baseURL always win"
// contract.
func TestNewProviderWithCredential_BedrockExplicitBaseURLWins(t *testing.T) {
	const explicit = "https://bedrock-runtime.eu-central-1.amazonaws.com"
	p := NewProviderWithCredential(
		"id", "anthropic.claude-haiku-4-5-20251001-v1:0",
		explicit,
		providers.ProviderDefaults{},
		false, nil,
		bedrockPlatform,
		&providers.PlatformConfig{Type: "bedrock", Region: "us-west-2"},
	)
	if p.baseURL != explicit {
		t.Errorf("p.baseURL = %q, want explicit %q", p.baseURL, explicit)
	}
}
