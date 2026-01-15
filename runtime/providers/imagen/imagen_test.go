package imagen

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/AltairaLabs/PromptKit/runtime/providers"
	"github.com/AltairaLabs/PromptKit/runtime/types"
)

// newTestProvider creates a test provider with default values
func newTestProvider(id string) *Provider {
	return NewProvider(Config{
		ID:               id,
		Model:            "imagen-4.0-generate-001",
		BaseURL:          "https://aiplatform.googleapis.com/v1",
		ApiKey:           "test-key",
		ProjectID:        "test-project",
		Location:         "us-central1",
		IncludeRawOutput: false,
		Defaults:         providers.ProviderDefaults{},
	})
}

// TestProviderIDNotHardcoded is a regression test for the bug where provider ID
// was hardcoded to "imagen" instead of using the ID from the config YAML.
// This test ensures the provider respects the metadata.name field from the YAML config.
//
// Bug context: Originally, NewProvider created an empty BaseProvider{}
// and had ID() method returning hardcoded "imagen". This caused providers
// configured as "imagen-provider" in YAML to be registered as "imagen",
// leading to "provider not found: imagen-provider" errors.
//
// Fix: Updated NewProvider to use providers.NewBaseProvider(id, ...)
// and removed the hardcoded ID() method, allowing BaseProvider.ID() to
// return the correct value from spec.ID.
func TestProviderIDNotHardcoded(t *testing.T) {
	// This is the actual metadata.name from imagen-provider.yaml
	configuredID := "imagen-provider"

	provider := newTestProvider(configuredID)

	// Before the fix, this would fail because ID() returned hardcoded "imagen"
	if provider.ID() != configuredID {
		t.Errorf("Provider ID bug detected! Expected %q from YAML config, but got hardcoded %q",
			configuredID, provider.ID())
	}
}

// TestProviderID ensures the provider uses the ID from spec, not a hardcoded value
func TestProviderID(t *testing.T) {
	tests := []struct {
		name       string
		providerID string
	}{
		{
			name:       "custom provider ID from config",
			providerID: "imagen-provider",
		},
		{
			name:       "different custom ID",
			providerID: "my-custom-imagen",
		},
		{
			name:       "simple ID",
			providerID: "imagen",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			provider := newTestProvider(tt.providerID)

			if provider.ID() != tt.providerID {
				t.Errorf("Expected provider ID %q, got %q", tt.providerID, provider.ID())
			}
		})
	}
}

// TestProviderIDFromFactory ensures the factory passes spec.ID correctly
func TestProviderIDFromFactory(t *testing.T) {
	tests := []struct {
		name       string
		spec       providers.ProviderSpec
		wantID     string
		wantErr    bool
		skipReason string
	}{
		{
			name: "uses spec.ID not hardcoded value",
			spec: providers.ProviderSpec{
				ID:      "imagen-provider",
				Type:    "imagen",
				Model:   "imagen-4.0-generate-001",
				BaseURL: "https://aiplatform.googleapis.com/v1",
				AdditionalConfig: map[string]interface{}{
					"project_id": "test-project-123",
					"location":   "us-central1",
				},
			},
			wantID:  "imagen-provider",
			wantErr: false,
		},
		{
			name: "different custom ID",
			spec: providers.ProviderSpec{
				ID:      "my-imagen-gen",
				Type:    "imagen",
				Model:   "imagen-4.0-generate-001",
				BaseURL: "https://aiplatform.googleapis.com/v1",
				AdditionalConfig: map[string]interface{}{
					"project_id": "test-project-456",
				},
			},
			wantID:  "my-imagen-gen",
			wantErr: false,
		},
		{
			name: "missing API key",
			spec: providers.ProviderSpec{
				ID:      "test-imagen",
				Type:    "imagen",
				Model:   "imagen-4.0-generate-001",
				BaseURL: "https://aiplatform.googleapis.com/v1",
				AdditionalConfig: map[string]interface{}{
					"project_id": "test-project",
				},
			},
			wantID:  "",
			wantErr: true,
		},
		{
			name: "project_id now optional",
			spec: providers.ProviderSpec{
				ID:      "test-imagen-no-project",
				Type:    "imagen",
				Model:   "imagen-4.0-generate-001",
				BaseURL: "https://generativelanguage.googleapis.com/v1beta",
			},
			wantID:  "test-imagen-no-project",
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Clear environment variables first
			t.Setenv("GOOGLE_API_KEY", "")
			t.Setenv("GEMINI_API_KEY", "")
			t.Setenv("GOOGLE_CLOUD_PROJECT", "")

			// Set API key environment variable for tests that need it
			if !tt.wantErr {
				t.Setenv("GOOGLE_API_KEY", "test-key-123")
			} else if tt.name == "missing project_id" {
				t.Setenv("GOOGLE_API_KEY", "test-key-123")
			}

			// Use CreateProviderFromSpec which uses the factory registry
			provider, err := providers.CreateProviderFromSpec(tt.spec)

			if tt.wantErr {
				if err == nil {
					t.Errorf("Expected error but got none")
				}
				return
			}

			if err != nil {
				t.Fatalf("Unexpected error: %v", err)
			}

			if provider.ID() != tt.wantID {
				t.Errorf("Expected provider ID %q, got %q", tt.wantID, provider.ID())
			}
		})
	}
}

// TestProviderDefaults ensures defaults are properly set
func TestProviderDefaults(t *testing.T) {
	defaults := providers.ProviderDefaults{
		MaxTokens:   4096,
		Temperature: 0.7,
	}

	provider := NewProvider(Config{
		ID:               "test-imagen",
		Model:            "imagen-4.0-generate-001",
		BaseURL:          "https://test.example.com",
		ApiKey:           "test-key",
		ProjectID:        "test-project",
		Location:         "us-central1",
		IncludeRawOutput: false,
		Defaults:         defaults,
	})

	if provider.Defaults.MaxTokens != defaults.MaxTokens {
		t.Errorf("Expected MaxTokens %d, got %d", defaults.MaxTokens, provider.Defaults.MaxTokens)
	}

	if provider.Defaults.Temperature != defaults.Temperature {
		t.Errorf("Expected Temperature %f, got %f", defaults.Temperature, provider.Defaults.Temperature)
	}
}

// TestProviderFieldsInitialization ensures all fields are correctly initialized
func TestProviderFieldsInitialization(t *testing.T) {
	id := "test-provider"
	model := "imagen-4.0-generate-001"
	baseURL := "https://custom.example.com"
	apiKey := "test-key-xyz"
	projectID := "my-gcp-project"
	location := "europe-west1"
	includeRaw := true

	provider := NewProvider(Config{
		ID:               id,
		Model:            model,
		BaseURL:          baseURL,
		ApiKey:           apiKey,
		ProjectID:        projectID,
		Location:         location,
		IncludeRawOutput: includeRaw,
		Defaults:         providers.ProviderDefaults{},
	})

	if provider.ID() != id {
		t.Errorf("Expected ID %q, got %q", id, provider.ID())
	}

	if provider.Model() != model {
		t.Errorf("Expected Model %q, got %q", model, provider.Model())
	}

	if provider.BaseURL != baseURL {
		t.Errorf("Expected BaseURL %q, got %q", baseURL, provider.BaseURL)
	}

	if provider.ApiKey != apiKey {
		t.Errorf("Expected ApiKey %q, got %q", apiKey, provider.ApiKey)
	}

	if provider.ProjectID != projectID {
		t.Errorf("Expected ProjectID %q, got %q", projectID, provider.ProjectID)
	}

	if provider.Location != location {
		t.Errorf("Expected Location %q, got %q", location, provider.Location)
	}

	if provider.ShouldIncludeRawOutput() != includeRaw {
		t.Errorf("Expected ShouldIncludeRawOutput %v, got %v", includeRaw, provider.ShouldIncludeRawOutput())
	}
}

// TestProviderDefaultValues ensures default values are applied when empty
func TestProviderDefaultValues(t *testing.T) {
	provider := NewProvider(Config{
		ID:               "test-id",
		Model:            "", // empty model - should use default
		BaseURL:          "", // empty baseURL - should use default
		ApiKey:           "test-key",
		ProjectID:        "test-project",
		Location:         "", // empty location - should use default
		IncludeRawOutput: false,
		Defaults:         providers.ProviderDefaults{},
	})

	expectedModel := "imagen-4.0-generate-001"
	if provider.Model() != expectedModel {
		t.Errorf("Expected default model %q, got %q", expectedModel, provider.Model())
	}

	expectedBaseURL := defaultBaseURL
	if provider.BaseURL != expectedBaseURL {
		t.Errorf("Expected default baseURL %q, got %q", expectedBaseURL, provider.BaseURL)
	}

	expectedLocation := "us-central1"
	if provider.Location != expectedLocation {
		t.Errorf("Expected default location %q, got %q", expectedLocation, provider.Location)
	}
}

// TestExtractPrompt tests prompt extraction from messages
func TestExtractPrompt(t *testing.T) {
	tests := []struct {
		name    string
		req     providers.PredictionRequest
		want    string
		wantErr bool
	}{
		{
			name: "extract from Content field",
			req: providers.PredictionRequest{
				Messages: []types.Message{
					{Role: "user", Content: "Generate an image"},
				},
			},
			want:    "Generate an image",
			wantErr: false,
		},
		{
			name: "extract from Parts",
			req: providers.PredictionRequest{
				Messages: []types.Message{
					{
						Role:    "user",
						Content: "",
						Parts: []types.ContentPart{
							{Type: "text", Text: stringPtr("Generate a red square")},
						},
					},
				},
			},
			want:    "Generate a red square",
			wantErr: false,
		},
		{
			name: "no messages",
			req: providers.PredictionRequest{
				Messages: []types.Message{},
			},
			want:    "",
			wantErr: true,
		},
		{
			name: "last message not from user",
			req: providers.PredictionRequest{
				Messages: []types.Message{
					{Role: "assistant", Content: "Here's an image"},
				},
			},
			want:    "",
			wantErr: true,
		},
		{
			name: "empty prompt",
			req: providers.PredictionRequest{
				Messages: []types.Message{
					{Role: "user", Content: "", Parts: []types.ContentPart{}},
				},
			},
			want:    "",
			wantErr: true,
		},
		{
			name: "parts with non-text content",
			req: providers.PredictionRequest{
				Messages: []types.Message{
					{
						Role: "user",
						Parts: []types.ContentPart{
							{Type: "image", Text: nil},
							{Type: "text", Text: stringPtr("This should be found")},
						},
					},
				},
			},
			want:    "This should be found",
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := extractPrompt(tt.req)
			if (err != nil) != tt.wantErr {
				t.Errorf("extractPrompt() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if got != tt.want {
				t.Errorf("extractPrompt() = %v, want %v", got, tt.want)
			}
		})
	}
}

// TestCalculateCost tests cost calculation
func TestCalculateCost(t *testing.T) {
	provider := newTestProvider("test-id")

	cost := provider.CalculateCost(1000, 500, 100)

	if cost.TotalCost != costPerImage {
		t.Errorf("Expected cost %f, got %f", costPerImage, cost.TotalCost)
	}

	if cost.InputTokens != 1000 {
		t.Errorf("Expected InputTokens 1000, got %d", cost.InputTokens)
	}

	if cost.OutputTokens != 500 {
		t.Errorf("Expected OutputTokens 500, got %d", cost.OutputTokens)
	}
}

// TestSupportsStreaming tests streaming support
func TestSupportsStreaming(t *testing.T) {
	provider := newTestProvider("test-id")

	if provider.SupportsStreaming() {
		t.Error("Imagen should not support streaming")
	}
}

// TestPredictStream tests that streaming returns error
func TestPredictStream(t *testing.T) {
	provider := newTestProvider("test-id")

	ctx := context.Background()
	req := providers.PredictionRequest{
		Messages: []types.Message{
			{Role: "user", Content: "Generate image"},
		},
	}

	_, err := provider.PredictStream(ctx, req)
	if err == nil {
		t.Error("Expected error for PredictStream, got nil")
	}

	expectedErr := "streaming not supported for Imagen"
	if err.Error() != expectedErr {
		t.Errorf("Expected error %q, got %q", expectedErr, err.Error())
	}
}

// TestClose tests the Close method
func TestClose(t *testing.T) {
	provider := newTestProvider("test-id")

	err := provider.Close()
	if err != nil {
		t.Errorf("Close() returned unexpected error: %v", err)
	}
}

// Helper function for tests
func stringPtr(s string) *string {
	return &s
}

// TestPredictErrorCases tests error handling in Predict method
func TestPredictErrorCases(t *testing.T) {
	provider := NewProvider(Config{
		ID:               "test-id",
		Model:            "imagen-4.0-generate-001",
		BaseURL:          "https://invalid-url-that-will-fail.example.com",
		ApiKey:           "test-key",
		ProjectID:        "test-project",
		Location:         "us-central1",
		IncludeRawOutput: false,
		Defaults:         providers.ProviderDefaults{},
	})

	ctx := context.Background()

	tests := []struct {
		name    string
		req     providers.PredictionRequest
		wantErr string
	}{
		{
			name: "no messages",
			req: providers.PredictionRequest{
				Messages: []types.Message{},
			},
			wantErr: "no messages provided",
		},
		{
			name: "last message not user",
			req: providers.PredictionRequest{
				Messages: []types.Message{
					{Role: "assistant", Content: "response"},
				},
			},
			wantErr: "last message must be from user",
		},
		{
			name: "empty prompt",
			req: providers.PredictionRequest{
				Messages: []types.Message{
					{Role: "user", Content: ""},
				},
			},
			wantErr: "no text prompt found in message",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := provider.Predict(ctx, tt.req)
			if err == nil {
				t.Fatal("Expected error, got nil")
			}
			if err.Error() != tt.wantErr {
				t.Errorf("Expected error %q, got %q", tt.wantErr, err.Error())
			}
		})
	}
}

// TestPredictSuccess tests successful image generation with mock server
func TestPredictSuccess(t *testing.T) {
	// Create mock server that returns a successful response
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Verify request method and headers
		if r.Method != "POST" {
			t.Errorf("Expected POST request, got %s", r.Method)
		}

		apiKey := r.Header.Get("x-goog-api-key")
		if apiKey != "test-key" {
			t.Errorf("Expected x-goog-api-key header 'test-key', got %q", apiKey)
		}

		contentType := r.Header.Get("Content-Type")
		if contentType != "application/json" {
			t.Errorf("Expected Content-Type 'application/json', got %q", contentType)
		}

		// Return mock response
		response := imagenResponse{
			Predictions: []imagenPrediction{
				{
					BytesBase64Encoded: "iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR42mNk+M9QDwADhgGAWjR9awAAAABJRU5ErkJggg==",
					MimeType:           "image/png",
				},
			},
		}

		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(response)
	}))
	defer server.Close()

	provider := NewProvider(Config{
		ID:               "test-id",
		Model:            "imagen-4.0-generate-001",
		BaseURL:          server.URL,
		ApiKey:           "test-key",
		ProjectID:        "test-project",
		Location:         "us-central1",
		IncludeRawOutput: false,
		Defaults:         providers.ProviderDefaults{},
	})

	ctx := context.Background()
	req := providers.PredictionRequest{
		Messages: []types.Message{
			{Role: "user", Content: "Generate a red square"},
		},
	}

	resp, err := provider.Predict(ctx, req)
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}

	// Verify response
	if resp.Content == "" {
		t.Error("Expected non-empty content")
	}

	if len(resp.Parts) != 2 {
		t.Errorf("Expected 2 parts (text + image), got %d", len(resp.Parts))
	}

	// Verify cost
	if resp.CostInfo == nil {
		t.Fatal("Expected CostInfo, got nil")
	}

	if resp.CostInfo.TotalCost != costPerImage {
		t.Errorf("Expected cost %f, got %f", costPerImage, resp.CostInfo.TotalCost)
	}

	// Verify latency is set
	if resp.Latency == 0 {
		t.Error("Expected non-zero latency")
	}
}

// TestPredictAPIError tests handling of API errors
func TestPredictAPIError(t *testing.T) {
	tests := []struct {
		name       string
		statusCode int
		response   string
		wantErrMsg string
	}{
		{
			name:       "400 bad request",
			statusCode: http.StatusBadRequest,
			response:   `{"error": {"message": "Invalid prompt"}}`,
			wantErrMsg: "API error 400",
		},
		{
			name:       "401 unauthorized",
			statusCode: http.StatusUnauthorized,
			response:   `{"error": {"message": "Invalid API key"}}`,
			wantErrMsg: "API error 401",
		},
		{
			name:       "500 server error",
			statusCode: http.StatusInternalServerError,
			response:   `{"error": {"message": "Internal server error"}}`,
			wantErrMsg: "API error 500",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(tt.statusCode)
				w.Write([]byte(tt.response))
			}))
			defer server.Close()

			provider := NewProvider(Config{
				ID:               "test-id",
				Model:            "imagen-4.0-generate-001",
				BaseURL:          server.URL,
				ApiKey:           "test-key",
				ProjectID:        "",
				Location:         "",
				IncludeRawOutput: false,
				Defaults:         providers.ProviderDefaults{},
			})

			ctx := context.Background()
			req := providers.PredictionRequest{
				Messages: []types.Message{
					{Role: "user", Content: "Generate image"},
				},
			}

			_, err := provider.Predict(ctx, req)
			if err == nil {
				t.Fatal("Expected error, got nil")
			}

			if err.Error()[:len(tt.wantErrMsg)] != tt.wantErrMsg {
				t.Errorf("Expected error to start with %q, got %q", tt.wantErrMsg, err.Error())
			}
		})
	}
}

// TestPredictEmptyPredictions tests handling of empty predictions
func TestPredictEmptyPredictions(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Return response with empty predictions
		response := imagenResponse{
			Predictions: []imagenPrediction{},
		}

		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(response)
	}))
	defer server.Close()

	provider := NewProvider(Config{
		ID:               "test-id",
		Model:            "imagen-4.0-generate-001",
		BaseURL:          server.URL,
		ApiKey:           "test-key",
		ProjectID:        "",
		Location:         "",
		IncludeRawOutput: false,
		Defaults:         providers.ProviderDefaults{},
	})

	ctx := context.Background()
	req := providers.PredictionRequest{
		Messages: []types.Message{
			{Role: "user", Content: "Generate image"},
		},
	}

	_, err := provider.Predict(ctx, req)
	if err == nil {
		t.Fatal("Expected error for empty predictions, got nil")
	}

	expectedErr := "no images generated"
	if err.Error() != expectedErr {
		t.Errorf("Expected error %q, got %q", expectedErr, err.Error())
	}
}

// TestPredictInvalidJSON tests handling of invalid JSON response
func TestPredictInvalidJSON(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("invalid json response"))
	}))
	defer server.Close()

	provider := NewProvider(Config{
		ID:               "test-id",
		Model:            "imagen-4.0-generate-001",
		BaseURL:          server.URL,
		ApiKey:           "test-key",
		ProjectID:        "",
		Location:         "",
		IncludeRawOutput: false,
		Defaults:         providers.ProviderDefaults{},
	})

	ctx := context.Background()
	req := providers.PredictionRequest{
		Messages: []types.Message{
			{Role: "user", Content: "Generate image"},
		},
	}

	_, err := provider.Predict(ctx, req)
	if err == nil {
		t.Fatal("Expected error for invalid JSON, got nil")
	}

	if err.Error()[:len("failed to unmarshal")] != "failed to unmarshal" {
		t.Errorf("Expected error to start with 'failed to unmarshal', got %q", err.Error())
	}
}

// TestPredictLowLevelErrors tests low-level error handling paths for full coverage
func TestPredictLowLevelErrors(t *testing.T) {
	tests := []struct {
		name        string
		setupServer func() *httptest.Server
		wantErrMsg  string
	}{
		{
			name: "http client error - connection refused",
			setupServer: func() *httptest.Server {
				// Create server and immediately close it to cause connection error
				server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
				server.Close()
				return server
			},
			wantErrMsg: "failed to make request",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := tt.setupServer()

			provider := NewProvider(Config{
				ID:               "test-id",
				Model:            "imagen-4.0-generate-001",
				BaseURL:          server.URL,
				ApiKey:           "test-key",
				ProjectID:        "",
				Location:         "",
				IncludeRawOutput: false,
				Defaults:         providers.ProviderDefaults{},
			})

			ctx := context.Background()
			req := providers.PredictionRequest{
				Messages: []types.Message{
					{Role: "user", Content: "Generate image"},
				},
			}

			_, err := provider.Predict(ctx, req)
			if err == nil {
				t.Fatal("Expected error, got nil")
			}

			if !containsString(err.Error(), tt.wantErrMsg) {
				t.Errorf("Expected error to contain %q, got %q", tt.wantErrMsg, err.Error())
			}
		})
	}
}

// Helper function to check if string contains substring
func containsString(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > len(substr) &&
		(s[:len(substr)] == substr || s[len(s)-len(substr):] == substr ||
			len(s) > len(substr) && findSubstring(s, substr)))
}

func findSubstring(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
