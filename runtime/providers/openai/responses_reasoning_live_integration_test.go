//go:build integration

package openai

import (
	"context"
	"os"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/AltairaLabs/PromptKit/runtime/providers"
	"github.com/AltairaLabs/PromptKit/runtime/types"
)

// TestOpenAI_ResponsesStreamingReasoning_Live proves the streaming Responses
// path surfaces reasoning against the real API.
//
// o-series models send NO response.reasoning_summary_text.delta events; the
// summary arrives only on the completed reasoning output item. Listening for
// the delta alone produced no reasoning at all, silently, on every streaming
// turn — and the pipeline always streams. A canned-body test cannot catch a
// wrong assumption about which events the vendor actually sends, so this asks
// the vendor.
//
// Run:
//
//	OPENAI_API_KEY=... go test -tags integration ./runtime/providers/openai/ \
//	    -run TestOpenAI_ResponsesStreamingReasoning_Live -v
func TestOpenAI_ResponsesStreamingReasoning_Live(t *testing.T) {
	key := os.Getenv("OPENAI_API_KEY")
	if key == "" {
		t.Skip("OPENAI_API_KEY not set")
	}
	model := os.Getenv("OPENAI_REASONING_MODEL")
	if model == "" {
		model = "o4-mini"
	}

	p, err := providers.CreateProviderFromSpec(providers.ProviderSpec{
		ID: "openai-live", Type: "openai", Model: model,
		BaseURL:  "https://api.openai.com/v1",
		Defaults: providers.ProviderDefaults{MaxTokens: 4096},
		AdditionalConfig: map[string]interface{}{
			"api_mode":          "responses",
			"reasoning_effort":  "medium",
			"reasoning_summary": "auto",
		},
	})
	require.NoError(t, err)
	defer func() { _ = p.Close() }()

	ch, err := p.PredictStream(context.Background(), providers.PredictionRequest{
		Messages: []types.Message{{
			Role: "user",
			Content: "A bat and a ball cost $1.10 in total. The bat costs $1.00 more " +
				"than the ball. How much does the ball cost?",
		}},
	})
	require.NoError(t, err)

	var reasoning, content strings.Builder
	for chunk := range ch {
		require.NoError(t, chunk.Error)
		reasoning.WriteString(chunk.Reasoning)
		if chunk.Delta != "" {
			content.WriteString(chunk.Delta)
		}
	}

	t.Logf("live streaming reasoning: %d chars; content: %.80q", reasoning.Len(), content.String())

	require.NotEmpty(t, reasoning.String(),
		"live streaming Responses returned no reasoning. Either the org cannot request "+
			"summaries, or the event this repo reads is not the one the vendor sends")
	assert.NotContains(t, content.String(), reasoning.String(),
		"reasoning leaked into spoken content")
}

// TestOpenAI_ResponsesToolRound_ReasoningIsEmpty_Live records a VENDOR
// behavior, so it is not rediscovered as a bug in this repo.
//
// When tools are present, o-series returns a reasoning item with an EMPTY
// summary — no reasoning text on tool-calling rounds, which is precisely the
// round a transcript most wants ("why did it call that?"). Claude and Gemini
// both supply it. If OpenAI starts supplying it, this test fails and tells us
// the limitation has lifted.
func TestOpenAI_ResponsesToolRound_ReasoningIsEmpty_Live(t *testing.T) {
	key := os.Getenv("OPENAI_API_KEY")
	if key == "" {
		t.Skip("OPENAI_API_KEY not set")
	}
	model := os.Getenv("OPENAI_REASONING_MODEL")
	if model == "" {
		model = "o4-mini"
	}

	tp := NewToolProvider("openai-live", model, "https://api.openai.com/v1",
		providers.ProviderDefaults{MaxTokens: 4096}, false,
		map[string]any{
			"api_mode":          "responses",
			"reasoning_effort":  "medium",
			"reasoning_summary": "auto",
		}, nil)
	defer func() { _ = tp.Close() }()

	tools, err := tp.BuildTooling([]*providers.ToolDescriptor{{
		Name:        "get_temperature",
		Description: "Get the current temperature for a city",
		InputSchema: []byte(`{"type":"object","properties":{"city":{"type":"string"}},"required":["city"]}`),
	}})
	require.NoError(t, err)

	ch, err := tp.PredictStreamWithTools(context.Background(), providers.PredictionRequest{
		Messages: []types.Message{{Role: "user", Content: "What's the temperature in Bristol? Use the tool."}},
	}, tools, "auto")
	require.NoError(t, err)

	var reasoning strings.Builder
	var sawToolCall bool
	for chunk := range ch {
		require.NoError(t, chunk.Error)
		reasoning.WriteString(chunk.Reasoning)
		if len(chunk.ToolCalls) > 0 {
			sawToolCall = true
		}
	}

	require.True(t, sawToolCall, "model did not call the tool; the premise is untested")
	assert.Emptyf(t, reasoning.String(),
		"OpenAI now returns reasoning on tool-calling rounds (%d chars). That is good news — "+
			"remove this expectation and treat OpenAI like claude/gemini in the "+
			"multi-provider live test", reasoning.Len())
}
