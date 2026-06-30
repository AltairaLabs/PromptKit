//go:build integration

package gemini

import (
	"context"
	"os"
	"strings"
	"testing"

	"github.com/AltairaLabs/PromptKit/runtime/providers"
	"github.com/AltairaLabs/PromptKit/runtime/types"
)

// TestGemini_Reasoning_Live verifies the live seam the hermetic tests can't: that
// `additional_config.include_thoughts` actually reaches the wire and a real Gemini
// 2.5 thinking model returns thought parts, which the streaming provider routes to
// StreamChunk.Reasoning — separate from spoken content.
//
// Run: GEMINI_API_KEY=... go test -tags integration ./runtime/providers/gemini/ -run TestGemini_Reasoning_Live -v
func TestGemini_Reasoning_Live(t *testing.T) {
	if os.Getenv("GEMINI_API_KEY") == "" {
		t.Skip("GEMINI_API_KEY not set")
	}

	tp := NewToolProvider(
		"gemini-reasoning",
		"gemini-2.5-flash",
		"https://generativelanguage.googleapis.com/v1beta",
		providers.ProviderDefaults{Temperature: 0, MaxTokens: 2048, TopP: 1.0},
		false,
	)
	defer tp.Close()

	// Mirror the arena/factory path: enable thought summaries via additional_config.
	applyThinkingConfig(tp.Provider, providers.ProviderSpec{
		AdditionalConfig: map[string]any{"include_thoughts": true, "thinking_budget": 1024},
	})

	req := providers.PredictionRequest{
		System: "Reply with ONLY the final answer, nothing else.",
		Messages: []types.Message{{
			Role: "user",
			Content: "A bat and a ball cost $1.10 in total. The bat costs $1.00 more " +
				"than the ball. How much does the ball cost?",
		}},
	}

	ch, err := tp.PredictStream(context.Background(), req)
	if err != nil {
		t.Fatalf("PredictStream: %v", err)
	}

	var reasoning, content strings.Builder
	for chunk := range ch {
		reasoning.WriteString(chunk.Reasoning)
		content.WriteString(chunk.Delta)
	}

	if reasoning.Len() == 0 {
		t.Fatalf("expected non-empty reasoning from a thinking model with include_thoughts=true; "+
			"got none (content=%q) — thinking config is not reaching the wire or the model is not "+
			"returning thought parts", content.String())
	}
	if r := reasoning.String(); strings.Contains(content.String(), r) {
		t.Fatalf("reasoning leaked into spoken content: content=%q", content.String())
	}
	t.Logf("captured reasoning (%d chars); spoken content=%q", reasoning.Len(), content.String())
}
