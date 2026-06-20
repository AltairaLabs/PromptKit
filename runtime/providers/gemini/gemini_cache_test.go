package gemini

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/AltairaLabs/PromptKit/runtime/providers"
	"github.com/AltairaLabs/PromptKit/runtime/types"
)

// geminiCachedBody: promptTokenCount (100) INCLUDES cachedContentTokenCount (80),
// so non-cached input is 20.
const geminiCachedBody = `{"candidates":[{"content":{"parts":[{"text":"ok"}],"role":"model"},` +
	`"finishReason":"STOP","index":0}],"usageMetadata":{"promptTokenCount":100,` +
	`"candidatesTokenCount":10,"cachedContentTokenCount":80,"totalTokenCount":110}}`

func newCachedGeminiServer(t *testing.T) string {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, geminiCachedBody)
	}))
	t.Cleanup(server.Close)
	return server.URL
}

// TestPredict_CapturesCachedTokens proves the non-streaming Predict path reads
// usageMetadata.cachedContentTokenCount into cost (it previously hardcoded 0).
func TestPredict_CapturesCachedTokens(t *testing.T) {
	t.Setenv("GEMINI_API_KEY", "test-key")
	provider, err := providers.CreateProviderFromSpec(providers.ProviderSpec{
		ID: "test-gemini", Type: "gemini", Model: "gemini-1.5-pro", BaseURL: newCachedGeminiServer(t),
	})
	if err != nil {
		t.Fatalf("CreateProviderFromSpec: %v", err)
	}

	resp, err := provider.Predict(context.Background(), providers.PredictionRequest{
		Messages: []types.Message{{Role: "user", Content: "hi"}},
	})
	if err != nil {
		t.Fatalf("Predict: %v", err)
	}
	if resp.CostInfo == nil {
		t.Fatal("no CostInfo")
	}
	if resp.CostInfo.CachedTokens != 80 {
		t.Errorf("CachedTokens = %d, want 80", resp.CostInfo.CachedTokens)
	}
	if resp.CostInfo.InputTokens != 20 {
		t.Errorf("InputTokens = %d, want 20 (100 promptTokenCount - 80 cached)", resp.CostInfo.InputTokens)
	}
}

// TestPredictWithTools_CapturesCachedTokens proves the non-streaming tools path
// also reads cachedContentTokenCount into cost.
func TestPredictWithTools_CapturesCachedTokens(t *testing.T) {
	t.Setenv("GEMINI_API_KEY", "test-key")
	provider, err := providers.CreateProviderFromSpec(providers.ProviderSpec{
		ID: "test-gemini", Type: "gemini", Model: "gemini-1.5-pro", BaseURL: newCachedGeminiServer(t),
	})
	if err != nil {
		t.Fatalf("CreateProviderFromSpec: %v", err)
	}
	tp, ok := provider.(providers.ToolSupport)
	if !ok {
		t.Skip("gemini provider does not expose PredictWithTools in this build")
	}

	resp, _, err := tp.PredictWithTools(context.Background(), providers.PredictionRequest{
		Messages: []types.Message{{Role: "user", Content: "hi"}},
	}, nil, "auto")
	if err != nil {
		t.Fatalf("PredictWithTools: %v", err)
	}
	if resp.CostInfo == nil {
		t.Fatal("no CostInfo")
	}
	if resp.CostInfo.CachedTokens != 80 {
		t.Errorf("CachedTokens = %d, want 80", resp.CostInfo.CachedTokens)
	}
}
