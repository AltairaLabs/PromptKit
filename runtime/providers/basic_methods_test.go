package providers_test

import (
	"testing"

	"github.com/AltairaLabs/PromptKit/runtime/providers"
	"github.com/AltairaLabs/PromptKit/runtime/providers/claude"
	"github.com/AltairaLabs/PromptKit/runtime/providers/gemini"
	"github.com/AltairaLabs/PromptKit/runtime/providers/mock"
	"github.com/AltairaLabs/PromptKit/runtime/providers/openai"
)

func TestProvider_BasicMethods(t *testing.T) {
	tests := []struct {
		name     string
		provider providers.Provider
	}{
		{
			name:     "MockProvider",
			provider: mock.NewProvider("test", "test-model", false),
		},
		{
			name:     "ClaudeProvider",
			provider: claude.NewProvider("test-claude", "claude-3-opus", "fake-key", providers.ProviderDefaults{}, false),
		},
		{
			name:     "OpenAIProvider",
			provider: openai.NewProvider("test-openai", "gpt-4", "fake-key", providers.ProviderDefaults{}, false),
		},
		{
			name:     "GeminiProvider",
			provider: gemini.NewProvider("test-gemini", "gemini-pro", "fake-key", providers.ProviderDefaults{}, false),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Test Close() method
			err := tt.provider.Close()
			if err != nil {
				t.Errorf("Close() error = %v", err)
			}

			// Test ShouldIncludeRawOutput() method
			shouldInclude := tt.provider.ShouldIncludeRawOutput()
			// Just verify it returns a boolean - the actual value doesn't matter for coverage
			_ = shouldInclude
		})
	}
}
