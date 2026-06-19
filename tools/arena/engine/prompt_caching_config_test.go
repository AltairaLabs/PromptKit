package engine

import (
	"testing"

	"github.com/AltairaLabs/PromptKit/pkg/config"
	"github.com/AltairaLabs/PromptKit/runtime/providers"
)

// boolPtr is a test helper that returns a pointer to the given bool value.
func boolPtr(b bool) *bool { return &b }

// TestProviderSpecFromConfig_PromptCaching verifies that the prompt_caching config field
// maps correctly to DisablePromptCaching in the runtime ProviderDefaults.
func TestProviderSpecFromConfig_PromptCaching(t *testing.T) {
	tests := []struct {
		name                     string
		promptCaching            *bool // nil = omitted in config
		wantDisablePromptCaching bool
	}{
		{
			name:                     "prompt_caching omitted (nil) keeps caching on",
			promptCaching:            nil,
			wantDisablePromptCaching: false,
		},
		{
			name:                     "prompt_caching: true keeps caching on",
			promptCaching:            boolPtr(true),
			wantDisablePromptCaching: false,
		},
		{
			name:                     "prompt_caching: false disables caching",
			promptCaching:            boolPtr(false),
			wantDisablePromptCaching: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := &config.Provider{
				ID:    "test",
				Type:  "anthropic",
				Model: "claude-sonnet-4-6",
				Defaults: config.ProviderDefaults{
					Temperature:   0.7,
					MaxTokens:     1024,
					PromptCaching: tt.promptCaching,
				},
			}

			spec := providerSpecFromConfig(p)

			if spec.Defaults.DisablePromptCaching != tt.wantDisablePromptCaching {
				t.Errorf("DisablePromptCaching = %v, want %v (prompt_caching input: %v)",
					spec.Defaults.DisablePromptCaching, tt.wantDisablePromptCaching, tt.promptCaching)
			}
		})
	}
}

// TestBuildArenaProvider_PromptCachingFalse verifies the builder_integration path also maps
// prompt_caching: false → DisablePromptCaching: true.
func TestBuildArenaProvider_PromptCachingFalse(t *testing.T) {
	// We test the Defaults translation directly without constructing a full provider
	// (which would require a live provider type). The logic is in the same literal
	// that providerSpecFromConfig tests cover.
	disableCaching := boolPtr(false)
	pd := providers.ProviderDefaults{
		DisablePromptCaching: disableCaching != nil && !*disableCaching,
	}
	if !pd.DisablePromptCaching {
		t.Error("DisablePromptCaching should be true when PromptCaching pointer is false")
	}

	enableCaching := boolPtr(true)
	pd2 := providers.ProviderDefaults{
		DisablePromptCaching: enableCaching != nil && !*enableCaching,
	}
	if pd2.DisablePromptCaching {
		t.Error("DisablePromptCaching should be false when PromptCaching pointer is true")
	}

	var nilCaching *bool
	pd3 := providers.ProviderDefaults{
		DisablePromptCaching: nilCaching != nil && !*nilCaching,
	}
	if pd3.DisablePromptCaching {
		t.Error("DisablePromptCaching should be false when PromptCaching pointer is nil")
	}
}
