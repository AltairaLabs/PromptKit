package gemini

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/AltairaLabs/PromptKit/runtime/v2/providers"
	"github.com/AltairaLabs/PromptKit/runtime/v2/types"
)

// liveToolResponseWire opens a Live session on model the way a host does, via
// the provider's CreateStreamSession (its socket redirected to a recording
// stand-in), sends responses the way the duplex stage does — with their parts
// only when the session reports it can send them — and returns the
// toolResponse message the server received.
func liveToolResponseWire(t *testing.T, model string, responses []providers.ToolResponse) map[string]any {
	t.Helper()
	url, msgs := recordLiveClient(t)
	p := NewProvider("live", model, "", providers.ProviderDefaults{}, false)
	p.newStreamSessionFn = func(ctx context.Context, _, key string, cfg *StreamSessionConfig) (*StreamSession, error) {
		return NewStreamSession(ctx, url, key, cfg)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	session, err := p.CreateStreamSession(ctx, liveAudioRequest())
	require.NoError(t, err)
	defer session.Close()

	ps, ok := session.(providers.ToolResponsePartsSupport)
	require.True(t, ok, "the Live session does not report ToolResponsePartsSupport")
	require.True(t, ps.SupportsToolResponseParts())
	require.NoError(t, session.(providers.ToolResponseSupport).SendToolResponses(ctx, responses))
	require.NoError(t, session.SendText(ctx, "done"))

	var raw []string
	select {
	case raw = <-msgs:
	case <-ctx.Done():
		t.Fatal("server never saw the end marker")
	}
	require.Len(t, raw, 1, "the round is one toolResponse message")
	var got map[string]any
	require.NoError(t, json.Unmarshal([]byte(raw[0]), &got))
	return got
}

func toolResponseOf(functionResponses ...map[string]any) map[string]any {
	frs := make([]any, len(functionResponses))
	for i := range functionResponses {
		frs[i] = functionResponses[i]
	}
	return map[string]any{"toolResponse": map[string]any{"functionResponses": frs}}
}

func inlinePart(mimeType, data string) map[string]any {
	return map[string]any{"inlineData": map[string]any{"mimeType": mimeType, "data": data}}
}

// On a Gemini 3 Live model a tool's image goes to the model in the function
// response's parts, as inline data, beside its response object.
func TestLiveToolResponse_ImageIsSentInFunctionResponseParts(t *testing.T) {
	got := liveToolResponseWire(t, "gemini-3.8-live", []providers.ToolResponse{
		{
			ToolCallID: "call_img",
			Parts:      []types.ContentPart{types.NewImagePartFromData("iVBORw0KGgo=", types.MIMETypeImagePNG, nil)},
		},
		{
			ToolCallID: "call_chart",
			Result:     `{"title":"Q3 sales"}`,
			Parts: []types.ContentPart{
				types.NewTextPart(`{"title":"Q3 sales"}`),
				types.NewImagePartFromData("/9j/4AAQ", types.MIMETypeImageJPEG, nil),
			},
		},
		{ToolCallID: "call_text", Result: "plain"},
	})

	assert.Equal(t, toolResponseOf(
		map[string]any{"id": "call_img", "response": map[string]any{"result": ""},
			"parts": []any{inlinePart("image/png", "iVBORw0KGgo=")}},
		map[string]any{"id": "call_chart", "response": map[string]any{"title": "Q3 sales"},
			"parts": []any{inlinePart("image/jpeg", "/9j/4AAQ")}},
		map[string]any{"id": "call_text", "response": map[string]any{"result": "plain"}},
	), got)
}

// Gemini Live closes the session on audio in a function response, and takes
// no other media there: those parts are described in the response, in the
// words the text providers use, and the images beside them still go.
func TestLiveToolResponse_UnsendableMediaIsDescribed(t *testing.T) {
	data := "AAAA"
	got := liveToolResponseWire(t, "gemini-3.8-live", []providers.ToolResponse{{
		ToolCallID: "call_many",
		Result:     "Recording and chart",
		Parts: []types.ContentPart{
			types.NewTextPart("Recording and chart"),
			types.NewAudioPartFromData("UklGRg==", types.MIMETypeAudioWAV),
			{Type: types.ContentTypeVideo, Media: &types.MediaContent{Data: &data, MIMEType: "video/mp4"}},
			types.NewImagePartFromData("R0lGOD", types.MIMETypeImageGIF, nil),
			types.NewImagePartFromData("UklGR", types.MIMETypeImageWebP, nil),
		},
	}})

	assert.Equal(t, toolResponseOf(map[string]any{
		"id": "call_many",
		"response": map[string]any{"result": "Recording and chart\n" +
			"[This tool returned an audio (audio/wav) that was not sent: " +
			"Gemini Live does not accept audio in a tool result.]\n" +
			"[This tool returned a video (video/mp4) that was not sent: " +
			"Gemini Live does not accept video in a tool result.]\n" +
			"[This tool returned an image (image/gif) that was not sent: " +
			"Gemini Live does not accept image/gif images.]"},
		"parts": []any{inlinePart("image/webp", "UklGR")},
	}), got)
}

// Gemini 2.x Live models take no media in a function response (an image sent
// there is never seen by the model): every part is described instead.
func TestLiveToolResponse_Gemini2ModelDescribesItsMedia(t *testing.T) {
	got := liveToolResponseWire(t, "gemini-2.5-flash-native-audio-preview-12-2025", []providers.ToolResponse{{
		ToolCallID: "call_img",
		Parts:      []types.ContentPart{types.NewImagePartFromData("iVBORw0KGgo=", types.MIMETypeImagePNG, nil)},
	}})

	assert.Equal(t, toolResponseOf(map[string]any{
		"id": "call_img",
		"response": map[string]any{"result": "[This tool returned an image (image/png) that was not sent: " +
			"gemini-2.5-flash-native-audio-preview-12-2025 does not accept media in a Live tool result " +
			"(Gemini 3 and later do).]"},
	}), got)
}

// Media that cannot be loaded is described, with why; the round is still
// answered.
func TestLiveToolResponse_UnloadableMediaIsDescribed(t *testing.T) {
	got := liveToolResponseWire(t, "gemini-3.8-live", []providers.ToolResponse{{
		ToolCallID: "call_ref",
		IsError:    false,
		Parts:      []types.ContentPart{types.NewImagePartFromStorageRef("media/abc", types.MIMETypeImagePNG, nil)},
	}})

	assert.Equal(t, toolResponseOf(map[string]any{
		"id": "call_ref",
		"response": map[string]any{"result": "[This tool returned an image (image/png, stored as media/abc) " +
			"that was not sent: it could not be loaded: storage service not configured but storage reference " +
			"provided: media/abc.]"},
	}), got)
}

// Whether a Live model takes media in a function response is read from its
// version, also when it is named by a Vertex resource; a name without a
// version is not known to take it.
func TestTakesToolMedia_ByModelVersion(t *testing.T) {
	cases := map[string]bool{
		"gemini-3.8-live":               true,
		"gemini-3.1-flash-live-preview": true,
		"projects/p1/locations/us-central1/publishers/google/models/gemini-3.8-live":       true,
		"projects/p1/locations/us-central1/publishers/google/models/gemini-live-2.5-flash": false,
		"gemini-2.5-flash-native-audio-preview-12-2025":                                    false,
		"gemini-live-latest":     false,
		"tunedModels/my-model":   false,
		"gemini-3-flash-preview": true,
		"gemini-flash-latest":    false,
	}
	for model, want := range cases {
		assert.Equal(t, want, takesToolMedia(model), model)
	}
}

// An image whose format is not stated is described, not sent.
func TestLiveToolResponse_ImageWithoutMIMETypeIsDescribed(t *testing.T) {
	data := "iVBORw0KGgo="
	got := liveToolResponseWire(t, "gemini-3.8-live", []providers.ToolResponse{{
		ToolCallID: "call_img",
		Parts:      []types.ContentPart{{Type: types.ContentTypeImage, Media: &types.MediaContent{Data: &data}}},
	}})

	assert.Equal(t, toolResponseOf(map[string]any{
		"id":       "call_img",
		"response": map[string]any{"result": "[This tool returned an image that was not sent: its image format is not stated.]"},
	}), got)
}
