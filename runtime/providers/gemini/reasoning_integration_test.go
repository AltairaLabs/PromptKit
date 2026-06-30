//go:build integration

package gemini

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

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

// newGeminiThinkingProvider builds a thinking-enabled Gemini tool provider.
func newGeminiThinkingProvider(t *testing.T) *ToolProvider {
	t.Helper()
	if os.Getenv("GEMINI_API_KEY") == "" {
		t.Skip("GEMINI_API_KEY not set")
	}
	tp := NewToolProvider("gemini-reasoning", "gemini-2.5-flash",
		"https://generativelanguage.googleapis.com/v1beta",
		providers.ProviderDefaults{Temperature: 0, MaxTokens: 2048, TopP: 1.0}, false)
	applyThinkingConfig(tp.Provider, providers.ProviderSpec{
		AdditionalConfig: map[string]any{"include_thoughts": true, "thinking_budget": 1024},
	})
	return tp
}

const geminiReasoningPrompt = "A bat and a ball cost $1.10 in total. The bat costs $1.00 more " +
	"than the ball. How much does the ball cost?"

// TestGemini_Reasoning_Live_NonStreaming covers the non-streaming Predict path.
func TestGemini_Reasoning_Live_NonStreaming(t *testing.T) {
	tp := newGeminiThinkingProvider(t)
	defer tp.Close()

	resp, err := tp.Predict(context.Background(), providers.PredictionRequest{
		Messages: []types.Message{{Role: "user", Content: geminiReasoningPrompt}},
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
	t.Logf("non-streaming reasoning %d chars; content=%q", len(resp.Reasoning.Text), resp.Content)
}

// weatherTool is a simple tool the model can call while reasoning.
func geminiWeatherTool(t *testing.T, tp *ToolProvider) providers.ProviderTools {
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

const geminiToolPrompt = "Work out the capital of France, then call get_weather for that city."

// TestGemini_Reasoning_Live_Tools covers the non-streaming tools path
// (parseToolResponse) — the path that was previously dropping reasoning.
func TestGemini_Reasoning_Live_Tools(t *testing.T) {
	tp := newGeminiThinkingProvider(t)
	defer tp.Close()

	resp, _, err := tp.PredictWithTools(context.Background(), providers.PredictionRequest{
		Messages: []types.Message{{Role: "user", Content: geminiToolPrompt}},
	}, geminiWeatherTool(t, tp), "auto")
	if err != nil {
		t.Fatalf("PredictWithTools: %v", err)
	}
	if resp.Reasoning == nil || resp.Reasoning.Text == "" {
		t.Fatalf("expected reasoning on the tools response; got none (content=%q)", resp.Content)
	}
	if strings.Contains(resp.Content, resp.Reasoning.Text) {
		t.Fatalf("reasoning leaked into content: %q", resp.Content)
	}
	t.Logf("tools reasoning %d chars", len(resp.Reasoning.Text))
}

// TestGemini_Reasoning_Live_StreamingTools covers the streaming tools path.
func TestGemini_Reasoning_Live_StreamingTools(t *testing.T) {
	tp := newGeminiThinkingProvider(t)
	defer tp.Close()

	ch, err := tp.PredictStreamWithTools(context.Background(), providers.PredictionRequest{
		Messages: []types.Message{{Role: "user", Content: geminiToolPrompt}},
	}, geminiWeatherTool(t, tp), "auto")
	if err != nil {
		t.Fatalf("PredictStreamWithTools: %v", err)
	}
	var reasoning, content strings.Builder
	for chunk := range ch {
		reasoning.WriteString(chunk.Reasoning)
		content.WriteString(chunk.Delta)
	}
	if reasoning.Len() == 0 {
		t.Fatalf("expected reasoning on the streaming-tools path; got none (content=%q)", content.String())
	}
	t.Logf("streaming-tools reasoning %d chars", reasoning.Len())
}

// TestGemini_Reasoning_Live_Realtime covers the Gemini Live (duplex) path: a
// native-audio thinking model emits thought parts that the stream session routes
// to StreamChunk.Reasoning, separate from the spoken transcript. Live sessions are
// best-effort, so it retries.
func TestGemini_Reasoning_Live_Realtime(t *testing.T) {
	if os.Getenv("GEMINI_API_KEY") == "" {
		t.Skip("GEMINI_API_KEY not set")
	}
	model := os.Getenv("GEMINI_REALTIME_MODEL")
	if model == "" {
		model = "gemini-2.5-flash-native-audio-preview-12-2025"
	}

	provider := NewProvider(model, model, "https://generativelanguage.googleapis.com/v1beta",
		providers.ProviderDefaults{Temperature: 0.7}, false)

	const attempts = 2
	for attempt := 1; attempt <= attempts; attempt++ {
		ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)

		session, err := provider.CreateStreamSession(ctx, &providers.StreamingInputConfig{
			Config: types.StreamingMediaConfig{
				Type: types.ContentTypeAudio, ChunkSize: 3200, SampleRate: 16000,
				Channels: 1, BitDepth: 16, Encoding: "pcm_linear16",
			},
			Metadata: map[string]interface{}{"response_modalities": []string{"AUDIO"}},
		})
		if err != nil {
			cancel()
			t.Fatalf("CreateStreamSession: %v", err)
		}

		if err := session.SendText(ctx, "Reason step by step: a bat and ball cost $1.10, "+
			"the bat is $1 more than the ball. How much is the ball?"); err != nil {
			session.Close()
			cancel()
			t.Fatalf("SendText: %v", err)
		}

		var reasoning, content strings.Builder
		timeout := time.After(30 * time.Second)
	collect:
		for {
			select {
			case chunk, ok := <-session.Response():
				if !ok {
					break collect
				}
				reasoning.WriteString(chunk.Reasoning)
				content.WriteString(chunk.Delta)
				if chunk.FinishReason != nil {
					break collect
				}
			case <-timeout:
				break collect
			case <-ctx.Done():
				break collect
			}
		}
		session.Close()
		cancel()

		if reasoning.Len() > 0 {
			if strings.Contains(content.String(), reasoning.String()) {
				t.Fatalf("realtime reasoning leaked into transcript: %q", content.String())
			}
			t.Logf("realtime reasoning %d chars; transcript=%q", reasoning.Len(), content.String())
			return
		}
		t.Logf("attempt %d/%d: no realtime reasoning emitted, retrying", attempt, attempts)
	}
	t.Fatalf("no realtime reasoning captured across %d attempts", attempts)
}
