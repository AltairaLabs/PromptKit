package gemini

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/AltairaLabs/PromptKit/runtime/v2/providers"
	"github.com/AltairaLabs/PromptKit/runtime/v2/types"
)

// The Interactions wire format has several requirements that are not guessable
// and each cost a live round trip to discover. They are pinned here so a
// refactor breaks a unit test rather than only a live one.

func marshalInput(t *testing.T, msgs []types.Message) []map[string]any {
	t.Helper()
	raw, err := json.Marshal(buildInteractionsInput(msgs))
	require.NoError(t, err)
	var out []map[string]any
	require.NoError(t, json.Unmarshal(raw, &out))
	return out
}

// TestBuildInteractionsInput_ReplaysThoughtBeforeFunctionCall pins the
// requirement that cost the most to find: a history whose function_call has no
// preceding thought is refused outright ("Request contains an invalid
// argument"), and so is an empty thought.
func TestBuildInteractionsInput_ReplaysThoughtBeforeFunctionCall(t *testing.T) {
	msgs := []types.Message{
		{Role: roleUser, Content: "temp in Bristol?"},
		{
			Role: roleAssistant,
			Reasoning: &types.ReasoningTrace{Opaque: []types.OpaqueReasoning{{
				Provider: providerNameGemini,
				Kind:     kindInteractionsThought,
				Data:     "SIG-ABC",
			}}},
			ToolCalls: []types.MessageToolCall{
				{ID: "call_1", Name: "get_temperature", Args: json.RawMessage(`{"city":"Bristol"}`)},
			},
		},
	}

	steps := marshalInput(t, msgs)
	require.Len(t, steps, 3)

	assert.Equal(t, stepTypeText, steps[0]["type"])
	assert.Equal(t, stepTypeThought, steps[1]["type"],
		"the thought must be replayed, and must come BEFORE the call it preceded")
	assert.Equal(t, "SIG-ABC", steps[1]["signature"])
	assert.NotContains(t, steps[1], "content",
		"a thought carries an opaque signature; a content field is rejected")
	assert.Equal(t, stepTypeFunctionCall, steps[2]["type"])
}

// TestFunctionCallStep_UsesIDNotCallID pins that call_id is rejected on a
// function_call ("Unknown parameter 'call_id'") and belongs on the result.
func TestFunctionCallStep_UsesIDNotCallID(t *testing.T) {
	raw, err := json.Marshal(functionCallStep(&types.MessageToolCall{
		ID: "call_9", Name: "probe", Args: json.RawMessage(`{"q":"x"}`),
	}))
	require.NoError(t, err)

	var step map[string]any
	require.NoError(t, json.Unmarshal(raw, &step))

	assert.Equal(t, "call_9", step["id"])
	assert.NotContains(t, step, "call_id", "call_id is rejected on a function_call")
	assert.Equal(t, map[string]any{"q": "x"}, step["arguments"],
		"arguments must be an object; a string is rejected")
}

// TestToolResultStep_ResultIsSingleItem pins that a list of content reads as a
// multimodal response, which Gemini 2.5 rejects.
func TestToolResultStep_ResultIsSingleItem(t *testing.T) {
	msg := types.NewToolResultMessage(
		types.NewTextToolResult("call_1", "get_temperature", `{"celsius":21}`))

	step := toolResultStep(&msg)
	require.NotNil(t, step)

	raw, err := json.Marshal(step)
	require.NoError(t, err)

	var decoded map[string]any
	require.NoError(t, json.Unmarshal(raw, &decoded))

	assert.Equal(t, "call_1", decoded["call_id"], "the result carries call_id, not the call")
	_, isList := decoded["result"].([]any)
	assert.False(t, isList,
		"result must be a single item; a list is treated as multimodal and rejected")
	assert.Equal(t, `{"celsius":21}`, decoded["result"].(map[string]any)["text"])
}

// TestToolResultStep_PrefersErrorText keeps a failed tool legible to the model
// rather than sending an empty result.
func TestToolResultStep_PrefersErrorText(t *testing.T) {
	msg := types.Message{Role: roleToolMessage, ToolResult: &types.MessageToolResult{
		ID: "c1", Name: "probe", Error: "boom",
	}}
	step := toolResultStep(&msg)
	require.NotNil(t, step)
	assert.Equal(t, "boom", step.Result.Text)
}

// TestToolResultStep_PresenceChangesTheWire pins the outcome that matters: a
// tool message carrying a result becomes a function_result step, and one
// carrying nothing contributes no step at all.
//
// Asserted on the encoded input rather than on a returned pointer, because the
// wire is what the model sees — and a nil check alone would pass against an
// encoder that never emitted the step.
func TestToolResultStep_PresenceChangesTheWire(t *testing.T) {
	withResult := marshalInput(t, []types.Message{
		types.NewToolResultMessage(types.NewTextToolResult("c1", "probe", "ok")),
	})
	require.Len(t, withResult, 1, "a tool result must reach the wire")
	assert.Equal(t, stepTypeFunctionResult, withResult[0]["type"])
	assert.Equal(t, "c1", withResult[0]["call_id"])

	empty := marshalInput(t, []types.Message{{Role: roleToolMessage}})
	assert.Empty(t, empty,
		"a tool message with no result must contribute nothing, not an empty step")
}

// TestBuildInteractionsInput_AssistantTextAndUserTurns covers the ordinary
// shapes, including that an assistant turn with text but no tools still replays.
func TestBuildInteractionsInput_AssistantTextAndUserTurns(t *testing.T) {
	steps := marshalInput(t, []types.Message{
		{Role: roleUser, Content: "hello"},
		{Role: roleAssistant, Content: "hi there"},
		{Role: roleUser, Content: "again"},
	})
	require.Len(t, steps, 3)
	for _, s := range steps {
		assert.Equal(t, stepTypeText, s["type"])
	}
	assert.Equal(t, "hi there", steps[1]["text"])
}

// TestThoughtSteps_SkipsForeignOpaque keeps a trace that travelled through
// another provider from producing a malformed step.
func TestThoughtSteps_SkipsForeignOpaque(t *testing.T) {
	assert.Empty(t, thoughtSteps(nil))
	assert.Empty(t, thoughtSteps(&types.ReasoningTrace{Text: "some reasoning"}),
		"reasoning text alone is not a replayable signature")
	assert.Empty(t, thoughtSteps(&types.ReasoningTrace{Opaque: []types.OpaqueReasoning{
		{Provider: "claude", Kind: "thinking_signature", Data: "x"},
		{Provider: providerNameGemini, Kind: kindThoughtSignature, Data: "y"},
		{Provider: providerNameGemini, Kind: kindInteractionsThought, Data: ""},
	}}), "only non-empty interactions signatures are replayable")

	got := thoughtSteps(&types.ReasoningTrace{Opaque: []types.OpaqueReasoning{
		{Provider: providerNameGemini, Kind: kindInteractionsThought, Data: "SIG"},
	}})
	require.Len(t, got, 1)
}

func TestBuildInteractionsTools(t *testing.T) {
	assert.Nil(t, buildInteractionsTools(nil), "no tooling yields no tools")
	assert.Nil(t, buildInteractionsTools("not-gemini-tooling"))
	assert.Nil(t, buildInteractionsTools(geminiToolDeclaration{}))

	decl := geminiToolDeclaration{FunctionDeclarations: []geminiFunctionDeclaration{{
		Name: "probe", Description: "a probe",
		Parameters: json.RawMessage(`{"type":"object"}`),
	}}}

	for _, tools := range []providers.ProviderTools{decl, &decl} {
		got := buildInteractionsTools(tools)
		require.Len(t, got, 1)
		assert.Equal(t, "function", got[0].Type, "tools are flat here, not functionDeclarations")
		assert.Equal(t, "probe", got[0].Name)
	}
}

// TestInteractionsResponseFormat_SchemaUnsanitized pins the difference from
// generateContent: that path strips keywords like additionalProperties because
// its responseSchema is an OpenAPI subset, while this API takes full JSON Schema.
func TestInteractionsResponseFormat_SchemaUnsanitized(t *testing.T) {
	assert.Nil(t, interactionsResponseFormat(nil))
	assert.Nil(t, interactionsResponseFormat(&providers.ResponseFormat{
		Type: providers.ResponseFormatText}))

	schema := `{"type":"object","properties":{"a":{"type":"string"}},"additionalProperties":false}`
	got := interactionsResponseFormat(&providers.ResponseFormat{
		Type: providers.ResponseFormatJSONSchema, JSONSchema: json.RawMessage(schema)})
	require.NotNil(t, got)
	assert.Equal(t, applicationJSON, got.MIMEType)
	assert.JSONEq(t, schema, string(got.Schema),
		"the schema must reach this API intact, additionalProperties included")

	// json_object mode constrains to JSON with no schema.
	jsonOnly := interactionsResponseFormat(&providers.ResponseFormat{
		Type: providers.ResponseFormatJSON})
	require.NotNil(t, jsonOnly)
	assert.Empty(t, jsonOnly.Schema)
}

// TestParseInteractionsResponse separates the three step kinds. Reasoning comes
// only from thought steps, so it can never be mistaken for the answer.
func TestParseInteractionsResponse(t *testing.T) {
	resp := &interactionsResponse{Steps: []interactionsStep{
		{Type: stepTypeThought, Signature: "SIG-1"},
		{Type: stepTypeFunctionCall, ID: "c1", Name: "probe",
			Arguments: json.RawMessage(`{"q":"x"}`), Signature: "FC-SIG"},
		{Type: stepTypeModelOutput, Content: []interactionsContent{
			{Type: "text", Text: `{"ok":`}, {Type: "text", Text: `true}`}}},
	}}

	content, calls, reasoning := parseInteractionsResponse(resp)

	assert.Equal(t, `{"ok":true}`, content, "content items concatenate")
	require.Len(t, calls, 1)
	assert.Equal(t, "c1", calls[0].ID)
	assert.Equal(t, "FC-SIG", calls[0].ProviderMetadata[providerMetaThoughtSignature])

	require.NotNil(t, reasoning)
	assert.Empty(t, reasoning.Text, "a thought signature is opaque, not readable text")
	require.Len(t, reasoning.Opaque, 1)
	assert.Equal(t, kindInteractionsThought, reasoning.Opaque[0].Kind)
	assert.Equal(t, "SIG-1", reasoning.Opaque[0].Data,
		"the signature must survive so later rounds can replay it")
}

// TestParseInteractionsResponse_ReasoningPresenceIsDiscriminated keeps "this
// turn produced no thought" distinguishable from "the trace was dropped", by
// driving both inputs through the same parser.
func TestParseInteractionsResponse_ReasoningPresenceIsDiscriminated(t *testing.T) {
	_, _, withThought := parseInteractionsResponse(&interactionsResponse{
		Steps: []interactionsStep{
			{Type: stepTypeThought, Signature: "SIG"},
			{Type: stepTypeModelOutput, Text: "hi"},
		}})
	require.NotNil(t, withThought, "a thought step must produce a trace")
	require.Len(t, withThought.Opaque, 1)

	_, _, noThought := parseInteractionsResponse(&interactionsResponse{
		Steps: []interactionsStep{{Type: stepTypeModelOutput, Text: "hi"}}})
	assert.Nil(t, noThought, "no thought means no trace, not an empty one")
}

// TestInteractionsStepText covers both shapes a step's text arrives in.
func TestInteractionsStepText(t *testing.T) {
	inline := interactionsStep{Text: "inline"}
	assert.Equal(t, "inline", inline.text())

	nested := interactionsStep{Content: []interactionsContent{{Text: "a"}, {Text: "b"}}}
	assert.Equal(t, "ab", nested.text())

	assert.Empty(t, (&interactionsStep{}).text())
}
