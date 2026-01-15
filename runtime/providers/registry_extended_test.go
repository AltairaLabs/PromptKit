package providers

import (
	"context"
	"testing"

	"github.com/AltairaLabs/PromptKit/runtime/types"
)

const testModelName = "test-model"

// mockProviderForTest is a minimal provider implementation for registry testing
type mockProviderForTest struct {
	id string
}

func (m *mockProviderForTest) ID() string                   { return m.id }
func (m *mockProviderForTest) Model() string                { return testModelName }
func (m *mockProviderForTest) Close() error                 { return nil }
func (m *mockProviderForTest) ShouldIncludeRawOutput() bool { return false }
func (m *mockProviderForTest) SupportsStreaming() bool      { return false }
func (m *mockProviderForTest) CalculateCost(_, _, _ int) types.CostInfo {
	return types.CostInfo{}
}
func (m *mockProviderForTest) Predict(_ context.Context, _ PredictionRequest) (PredictionResponse, error) {
	return PredictionResponse{}, nil
}
func (m *mockProviderForTest) PredictStream(_ context.Context, _ PredictionRequest) (<-chan StreamChunk, error) {
	return nil, nil
}

// Register test factories in init to test default base URL assignment
func init() {
	// Register a test factory to help with coverage
	RegisterProviderFactory("test-provider", func(spec ProviderSpec) (Provider, error) {
		return &mockProviderForTest{id: spec.ID}, nil
	})
}

func TestCreateProviderFromSpecDefaultBaseURLs(t *testing.T) {
	tests := []struct {
		name            string
		providerType    string
		expectedBaseURL string
	}{
		{"openai default", "openai", "https://api.openai.com/v1"},
		{"gemini default", "gemini", "https://generativelanguage.googleapis.com/v1beta"},
		{"claude default", "claude", "https://api.anthropic.com"},
		{"imagen default", "imagen", "https://generativelanguage.googleapis.com/v1beta"},
		{"ollama default", "ollama", "http://localhost:11434"},
		{"mock default", "mock", ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Register a temporary factory for this type if not already registered
			originalFactory := providerFactories[tt.providerType]
			capturedBaseURL := ""
			providerFactories[tt.providerType] = func(spec ProviderSpec) (Provider, error) {
				capturedBaseURL = spec.BaseURL
				return &mockProviderForTest{id: spec.ID}, nil
			}
			defer func() {
				if originalFactory != nil {
					providerFactories[tt.providerType] = originalFactory
				} else {
					delete(providerFactories, tt.providerType)
				}
			}()

			spec := ProviderSpec{
				ID:    "test-" + tt.providerType,
				Type:  tt.providerType,
				Model: testModelName,
				// No BaseURL - should use default
			}

			_, err := CreateProviderFromSpec(spec)
			if err != nil {
				t.Fatalf("Expected no error, got %v", err)
			}

			if capturedBaseURL != tt.expectedBaseURL {
				t.Errorf("Expected baseURL %q, got %q", tt.expectedBaseURL, capturedBaseURL)
			}
		})
	}
}

func TestCreateProviderFromSpecCustomBaseURL(t *testing.T) {
	customURL := "https://custom.api.example.com"
	spec := ProviderSpec{
		ID:      "test",
		Type:    "test-provider",
		Model:   testModelName,
		BaseURL: customURL,
	}

	provider, err := CreateProviderFromSpec(spec)
	if err != nil {
		t.Fatalf("Expected no error, got %v", err)
	}
	if provider == nil {
		t.Fatal("Expected provider but got nil")
	}
}

func TestCreateProviderFromSpecUnsupported(t *testing.T) {
	spec := ProviderSpec{
		ID:    "test",
		Type:  "unsupported-type",
		Model: testModelName,
	}

	provider, err := CreateProviderFromSpec(spec)
	if err == nil {
		t.Error("Expected error but got none")
	}
	if provider != nil {
		t.Errorf("Expected nil provider but got %v", provider)
	}
}

func TestCreateProviderFromSpecEmptyType(t *testing.T) {
	spec := ProviderSpec{
		ID:    "test",
		Type:  "",
		Model: testModelName,
	}

	provider, err := CreateProviderFromSpec(spec)
	if err == nil {
		t.Error("Expected error but got none")
	}
	if provider != nil {
		t.Errorf("Expected nil provider but got %v", provider)
	}
}
