package gemini

import (
	"encoding/json"
	"testing"

	"github.com/AltairaLabs/PromptKit/runtime/v2/providers"
	"github.com/AltairaLabs/PromptKit/runtime/v2/types"
)

// sentInputJSON returns the last request's input list, canonicalized.
func (s *interactionsParityServer) sentInputJSON(t *testing.T) string {
	t.Helper()
	var body struct {
		Input []any `json:"input"`
	}
	if err := json.Unmarshal(s.lastBody, &body); err != nil {
		t.Fatalf("interactions body not JSON: %v (%s)", err, s.lastBody)
	}
	return canonicalJSON(t, body.Input)
}

// The Interactions API reads bare content items in the input as the user's.
// A replayed assistant reply has to go out as a model_output step, or the
// model reads its own earlier reply as something the user said: asked to
// quote the last thing it said, it answered "NONE" for the bare-item history
// and quoted the reply for the step history (probed live, gemini-3.7-flash).
func TestInteractions_ReplaysTurnsAsUserInputAndModelOutputSteps(t *testing.T) {
	history := []types.Message{
		{Role: roleUser, Content: "Hi, I'm Zed."},
		{Role: roleAssistant, Content: "Hello Zed!", Reasoning: &types.ReasoningTrace{
			Opaque: []types.OpaqueReasoning{{Provider: providerNameGemini, Kind: kindInteractionsThought, Data: "SIG"}},
		}},
		{Role: roleUser, Content: "What did you say?"},
	}
	want := canonicalJSON(t, []any{
		map[string]any{"type": "user_input", "content": []any{
			map[string]any{"type": "text", "text": "Hi, I'm Zed."}}},
		map[string]any{"type": "thought", "signature": "SIG"},
		map[string]any{"type": "model_output", "content": []any{
			map[string]any{"type": "text", "text": "Hello Zed!"}}},
		map[string]any{"type": "user_input", "content": []any{
			map[string]any{"type": "text", "text": "What did you say?"}}},
	})

	for _, stream := range []bool{false, true} {
		name := map[bool]string{false: "unary", true: "stream"}[stream]
		t.Run(name, func(t *testing.T) {
			srv := newInteractionsParityServer(t, okUnaryBody, okStreamBody)
			tp := newInteractionsParityProvider(t, uniqueProviderID("int-turns"), srv.url, nil)
			runInteractionsTurn(t, tp, providers.PredictionRequest{Messages: history}, stream)
			if got := srv.sentInputJSON(t); got != want {
				t.Errorf("input =\n%s\nwant\n%s", got, want)
			}
		})
	}
}

// A reply with a preamble and a tool call comes back from the API as thought,
// model_output, function_call (captured live, 3 of 3 on gemini-3.7-flash, with
// the model asked to say what it will do before calling). It is replayed in
// that order, as generateContent replays a text part before a functionCall.
func TestInteractions_ReplaysPreambleBeforeToolCall(t *testing.T) {
	history := []types.Message{
		{Role: roleUser, Content: "Temperature in Bristol?"},
		{Role: roleAssistant, Content: "I will look it up.",
			Reasoning: &types.ReasoningTrace{Opaque: []types.OpaqueReasoning{
				{Provider: providerNameGemini, Kind: kindInteractionsThought, Data: "SIG"}}},
			ToolCalls: []types.MessageToolCall{{ID: "c1", Name: "get_temp", Args: json.RawMessage(`{"city":"Bristol"}`)}},
		},
		types.NewToolResultMessage(types.NewTextToolResult("c1", "get_temp", `{"celsius":21}`)),
	}
	want := canonicalJSON(t, []any{
		map[string]any{"type": "user_input", "content": []any{
			map[string]any{"type": "text", "text": "Temperature in Bristol?"}}},
		map[string]any{"type": "thought", "signature": "SIG"},
		map[string]any{"type": "model_output", "content": []any{
			map[string]any{"type": "text", "text": "I will look it up."}}},
		map[string]any{"type": "function_call", "id": "c1", "name": "get_temp",
			"arguments": map[string]any{"city": "Bristol"}},
		map[string]any{"type": "function_result", "name": "get_temp", "call_id": "c1",
			"result": map[string]any{"type": "text", "text": `{"celsius":21}`}},
	})
	srv := newInteractionsParityServer(t, okUnaryBody, okStreamBody)
	tp := newInteractionsParityProvider(t, uniqueProviderID("int-preamble"), srv.url, nil)
	runInteractionsTurn(t, tp, providers.PredictionRequest{Messages: history}, false)
	if got := srv.sentInputJSON(t); got != want {
		t.Errorf("input =\n%s\nwant\n%s", got, want)
	}
}
