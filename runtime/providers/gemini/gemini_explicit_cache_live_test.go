package gemini

import (
	"context"
	"encoding/json"
	"os"
	"strings"
	"testing"

	"github.com/AltairaLabs/PromptKit/runtime/providers"
	"github.com/AltairaLabs/PromptKit/runtime/types"
)

// TestExplicitCaching_Live exercises explicit context caching against the real
// Gemini API across every generateContent path — non-streaming Predict, the tool
// loop (PredictWithTools), and streaming (PredictStream) — and asserts the cache
// hits immediately (cachedContentTokenCount > 0 from the first reference), which
// is the warmup gap #1404 closes.
//
// Gated: runs only when GEMINI_LIVE_CACHE=1 and GEMINI_API_KEY is set, so it
// never runs in CI. Costs are minimal: one ~1.5k-token prefix cached once, a few
// short generations referencing it, then the cache is deleted.
func TestExplicitCaching_Live(t *testing.T) {
	if os.Getenv("GEMINI_LIVE_CACHE") != "1" || os.Getenv("GEMINI_API_KEY") == "" {
		t.Skip("set GEMINI_LIVE_CACHE=1 and GEMINI_API_KEY to run the live explicit-caching test")
	}

	// ~1.5k-token stable system prefix (well above the 2.5 cache floor).
	system := strings.Repeat(
		"You are a meticulous coding agent working in a large Go monorepo. "+
			"Follow existing conventions precisely and never invent APIs. ", 60)

	newProvider := func(t *testing.T) *ToolProvider {
		p, err := providers.CreateProviderFromSpec(providers.ProviderSpec{
			ID: "live-gemini", Type: "gemini", Model: "gemini-2.5-flash",
			AdditionalConfig: map[string]any{"explicit_caching": true},
			// maxOutputTokens caps the TOTAL output, and gemini-2.5-flash is a
			// thinking model — it spends output budget on internal reasoning
			// (thoughtsTokenCount) before any visible text, so a cap below the
			// thinking spend returns finishReason=MAX_TOKENS with empty content,
			// which the provider treats as an error. The cap is a ceiling, not a
			// cost lever (cost is the tokens actually generated — a few here), so
			// it's set generously rather than tuned to a fragile minimum.
			Defaults: providers.ProviderDefaults{MaxTokens: 1024},
		})
		if err != nil {
			t.Fatalf("CreateProviderFromSpec: %v", err)
		}
		tp := p.(*ToolProvider)
		t.Cleanup(func() {
			for _, name := range tp.cache.trackedNames() {
				tp.deleteCachedContent(context.Background(), name)
			}
		})
		return tp
	}

	t.Run("Predict", func(t *testing.T) {
		tp := newProvider(t)
		// Round 1 must already hit (explicit caching, unlike implicit warmup).
		for round := 1; round <= 2; round++ {
			resp, err := tp.Predict(context.Background(), providers.PredictionRequest{
				System:   system,
				Messages: []types.Message{{Role: "user", Content: "Reply with the single word OK."}},
			})
			if err != nil {
				t.Fatalf("round %d Predict: %v", round, err)
			}
			if resp.CostInfo == nil || resp.CostInfo.CachedTokens <= 0 {
				t.Fatalf("round %d: expected cachedTokens > 0, got %+v", round, resp.CostInfo)
			}
			t.Logf("Predict round %d: cachedTokens=%d inputTokens=%d", round, resp.CostInfo.CachedTokens, resp.CostInfo.InputTokens)
		}
	})

	t.Run("PredictWithTools", func(t *testing.T) {
		tp := newProvider(t)
		tools, _ := tp.BuildTooling([]*providers.ToolDescriptor{
			{Name: "get_status", Description: "Get the build status", InputSchema: json.RawMessage(`{"type":"object","properties":{}}`)},
		})
		resp, _, err := tp.PredictWithTools(context.Background(), providers.PredictionRequest{
			System:   system,
			Messages: []types.Message{{Role: "user", Content: "Reply with the single word OK."}},
		}, tools, "")
		if err != nil {
			t.Fatalf("PredictWithTools: %v", err)
		}
		if resp.CostInfo == nil || resp.CostInfo.CachedTokens <= 0 {
			t.Fatalf("expected cachedTokens > 0, got %+v", resp.CostInfo)
		}
		t.Logf("PredictWithTools: cachedTokens=%d inputTokens=%d", resp.CostInfo.CachedTokens, resp.CostInfo.InputTokens)
	})

	t.Run("ThinkingBudgetFixesCutoff", func(t *testing.T) {
		// A tight maxTokens (32) is exhausted by thinking alone on 2.5-flash, so
		// with default thinking it returns MAX_TOKENS with no content...
		withThinking, err := providers.CreateProviderFromSpec(providers.ProviderSpec{
			ID: "live-gemini", Type: "gemini", Model: "gemini-2.5-flash",
			Defaults: providers.ProviderDefaults{MaxTokens: 32},
		})
		if err != nil {
			t.Fatalf("CreateProviderFromSpec: %v", err)
		}
		if _, err := withThinking.Predict(context.Background(), providers.PredictionRequest{
			Messages: []types.Message{{Role: "user", Content: "Reply with the single word OK."}},
		}); err == nil {
			t.Log("note: thinking did not exhaust 32 tokens this run; cutoff contrast is best-effort")
		}

		// ...but disabling thinking (thinking_budget: 0) leaves the whole cap for
		// the answer, so the same tight cap now succeeds with content.
		noThinking, err := providers.CreateProviderFromSpec(providers.ProviderSpec{
			ID: "live-gemini", Type: "gemini", Model: "gemini-2.5-flash",
			AdditionalConfig: map[string]any{"thinking_budget": 0},
			Defaults:         providers.ProviderDefaults{MaxTokens: 32},
		})
		if err != nil {
			t.Fatalf("CreateProviderFromSpec: %v", err)
		}
		resp, err := noThinking.Predict(context.Background(), providers.PredictionRequest{
			Messages: []types.Message{{Role: "user", Content: "Reply with the single word OK."}},
		})
		if err != nil {
			t.Fatalf("with thinking_budget=0 the tight cap must produce an answer, got: %v", err)
		}
		if strings.TrimSpace(resp.Content) == "" {
			t.Fatal("expected non-empty content with thinking disabled")
		}
		t.Logf("thinking_budget=0 @ maxTokens=32: content=%q", resp.Content)
	})

	t.Run("PredictStream", func(t *testing.T) {
		tp := newProvider(t)
		ch, err := tp.PredictStream(context.Background(), providers.PredictionRequest{
			System:   system,
			Messages: []types.Message{{Role: "user", Content: "Reply with the single word OK."}},
		})
		if err != nil {
			t.Fatalf("PredictStream: %v", err)
		}
		var cached int
		for chunk := range ch {
			if chunk.Error != nil {
				t.Fatalf("stream error: %v", chunk.Error)
			}
			if chunk.CostInfo != nil && chunk.CostInfo.CachedTokens > 0 {
				cached = chunk.CostInfo.CachedTokens
			}
		}
		if cached <= 0 {
			t.Fatalf("expected cachedTokens > 0 in stream, got %d", cached)
		}
		t.Logf("PredictStream: cachedTokens=%d", cached)
	})
}
