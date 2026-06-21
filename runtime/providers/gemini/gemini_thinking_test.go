package gemini

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/AltairaLabs/PromptKit/runtime/providers"
	"github.com/AltairaLabs/PromptKit/runtime/types"
)

// genConfigFrom returns the generationConfig object from a captured request body.
func genConfigFrom(t *testing.T, body map[string]any) map[string]any {
	t.Helper()
	gc, ok := body["generationConfig"].(map[string]any)
	if !ok {
		t.Fatalf("no generationConfig in request body: %v", body)
	}
	return gc
}

// thinking_budget config flows into generationConfig.thinkingConfig on the
// non-tool (struct) path, including the budget-0 "disable" case (a pointer, so
// 0 is distinct from unset).
func TestThinking_BudgetInRequest_Predict(t *testing.T) {
	for _, tc := range []struct {
		name   string
		budget any
		want   float64
	}{
		{"positive", 256, 256},
		{"disabled", 0, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			srv := newExplicitCacheServer(t, http.StatusOK)
			p := newExplicitCacheProvider(t, srv.url, map[string]any{"thinking_budget": tc.budget})
			if _, err := p.Predict(context.Background(), providers.PredictionRequest{
				Messages: []types.Message{{Role: "user", Content: "hi"}}, MaxTokens: 1024,
			}); err != nil {
				t.Fatalf("Predict: %v", err)
			}
			gc := genConfigFrom(t, srv.genBody(t))
			tcfg, ok := gc["thinkingConfig"].(map[string]any)
			if !ok {
				t.Fatalf("no thinkingConfig in generationConfig: %v", gc)
			}
			if tcfg["thinkingBudget"] != tc.want {
				t.Errorf("thinkingBudget = %v, want %v", tcfg["thinkingBudget"], tc.want)
			}
		})
	}
}

// thinking_budget + include_thoughts flow into the tool (map) path too.
func TestThinking_ConfigInRequest_Tools(t *testing.T) {
	srv := newExplicitCacheServer(t, http.StatusOK)
	p := newExplicitCacheProvider(t, srv.url, map[string]any{
		"thinking_budget": 128, "include_thoughts": true,
	})
	tp := p.(*ToolProvider)
	tools, _ := tp.BuildTooling([]*providers.ToolDescriptor{
		{Name: "noop", Description: "noop", InputSchema: []byte(`{"type":"object"}`)},
	})
	if _, _, err := tp.PredictWithTools(context.Background(), providers.PredictionRequest{
		Messages: []types.Message{{Role: "user", Content: "hi"}}, MaxTokens: 1024,
	}, tools, ""); err != nil {
		t.Fatalf("PredictWithTools: %v", err)
	}
	tcfg, ok := genConfigFrom(t, srv.genBody(t))["thinkingConfig"].(map[string]any)
	if !ok {
		t.Fatalf("no thinkingConfig in tool request")
	}
	if tcfg["thinkingBudget"] != float64(128) {
		t.Errorf("thinkingBudget = %v, want 128", tcfg["thinkingBudget"])
	}
	if tcfg["includeThoughts"] != true {
		t.Errorf("includeThoughts = %v, want true", tcfg["includeThoughts"])
	}
}

// With no thinking config, no thinkingConfig is sent (the model applies its own
// default thinking).
func TestThinking_NotConfigured_Omitted(t *testing.T) {
	srv := newExplicitCacheServer(t, http.StatusOK)
	p := newExplicitCacheProvider(t, srv.url, nil)
	if _, err := p.Predict(context.Background(), providers.PredictionRequest{
		Messages: []types.Message{{Role: "user", Content: "hi"}}, MaxTokens: 1024,
	}); err != nil {
		t.Fatalf("Predict: %v", err)
	}
	if _, has := genConfigFrom(t, srv.genBody(t))["thinkingConfig"]; has {
		t.Error("thinkingConfig must be omitted when not configured")
	}
}

// geminiThinkingConfigFor returns the config and warns (covered) when the cap
// can't cover the budget.
func TestThinking_BudgetExceedsMax_StillReturnsConfig(t *testing.T) {
	budget := 1000
	p := &Provider{thinkingBudget: &budget}
	tc := p.geminiThinkingConfigFor(100) // maxTokens < budget → warn path
	if tc == nil || tc.ThinkingBudget == nil || *tc.ThinkingBudget != 1000 {
		t.Fatalf("expected thinkingConfig with budget 1000, got %+v", tc)
	}
}

// The MAX_TOKENS-with-no-content error now names the thinking-budget cause and
// the fix, instead of the misleading "this should not happen".
func TestThinking_MaxTokensError_IsActionable(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		// Candidate with empty parts + MAX_TOKENS (thinking consumed the budget).
		_, _ = io.WriteString(w, `{"candidates":[{"content":{"parts":[],"role":"model"},`+
			`"finishReason":"MAX_TOKENS","index":0}],"usageMetadata":{"promptTokenCount":5,`+
			`"candidatesTokenCount":0,"thoughtsTokenCount":16,"totalTokenCount":21}}`)
	}))
	t.Cleanup(srv.Close)

	p := newExplicitCacheProvider(t, srv.URL, nil)
	_, err := p.Predict(context.Background(), providers.PredictionRequest{
		Messages: []types.Message{{Role: "user", Content: "hi"}}, MaxTokens: 16,
	})
	if err == nil {
		t.Fatal("expected an error on MAX_TOKENS with no content")
	}
	for _, want := range []string{"thinking_budget", "max_tokens", "output-token limit"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error message missing %q: %v", want, err)
		}
	}
}
