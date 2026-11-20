package gemini

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"
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
	return liveToolResponseWireWith(t, model, nil, responses)
}

// liveToolResponseWireWith is liveToolResponseWire for a provider built from
// its spec with additional_config additional.
func liveToolResponseWireWith(
	t *testing.T, model string, additional map[string]any, responses []providers.ToolResponse,
) map[string]any {
	t.Helper()
	url, msgs := recordLiveToolClient(t)
	config := map[string]any{"live_url": url}
	for k, v := range additional {
		config[k] = v
	}
	t.Setenv("GEMINI_API_KEY", "k")
	built, err := providers.CreateProviderFromSpec(providers.ProviderSpec{
		ID: "live", Type: "gemini", Model: model, AdditionalConfig: config,
	})
	require.NoError(t, err)
	p := built.(*ToolProvider)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	session, err := p.CreateStreamSession(ctx, liveAudioRequest())
	require.NoError(t, err)
	// Closed before the server (cleanups run last-registered first), so the
	// session never reconnects to a server the test has finished with.
	t.Cleanup(func() { _ = session.Close() })

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

// recordLiveToolClient runs a Live API stand-in that completes setup and,
// once the client sends the text "done", reports every client message before
// it, raw. Its handler never touches t: it can outlive the test (a hijacked
// connection is not waited for), and a reconnecting session would reach it
// again. The server closes when the test ends.
func recordLiveToolClient(t *testing.T) (wsURL string, msgs <-chan []string) {
	t.Helper()
	out := make(chan []string, 1)
	upgrader := websocket.Upgrader{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer conn.Close()
		if _, _, err := conn.ReadMessage(); err != nil { // setup
			return
		}
		reply, _ := json.Marshal(ServerMessage{SetupComplete: &SetupComplete{}})
		if conn.WriteMessage(websocket.TextMessage, reply) != nil {
			return
		}
		var got []string
		for {
			_, data, err := conn.ReadMessage()
			if err != nil {
				return
			}
			if strings.Contains(string(data), `"done"`) {
				select {
				case out <- got:
				default:
				}
				continue
			}
			got = append(got, string(data))
		}
	}))
	t.Cleanup(server.Close)
	return "ws" + strings.TrimPrefix(server.URL, "http"), out
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

// A Live model's name does not decide whether a tool's image is sent: with
// nothing configured it goes in the function response's parts, on any model.
func TestLiveToolResponse_ImageIsSentWhateverTheModelName(t *testing.T) {
	got := liveToolResponseWire(t, "gemini-2.5-flash-native-audio-preview-12-2025", []providers.ToolResponse{{
		ToolCallID: "call_img",
		Parts:      []types.ContentPart{types.NewImagePartFromData("iVBORw0KGgo=", types.MIMETypeImagePNG, nil)},
	}})

	assert.Equal(t, toolResponseOf(map[string]any{
		"id":       "call_img",
		"response": map[string]any{"result": ""},
		"parts":    []any{inlinePart("image/png", "iVBORw0KGgo=")},
	}), got)
}

// A Live model whose function responses take no media (Gemini 2.x Live
// models ignore it without an error, so the provider cannot learn it) is
// configured with additional_config.function_response_media: false, and every
// part is described instead.
func TestLiveToolResponse_ConfiguredWithoutMediaDescribesIt(t *testing.T) {
	got := liveToolResponseWireWith(t, "gemini-2.5-flash-native-audio-preview-12-2025",
		map[string]any{"function_response_media": false}, []providers.ToolResponse{{
			ToolCallID: "call_img",
			Parts:      []types.ContentPart{types.NewImagePartFromData("iVBORw0KGgo=", types.MIMETypeImagePNG, nil)},
		}})

	assert.Equal(t, toolResponseOf(map[string]any{
		"id": "call_img",
		"response": map[string]any{"result": "[This tool returned an image (image/png) that was not sent: " +
			"Gemini Live is configured to take no media in a tool result " +
			"(additional_config.function_response_media).]"},
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

// A media part with no media content is described, never sent as nothing.
func TestLiveToolResponse_PartWithoutMediaIsDescribed(t *testing.T) {
	got := liveToolResponseWire(t, "gemini-3.8-live", []providers.ToolResponse{{
		ToolCallID: "call_empty", Parts: []types.ContentPart{{Type: types.ContentTypeImage}},
	}})

	assert.Equal(t, toolResponseOf(map[string]any{
		"id":       "call_empty",
		"response": map[string]any{"result": "[This tool returned an image that was not sent: it has no media content.]"},
	}), got)
}
