package gemini

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/AltairaLabs/PromptKit/runtime/providers"
	"github.com/AltairaLabs/PromptKit/runtime/types"
)

func TestNormalizeFinishReason(t *testing.T) {
	cases := map[string]string{
		"STOP":                    types.FinishReasonStop,
		"MAX_TOKENS":              types.FinishReasonMaxOutputTokens,
		"SAFETY":                  types.FinishReasonSafety,
		"RECITATION":              types.FinishReasonSafety,
		"PROHIBITED_CONTENT":      types.FinishReasonSafety,
		"SPII":                    types.FinishReasonSafety,
		"BLOCKLIST":               types.FinishReasonSafety,
		"MALFORMED_FUNCTION_CALL": types.FinishReasonRefusal,
		"":                        "",
		"NEW_REASON":              "NEW_REASON",
	}
	for raw, want := range cases {
		if got := normalizeFinishReason(raw); got != want {
			t.Errorf("normalizeFinishReason(%q) = %q, want %q", raw, got, want)
		}
	}
}

// MAX_TOKENS with content parts present must return the partial content plus the
// canonical max_output_tokens reason (no error).
func TestGemini_Predict_MaxTokensWithContent(t *testing.T) {
	resp := geminiResponse{
		Candidates: []geminiCandidate{{
			Content:      geminiContent{Parts: []geminiPart{{Text: "partial answer"}}},
			FinishReason: "MAX_TOKENS",
		}},
		UsageMetadata: &geminiUsage{PromptTokenCount: 10, CandidatesTokenCount: 5},
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	provider := NewProvider("test", "gemini-2.0-flash", server.URL,
		providers.ProviderDefaults{MaxTokens: 100}, false)
	got, err := provider.Predict(context.Background(), providers.PredictionRequest{
		Messages: []types.Message{{Role: "user", Content: "hi"}},
	})
	if err != nil {
		t.Fatalf("Predict returned error: %v", err)
	}
	if got.Content != "partial answer" {
		t.Errorf("Content = %q, want %q", got.Content, "partial answer")
	}
	if got.FinishReason != types.FinishReasonMaxOutputTokens {
		t.Errorf("FinishReason = %q, want %q", got.FinishReason, types.FinishReasonMaxOutputTokens)
	}
}

// MAX_TOKENS with zero content parts must still error (unchanged behavior).
func TestGemini_Predict_MaxTokensNoContent_StillErrors(t *testing.T) {
	resp := geminiResponse{
		Candidates: []geminiCandidate{{
			Content:      geminiContent{Parts: []geminiPart{}},
			FinishReason: "MAX_TOKENS",
		}},
		UsageMetadata: &geminiUsage{PromptTokenCount: 10},
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	provider := NewProvider("test", "gemini-2.0-flash", server.URL,
		providers.ProviderDefaults{MaxTokens: 100}, false)
	_, err := provider.Predict(context.Background(), providers.PredictionRequest{
		Messages: []types.Message{{Role: "user", Content: "hi"}},
	})
	if err == nil {
		t.Fatal("expected an error for MAX_TOKENS with no content, got nil")
	}
}
