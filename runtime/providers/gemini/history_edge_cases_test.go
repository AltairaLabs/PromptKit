package gemini

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/AltairaLabs/PromptKit/runtime/v2/providers"
	"github.com/AltairaLabs/PromptKit/runtime/v2/types"
)

func contentsJSON(t *testing.T, msgs []types.Message) string {
	t.Helper()
	contents, err := convProvider().buildToolContents(context.Background(), msgs)
	require.NoError(t, err)
	b, err := json.Marshal(contents)
	require.NoError(t, err)
	return string(b)
}

// A role=tool message without a ToolResult is sent as the user's text rather
// than dereferenced, on the plain path as on the tool path.
func TestPlainPath_ToolMessageWithoutResultIsText(t *testing.T) {
	got := captureContents(t, false, func(p *ToolProvider) {
		_, err := p.Predict(context.Background(), providers.PredictionRequest{Messages: []types.Message{
			{Role: "user", Content: "q"},
			{Role: "tool", Content: "stray output"},
		}})
		require.NoError(t, err)
	})
	assert.JSONEq(t, `[
		{"role":"user","parts":[{"text":"q"}]},
		{"role":"user","parts":[{"text":"stray output"}]}
	]`, string(got))
}

// A media part that cannot be sent fails the request, as a media part that
// cannot be loaded does, instead of going out without it.
func TestBuildToolContents_UnsendablePartsAreErrors(t *testing.T) {
	cases := map[string]struct {
		part types.ContentPart
		want string
	}{
		"image without media": {types.ContentPart{Type: types.ContentTypeImage}, "image part missing media field"},
		"unknown part type":   {types.ContentPart{Type: "hologram"}, "unsupported part type: hologram"},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			msg := types.Message{Role: "user", Parts: []types.ContentPart{
				types.NewTextPart("look"),
				types.NewImagePartFromData("aW1n", types.MIMETypeImagePNG, nil),
				tc.part,
			}}
			_, err := convProvider().buildToolContents(context.Background(), []types.Message{msg})
			require.Error(t, err)
			assert.Equal(t, "failed to convert message 0: "+tc.want, err.Error())
		})
	}
}

// Gemini 3 refuses a current-turn function call whose first part carries no
// thought signature, so a call recorded without one (history from another
// provider, or hand-built) gets Google's documented placeholder. Calls before
// the latest user text, and calls that carry a signature, are left as they are.
func TestBuildToolContents_UnsignedCurrentTurnCallGetsPlaceholderSignature(t *testing.T) {
	got := contentsJSON(t, []types.Message{
		{Role: "user", Content: "first"},
		{Role: "assistant", ToolCalls: []types.MessageToolCall{{ID: "a", Name: "f", Args: json.RawMessage(`{}`)}}},
		{Role: "tool", ToolResult: &types.MessageToolResult{ID: "a", Name: "f", Parts: []types.ContentPart{types.NewTextPart("1")}}},
		{Role: "user", Content: "second"},
		{Role: "assistant", Content: "Checking.", ToolCalls: []types.MessageToolCall{
			{ID: "b", Name: "f", Args: json.RawMessage(`{}`)},
			{ID: "c", Name: "f", Args: json.RawMessage(`{}`)},
		}},
		{Role: "tool", ToolResult: &types.MessageToolResult{ID: "b", Name: "f", Parts: []types.ContentPart{types.NewTextPart("2")}}},
		{Role: "tool", ToolResult: &types.MessageToolResult{ID: "c", Name: "f", Parts: []types.ContentPart{types.NewTextPart("3")}}},
		{Role: "assistant", ToolCalls: []types.MessageToolCall{{ID: "d", Name: "f", Args: json.RawMessage(`{}`),
			ProviderMetadata: map[string]string{providerMetaThoughtSignature: "real-sig"}}}},
	})
	assert.JSONEq(t, `[
		{"role":"user","parts":[{"text":"first"}]},
		{"role":"model","parts":[{"functionCall":{"name":"f","args":{}}}]},
		{"role":"user","parts":[{"functionResponse":{"name":"f","response":{"result":1}}}]},
		{"role":"user","parts":[{"text":"second"}]},
		{"role":"model","parts":[
			{"text":"Checking."},
			{"functionCall":{"name":"f","args":{}},"thoughtSignature":"skip_thought_signature_validator"},
			{"functionCall":{"name":"f","args":{}}}]},
		{"role":"user","parts":[
			{"functionResponse":{"name":"f","response":{"result":2}}},
			{"functionResponse":{"name":"f","response":{"result":3}}}]},
		{"role":"model","parts":[{"functionCall":{"name":"f","args":{}},"thoughtSignature":"real-sig"}]}
	]`, got)
}

// With no user text in the history, every model step is in the current turn.
func TestBuildToolContents_NoUserTextSignsFromTheStart(t *testing.T) {
	got := contentsJSON(t, []types.Message{
		{Role: "assistant", ToolCalls: []types.MessageToolCall{{ID: "a", Name: "f", Args: json.RawMessage(`{}`)}}},
		{Role: "tool", ToolResult: &types.MessageToolResult{ID: "a", Name: "f", Parts: []types.ContentPart{types.NewTextPart("1")}}},
	})
	assert.JSONEq(t, `[
		{"role":"model","parts":[{"functionCall":{"name":"f","args":{}},"thoughtSignature":"skip_thought_signature_validator"}]},
		{"role":"user","parts":[{"functionResponse":{"name":"f","response":{"result":1}}}]}
	]`, got)
}
