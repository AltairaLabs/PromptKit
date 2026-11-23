//go:build integration

package gemini

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/AltairaLabs/PromptKit/runtime/v2/providers"
	"github.com/AltairaLabs/PromptKit/runtime/v2/types"
)

// TestGemini_Interactions_ToolLoopReturnsJSON_Live is the end-to-end proof for
// #1851 and #2771: a full tool loop whose FINAL answer conforms to the
// caller's schema, with no api_mode (generateContent) and with the
// Interactions API pinned.
//
// Neither API can apply the schema to a round that may call tools — on either
// one the model keeps calling the tool and never answers — so the provider
// sends tool rounds without it and asks for the closing answer again under it
// (see schema_with_tools.go).
//
// Run:
//
//	GEMINI_API_KEY=... go test -tags integration ./runtime/providers/gemini/ \
//	    -run TestGemini_Interactions_ToolLoopReturnsJSON_Live -v
func TestGemini_Interactions_ToolLoopReturnsJSON_Live(t *testing.T) {
	if os.Getenv("GEMINI_API_KEY") == "" {
		t.Skip("GEMINI_API_KEY not set")
	}

	models := []string{"gemini-3.7-flash"}
	if m := os.Getenv("GEMINI_INTERACTIONS_MODELS"); m != "" {
		models = []string{m}
	}

	for _, model := range models {
		for _, mode := range []APIMode{APIModeGenerateContent, APIModeInteractions} {
			t.Run(model+"/"+string(mode), func(t *testing.T) {
				tp := NewToolProvider("gemini-interactions", model,
					"https://generativelanguage.googleapis.com/v1beta",
					providers.ProviderDefaults{MaxTokens: 4096}, false)
				defer func() { _ = tp.Close() }()
				tp.apiMode = mode
				celsius := toolLoopReturnsJSON(t, tp, model)
				assert.InDelta(t, 21, celsius, 0.001, "answer should reflect the tool result")
			})
		}
	}
}

// toolLoopReturnsJSON runs the loop to its final answer, requires that answer
// to conform to the schema, and returns its celsius.
func toolLoopReturnsJSON(t *testing.T, tp *ToolProvider, model string) float64 {
	tools, err := tp.BuildTooling([]*providers.ToolDescriptor{{
		Name:        "get_temperature",
		Description: "Get the current temperature for a city in Celsius",
		InputSchema: json.RawMessage(
			`{"type":"object","properties":{"city":{"type":"string"}},"required":["city"]}`),
	}})
	require.NoError(t, err)

	req := providers.PredictionRequest{
		Messages: []types.Message{{
			Role:    "user",
			Content: "What's the temperature in Bristol? Use the tool, then answer.",
		}},
		MaxTokens: 4096,
		ResponseFormat: &providers.ResponseFormat{
			Type: providers.ResponseFormatJSONSchema,
			JSONSchema: json.RawMessage(`{"type":"object","properties":{` +
				`"city":{"type":"string"},"celsius":{"type":"number"}},` +
				`"required":["city","celsius"],"additionalProperties":false}`),
		},
	}

	const maxRounds = 4
	for round := 1; round <= maxRounds; round++ {
		resp, calls, cErr := tp.PredictWithTools(context.Background(), req, tools, "auto")
		require.NoErrorf(t, cErr, "%s round %d", model, round)

		if len(calls) == 0 {
			t.Logf("%s: answered on round %d: %.90q", model, round, resp.Content)

			var out struct {
				City    *string  `json:"city"`
				Celsius *float64 `json:"celsius"`
			}
			require.NoErrorf(t, json.Unmarshal([]byte(resp.Content), &out),
				"%s: final answer is not JSON, so the schema did not apply: %q",
				model, resp.Content)
			require.NotNil(t, out.City, "schema requires city")
			require.NotNil(t, out.Celsius, "schema requires celsius")
			return *out.Celsius
		}

		t.Logf("%s round %d: %d tool call(s)", model, round, len(calls))
		// Carry Reasoning forward, as the runtime's tool loop does when
		// it appends the round's response. The Interactions API refuses
		// a history whose function_call has no preceding thought, so
		// dropping it here would fail the next round.
		req.Messages = append(req.Messages, types.Message{
			Role:      roleAssistant,
			ToolCalls: calls,
			Reasoning: resp.Reasoning,
		})
		for i := range calls {
			req.Messages = append(req.Messages, types.NewToolResultMessage(
				types.NewTextToolResult(calls[i].ID, calls[i].Name, `{"celsius": 21}`)))
		}
	}
	t.Fatalf("%s: never produced a final answer in %d rounds", model, maxRounds)
	return 0
}

// TestGemini_InteractionsStreaming_ToolLoopReturnsJSON_Live is the end-to-end
// proof for the STREAMING path, which is the one that matters: the pipeline
// streams by default, so a fix that only covered Predict would leave real
// traffic unconstrained.
//
// Drives a real streaming tool loop, with no api_mode and with the
// Interactions API pinned, and asserts the final streamed answer conforms to
// the caller's schema.
func TestGemini_InteractionsStreaming_ToolLoopReturnsJSON_Live(t *testing.T) {
	if os.Getenv("GEMINI_API_KEY") == "" {
		t.Skip("GEMINI_API_KEY not set")
	}
	model := os.Getenv("GEMINI_3_MODEL")
	if model == "" {
		model = "gemini-3.7-flash"
	}

	for _, mode := range []APIMode{APIModeGenerateContent, APIModeInteractions} {
		t.Run(string(mode), func(t *testing.T) {
			tp := NewToolProvider("gemini-int-stream", model,
				"https://generativelanguage.googleapis.com/v1beta",
				providers.ProviderDefaults{MaxTokens: 4096}, false)
			defer func() { _ = tp.Close() }()
			tp.apiMode = mode
			celsius := streamingToolLoopReturnsJSON(t, tp, model)
			assert.InDelta(t, 21, celsius, 0.001, "answer should reflect the tool result")
		})
	}
}

// streamingToolLoopReturnsJSON streams the loop to its final answer, requires
// that answer to conform to the schema, and returns its celsius.
func streamingToolLoopReturnsJSON(t *testing.T, tp *ToolProvider, model string) float64 {
	tools, err := tp.BuildTooling([]*providers.ToolDescriptor{{
		Name:        "get_temperature",
		Description: "Get the current temperature for a city in Celsius",
		InputSchema: json.RawMessage(
			`{"type":"object","properties":{"city":{"type":"string"}},"required":["city"]}`),
	}})
	require.NoError(t, err)

	req := providers.PredictionRequest{
		Messages: []types.Message{{
			Role:    "user",
			Content: "What's the temperature in Bristol? Use the tool, then answer.",
		}},
		MaxTokens: 4096,
		ResponseFormat: &providers.ResponseFormat{
			Type: providers.ResponseFormatJSONSchema,
			JSONSchema: json.RawMessage(`{"type":"object","properties":{` +
				`"city":{"type":"string"},"celsius":{"type":"number"}},` +
				`"required":["city","celsius"],"additionalProperties":false}`),
		},
	}

	const maxRounds = 4
	for round := 1; round <= maxRounds; round++ {
		ch, sErr := tp.PredictStreamWithTools(context.Background(), req, tools, "auto")
		require.NoErrorf(t, sErr, "round %d", round)

		var content string
		var calls []types.MessageToolCall
		var opaque []types.OpaqueReasoning
		for chunk := range ch {
			require.NoError(t, chunk.Error)
			if chunk.Content != "" {
				content = chunk.Content
			}
			if len(chunk.ToolCalls) > 0 {
				calls = chunk.ToolCalls
			}
			opaque = append(opaque, chunk.OpaqueReasoning...)
		}

		if len(calls) == 0 {
			t.Logf("%s: streamed answer on round %d: %.90q", model, round, content)

			var out struct {
				City    *string  `json:"city"`
				Celsius *float64 `json:"celsius"`
			}
			require.NoErrorf(t, json.Unmarshal([]byte(content), &out),
				"streamed answer is not JSON, so the schema did not apply: %q", content)
			require.NotNil(t, out.City, "schema requires city")
			require.NotNil(t, out.Celsius, "schema requires celsius")
			return *out.Celsius
		}

		t.Logf("%s round %d: %d streamed tool call(s), %d thought signature(s)",
			model, round, len(calls), len(opaque))
		if tp.apiMode == APIModeInteractions {
			// generateContent carries its signatures on the tool calls instead.
			require.NotEmptyf(t, opaque,
				"round %d streamed no thought signature; the next round's history would be rejected", round)
		}

		// Carry the signatures forward exactly as the runtime's tool loop does
		// when it accumulates a round's reasoning onto the response message.
		req.Messages = append(req.Messages, types.Message{
			Role:      roleAssistant,
			ToolCalls: calls,
			Reasoning: &types.ReasoningTrace{Opaque: opaque},
		})
		for i := range calls {
			req.Messages = append(req.Messages, types.NewToolResultMessage(
				types.NewTextToolResult(calls[i].ID, calls[i].Name, `{"celsius": 21}`)))
		}
	}
	t.Fatalf("%s: streaming loop never produced a final answer in %d rounds", model, maxRounds)
	return 0
}

// TestGemini_ToolChoice_Live proves the tool choice reaches the model on both
// APIs, unary and streaming: naming get_status yields a get_status call even
// though get_weather is the obvious fit, and none yields no call.
//
//	GEMINI_API_KEY=... go test -tags integration ./runtime/providers/gemini/ \
//	    -run TestGemini_ToolChoice_Live -v
func TestGemini_ToolChoice_Live(t *testing.T) {
	if os.Getenv("GEMINI_API_KEY") == "" {
		t.Skip("GEMINI_API_KEY not set")
	}
	model := "gemini-3.7-flash"
	if m := os.Getenv("GEMINI_INTERACTIONS_MODELS"); m != "" {
		model = m
	}
	req := providers.PredictionRequest{
		Messages: []types.Message{{Role: roleUser, Content: "What is the weather in Paris right now?"}},
	}
	for _, mode := range []APIMode{APIModeGenerateContent, APIModeInteractions} {
		for _, tc := range []struct {
			choice   string
			wantCall string
		}{{"get_status", "get_status"}, {"none", ""}} {
			for _, stream := range []bool{false, true} {
				name := fmt.Sprintf("%s/%s/stream=%v", mode, tc.choice, stream)
				t.Run(name, func(t *testing.T) {
					tp := NewToolProvider("gemini-tool-choice", model,
						"https://generativelanguage.googleapis.com/v1beta",
						providers.ProviderDefaults{MaxTokens: 4096}, false)
					defer func() { _ = tp.Close() }()
					tp.apiMode = mode
					tools, err := tp.BuildTooling([]*providers.ToolDescriptor{
						{Name: "get_weather", Description: "Get the current weather for a city",
							InputSchema: json.RawMessage(`{"type":"object","properties":{"city":{"type":"string"}},"required":["city"]}`)},
						{Name: "get_status", Description: "Get the build status",
							InputSchema: json.RawMessage(`{"type":"object","properties":{}}`)},
					})
					require.NoError(t, err)

					var calls []types.MessageToolCall
					if stream {
						ch, err := tp.PredictStreamWithTools(context.Background(), req, tools, tc.choice)
						require.NoError(t, err)
						for chunk := range ch {
							require.NoError(t, chunk.Error)
							if len(chunk.ToolCalls) > 0 {
								calls = chunk.ToolCalls
							}
						}
					} else {
						_, calls, err = tp.PredictWithTools(context.Background(), req, tools, tc.choice)
						require.NoError(t, err)
					}

					if tc.wantCall == "" {
						assert.Empty(t, calls, "tool choice none must yield no call")
						return
					}
					require.NotEmpty(t, calls, "named tool choice must yield a call")
					assert.Equal(t, tc.wantCall, calls[0].Name)
					t.Logf("%s: called %s", name, calls[0].Name)
				})
			}
		}
	}
}

// TestGemini_Interactions_MaxTokens_Live proves the provider's max_tokens
// reaches the Interactions API: a 200-word story under a 400-token cap
// (thinking counts toward it) comes back cut short.
//
//	GEMINI_API_KEY=... go test -tags integration ./runtime/providers/gemini/ \
//	    -run TestGemini_Interactions_MaxTokens_Live -v
func TestGemini_Interactions_MaxTokens_Live(t *testing.T) {
	if os.Getenv("GEMINI_API_KEY") == "" {
		t.Skip("GEMINI_API_KEY not set")
	}
	model := "gemini-2.5-flash"
	if m := os.Getenv("GEMINI_INTERACTIONS_MODELS"); m != "" {
		model = m
	}
	tp := NewToolProvider("gemini-int-maxtokens", model,
		"https://generativelanguage.googleapis.com/v1beta",
		providers.ProviderDefaults{MaxTokens: 400}, false)
	defer func() { _ = tp.Close() }()
	tp.apiMode = APIModeInteractions
	tools, err := tp.BuildTooling([]*providers.ToolDescriptor{
		{Name: "get_status", Description: "Get the build status", InputSchema: json.RawMessage(`{"type":"object","properties":{}}`)},
	})
	require.NoError(t, err)

	resp, _, err := tp.PredictWithTools(context.Background(), providers.PredictionRequest{
		Messages: []types.Message{{Role: roleUser, Content: "Write a 200-word story about a lighthouse. Do not call any tool."}},
	}, tools, "none")
	require.NoError(t, err)
	words := len(strings.Fields(resp.Content))
	t.Logf("400-token cap: %d words, usage %+v", words, resp.CostInfo)
	assert.Less(t, words, 150, "the cap must cut a 200-word story short")
}

// interactionsLiveModel is the model the #2912 live tests run against:
// GEMINI_INTERACTIONS_MODELS, else the Gemini 3 flash the other live tests use.
func interactionsLiveModel() string {
	if m := os.Getenv("GEMINI_INTERACTIONS_MODELS"); m != "" {
		return m
	}
	return "gemini-3.7-flash"
}

// newInteractionsLiveProvider builds an api_mode interactions provider from a
// spec, as a host does.
func newInteractionsLiveProvider(t *testing.T, id string, maxTokens int, extra map[string]any) *ToolProvider {
	t.Helper()
	cfg := map[string]any{"api_mode": "interactions"}
	for k, v := range extra {
		cfg[k] = v
	}
	p, err := providers.CreateProviderFromSpec(providers.ProviderSpec{
		ID: id, Type: "gemini", Model: interactionsLiveModel(),
		BaseURL:          "https://generativelanguage.googleapis.com/v1beta",
		Defaults:         providers.ProviderDefaults{MaxTokens: maxTokens},
		AdditionalConfig: cfg, IncludeRawOutput: true,
	})
	require.NoError(t, err)
	t.Cleanup(func() { _ = p.Close() })
	return p.(*ToolProvider)
}

// liveInteractionsTurn runs one unary or streamed turn and returns its content,
// reasoning text and finish reason.
func liveInteractionsTurn(t *testing.T, tp *ToolProvider, prompt string, stream bool) (string, string, string) {
	t.Helper()
	req := providers.PredictionRequest{Messages: []types.Message{{Role: roleUser, Content: prompt}}}
	if !stream {
		resp, _, err := tp.PredictWithTools(context.Background(), req, nil, "")
		require.NoError(t, err)
		reasoning := ""
		if resp.Reasoning != nil {
			reasoning = resp.Reasoning.Text
		}
		return resp.Content, reasoning, resp.FinishReason
	}
	ch, err := tp.PredictStreamWithTools(context.Background(), req, nil, "")
	require.NoError(t, err)
	var content, finish string
	var reasoning strings.Builder
	for c := range ch {
		require.NoError(t, c.Error)
		content = c.Content
		reasoning.WriteString(c.Reasoning)
		if c.FinishReason != nil {
			finish = *c.FinishReason
		}
	}
	return content, reasoning.String(), finish
}

// TestGemini_Interactions_TruncationFinishReason_Live proves a reply the
// output cap cuts off reports max_output_tokens, unary and streaming, and an
// uncapped one stop (#2912).
//
//	GEMINI_API_KEY=... go test -tags integration ./runtime/providers/gemini/ \
//	    -run TestGemini_Interactions_TruncationFinishReason_Live -v
func TestGemini_Interactions_TruncationFinishReason_Live(t *testing.T) {
	if os.Getenv("GEMINI_API_KEY") == "" {
		t.Skip("GEMINI_API_KEY not set")
	}
	const prompt = "Write a 150-word story about a lighthouse."
	for _, stream := range []bool{false, true} {
		for rep := 1; rep <= 2; rep++ {
			t.Run(fmt.Sprintf("stream=%v/capped/%d", stream, rep), func(t *testing.T) {
				tp := newInteractionsLiveProvider(t, "gemini-int-trunc", 40, map[string]any{"thinking_level": "low"})
				content, _, finish := liveInteractionsTurn(t, tp, prompt, stream)
				t.Logf("finish=%q words=%d", finish, len(strings.Fields(content)))
				assert.Equal(t, types.FinishReasonMaxOutputTokens, finish)
			})
		}
		t.Run(fmt.Sprintf("stream=%v/uncapped", stream), func(t *testing.T) {
			tp := newInteractionsLiveProvider(t, "gemini-int-trunc", 4000, map[string]any{"thinking_level": "low"})
			content, _, finish := liveInteractionsTurn(t, tp, prompt, stream)
			t.Logf("finish=%q words=%d", finish, len(strings.Fields(content)))
			assert.Equal(t, types.FinishReasonStop, finish)
		})
	}
}

// TestGemini_Interactions_ThinkingLevel_Live proves thinking_level reaches the
// API and changes how much the model thinks (usage.total_thought_tokens from
// the raw response), and include_thoughts returns a thought summary as
// reasoning, unary and streaming (#2912).
//
//	GEMINI_API_KEY=... go test -tags integration ./runtime/providers/gemini/ \
//	    -run TestGemini_Interactions_ThinkingLevel_Live -v
func TestGemini_Interactions_ThinkingLevel_Live(t *testing.T) {
	if os.Getenv("GEMINI_API_KEY") == "" {
		t.Skip("GEMINI_API_KEY not set")
	}
	const prompt = "How many positive integers below 200 are divisible by 3 or 7 but not by both? " +
		"Answer with the number only."

	thoughtTokens := func(t *testing.T, level string) int {
		tp := newInteractionsLiveProvider(t, "gemini-int-level-"+level, 8000, map[string]any{"thinking_level": level})
		resp, _, err := tp.PredictWithTools(context.Background(), providers.PredictionRequest{
			Messages: []types.Message{{Role: roleUser, Content: prompt}},
		}, nil, "")
		require.NoError(t, err)
		sent, ok := resp.RawRequest.(interactionsRequest)
		require.True(t, ok, "raw request is %T", resp.RawRequest)
		assert.Equal(t, level, sent.Generation["thinking_level"])
		var raw interactionsResponse
		require.NoError(t, json.Unmarshal(resp.Raw, &raw))
		require.NotNil(t, raw.Usage)
		return raw.Usage.TotalThoughtTokens
	}
	for rep := 1; rep <= 2; rep++ {
		t.Run(fmt.Sprintf("low vs high/%d", rep), func(t *testing.T) {
			low, high := thoughtTokens(t, "low"), thoughtTokens(t, "high")
			t.Logf("thought tokens: low=%d high=%d", low, high)
			assert.Greater(t, high, low, "thinking_level high must think more than low")
		})
	}

	for _, stream := range []bool{false, true} {
		for rep := 1; rep <= 2; rep++ {
			t.Run(fmt.Sprintf("include_thoughts/stream=%v/%d", stream, rep), func(t *testing.T) {
				tp := newInteractionsLiveProvider(t, "gemini-int-summaries", 8000,
					map[string]any{"thinking_level": "high", "include_thoughts": true})
				content, reasoning, finish := liveInteractionsTurn(t, tp, prompt, stream)
				t.Logf("answer=%d chars reasoning=%d chars finish=%q", len(content), len(reasoning), finish)
				assert.NotEmpty(t, reasoning, "include_thoughts must return a thought summary")
				assert.Equal(t, types.FinishReasonStop, finish)
			})
		}
	}
}
