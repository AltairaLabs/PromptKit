package openai

import (
	"testing"

	"github.com/AltairaLabs/PromptKit/runtime/providers"
	"github.com/AltairaLabs/PromptKit/runtime/types"
)

// TestBuildResponsesRequest_ReasoningEffort verifies that reasoning_effort
// configured via additional_config is sent as reasoning.effort in Responses
// API requests. Regression: gpt-5-pro defaults to reasoning.effort=high on
// the server side, which on simple prompts burns 20+ seconds of silent
// reasoning — configurable effort lets callers opt out.
func TestBuildResponsesRequest_ReasoningEffort(t *testing.T) {
	tests := []struct {
		name   string
		config map[string]any
		want   string // "" means the "reasoning" key should be absent
	}{
		{
			name:   "absent when not configured",
			config: nil,
			want:   "",
		},
		{
			name:   "absent when empty",
			config: map[string]any{"reasoning_effort": ""},
			want:   "",
		},
		{
			name:   "absent on unknown value",
			config: map[string]any{"reasoning_effort": "aggressive"},
			want:   "",
		},
		{
			name:   "minimal",
			config: map[string]any{"reasoning_effort": "minimal"},
			want:   "minimal",
		},
		{
			name:   "low",
			config: map[string]any{"reasoning_effort": "low"},
			want:   "low",
		},
		{
			name:   "medium",
			config: map[string]any{"reasoning_effort": "medium"},
			want:   "medium",
		},
		{
			name:   "high",
			config: map[string]any{"reasoning_effort": "high"},
			want:   "high",
		},
		{
			name:   "normalizes case",
			config: map[string]any{"reasoning_effort": "MINIMAL"},
			want:   "minimal",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			provider := NewProviderWithConfig(
				"test", "gpt-5-pro", "https://api.openai.com/v1",
				providers.ProviderDefaults{}, false, tt.config,
			)
			req := providers.PredictionRequest{
				Messages: []types.Message{{Role: "user", Content: "hi"}},
			}

			result := provider.buildResponsesRequest(req, nil, "")
			reasoning, present := result["reasoning"]

			if tt.want == "" {
				if present {
					t.Errorf("expected no reasoning field, got %v", reasoning)
				}
				return
			}
			if !present {
				t.Fatalf("expected reasoning.effort=%q, but reasoning field missing", tt.want)
			}
			m, ok := reasoning.(map[string]any)
			if !ok {
				t.Fatalf("reasoning field is %T, want map[string]any", reasoning)
			}
			if got := m["effort"]; got != tt.want {
				t.Errorf("reasoning.effort = %v, want %q", got, tt.want)
			}
		})
	}
}

// TestGetReasoningEffort_DirectParsing covers getReasoningEffort's behavior
// independent of the request builder.
func TestGetReasoningEffort_DirectParsing(t *testing.T) {
	tests := []struct {
		name string
		cfg  map[string]any
		want string
	}{
		{"nil config", nil, ""},
		{"missing key", map[string]any{"other": "x"}, ""},
		{"wrong type", map[string]any{"reasoning_effort": 42}, ""},
		{"empty string", map[string]any{"reasoning_effort": ""}, ""},
		{"unknown value", map[string]any{"reasoning_effort": "garbage"}, ""},
		{"minimal", map[string]any{"reasoning_effort": "minimal"}, "minimal"},
		{"low", map[string]any{"reasoning_effort": "low"}, "low"},
		{"medium", map[string]any{"reasoning_effort": "medium"}, "medium"},
		{"high", map[string]any{"reasoning_effort": "high"}, "high"},
		{"uppercase", map[string]any{"reasoning_effort": "HIGH"}, "high"},
		{"mixed case", map[string]any{"reasoning_effort": "Low"}, "low"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := getReasoningEffort(tt.cfg)
			if got != tt.want {
				t.Errorf("getReasoningEffort(%v) = %q, want %q", tt.cfg, got, tt.want)
			}
		})
	}
}
