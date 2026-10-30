package vllm

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/AltairaLabs/PromptKit/runtime/v2/providers"
	"github.com/AltairaLabs/PromptKit/runtime/v2/types"
)

const (
	imageTestURL  = "https://example.com/cat.png"
	imageTestData = "aW1hZ2VieXRlcw=="
)

// imageRequestPaths are the four request entry points; each builds its own
// request body, so each must carry the image (#2281).
var imageRequestPaths = []struct {
	name string
	send func(ctx context.Context, p providers.Provider, req providers.PredictionRequest) error
}{
	{"Predict", func(ctx context.Context, p providers.Provider, req providers.PredictionRequest) error {
		_, err := p.Predict(ctx, req)
		return err
	}},
	{"PredictStream", func(ctx context.Context, p providers.Provider, req providers.PredictionRequest) error {
		return drain(p.PredictStream(ctx, req))
	}},
	{"PredictWithTools", func(ctx context.Context, p providers.Provider, req providers.PredictionRequest) error {
		tp := p.(providers.ToolSupport)
		tools, err := tp.BuildTooling(imageTestTools())
		if err != nil {
			return err
		}
		_, _, err = tp.PredictWithTools(ctx, req, tools, "auto")
		return err
	}},
	{"PredictStreamWithTools", func(ctx context.Context, p providers.Provider, req providers.PredictionRequest) error {
		tp := p.(providers.ToolSupport)
		tools, err := tp.BuildTooling(imageTestTools())
		if err != nil {
			return err
		}
		return drain(tp.PredictStreamWithTools(ctx, req, tools, "auto"))
	}},
}

// TestImageParts_ReachTheWire sends a text+URL-image+inline-image message
// through a spec-built provider on every request path and checks the request
// body vLLM receives carries both images as OpenAI-style image_url parts.
func TestImageParts_ReachTheWire(t *testing.T) {
	for _, path := range imageRequestPaths {
		t.Run(path.name, func(t *testing.T) {
			p, captured := newSpecProviderWithCapture(t)

			msg := types.Message{Role: "user"}
			msg.AddTextPart("What is in these pictures?")
			msg.AddImagePartFromURL(imageTestURL, nil)
			msg.AddPart(types.NewImagePartFromData(imageTestData, "image/png", nil))

			err := path.send(t.Context(), p, providers.PredictionRequest{
				System:   "be brief",
				Messages: []types.Message{msg},
			})
			require.NoError(t, err)

			user := lastUserMessage(t, captured())
			parts, ok := user["content"].([]any)
			require.True(t, ok, "image message content must be a part array, got %T: %v",
				user["content"], user["content"])
			require.Len(t, parts, 3)

			assert.Equal(t, map[string]any{"type": "text", "text": "What is in these pictures?"}, parts[0])
			assert.Equal(t, map[string]any{
				"type": "image_url", "image_url": map[string]any{"url": imageTestURL},
			}, parts[1])
			assert.Equal(t, map[string]any{
				"type": "image_url", "image_url": map[string]any{"url": "data:image/png;base64," + imageTestData},
			}, parts[2])
		})
	}
}

// TestTextOnlyMessages_KeepStringContent pins the unchanged wire shape for a
// message with no image: content stays a plain string on every path.
func TestTextOnlyMessages_KeepStringContent(t *testing.T) {
	for _, path := range imageRequestPaths {
		t.Run(path.name, func(t *testing.T) {
			p, captured := newSpecProviderWithCapture(t)

			msg := types.Message{Role: "user"}
			msg.AddTextPart("hello ")
			msg.AddTextPart("there")

			err := path.send(t.Context(), p, providers.PredictionRequest{
				Messages: []types.Message{msg, {Role: "user", Content: "plain"}},
			})
			require.NoError(t, err)

			raw, ok := captured()["messages"].([]any)
			require.True(t, ok)
			require.Len(t, raw, 2)
			assert.Equal(t, "hello there", raw[0].(map[string]any)["content"])
			assert.Equal(t, "plain", raw[1].(map[string]any)["content"])
		})
	}
}

// An image that cannot be loaded fails the request rather than being
// silently dropped from it.
func TestImageParts_UnloadableImageFailsRequest(t *testing.T) {
	p, _ := newSpecProviderWithCapture(t)

	msg := types.Message{Role: "user"}
	msg.AddPart(types.ContentPart{Type: types.ContentTypeImage, Media: &types.MediaContent{MIMEType: "image/png"}})

	_, err := p.Predict(t.Context(), providers.PredictionRequest{Messages: []types.Message{msg}})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to build multimodal content for message 0")
}

// newSpecProviderWithCapture builds a vLLM provider from a spec, pointed at a
// server that records the last request body and answers in JSON or SSE to
// match the request's stream flag.
func newSpecProviderWithCapture(t *testing.T) (providers.Provider, func() map[string]any) {
	t.Helper()
	var (
		mu   sync.Mutex
		body map[string]any
	)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var got map[string]any
		if err := json.NewDecoder(r.Body).Decode(&got); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		mu.Lock()
		body = got
		mu.Unlock()

		if stream, _ := got["stream"].(bool); stream {
			w.Header().Set("Content-Type", "text/event-stream")
			_, _ = w.Write([]byte("data: {\"choices\":[{\"index\":0,\"delta\":{\"content\":\"ok\"}," +
				"\"finish_reason\":\"stop\"}]}\n\ndata: [DONE]\n\n"))
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(vllmChatResponse{
			Choices: []vllmChatChoice{{
				Message:      vllmMessage{Role: "assistant", Content: "ok"},
				FinishReason: "stop",
			}},
		})
	}))
	t.Cleanup(srv.Close)

	p, err := providers.CreateProviderFromSpec(providers.ProviderSpec{
		ID: "vllm-image", Type: "vllm", Model: "llava", BaseURL: srv.URL,
	})
	require.NoError(t, err)
	t.Cleanup(func() { _ = p.Close() })

	return p, func() map[string]any {
		mu.Lock()
		defer mu.Unlock()
		require.NotNil(t, body, "server received no request")
		return body
	}
}

func lastUserMessage(t *testing.T, body map[string]any) map[string]any {
	t.Helper()
	raw, ok := body["messages"].([]any)
	require.True(t, ok, "request body has no messages array: %v", body)
	for i := len(raw) - 1; i >= 0; i-- {
		if m, ok := raw[i].(map[string]any); ok && m["role"] == "user" {
			return m
		}
	}
	t.Fatalf("no user message on the wire: %v", raw)
	return nil
}

func imageTestTools() []*providers.ToolDescriptor {
	return []*providers.ToolDescriptor{{
		Name:        "describe",
		Description: "Describe an image",
		InputSchema: json.RawMessage(`{"type":"object"}`),
	}}
}

// drain consumes a stream so its request completes, surfacing any chunk error.
func drain(ch <-chan providers.StreamChunk, err error) error {
	if err != nil {
		return err
	}
	var errs []string
	for chunk := range ch {
		if chunk.Error != nil {
			errs = append(errs, chunk.Error.Error())
		}
	}
	if len(errs) > 0 {
		return errors.New(strings.Join(errs, "; "))
	}
	return nil
}
