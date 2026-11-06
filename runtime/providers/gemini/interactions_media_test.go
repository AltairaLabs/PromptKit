package gemini

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/AltairaLabs/PromptKit/runtime/v2/providers"
	"github.com/AltairaLabs/PromptKit/runtime/v2/types"
)

// A Gemini request with tools and a response schema goes to the Interactions
// API, which used to build every step from the message's text and drop its
// media (#2479). These tests drive both tool entry points against a server
// that records the body, so they pin the wire shape that reaches Google.

// interactionsMediaServer records each request body and answers with a
// minimal completed interaction (non-streaming) or an empty event stream.
type interactionsMediaServer struct {
	*httptest.Server
	hits   atomic.Int32
	bodies chan []byte
}

func newInteractionsMediaServer(t *testing.T) *interactionsMediaServer {
	t.Helper()
	s := &interactionsMediaServer{bodies: make(chan []byte, 4)}
	s.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		s.hits.Add(1)
		body, _ := io.ReadAll(r.Body)
		s.bodies <- body
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"i1","steps":[{"type":"model_output","content":[{"type":"text","text":"{}"}]}]}`))
	}))
	t.Cleanup(s.Close)
	return s
}

// predictInteractions sends msgs through the public tool entry point with a
// schema, so the request is routed to the Interactions API.
func predictInteractions(t *testing.T, url string, stream bool, msgs []types.Message) error {
	t.Helper()
	tp := interactionsTestProvider(t, url)
	tools, err := tp.BuildTooling([]*providers.ToolDescriptor{{
		Name: "probe", Description: "p", InputSchema: json.RawMessage(`{"type":"object"}`)}})
	require.NoError(t, err)
	req := providers.PredictionRequest{Messages: msgs, ResponseFormat: schemaFormat()}
	if !stream {
		_, _, err = tp.PredictWithTools(context.Background(), req, tools, "auto")
		return err
	}
	ch, err := tp.PredictStreamWithTools(context.Background(), req, tools, "auto")
	if err != nil {
		return err
	}
	for range ch { //nolint:revive // drain so the consumer goroutine exits
	}
	return nil
}

// sentInput decodes the input list of the one request the server received.
func (s *interactionsMediaServer) sentInput(t *testing.T) []map[string]any {
	t.Helper()
	require.EqualValues(t, 1, s.hits.Load(), "want exactly one request")
	var body struct {
		Input []map[string]any `json:"input"`
	}
	require.NoError(t, json.Unmarshal(<-s.bodies, &body))
	return body.Input
}

func imageMessage() types.Message {
	msg := types.Message{Role: roleUser}
	msg.AddTextPart("what is in this picture?")
	msg.AddPart(types.NewImagePartFromData("aW1n", types.MIMETypeImagePNG, nil))
	return msg
}

func TestInteractions_SendsMessageMedia(t *testing.T) {
	for _, stream := range []bool{false, true} {
		t.Run(fmt.Sprintf("stream=%v", stream), func(t *testing.T) {
			srv := newInteractionsMediaServer(t)
			require.NoError(t, predictInteractions(t, srv.URL, stream, []types.Message{imageMessage()}))

			input := srv.sentInput(t)
			require.Len(t, input, 2, "the text and the image, in part order")
			assert.Equal(t, map[string]any{"type": "text", "text": "what is in this picture?"}, input[0])
			assert.Equal(t, map[string]any{"type": "image", "data": "aW1n", "mime_type": "image/png"}, input[1])
		})
	}
}

// Every modality the Interactions API accepts goes out as its own content
// type, with a MIME type from that type's documented list.
func TestInteractions_SendsEachModality(t *testing.T) {
	msg := types.Message{Role: roleUser}
	msg.AddPart(types.ContentPart{Type: types.ContentTypeAudio, Media: &types.MediaContent{
		Data: strRef("YXVk"), MIMEType: types.MIMETypeAudioWAV}})
	msg.AddPart(types.ContentPart{Type: types.ContentTypeVideo, Media: &types.MediaContent{
		Data: strRef("dmlk"), MIMEType: types.MIMETypeVideoMP4}})
	msg.AddPart(types.ContentPart{Type: types.ContentTypeDocument, Media: &types.MediaContent{
		Data: strRef("ZG9j"), MIMEType: types.MIMETypePDF}})
	// A part with no MIME type gets the same default generateContent uses.
	msg.AddPart(types.ContentPart{Type: types.ContentTypeImage, Media: &types.MediaContent{Data: strRef("aW1n")}})

	srv := newInteractionsMediaServer(t)
	require.NoError(t, predictInteractions(t, srv.URL, false, []types.Message{msg}))

	assert.Equal(t, []map[string]any{
		{"type": "audio", "data": "YXVk", "mime_type": "audio/wav"},
		{"type": "video", "data": "dmlk", "mime_type": "video/mp4"},
		{"type": "document", "data": "ZG9j", "mime_type": "application/pdf"},
		{"type": "image", "data": "aW1n", "mime_type": "image/png"},
	}, srv.sentInput(t))
}

// An assistant message's media is replayed too, so a generated image is not
// lost from the history.
func TestInteractions_ReplaysAssistantMedia(t *testing.T) {
	assistant := types.Message{Role: roleAssistant}
	assistant.AddTextPart("here it is")
	assistant.AddPart(types.NewImagePartFromData("Z2Vu", types.MIMETypeImagePNG, nil))

	srv := newInteractionsMediaServer(t)
	require.NoError(t, predictInteractions(t, srv.URL, false, []types.Message{
		{Role: roleUser, Content: "draw a cat"}, assistant, {Role: roleUser, Content: "thanks"},
	}))

	input := srv.sentInput(t)
	require.Len(t, input, 4)
	assert.Equal(t, map[string]any{"type": "image", "data": "Z2Vu", "mime_type": "image/png"}, input[2])
}

// A tool result's image goes out in function_result's list form, the only
// form that carries an image.
func TestInteractions_SendsToolResultImage(t *testing.T) {
	result := &types.MessageToolResult{ID: "call-1", Name: "probe", Parts: []types.ContentPart{
		types.NewTextPart("chart drawn"),
		types.NewImagePartFromData("Y2hy", types.MIMETypeImagePNG, nil),
	}}
	srv := newInteractionsMediaServer(t)
	require.NoError(t, predictInteractions(t, srv.URL, false, toolResultHistory(result)))

	input := srv.sentInput(t)
	last := input[len(input)-1]
	assert.Equal(t, "function_result", last["type"])
	assert.Equal(t, []any{
		map[string]any{"type": "text", "text": "chart drawn"},
		map[string]any{"type": "image", "data": "Y2hy", "mime_type": "image/png"},
	}, last["result"])
}

// A text-only tool result keeps the single-item form, which Gemini 2.5
// accepts and the list form is refused by.
func TestInteractions_TextToolResultStaysSingleItem(t *testing.T) {
	result := &types.MessageToolResult{ID: "call-1", Name: "probe", Parts: []types.ContentPart{
		types.NewTextPart(`{"ok":true}`),
	}}
	srv := newInteractionsMediaServer(t)
	require.NoError(t, predictInteractions(t, srv.URL, false, toolResultHistory(result)))

	input := srv.sentInput(t)
	assert.Equal(t, map[string]any{"type": "text", "text": `{"ok":true}`}, input[len(input)-1]["result"])
}

func toolResultHistory(result *types.MessageToolResult) []types.Message {
	return []types.Message{
		{Role: roleUser, Content: "draw a chart"},
		{Role: roleAssistant, ToolCalls: []types.MessageToolCall{
			{ID: "call-1", Name: "probe", Args: json.RawMessage(`{}`)}}},
		{Role: roleToolMessage, ToolResult: result},
	}
}

// Media the Interactions API cannot carry fails the request before any HTTP
// call, with an UnsupportedContentError naming the part, rather than going
// out as the text alone.
func TestInteractions_UnsupportedMediaFailsBeforeTheCall(t *testing.T) {
	textDoc := types.Message{Role: roleUser}
	textDoc.AddTextPart("summarize")
	textDoc.AddPart(types.ContentPart{Type: types.ContentTypeDocument, Media: &types.MediaContent{
		Data: strRef("dHh0"), MIMEType: "text/plain"}})

	cases := []struct {
		name        string
		msgs        []types.Message
		contentType string
		mimeType    string
		partIndex   int
	}{
		{
			// generateContent reads text/* documents; Interactions lists no such type.
			name: "plain-text document", msgs: []types.Message{textDoc},
			contentType: "document", mimeType: "text/plain", partIndex: 1,
		},
		{
			name: "audio in a tool result",
			msgs: toolResultHistory(&types.MessageToolResult{ID: "call-1", Name: "probe", Parts: []types.ContentPart{
				{Type: types.ContentTypeAudio, Media: &types.MediaContent{Data: strRef("YXVk"), MIMEType: types.MIMETypeAudioWAV}},
			}}),
			contentType: "audio", mimeType: types.MIMETypeAudioWAV, partIndex: 0,
		},
	}
	for _, tc := range cases {
		for _, stream := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/stream=%v", tc.name, stream), func(t *testing.T) {
				srv := newInteractionsMediaServer(t)
				err := predictInteractions(t, srv.URL, stream, tc.msgs)

				var unsupported *providers.UnsupportedContentError
				require.True(t, errors.As(err, &unsupported), "want UnsupportedContentError, got %v", err)
				assert.Equal(t, "g-int", unsupported.Provider)
				assert.Equal(t, tc.contentType, unsupported.ContentType)
				assert.Equal(t, tc.mimeType, unsupported.MIMEType)
				assert.Equal(t, tc.partIndex, unsupported.PartIndex)
				assert.Zero(t, srv.hits.Load(), "a request went out without the message's media")
			})
		}
	}
}

// The media is loaded through one loader per message, so the per-message caps
// apply: one message over MaxMediaItems fails before the call, while a
// history whose messages are each at the cap goes out whole.
func TestInteractions_MediaCapsArePerMessage(t *testing.T) {
	images := func(n int) types.Message {
		msg := types.Message{Role: roleUser}
		for i := 0; i < n; i++ {
			msg.AddPart(types.NewImagePartFromData("aW1n", types.MIMETypeImagePNG, nil))
		}
		return msg
	}

	t.Run("one message over the cap", func(t *testing.T) {
		for _, stream := range []bool{false, true} {
			srv := newInteractionsMediaServer(t)
			err := predictInteractions(t, srv.URL, stream, []types.Message{images(providers.MaxMediaItems + 1)})
			require.ErrorIs(t, err, providers.ErrMediaItemLimitExceeded, "stream=%v", stream)
			assert.Zero(t, srv.hits.Load(), "stream=%v", stream)
		}
	})

	t.Run("every message at the cap", func(t *testing.T) {
		srv := newInteractionsMediaServer(t)
		full := images(providers.MaxMediaItems)
		require.NoError(t, predictInteractions(t, srv.URL, false, []types.Message{
			full, {Role: roleAssistant, Content: "ok"}, full,
		}))
		assert.Len(t, srv.sentInput(t), 2*providers.MaxMediaItems+1)
	})
}

// Media that cannot be loaded fails the request rather than being left out.
func TestInteractions_UnloadableMediaFailsBeforeTheCall(t *testing.T) {
	msg := types.Message{Role: roleUser}
	msg.AddTextPart("look")
	msg.AddPart(types.ContentPart{Type: types.ContentTypeImage, Media: &types.MediaContent{MIMEType: types.MIMETypeImagePNG}})

	srv := newInteractionsMediaServer(t)
	err := predictInteractions(t, srv.URL, false, []types.Message{msg})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "image part")
	assert.Zero(t, srv.hits.Load())
}

func strRef(s string) *string { return &s }
