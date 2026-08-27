//go:build integration

package claude

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

// TestClaude_ToolLoop_HonorsResponseFormat_Live is the end-to-end proof for
// issue #1848: a caller sets a JSON schema, the conversation runs a tool loop,
// and the FINAL answer conforms to the schema.
//
// This is the shape a `mode: function` agent depends on — its contract is "the
// response conforms to this schema". Before the fix the constraint never
// reached the API on tool-using turns, so the agent received prose and rejected
// its own model's perfectly reasonable answer.
//
// Run:
//
//	ANTHROPIC_API_KEY=... go test -tags integration ./runtime/providers/claude/ \
//	    -run TestClaude_ToolLoop_HonorsResponseFormat_Live -v
func TestClaude_ToolLoop_HonorsResponseFormat_Live(t *testing.T) {
	key := os.Getenv("ANTHROPIC_API_KEY")
	if key == "" {
		key = os.Getenv("CLAUDE_API_KEY")
	}
	if key == "" {
		t.Skip("ANTHROPIC_API_KEY not set")
	}
	model := os.Getenv("CLAUDE_MODEL")
	if model == "" {
		model = "claude-sonnet-4-6"
	}

	tp := NewToolProvider("claude-schema", model, "https://api.anthropic.com/v1",
		providers.ProviderDefaults{MaxTokens: 2048}, false)
	defer func() { _ = tp.Close() }()

	tools, err := tp.BuildTooling([]*providers.ToolDescriptor{{
		Name:        "get_temperature",
		Description: "Get the current temperature for a city",
		InputSchema: json.RawMessage(
			`{"type":"object","properties":{"city":{"type":"string"}},"required":["city"]}`),
	}})
	require.NoError(t, err)

	schema := json.RawMessage(`{"type":"object","properties":{"city":{"type":"string"},` +
		`"celsius":{"type":"number"}},"required":["city","celsius"],"additionalProperties":false}`)
	rf := &providers.ResponseFormat{Type: providers.ResponseFormatJSONSchema, JSONSchema: schema}

	msgs := []types.Message{{
		Role:    "user",
		Content: "What's the temperature in Bristol? Use the tool, then answer.",
	}}

	// Round 1 — the model should call the tool despite output_config being set.
	resp, calls, err := tp.PredictWithTools(context.Background(),
		providers.PredictionRequest{Messages: msgs, MaxTokens: 2048, ResponseFormat: rf}, tools, "auto")
	require.NoError(t, err, "structured outputs must not break tool use")
	require.NotEmpty(t, calls, "model did not call the tool; the premise is untested")
	t.Logf("round 1: %d tool call(s), content=%.60q", len(calls), resp.Content)

	// Feed the tool result back.
	msgs = append(msgs,
		types.Message{Role: roleAssistant, ToolCalls: calls},
		types.NewToolResultMessage(types.NewTextToolResult(
			calls[0].ID, calls[0].Name, `{"celsius": 21}`)),
	)

	// Round 2 — the final answer must conform to the caller's schema.
	final, moreCalls, err := tp.PredictWithTools(context.Background(),
		providers.PredictionRequest{Messages: msgs, MaxTokens: 2048, ResponseFormat: rf}, tools, "auto")
	require.NoError(t, err)
	t.Logf("round 2: %d tool call(s), content=%.200q", len(moreCalls), final.Content)

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
