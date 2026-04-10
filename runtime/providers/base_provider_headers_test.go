package providers

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/AltairaLabs/PromptKit/runtime/types"
)

func TestApplyCustomHeaders_AddsHeaders(t *testing.T) {
	bp := NewBaseProvider("test", false, &http.Client{})
	bp.SetCustomHeaders(map[string]string{
		"X-Title":      "My App",
		"HTTP-Referer": "https://myapp.com",
	})

	req, _ := http.NewRequest("POST", "http://example.com", nil)
	if err := bp.ApplyCustomHeaders(req); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if got := req.Header.Get("X-Title"); got != "My App" {
		t.Errorf("X-Title = %q, want %q", got, "My App")
	}
	if got := req.Header.Get("HTTP-Referer"); got != "https://myapp.com" {
		t.Errorf("HTTP-Referer = %q, want %q", got, "https://myapp.com")
	}
}

func TestApplyCustomHeaders_CollisionError(t *testing.T) {
	bp := NewBaseProvider("test", false, &http.Client{})
	bp.SetCustomHeaders(map[string]string{
		"Authorization": "Bearer custom-key",
	})

	req, _ := http.NewRequest("POST", "http://example.com", nil)
	req.Header.Set("Authorization", "Bearer built-in-key")

	err := bp.ApplyCustomHeaders(req)
	if err == nil {
		t.Fatal("expected collision error, got nil")
	}
}

func TestApplyCustomHeaders_CaseInsensitiveCollision(t *testing.T) {
	bp := NewBaseProvider("test", false, &http.Client{})
	bp.SetCustomHeaders(map[string]string{
		"content-type": "text/plain",
	})

	req, _ := http.NewRequest("POST", "http://example.com", nil)
	req.Header.Set("Content-Type", "application/json")

	err := bp.ApplyCustomHeaders(req)
	if err == nil {
		t.Fatal("expected collision error for case-insensitive match, got nil")
	}
}

func TestApplyCustomHeaders_NilHeaders(t *testing.T) {
	bp := NewBaseProvider("test", false, &http.Client{})

	req, _ := http.NewRequest("POST", "http://example.com", nil)
	if err := bp.ApplyCustomHeaders(req); err != nil {
		t.Fatalf("unexpected error with nil headers: %v", err)
	}
}

// headersTestProvider is a minimal Provider that embeds BaseProvider for
// testing the headersConfigurable wiring in CreateProviderFromSpec.
type headersTestProvider struct {
	BaseProvider
}

func (p *headersTestProvider) Predict(_ context.Context, _ PredictionRequest) (PredictionResponse, error) {
	return PredictionResponse{}, nil
}

func (p *headersTestProvider) PredictStream(_ context.Context, _ PredictionRequest) (<-chan StreamChunk, error) {
	return nil, nil
}

func (p *headersTestProvider) SupportsStreaming() bool { return false }
func (p *headersTestProvider) Model() string           { return "test" }
func (p *headersTestProvider) Close() error            { return nil }
func (p *headersTestProvider) CalculateCost(_, _, _ int) types.CostInfo {
	return types.CostInfo{}
}

func TestCreateProviderFromSpec_AppliesHeaders(t *testing.T) {
	factoryName := "test-headers-provider"
	RegisterProviderFactory(factoryName, func(spec ProviderSpec) (Provider, error) {
		p := &headersTestProvider{
			BaseProvider: NewBaseProvider(spec.ID, false, &http.Client{}),
		}
		return p, nil
	})
	defer func() {
		delete(providerFactories, factoryName)
	}()

	spec := ProviderSpec{
		ID:      "test",
		Type:    factoryName,
		Headers: map[string]string{"X-Custom": "value"},
	}

	provider, err := CreateProviderFromSpec(spec)
	if err != nil {
		t.Fatalf("CreateProviderFromSpec failed: %v", err)
	}

	hp := provider.(*headersTestProvider)
	req, _ := http.NewRequest("POST", "http://example.com", nil)
	if err := hp.ApplyCustomHeaders(req); err != nil {
		t.Fatalf("ApplyCustomHeaders failed: %v", err)
	}
	if got := req.Header.Get("X-Custom"); got != "value" {
		t.Errorf("X-Custom = %q, want %q", got, "value")
	}
}

// TestMakeRawRequest_AppliesCustomHeaders verifies that providers going
// through the shared MakeRawRequest / MakeJSONRequest helpers pick up
// custom headers automatically, without each provider having to wire
// ApplyCustomHeaders into its request construction manually. This is the
// path used by Ollama's ToolProvider.
func TestMakeRawRequest_AppliesCustomHeaders(t *testing.T) {
	var receivedHeaders http.Header
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedHeaders = r.Header.Clone()
		w.WriteHeader(http.StatusOK)
		_, _ = io.WriteString(w, `{}`)
	}))
	defer server.Close()

	bp := NewBaseProvider("test", false, &http.Client{})
	bp.SetCustomHeaders(map[string]string{
		"X-Title":      "My App",
		"HTTP-Referer": "https://myapp.com",
	})

	_, err := bp.MakeJSONRequest(context.Background(), server.URL, map[string]any{"ok": true},
		RequestHeaders{"Content-Type": "application/json"}, "TestProvider")
	if err != nil {
		t.Fatalf("MakeJSONRequest: %v", err)
	}

	if got := receivedHeaders.Get("X-Title"); got != "My App" {
		t.Errorf("X-Title = %q, want %q", got, "My App")
	}
	if got := receivedHeaders.Get("HTTP-Referer"); got != "https://myapp.com" {
		t.Errorf("HTTP-Referer = %q, want %q", got, "https://myapp.com")
	}
}

// TestMakeRawRequest_CustomHeaderCollision verifies that a collision
// between a custom header and a caller-supplied header aborts the
// request before any retry attempts — the caller gets the failure
// immediately instead of burning retry budget on a deterministic
// client-side error.
func TestMakeRawRequest_CustomHeaderCollision(t *testing.T) {
	var attempts int32
	server := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, _ *http.Request) {
		atomic.AddInt32(&attempts, 1)
	}))
	defer server.Close()

	bp := NewBaseProvider("test", false, &http.Client{})
	bp.SetCustomHeaders(map[string]string{
		"Content-Type": "text/plain",
	})

	_, err := bp.MakeJSONRequest(context.Background(), server.URL, map[string]any{"ok": true},
		RequestHeaders{"Content-Type": "application/json"}, "TestProvider")
	if err == nil {
		t.Fatal("expected collision error, got nil")
	}
	if !strings.Contains(err.Error(), "custom header") {
		t.Errorf("unexpected error: %v", err)
	}
	if atomic.LoadInt32(&attempts) != 0 {
		t.Errorf("expected 0 HTTP attempts on collision, got %d", atomic.LoadInt32(&attempts))
	}
}
