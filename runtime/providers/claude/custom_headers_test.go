package claude

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
		_, _ = io.WriteString(w, `{"id":"msg_x","type":"message","role":"assistant","content":[{"type":"text","text":"hello"}],"model":"claude-sonnet-4-20250514","stop_reason":"end_turn","usage":{"input_tokens":1,"output_tokens":1}}`)
	}))
	defer server.Close()

	t.Setenv("ANTHROPIC_API_KEY", "test-key")

	spec := providers.ProviderSpec{
		ID:      "test-claude",
		Type:    "claude",
		Model:   "claude-3-5-sonnet-20241022",
		BaseURL: server.URL,
		Headers: map[string]string{
			"X-Custom-Header": "custom-value",
			"X-App-Name":      "my-app",
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

	if got := receivedHeaders.Get("X-Custom-Header"); got != "custom-value" {
		t.Errorf("X-Custom-Header = %q, want %q", got, "custom-value")
	}
	if got := receivedHeaders.Get("X-App-Name"); got != "my-app" {
		t.Errorf("X-App-Name = %q, want %q", got, "my-app")
	}
}

func TestToolProvider_CustomHeaders_CollisionRejected(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("request should not have been sent")
	}))
	defer server.Close()

	t.Setenv("ANTHROPIC_API_KEY", "test-key")

	spec := providers.ProviderSpec{
		ID:      "test-claude",
		Type:    "claude",
		Model:   "claude-3-5-sonnet-20241022",
		BaseURL: server.URL,
		Headers: map[string]string{
			"X-Api-Key": "conflict", // collides with built-in x-api-key header
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
