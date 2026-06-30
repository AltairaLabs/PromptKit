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
// via the Responses API (reasoning_summary opt-in) streams reasoning summaries on
// StreamChunk.Reasoning, separate from spoken content.
//
// Run: OPENAI_API_KEY=... go test -tags integration ./runtime/providers/openai/ -run TestOpenAI_Reasoning_Live -v
// Override the model with OPENAI_REASONING_MODEL if the default is unavailable.
//
// OpenAI reasoning summaries are best-effort: the model decides per-call whether
// to emit one, so this retries a few times and fails only if reasoning is never
// captured. Requires a verified OpenAI org (skips on that account gate).
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
		providers.ProviderDefaults{MaxTokens: 4096}, false,
		// OpenAI exposes reasoning only as summaries via the Responses API.
		// reasoning_summary is opt-in (requires a verified org). "detailed" + high
		// effort maximizes the chance the model emits a summary.
		map[string]any{"api_mode": "responses", "reasoning_effort": "high", "reasoning_summary": "detailed"},
	)
	defer provider.Close()

	// A multi-step puzzle the model can't shortcut, so it reliably reasons.
	req := providers.PredictionRequest{
		Messages: []types.Message{{
			Role: "user",
			Content: "I have 17 coins totaling exactly $1.00, using only nickels, dimes, " +
				"and quarters. How many of each? Give the counts.",
		}},
	}

	const attempts = 3
	var reasoning, content strings.Builder
	for attempt := 1; attempt <= attempts; attempt++ {
		reasoning.Reset()
		content.Reset()

		ch, err := provider.PredictStream(context.Background(), req)
		if err != nil {
			// Reasoning summaries require a verified OpenAI org; treat that account
			// gate as a skip, not a code failure.
			if strings.Contains(err.Error(), "must be verified") || strings.Contains(err.Error(), "Verify Organization") {
				t.Skipf("OpenAI org not verified for reasoning summaries (account gate): %v", err)
			}
			t.Fatalf("PredictStream: %v", err)
		}
		for chunk := range ch {
			reasoning.WriteString(chunk.Reasoning)
			content.WriteString(chunk.Delta)
		}
		if reasoning.Len() > 0 {
			break // captured a summary
		}
		t.Logf("attempt %d/%d: no reasoning summary emitted (OpenAI best-effort), retrying", attempt, attempts)
	}

	if reasoning.Len() == 0 {
		t.Fatalf("no reasoning summary captured across %d attempts (content=%q)", attempts, content.String())
	}
	if r := reasoning.String(); strings.Contains(content.String(), r) {
		t.Fatalf("reasoning leaked into spoken content: %q", content.String())
	}
	t.Logf("captured reasoning (%d chars); spoken content=%q", reasoning.Len(), content.String())
}

// --- Full mode matrix (non-streaming, tools, streaming+tools) ---

func newOpenAIThinkingTool(t *testing.T) *ToolProvider {
	t.Helper()
	if os.Getenv("OPENAI_API_KEY") == "" {
		t.Skip("OPENAI_API_KEY not set")
	}
	model := os.Getenv("OPENAI_REASONING_MODEL")
	if model == "" {
		model = "o4-mini"
	}
	return NewToolProvider("openai-reasoning", model, "https://api.openai.com/v1",
		providers.ProviderDefaults{MaxTokens: 4096}, false,
		map[string]any{"api_mode": "responses", "reasoning_effort": "high", "reasoning_summary": "detailed"},
		nil)
}

// skipOnOrgGate skips when OpenAI rejects summaries for an unverified org.
func skipOnOrgGate(t *testing.T, err error) {
	t.Helper()
	if err != nil && (strings.Contains(err.Error(), "must be verified") ||
		strings.Contains(err.Error(), "Verify Organization")) {
		t.Skipf("OpenAI org not verified for reasoning summaries: %v", err)
	}
}

const openAIReasoningPrompt = "I have 17 coins totaling exactly $1.00, using only nickels, dimes, " +
	"and quarters. How many of each? Give the counts."

func openAIWeatherTool(t *testing.T, tp *ToolProvider) providers.ProviderTools {
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

// A prompt that reliably forces multi-step reasoning (the coin puzzle) before a
// deterministic tool call — o4-mini shortcuts trivial tool prompts and emits no
// summary, making the test flaky against OpenAI's best-effort summaries.
const openAIToolPrompt = "I have 17 coins totaling exactly $1.00 using only nickels, dimes, " +
	"and quarters. Work out how many dimes there are, then call get_weather for Paris."

// TestOpenAI_Reasoning_Live_NonStreaming covers the non-streaming Predict path.
func TestOpenAI_Reasoning_Live_NonStreaming(t *testing.T) {
	tp := newOpenAIThinkingTool(t)
	defer tp.Close()

	const attempts = 3
	for i := 1; i <= attempts; i++ {
		resp, err := tp.Predict(context.Background(), providers.PredictionRequest{
			Messages: []types.Message{{Role: "user", Content: openAIReasoningPrompt}},
		})
		skipOnOrgGate(t, err)
		if err != nil {
			t.Fatalf("Predict: %v", err)
		}
		if resp.Reasoning != nil && resp.Reasoning.Text != "" {
			if strings.Contains(resp.Content, resp.Reasoning.Text) {
				t.Fatalf("reasoning leaked into content: %q", resp.Content)
			}
			t.Logf("non-streaming reasoning %d chars", len(resp.Reasoning.Text))
			return
		}
	}
	t.Fatalf("no reasoning captured across %d attempts", attempts)
}

// TestOpenAI_Reasoning_Live_Tools covers the non-streaming tools path.
func TestOpenAI_Reasoning_Live_Tools(t *testing.T) {
	tp := newOpenAIThinkingTool(t)
	defer tp.Close()
	tools := openAIWeatherTool(t, tp)

	const attempts = 3
	for i := 1; i <= attempts; i++ {
		resp, _, err := tp.PredictWithTools(context.Background(), providers.PredictionRequest{
			Messages: []types.Message{{Role: "user", Content: openAIToolPrompt}},
		}, tools, "auto")
		skipOnOrgGate(t, err)
		if err != nil {
			t.Fatalf("PredictWithTools: %v", err)
		}
		if resp.Reasoning != nil && resp.Reasoning.Text != "" {
			t.Logf("tools reasoning %d chars", len(resp.Reasoning.Text))
			return
		}
	}
	t.Fatalf("no reasoning captured across %d attempts", attempts)
}

// TestOpenAI_Reasoning_Live_StreamingTools covers the streaming tools path.
func TestOpenAI_Reasoning_Live_StreamingTools(t *testing.T) {
	tp := newOpenAIThinkingTool(t)
	defer tp.Close()
	tools := openAIWeatherTool(t, tp)

	const attempts = 3
	for i := 1; i <= attempts; i++ {
		ch, err := tp.PredictStreamWithTools(context.Background(), providers.PredictionRequest{
			Messages: []types.Message{{Role: "user", Content: openAIToolPrompt}},
		}, tools, "auto")
		skipOnOrgGate(t, err)
		if err != nil {
			t.Fatalf("PredictStreamWithTools: %v", err)
		}
		var reasoning strings.Builder
		for chunk := range ch {
			reasoning.WriteString(chunk.Reasoning)
		}
		if reasoning.Len() > 0 {
			t.Logf("streaming-tools reasoning %d chars", reasoning.Len())
			return
		}
	}
	t.Fatalf("no reasoning captured across %d attempts", attempts)
}
