package openai

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/AltairaLabs/PromptKit/runtime/providers"
	"github.com/AltairaLabs/PromptKit/runtime/types"
)

func TestToolProvider_CustomHeaders(t *testing.T) {
	var receivedHeaders http.Header
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedHeaders = r.Header.Clone()
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = io.WriteString(w, `{"id":"x","object":"chat.completion","created":1,"model":"gpt-4o-mini","choices":[{"index":0,"message":{"role":"assistant","content":"hello"},"finish_reason":"stop"}],"usage":{"prompt_tokens":1,"completion_tokens":1,"total_tokens":2}}`)
	}))
	defer server.Close()

	t.Setenv("OPENAI_API_KEY", "test-key")

	spec := providers.ProviderSpec{
		ID:      "test-openai",
		Type:    "openai",
		Model:   "gpt-4o-mini",
		BaseURL: server.URL,
		Headers: map[string]string{
			"X-Title":      "My App",
			"HTTP-Referer": "https://myapp.com",
		},
	}

	provider, err := providers.CreateProviderFromSpec(spec)
	if err != nil {
		t.Fatalf("CreateProviderFromSpec: %v", err)
	}

	_, err = provider.Predict(context.Background(), providers.PredictionRequest{
		Messages: []types.Message{{Role: "user", Content: "hi"}},
	})
	if err != nil {
		t.Fatalf("Predict: %v", err)
	}

	if got := receivedHeaders.Get("X-Title"); got != "My App" {
		t.Errorf("X-Title = %q, want %q", got, "My App")
	}
	if got := receivedHeaders.Get("HTTP-Referer"); got != "https://myapp.com" {
		t.Errorf("HTTP-Referer = %q, want %q", got, "https://myapp.com")
	}
}

func TestToolProvider_CustomHeaders_CollisionRejected(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("request should not have been sent")
	}))
	defer server.Close()

	t.Setenv("OPENAI_API_KEY", "test-key")

	spec := providers.ProviderSpec{
		ID:      "test-openai",
		Type:    "openai",
		Model:   "gpt-4o-mini",
		BaseURL: server.URL,
		Headers: map[string]string{
			"Authorization": "Bearer conflict",
		},
	}

	provider, err := providers.CreateProviderFromSpec(spec)
	if err != nil {
		t.Fatalf("CreateProviderFromSpec: %v", err)
	}

	_, err = provider.Predict(context.Background(), providers.PredictionRequest{
		Messages: []types.Message{{Role: "user", Content: "hi"}},
	})
	if err == nil {
		t.Fatal("expected collision error, got nil")
	}
}
