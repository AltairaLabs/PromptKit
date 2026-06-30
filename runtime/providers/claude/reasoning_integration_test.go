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

// newClaudeThinkingProvider builds an extended-thinking-enabled Claude tool provider.
func newClaudeThinkingProvider(t *testing.T) *ToolProvider {
	t.Helper()
	if os.Getenv("ANTHROPIC_API_KEY") == "" && os.Getenv("CLAUDE_API_KEY") == "" {
		t.Skip("ANTHROPIC_API_KEY not set")
	}
	model := os.Getenv("CLAUDE_THINKING_MODEL")
	if model == "" {
		model = "claude-sonnet-4-5"
	}
	tp := NewToolProvider("claude-reasoning", model, "https://api.anthropic.com/v1",
		providers.ProviderDefaults{MaxTokens: 4096}, false)
	applyThinkingConfig(tp.Provider, providers.ProviderSpec{
		AdditionalConfig: map[string]any{"thinking_budget": 2048},
	})
	return tp
}

const claudeReasoningPrompt = "A bat and a ball cost $1.10 in total. The bat costs $1.00 more " +
	"than the ball. How much does the ball cost?"

// TestClaude_Reasoning_Live_NonStreaming covers the non-streaming Predict path.
func TestClaude_Reasoning_Live_NonStreaming(t *testing.T) {
	tp := newClaudeThinkingProvider(t)
	defer tp.Close()

	resp, err := tp.Predict(context.Background(), providers.PredictionRequest{
		Messages: []types.Message{{Role: "user", Content: claudeReasoningPrompt}},
	})
	if err != nil {
		t.Fatalf("Predict: %v", err)
	}
	if resp.Reasoning == nil || resp.Reasoning.Text == "" {
		t.Fatalf("expected reasoning on the non-streaming response; got none (content=%q)", resp.Content)
	}
	if strings.Contains(resp.Content, resp.Reasoning.Text) {
		t.Fatalf("reasoning leaked into content: %q", resp.Content)
	}
	t.Logf("non-streaming reasoning %d chars", len(resp.Reasoning.Text))
}

func claudeWeatherTool(t *testing.T, tp *ToolProvider) providers.ProviderTools {
	t.Helper()
	tools, err := tp.BuildTooling([]*providers.ToolDescriptor{{
		Name:        "get_weather",
		Description: "Get the current weather for a city",
		InputSchema: []byte(`{"type":"object","properties":{"city":{"type":"string"}},"required":["city"]}`),
	}})
	if err != nil {
		t.Fatalf("BuildTooling: %v", err)
	}
	return tools
}

const claudeToolPrompt = "Work out the capital of France, then call get_weather for that city."

// TestClaude_Reasoning_Live_Tools covers the non-streaming tools path.
func TestClaude_Reasoning_Live_Tools(t *testing.T) {
	tp := newClaudeThinkingProvider(t)
	defer tp.Close()

	resp, _, err := tp.PredictWithTools(context.Background(), providers.PredictionRequest{
		Messages: []types.Message{{Role: "user", Content: claudeToolPrompt}},
	}, claudeWeatherTool(t, tp), "auto")
	if err != nil {
		t.Fatalf("PredictWithTools: %v", err)
	}
	if resp.Reasoning == nil || resp.Reasoning.Text == "" {
		t.Fatalf("expected reasoning on the tools response; got none (content=%q)", resp.Content)
	}
	t.Logf("tools reasoning %d chars", len(resp.Reasoning.Text))
}

// TestClaude_Reasoning_Live_StreamingTools covers the streaming tools path.
func TestClaude_Reasoning_Live_StreamingTools(t *testing.T) {
	tp := newClaudeThinkingProvider(t)
	defer tp.Close()

	ch, err := tp.PredictStreamWithTools(context.Background(), providers.PredictionRequest{
		Messages: []types.Message{{Role: "user", Content: claudeToolPrompt}},
	}, claudeWeatherTool(t, tp), "auto")
	if err != nil {
		t.Fatalf("PredictStreamWithTools: %v", err)
	}
	var reasoning strings.Builder
	for chunk := range ch {
		reasoning.WriteString(chunk.Reasoning)
	}
	if reasoning.Len() == 0 {
		t.Fatal("expected reasoning on the streaming-tools path; got none")
	}
	t.Logf("streaming-tools reasoning %d chars", reasoning.Len())
}
