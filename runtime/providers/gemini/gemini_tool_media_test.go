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

// badgeRound is a history whose last round is two parallel tool calls: one
// returns text and two images, the other text only.
func badgeRound(badgeParts ...types.ContentPart) []types.Message {
	return []types.Message{
		{Role: "user", Content: "What numbers are on the badges?"},
		{Role: "assistant", ToolCalls: []types.MessageToolCall{
			{ID: "call_1", Name: "get_badges", Args: json.RawMessage(`{}`)},
			{ID: "call_2", Name: "get_date", Args: json.RawMessage(`{}`)},
		}},
		{Role: "tool", ToolResult: &types.MessageToolResult{ID: "call_1", Name: "get_badges", Parts: badgeParts}},
		{Role: "tool", ToolResult: &types.MessageToolResult{ID: "call_2", Name: "get_date",
			Parts: []types.ContentPart{types.NewTextPart("Tuesday")}}},
	}
}

func twoBadges() []types.ContentPart {
	return []types.ContentPart{
		types.NewTextPart("Two badges"),
		types.NewImagePartFromData("UE5HMQ==", types.MIMETypeImagePNG, nil),
		types.NewImagePartFromData("UE5HMg==", types.MIMETypeImagePNG, nil),
	}
}

// noFunctionResponseMedia configures a provider whose model takes no media
// in a function response.
var noFunctionResponseMedia = map[string]any{"function_response_media": false}

func predictContents(t *testing.T, model string, msgs []types.Message) string {
	t.Helper()
	return predictContentsWith(t, model, nil, msgs)
}

// predictContentsWith is predictContents for a provider with additional_config
// additional.
func predictContentsWith(t *testing.T, model string, additional map[string]any, msgs []types.Message) string {
	t.Helper()
	return string(captureModelContentsWith(t, model, additional, false, func(p *ToolProvider) {
		_, err := p.Predict(context.Background(), providers.PredictionRequest{Messages: msgs})
		require.NoError(t, err)
	}))
}

// On Gemini 3 every media part of a tool result goes in its function
// response's parts, in order (Gemini's multimodal function responses).
func TestToolResultMedia_Gemini3SendsEveryPartInTheFunctionResponse(t *testing.T) {
	got := predictContents(t, "gemini-3.8-flash", badgeRound(twoBadges()...))

	assert.JSONEq(t, `[
		{"role":"user","parts":[{"text":"What numbers are on the badges?"}]},
		{"role":"model","parts":[
			{"functionCall":{"name":"get_badges","args":{}},"thoughtSignature":"skip_thought_signature_validator"},
			{"functionCall":{"name":"get_date","args":{}}}]},
		{"role":"user","parts":[
			{"functionResponse":{"name":"get_badges","response":{"result":"Two badges"},"parts":[
				{"inlineData":{"mimeType":"image/png","data":"UE5HMQ=="}},
				{"inlineData":{"mimeType":"image/png","data":"UE5HMg=="}}]}},
			{"functionResponse":{"name":"get_date","response":{"result":"Tuesday"}}}]}
	]`, got)
}

// On a model whose function responses take no media (Gemini 2.x refuses it:
// "Multimodal function responses are not supported for this model"), a tool
// result's media follows the round's function responses in a user content of
// its own, and the function response says it is attached there.
func TestToolResultMedia_WithoutFunctionResponseMediaSendsItInAFollowingUserContent(t *testing.T) {
	got := predictContentsWith(t, "gemini-2.5-flash", noFunctionResponseMedia, badgeRound(twoBadges()...))

	assert.JSONEq(t, `[
		{"role":"user","parts":[{"text":"What numbers are on the badges?"}]},
		{"role":"model","parts":[
			{"functionCall":{"name":"get_badges","args":{}}},
			{"functionCall":{"name":"get_date","args":{}}}]},
		{"role":"user","parts":[
			{"functionResponse":{"name":"get_badges","response":{"result":
				"Two badges\n[The 2 media parts this tool returned are attached in the next user message.]"}}},
			{"functionResponse":{"name":"get_date","response":{"result":"Tuesday"}}}]},
		{"role":"user","parts":[
			{"text":"Media returned by tool get_badges (call call_1):"},
			{"inlineData":{"mimeType":"image/png","data":"UE5HMQ=="}},
			{"inlineData":{"mimeType":"image/png","data":"UE5HMg=="}}]}
	]`, got)
}

// Media Gemini does not take is described in the function response; what
// can be sent still is. A JSON object result keeps
// its fields when there is no note.
func TestToolResultMedia_UnsendableMediaIsDescribed(t *testing.T) {
	data := "Qk0="
	got := predictContents(t, "gemini-3.8-flash", []types.Message{
		{Role: "user", Content: "q"},
		{Role: "assistant", ToolCalls: []types.MessageToolCall{
			{ID: "c1", Name: "snap", Args: json.RawMessage(`{}`)},
			{ID: "c2", Name: "chart", Args: json.RawMessage(`{}`)},
		}},
		{Role: "tool", ToolResult: &types.MessageToolResult{ID: "c1", Name: "snap", Parts: []types.ContentPart{
			{Type: types.ContentTypeImage, Media: &types.MediaContent{Data: &data, MIMEType: "image/bmp"}},
			types.NewImagePartFromData("SlBH", types.MIMETypeImageJPEG, nil),
		}}},
		{Role: "tool", ToolResult: &types.MessageToolResult{ID: "c2", Name: "chart", Parts: []types.ContentPart{
			types.NewTextPart(`{"title":"Q3"}`),
			types.NewImagePartFromData("UE5H", types.MIMETypeImagePNG, nil),
		}}},
	})

	assert.JSONEq(t, `[
		{"role":"user","parts":[{"text":"q"}]},
		{"role":"model","parts":[
			{"functionCall":{"name":"snap","args":{}},"thoughtSignature":"skip_thought_signature_validator"},
			{"functionCall":{"name":"chart","args":{}}}]},
		{"role":"user","parts":[
			{"functionResponse":{"name":"snap","response":{"result":
				"[This tool returned an image (image/bmp) that was not sent: Gemini does not accept image/bmp images.]"},
				"parts":[{"inlineData":{"mimeType":"image/jpeg","data":"SlBH"}}]}},
			{"functionResponse":{"name":"chart","response":{"title":"Q3"},
				"parts":[{"inlineData":{"mimeType":"image/png","data":"UE5H"}}]}}]}
	]`, got)
}

// A tool result with only media: in a function response that takes media it
// is carried with an empty result; in one that takes none the response says
// where the one part went, and audio goes as a user message would send it.
func TestToolResultMedia_MediaOnlyResults(t *testing.T) {
	image := []types.ContentPart{types.NewImagePartFromData("SlBH", types.MIMETypeImageJPEG, nil)}
	audio := []types.ContentPart{types.NewAudioPartFromData("UklGRg==", types.MIMETypeAudioWAV)}
	history := func(parts []types.ContentPart) []types.Message {
		return []types.Message{
			{Role: "user", Content: "q"},
			{Role: "assistant", ToolCalls: []types.MessageToolCall{{ID: "c1", Name: "snap", Args: json.RawMessage(`{}`)}}},
			{Role: "tool", ToolResult: &types.MessageToolResult{ID: "c1", Name: "snap", Parts: parts}},
		}
	}

	assert.JSONEq(t, `[
		{"role":"user","parts":[{"text":"q"}]},
		{"role":"model","parts":[{"functionCall":{"name":"snap","args":{}},"thoughtSignature":"skip_thought_signature_validator"}]},
		{"role":"user","parts":[{"functionResponse":{"name":"snap","response":{"result":""},
			"parts":[{"inlineData":{"mimeType":"image/jpeg","data":"SlBH"}}]}}]}
	]`, predictContents(t, "gemini-3.8-flash", history(image)))
	assert.JSONEq(t, `[
		{"role":"user","parts":[{"text":"q"}]},
		{"role":"model","parts":[{"functionCall":{"name":"snap","args":{}}}]},
		{"role":"user","parts":[{"functionResponse":{"name":"snap","response":{"result":
			"[The audio this tool returned is attached in the next user message.]"}}}]},
		{"role":"user","parts":[{"text":"Media returned by tool snap (call c1):"},
			{"inlineData":{"mimeType":"audio/wav","data":"UklGRg=="}}]}
	]`, predictContentsWith(t, "gemini-2.5-flash", noFunctionResponseMedia, history(audio)))
}

// A media part with no media content is described, never sent as nothing.
func TestToolResultMedia_PartWithoutMediaIsDescribed(t *testing.T) {
	got := predictContents(t, "gemini-3.8-flash", []types.Message{
		{Role: "user", Content: "q"},
		{Role: "assistant", ToolCalls: []types.MessageToolCall{{ID: "c1", Name: "snap", Args: json.RawMessage(`{}`)}}},
		{Role: "tool", ToolResult: &types.MessageToolResult{ID: "c1", Name: "snap",
			Parts: []types.ContentPart{{Type: types.ContentTypeImage}}}},
	})
	assert.JSONEq(t, `[
		{"role":"user","parts":[{"text":"q"}]},
		{"role":"model","parts":[{"functionCall":{"name":"snap","args":{}},"thoughtSignature":"skip_thought_signature_validator"}]},
		{"role":"user","parts":[{"functionResponse":{"name":"snap","response":{"result":
			"[This tool returned an image that was not sent: it has no media content.]"}}}]}
	]`, got)
}
