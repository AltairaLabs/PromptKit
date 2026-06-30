//go:build integration

package openai

import (
	"context"
	"os"
	"strings"
	"testing"

	"github.com/AltairaLabs/PromptKit/runtime/providers"
	"github.com/AltairaLabs/PromptKit/runtime/types"
)

// TestOpenAI_Reasoning_Live verifies the live seam: a real OpenAI reasoning model
// with additional_config.reasoning_effort streams reasoning tokens on
// StreamChunk.Reasoning, separate from spoken content.
//
// Run: OPENAI_API_KEY=... go test -tags integration ./runtime/providers/openai/ -run TestOpenAI_Reasoning_Live -v
// Override the model with OPENAI_REASONING_MODEL if the default is unavailable.
//
// NOTE: whether reasoning text is exposed depends on the model and api_mode —
// some OpenAI reasoning models only return encrypted reasoning via the Responses
// API. This test documents the expectation and reveals what the configured model
// actually returns.
func TestOpenAI_Reasoning_Live(t *testing.T) {
	if os.Getenv("OPENAI_API_KEY") == "" {
		t.Skip("OPENAI_API_KEY not set")
	}

	model := os.Getenv("OPENAI_REASONING_MODEL")
	if model == "" {
		model = "o4-mini"
	}

	provider := NewProviderWithConfig(
		"openai-reasoning", model, "https://api.openai.com/v1",
		providers.ProviderDefaults{MaxTokens: 3072}, false,
		map[string]any{"reasoning_effort": "medium"},
	)
	defer provider.Close()

	req := providers.PredictionRequest{
		System: "Reply with ONLY the final answer, nothing else.",
		Messages: []types.Message{{
			Role: "user",
			Content: "A bat and a ball cost $1.10 in total. The bat costs $1.00 more " +
				"than the ball. How much does the ball cost?",
		}},
	}

	ch, err := provider.PredictStream(context.Background(), req)
	if err != nil {
		t.Fatalf("PredictStream: %v", err)
	}

	var reasoning, content strings.Builder
	for chunk := range ch {
		reasoning.WriteString(chunk.Reasoning)
		content.WriteString(chunk.Delta)
	}

	if reasoning.Len() == 0 {
		t.Fatalf("expected non-empty reasoning from a reasoning model with reasoning_effort set; "+
			"got none (content=%q) — the model/api_mode may not stream reasoning_content (some only "+
			"expose encrypted reasoning via the Responses API)", content.String())
	}
	if r := reasoning.String(); strings.Contains(content.String(), r) {
		t.Fatalf("reasoning leaked into spoken content: %q", content.String())
	}
	t.Logf("captured reasoning (%d chars); spoken content=%q", reasoning.Len(), content.String())
}
