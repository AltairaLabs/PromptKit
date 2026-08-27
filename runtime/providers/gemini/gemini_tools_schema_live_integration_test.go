//go:build integration

package gemini

import (
	"context"
	"encoding/json"
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/AltairaLabs/PromptKit/runtime/providers"
	"github.com/AltairaLabs/PromptKit/runtime/types"
)

// TestGemini_ToolLoop_HonorsResponseFormat_Live records what Gemini actually
// permits, which is NOT what Claude permits.
//
// The all-paths wire matrix flagged that Gemini's tool builder never applied
// ResponseFormat. Sending it there looked like the obvious fix and would have
// been a regression: Gemini returns HTTP 400 "Function calling with a response
// mime type: 'application/json' is unsupported". So a tool-carrying round must
// NOT send the schema, and the provider warns instead of dropping it silently.
//
// This test pins both halves: tool use keeps working with a schema configured,
// and a round without tools is genuinely constrained by it.
func TestGemini_ToolLoop_HonorsResponseFormat_Live(t *testing.T) {
	if os.Getenv("GEMINI_API_KEY") == "" {
		t.Skip("GEMINI_API_KEY not set")
	}
	model := os.Getenv("GEMINI_MODEL")
	if model == "" {
		model = "gemini-2.5-flash"
	}

	tp := NewToolProvider("gemini-schema", model,
		"https://generativelanguage.googleapis.com/v1beta",
		providers.ProviderDefaults{MaxTokens: 2048}, false)
	defer func() { _ = tp.Close() }()

	tools, err := tp.BuildTooling([]*providers.ToolDescriptor{{
		Name:        "get_temperature",
		Description: "Get the current temperature for a city",
		InputSchema: json.RawMessage(
			`{"type":"object","properties":{"city":{"type":"string"}},"required":["city"]}`),
	}})
	require.NoError(t, err)

	rf := &providers.ResponseFormat{
		Type: providers.ResponseFormatJSONSchema,
		JSONSchema: json.RawMessage(`{"type":"object","properties":{"city":{"type":"string"},` +
			`"celsius":{"type":"number"}},"required":["city","celsius"]}`),
	}

	msgs := []types.Message{{
		Role:    "user",
		Content: "What's the temperature in Bristol? Use the tool, then answer.",
	}}

	// Round 1 WITH tools and a schema configured: must still succeed. Before
	// the guard, this returned 400 and broke the whole turn.
	resp, calls, err := tp.PredictWithTools(context.Background(),
		providers.PredictionRequest{Messages: msgs, MaxTokens: 2048, ResponseFormat: rf}, tools, "auto")
	require.NoError(t, err,
		"a configured response schema must not break tool use — the provider is "+
			"expected to drop the schema for tool-carrying rounds, not send it")
	require.NotEmpty(t, calls, "model did not call the tool; the premise is untested")
	t.Logf("round 1 (tools, schema dropped): %d tool call(s), content=%.60q", len(calls), resp.Content)

	msgs = append(msgs,
		types.Message{Role: roleAssistant, ToolCalls: calls},
		types.NewToolResultMessage(types.NewTextToolResult(
			calls[0].ID, calls[0].Name, `{"celsius": 21}`)),
	)

	// Round 2 WITHOUT tools: no conflict, so the schema applies and the answer
	// must conform. This is the path a final answer-only round takes.
	final, _, err := tp.PredictWithTools(context.Background(),
		providers.PredictionRequest{Messages: msgs, MaxTokens: 2048, ResponseFormat: rf}, nil, "")
	require.NoError(t, err)
	t.Logf("round 2 (no tools, schema applied): content=%.200q", final.Content)

	require.NotEmpty(t, final.Content, "final round produced no content")

	var out struct {
		City    string   `json:"city"`
		Celsius *float64 `json:"celsius"`
	}
	require.NoErrorf(t, json.Unmarshal([]byte(final.Content), &out),
		"final answer is not JSON, so the caller's schema was not applied: %q", final.Content)
	assert.NotEmpty(t, out.City, "schema requires city")
	require.NotNil(t, out.Celsius, "schema requires celsius")
	assert.InDelta(t, 21, *out.Celsius, 0.001, "answer should reflect the tool result")
}

// TestGemini3_ToolRound_HonorsSchema_Live is the other half: Gemini 3 DOES
// accept a response schema alongside tools and honors it, so it must not be
// dropped there.
//
// Getting this wrong in either direction is costly. Sending the schema to
// Gemini 2.5 with tools fails the whole request with HTTP 400; dropping it for
// Gemini 3 silently discards a capability the model has. rejectsSchemaWithTools
// draws that line, and these two tests pin both sides of it against the live
// API rather than against a doc.
func TestGemini3_ToolRound_HonorsSchema_Live(t *testing.T) {
	if os.Getenv("GEMINI_API_KEY") == "" {
		t.Skip("GEMINI_API_KEY not set")
	}
	model := os.Getenv("GEMINI_3_MODEL")
	if model == "" {
		model = "gemini-3.7-flash"
	}
	require.Falsef(t, rejectsSchemaWithTools(model),
		"%s is expected to accept a schema with tools; adjust the test model", model)

	tp := NewToolProvider("gemini3-schema", model,
		"https://generativelanguage.googleapis.com/v1beta",
		providers.ProviderDefaults{MaxTokens: 4096}, false)
	defer func() { _ = tp.Close() }()

	tools, err := tp.BuildTooling([]*providers.ToolDescriptor{{
		Name:        "get_temperature",
		Description: "Get the current temperature for a city",
		InputSchema: json.RawMessage(
			`{"type":"object","properties":{"city":{"type":"string"}},"required":["city"]}`),
	}})
	require.NoError(t, err)

	rf := &providers.ResponseFormat{
		Type: providers.ResponseFormatJSONSchema,
		JSONSchema: json.RawMessage(`{"type":"object","properties":{"city":{"type":"string"},` +
			`"celsius":{"type":"number"}},"required":["city","celsius"]}`),
	}

	// The request carries BOTH tools and the schema. On Gemini 2.5 this would
	// be a 400; on Gemini 3 it must succeed.
	_, calls, err := tp.PredictWithTools(context.Background(), providers.PredictionRequest{
		Messages:       []types.Message{{Role: "user", Content: "Temperature in Bristol? Use the tool, then answer."}},
		MaxTokens:      4096,
		ResponseFormat: rf,
	}, tools, "auto")
	require.NoErrorf(t, err,
		"%s rejected tools+schema together. If this generation now behaves like 2.5, "+
			"add it to rejectsSchemaWithTools", model)
	require.NotEmpty(t, calls, "model did not call the tool; the premise is untested")
	t.Logf("%s: tools+schema accepted, %d tool call(s)", model, len(calls))
}
