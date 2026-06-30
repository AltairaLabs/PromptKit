//go:build integration

package claude

import (
	"context"
	"os"
	"strings"
	"testing"

	"github.com/AltairaLabs/PromptKit/runtime/providers"
	"github.com/AltairaLabs/PromptKit/runtime/types"
)

// TestClaude_Reasoning_Live verifies the live seam: a real Claude call with
// extended thinking enabled (additional_config.thinking_budget) streams
// thinking_delta text on StreamChunk.Reasoning, separate from spoken content.
//
// Run: ANTHROPIC_API_KEY=... go test -tags integration ./runtime/providers/claude/ -run TestClaude_Reasoning_Live -v
// Override the model with CLAUDE_THINKING_MODEL if the default is unavailable.
func TestClaude_Reasoning_Live(t *testing.T) {
	if os.Getenv("ANTHROPIC_API_KEY") == "" && os.Getenv("CLAUDE_API_KEY") == "" {
		t.Skip("ANTHROPIC_API_KEY not set")
	}

	model := os.Getenv("CLAUDE_THINKING_MODEL")
	if model == "" {
		model = "claude-sonnet-4-5"
	}

	tp := NewToolProvider("claude-reasoning", model, "https://api.anthropic.com/v1",
		providers.ProviderDefaults{MaxTokens: 3072}, false)
	defer tp.Close()

	applyThinkingConfig(tp.Provider, providers.ProviderSpec{
		AdditionalConfig: map[string]any{"thinking_budget": 2048},
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
		if chunk.Content != "" {
			content.Reset()
			content.WriteString(chunk.Content) // accumulated spoken text
		}
	}

	if reasoning.Len() == 0 {
		t.Fatalf("expected non-empty reasoning from extended thinking; got none (content=%q) — "+
			"the thinking block is not reaching the wire or the model is not returning thinking deltas",
			content.String())
	}
	if r := reasoning.String(); strings.Contains(content.String(), r) {
		t.Fatalf("reasoning leaked into spoken content: %q", content.String())
	}
	t.Logf("captured reasoning (%d chars); spoken content=%q", reasoning.Len(), content.String())
}
