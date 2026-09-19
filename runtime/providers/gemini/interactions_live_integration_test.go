//go:build integration

package gemini

import (
	"context"
	"encoding/json"
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/AltairaLabs/PromptKit/runtime/v2/providers"
	"github.com/AltairaLabs/PromptKit/runtime/v2/types"
)

// TestGemini_Interactions_ToolLoopReturnsJSON_Live is the end-to-end proof for
// #1851: a full tool loop whose FINAL answer conforms to the caller's schema.
//
// generateContent cannot do this. It applies the schema to every turn, so
// Gemini 2.5 rejects the request and Gemini 3 keeps calling the tool and never
// answers. The Interactions API constrains only the answering turn.
//
// Run:
//
//	GEMINI_API_KEY=... go test -tags integration ./runtime/providers/gemini/ \
//	    -run TestGemini_Interactions_ToolLoopReturnsJSON_Live -v
func TestGemini_Interactions_ToolLoopReturnsJSON_Live(t *testing.T) {
	if os.Getenv("GEMINI_API_KEY") == "" {
		t.Skip("GEMINI_API_KEY not set")
	}

	// Only generations that honor response_format on this API are exercised
	// for conformance. Gemini 2.5 accepts the field and ignores it — verified
	// live with no tools involved — so it stays on generateContent and is
	// covered by the routing assertion below rather than by a conformance
	// expectation it cannot meet.
	models := []string{"gemini-3.7-flash"}
	if m := os.Getenv("GEMINI_INTERACTIONS_MODELS"); m != "" {
		models = []string{m}
	}

	for _, model := range models {
		t.Run(model, func(t *testing.T) {
			tp := NewToolProvider("gemini-interactions", model,
				"https://generativelanguage.googleapis.com/v1beta",
				providers.ProviderDefaults{MaxTokens: 4096}, false)
			defer func() { _ = tp.Close() }()

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

			// The schema is set and tools are present, so this must route to
			// interactions rather than generateContent.
			require.Equal(t, APIModeInteractions, tp.resolveAPIMode(req.ResponseFormat, true),
				"a schema alongside tools must route to the Interactions API")

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
					assert.InDelta(t, 21, *out.Celsius, 0.001,
						"answer should reflect the tool result")
					return
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
		})
	}
}

// TestGemini_InteractionsRouting_Live pins both sides of the routing decision
// against the live API, so neither half can rot silently.
//
// Routing 2.5 to the Interactions API would be a regression dressed as a fix:
// it accepts response_format and ignores it, so the caller would still get
// prose while the request quietly moved to a different API.
func TestGemini_InteractionsRouting_Live(t *testing.T) {
	if os.Getenv("GEMINI_API_KEY") == "" {
		t.Skip("GEMINI_API_KEY not set")
	}

	rf := &providers.ResponseFormat{
		Type:       providers.ResponseFormatJSONSchema,
		JSONSchema: json.RawMessage(`{"type":"object","properties":{"a":{"type":"string"}}}`),
	}

	cases := []struct {
		model string
		want  APIMode
		why   string
	}{
		{"gemini-3.7-flash", APIModeInteractions, "honors response_format on interactions"},
		{"gemini-2.5-flash", APIModeGenerateContent, "ignores response_format on interactions"},
		{"gemini-2.5-pro", APIModeGenerateContent, "same generation as 2.5-flash"},
	}

	for _, tc := range cases {
		t.Run(tc.model, func(t *testing.T) {
			tp := NewToolProvider("route", tc.model,
				"https://generativelanguage.googleapis.com/v1beta",
				providers.ProviderDefaults{MaxTokens: 512}, false)
			defer func() { _ = tp.Close() }()

			assert.Equalf(t, tc.want, tp.resolveAPIMode(rf, true),
				"%s: %s", tc.model, tc.why)

			// Without tools there is no conflict to route around, so every
			// model stays on generateContent.
			assert.Equal(t, APIModeGenerateContent, tp.resolveAPIMode(rf, false),
				"a tool-free round has no reason to change API")

			// Explicit config always wins, whatever the model.
			tp.apiMode = APIModeInteractions
			assert.Equal(t, APIModeInteractions, tp.resolveAPIMode(nil, false),
				"explicit api_mode must override the automatic choice")
		})
	}
}

// TestGemini_InteractionsStreaming_ToolLoopReturnsJSON_Live is the end-to-end
// proof for the STREAMING path, which is the one that matters: the pipeline
// streams by default, so a fix that only covered Predict would leave real
// traffic unconstrained.
//
// Drives a real streaming tool loop and asserts the final streamed answer
// conforms to the caller's schema.
func TestGemini_InteractionsStreaming_ToolLoopReturnsJSON_Live(t *testing.T) {
	if os.Getenv("GEMINI_API_KEY") == "" {
		t.Skip("GEMINI_API_KEY not set")
	}
	model := os.Getenv("GEMINI_3_MODEL")
	if model == "" {
		model = "gemini-3.7-flash"
	}

	tp := NewToolProvider("gemini-int-stream", model,
		"https://generativelanguage.googleapis.com/v1beta",
		providers.ProviderDefaults{MaxTokens: 4096}, false)
	defer func() { _ = tp.Close() }()

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
			assert.InDelta(t, 21, *out.Celsius, 0.001, "answer should reflect the tool result")
			return
		}

		t.Logf("%s round %d: %d streamed tool call(s), %d thought signature(s)",
			model, round, len(calls), len(opaque))
		require.NotEmptyf(t, opaque,
			"round %d streamed no thought signature; the next round's history would be rejected", round)

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
}
