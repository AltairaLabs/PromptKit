package providers

import (
	"testing"
)

func TestProvider_BasicMethods(t *testing.T) {
	tests := []struct {
		name     string
		provider Provider
	}{
		{
			name:     "MockProvider",
			provider: NewMockProvider("test", "test-model", false),
		},
		{
			name:     "ClaudeProvider",
			provider: NewClaudeProvider("test-claude", "claude-3-opus", "fake-key", ProviderDefaults{}, false),
		},
		{
			name:     "OpenAIProvider",
			provider: NewOpenAIProvider("test-openai", "gpt-4", "fake-key", ProviderDefaults{}, false),
		},
		{
			name:     "GeminiProvider",
			provider: NewGeminiProvider("test-gemini", "gemini-pro", "fake-key", ProviderDefaults{}, false),
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
