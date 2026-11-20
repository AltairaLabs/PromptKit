package gemini

import (
	"context"
	"encoding/json"
	"io"
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

// multimodalFunctionResponseRefusalBody is the 400 Gemini returns, observed
// live on gemini-2.5-flash, for a function response that carries media.
const multimodalFunctionResponseRefusalBody = `{"error":{"code":400,` +
	`"message":"Multimodal function responses are not supported for this model.","status":"INVALID_ARGUMENT"}}`

// toolMediaServer is a generateContent stand-in that records each request's
// contents and, when refuse is set, answers a request whose function responses
// carry media with Gemini's refusal.
type toolMediaServer struct {
	refuse bool
	mu     sync.Mutex
	bodies []string
}

func (s *toolMediaServer) start(t *testing.T) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		var req struct {
			Contents json.RawMessage `json:"contents"`
		}
		_ = json.Unmarshal(raw, &req)
		s.mu.Lock()
		s.bodies = append(s.bodies, string(req.Contents))
		s.mu.Unlock()
		stream := strings.HasSuffix(r.URL.Path, "streamGenerateContent")
		if s.refuse && functionResponsesCarryMedia(req.Contents) {
			w.WriteHeader(http.StatusBadRequest)
			_, _ = io.WriteString(w, multimodalFunctionResponseRefusalBody)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		if stream {
			_, _ = io.WriteString(w, "["+geminiSuccessBody+"]")
			return
		}
		_, _ = io.WriteString(w, geminiSuccessBody)
	}))
	t.Cleanup(srv.Close)
	return srv
}

func (s *toolMediaServer) requests() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]string(nil), s.bodies...)
}

// functionResponsesCarryMedia reports whether any function response in
// contents has parts.
func functionResponsesCarryMedia(contents json.RawMessage) bool {
	var cs []struct {
		Parts []struct {
			FunctionResponse *struct {
				Parts []any `json:"parts"`
			} `json:"functionResponse"`
		} `json:"parts"`
	}
	_ = json.Unmarshal(contents, &cs)
	for _, c := range cs {
		for _, p := range c.Parts {
			if p.FunctionResponse != nil && len(p.FunctionResponse.Parts) > 0 {
				return true
			}
		}
	}
	return false
}

// toolMediaProvider builds a Gemini provider on model from its spec, as a
// host does, with additional_config and its API at srv.
func toolMediaProvider(t *testing.T, srv *httptest.Server, model string, additional map[string]any) *ToolProvider {
	t.Helper()
	t.Setenv("GEMINI_API_KEY", "k")
	p, err := providers.CreateProviderFromSpec(providers.ProviderSpec{
		ID: "g", Type: "gemini", Model: model, BaseURL: srv.URL, AdditionalConfig: additional,
	})
	require.NoError(t, err)
	return p.(*ToolProvider)
}

// cameraRound is a history whose last round is a tool returning an image.
func cameraRound() []types.Message {
	return []types.Message{
		{Role: "user", Content: "Take a picture"},
		{Role: "assistant", ToolCalls: []types.MessageToolCall{{ID: "c1", Name: "camera", Args: json.RawMessage(`{}`)}}},
		{Role: "tool", ToolResult: &types.MessageToolResult{ID: "c1", Name: "camera", Parts: []types.ContentPart{
			types.NewTextPart("done"), types.NewImagePartFromData("aW1n", types.MIMETypeImagePNG, nil),
		}}},
	}
}

const (
	cameraMediaInFunctionResponse = `[
		{"role":"user","parts":[{"text":"Take a picture"}]},
		{"role":"model","parts":[{"functionCall":{"name":"camera","args":{}},
			"thoughtSignature":"skip_thought_signature_validator"}]},
		{"role":"user","parts":[{"functionResponse":{"name":"camera","response":{"result":"done"},
			"parts":[{"inlineData":{"mimeType":"image/png","data":"aW1n"}}]}}]}
	]`
	cameraMediaAfterTheRound = `[
		{"role":"user","parts":[{"text":"Take a picture"}]},
		{"role":"model","parts":[{"functionCall":{"name":"camera","args":{}}}]},
		{"role":"user","parts":[{"functionResponse":{"name":"camera","response":{
			"result":"done\n[The image this tool returned is attached in the next user message.]"}}}]},
		{"role":"user","parts":[{"text":"Media returned by tool camera (call c1):"},
			{"inlineData":{"mimeType":"image/png","data":"aW1n"}}]}
	]`
)

// A model's name does not decide where a tool's media goes: a model the API
// lets take media in a function response gets it there, whatever its name.
func TestToolMedia_GoesInTheFunctionResponseWhateverTheModelName(t *testing.T) {
	for _, model := range []string{"gemini-2.5-flash", "gemini-flash-latest", "my-tuned-model"} {
		server := &toolMediaServer{}
		p := toolMediaProvider(t, server.start(t), model, nil)
		_, err := p.Predict(context.Background(), providers.PredictionRequest{Messages: cameraRound()})
		require.NoError(t, err, model)
		reqs := server.requests()
		require.Len(t, reqs, 1, model)
		assert.JSONEq(t, cameraMediaInFunctionResponse, reqs[0], model)
	}
}

// When the API refuses media in a function response, the provider sends the
// round again with the media in a user content after it, and from then on
// sends that shape at once: on the same call path and on the others.
func TestToolMedia_LearnsFromTheAPIsRefusal(t *testing.T) {
	server := &toolMediaServer{refuse: true}
	p := toolMediaProvider(t, server.start(t), "gemini-2.5-flash", nil)

	resp, err := p.Predict(context.Background(), providers.PredictionRequest{Messages: cameraRound()})
	require.NoError(t, err)
	assert.Equal(t, "hello", resp.Content)
	reqs := server.requests()
	require.Len(t, reqs, 2)
	assert.JSONEq(t, cameraMediaInFunctionResponse, reqs[0])
	assert.JSONEq(t, cameraMediaAfterTheRound, reqs[1])

	ch, err := p.PredictStream(context.Background(), providers.PredictionRequest{Messages: cameraRound()})
	require.NoError(t, err)
	for range ch {
	}
	reqs = server.requests()
	require.Len(t, reqs, 3, "the learned shape is sent at once")
	assert.JSONEq(t, cameraMediaAfterTheRound, reqs[2])
}

// Every call path learns from the refusal and answers from its retry.
func TestToolMedia_EveryCallPathLearnsFromTheAPIsRefusal(t *testing.T) {
	drain := func(t *testing.T, ch <-chan providers.StreamChunk) string {
		t.Helper()
		var text string
		for c := range ch {
			require.NoError(t, c.Error)
			text += c.Delta
		}
		return text
	}
	paths := map[string]func(t *testing.T, p *ToolProvider, tools providers.ProviderTools) string{
		"PredictWithTools": func(t *testing.T, p *ToolProvider, tools providers.ProviderTools) string {
			resp, _, err := p.PredictWithTools(context.Background(),
				providers.PredictionRequest{Messages: cameraRound()}, tools, "auto")
			require.NoError(t, err)
			return resp.Content
		},
		"PredictStream": func(t *testing.T, p *ToolProvider, _ providers.ProviderTools) string {
			ch, err := p.PredictStream(context.Background(), providers.PredictionRequest{Messages: cameraRound()})
			require.NoError(t, err)
			return drain(t, ch)
		},
		"PredictStreamWithTools": func(t *testing.T, p *ToolProvider, tools providers.ProviderTools) string {
			ch, err := p.PredictStreamWithTools(context.Background(),
				providers.PredictionRequest{Messages: cameraRound()}, tools, "auto")
			require.NoError(t, err)
			return drain(t, ch)
		},
	}
	for name, call := range paths {
		t.Run(name, func(t *testing.T) {
			server := &toolMediaServer{refuse: true}
			p := toolMediaProvider(t, server.start(t), "gemini-2.5-flash", nil)
			tools, err := p.BuildTooling([]*providers.ToolDescriptor{{
				Name: "camera", Description: "c", InputSchema: json.RawMessage(`{"type":"object"}`),
			}})
			require.NoError(t, err)

			assert.Equal(t, "hello", call(t, p, tools))
			reqs := server.requests()
			require.Len(t, reqs, 2)
			assert.JSONEq(t, cameraMediaInFunctionResponse, reqs[0])
			assert.JSONEq(t, cameraMediaAfterTheRound, reqs[1])
		})
	}
}

// additional_config.function_response_media declares where the media goes,
// and is never second-guessed: false sends it after the round at once; true
// sends it in the function response and returns the API's refusal.
func TestToolMedia_ConfigDeclaresWhereTheMediaGoes(t *testing.T) {
	off := &toolMediaServer{}
	p := toolMediaProvider(t, off.start(t), "gemini-3.8-flash",
		map[string]any{"function_response_media": false})
	_, err := p.Predict(context.Background(), providers.PredictionRequest{Messages: cameraRound()})
	require.NoError(t, err)
	require.Len(t, off.requests(), 1)
	assert.JSONEq(t, cameraMediaAfterTheRound, off.requests()[0])

	on := &toolMediaServer{refuse: true}
	p = toolMediaProvider(t, on.start(t), "gemini-2.5-flash",
		map[string]any{"function_response_media": true})
	_, err = p.Predict(context.Background(), providers.PredictionRequest{Messages: cameraRound()})
	var httpErr *providers.ProviderHTTPError
	require.ErrorAs(t, err, &httpErr)
	assert.Equal(t, http.StatusBadRequest, httpErr.StatusCode)
	assert.Len(t, on.requests(), 1, "a declared shape is not retried")
}

// A function_response_media that is not a boolean is a configuration error.
func TestToolMedia_ConfigMustBeABoolean(t *testing.T) {
	_, err := providers.CreateProviderFromSpec(providers.ProviderSpec{
		ID: "g", Type: "gemini", Model: "m", AdditionalConfig: map[string]any{"function_response_media": "no"},
	})
	require.EqualError(t, err,
		`gemini provider "g": additional_config.function_response_media must be true or false, got "no"`)
}
