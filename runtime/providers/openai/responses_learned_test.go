package openai

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

// The 404 bodies OpenAI returns on v1/chat/completions for a model it serves
// only on v1/responses, captured live (2026-11-20): gpt-5-pro and its dated
// snapshot, gpt-5.3-codex, and gpt-5.2-pro / gpt-5.4-pro / gpt-5.5-pro / o3-pro.
const (
	chatRejectOnlyResponses = `{"error":{"message":"This model is only supported in v1/responses and not in ` +
		`v1/chat/completions.","type":"invalid_request_error","param":"model","code":null}}`
	chatRejectUseResponses = `{"error":{"message":"This model is not supported in the v1/chat/completions ` +
		`endpoint. Use the v1/responses endpoint instead.","type":"invalid_request_error","param":"model","code":null}}`
	chatRejectNotChatModel = `{"error":{"message":"This is not a chat model and thus not supported in the ` +
		`v1/chat/completions endpoint. Did you mean to use v1/completions?","type":"invalid_request_error",` +
		`"param":"model","code":null}}`
	chatRejectModelNotFound = `{"error":{"message":"The model ` + "`nope`" + ` does not exist or you do not have ` +
		`access to it.","type":"invalid_request_error","param":null,"code":"model_not_found"}}`
)

// responsesOnlyHost is an OpenAI-compatible host that serves the model on
// /responses only: /chat/completions gets reject (a 404 body). It records the
// path and body of every request.
type responsesOnlyHost struct {
	srv    *httptest.Server
	mu     sync.Mutex
	paths  []string
	bodies []map[string]any
}

func newResponsesOnlyHost(t *testing.T, reject string) *responsesOnlyHost {
	t.Helper()
	h := &responsesOnlyHost{}
	h.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		var body map[string]any
		_ = json.Unmarshal(raw, &body)
		h.mu.Lock()
		h.paths = append(h.paths, r.URL.Path)
		h.bodies = append(h.bodies, body)
		h.mu.Unlock()
		if !strings.HasSuffix(r.URL.Path, "/responses") {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusNotFound)
			_, _ = io.WriteString(w, reject)
			return
		}
		if body["stream"] == true {
			w.Header().Set("Content-Type", "text/event-stream")
			_, _ = io.WriteString(w, "data: {\"type\":\"response.output_text.delta\",\"delta\":\"OK\"}\n\n")
			_, _ = io.WriteString(w, "data: {\"type\":\"response.completed\",\"response\":{\"status\":\"completed\","+
				"\"usage\":{\"input_tokens\":3,\"output_tokens\":1}}}\n\n")
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"id":"r1","object":"response","status":"completed","model":"m",`+
			`"output":[{"type":"message","role":"assistant","content":[{"type":"output_text","text":"OK"}]}],`+
			`"usage":{"input_tokens":3,"output_tokens":1}}`)
	}))
	t.Cleanup(h.srv.Close)
	return h
}

func (h *responsesOnlyHost) takePaths() []string {
	h.mu.Lock()
	defer h.mu.Unlock()
	got := h.paths
	h.paths = nil
	return got
}

// proxyProvider builds an openai provider for model behind the host, through
// the factory, as a provider config with a base_url does.
func proxyProvider(t *testing.T, h *responsesOnlyHost, model string, extra map[string]any) providers.Provider {
	t.Helper()
	t.Setenv("OPENAI_API_KEY", "test-key")
	p, err := providers.CreateProviderFromSpec(providers.ProviderSpec{
		ID: "proxy", Type: "openai", Model: model, BaseURL: h.srv.URL,
		Defaults:         providers.ProviderDefaults{MaxTokens: 32},
		AdditionalConfig: extra,
	})
	require.NoError(t, err)
	return p
}

func hiRequest() providers.PredictionRequest {
	return providers.PredictionRequest{
		Messages:  []types.Message{{Role: "user", Content: "hi"}},
		MaxTokens: 32,
	}
}

// A model a proxy serves only on Responses, under a name the -pro rule does
// not match (a dated snapshot), is sent to Chat Completions first. The 404
// says to use Responses; the provider resends there, and remembers it.
func TestLearnedResponses_PredictSwitchesAndRemembers(t *testing.T) {
	h := newResponsesOnlyHost(t, chatRejectOnlyResponses)
	p := proxyProvider(t, h, "gpt-5-pro-2025-10-06", nil)

	resp, err := p.Predict(context.Background(), hiRequest())

	require.NoError(t, err)
	assert.Equal(t, "OK", resp.Content)
	assert.Equal(t, []string{"/chat/completions", "/responses"}, h.takePaths())

	resp, err = p.Predict(context.Background(), hiRequest())

	require.NoError(t, err)
	assert.Equal(t, "OK", resp.Content)
	assert.Equal(t, []string{"/responses"}, h.takePaths(), "the second call goes straight to Responses")
}

// Each wording OpenAI uses for a Responses-only model is recognized.
func TestLearnedResponses_EveryRejectionWording(t *testing.T) {
	for name, reject := range map[string]string{
		"only supported in v1/responses": chatRejectOnlyResponses,
		"use the v1/responses endpoint":  chatRejectUseResponses,
		"not a chat model":               chatRejectNotChatModel,
	} {
		t.Run(name, func(t *testing.T) {
			h := newResponsesOnlyHost(t, reject)
			p := proxyProvider(t, h, "alias", nil)

			resp, err := p.Predict(context.Background(), hiRequest())

			require.NoError(t, err)
			assert.Equal(t, "OK", resp.Content)
			assert.Equal(t, []string{"/chat/completions", "/responses"}, h.takePaths())
		})
	}
}

// The tool path switches too, and sends the tools in the Responses shape.
func TestLearnedResponses_PredictWithToolsSwitches(t *testing.T) {
	h := newResponsesOnlyHost(t, chatRejectUseResponses)
	tp := proxyProvider(t, h, "codex-alias", nil).(providers.ToolSupport)
	tools, err := tp.BuildTooling([]*providers.ToolDescriptor{{
		Name: "lookup", Description: "Look up", InputSchema: json.RawMessage(`{"type":"object"}`),
	}})
	require.NoError(t, err)

	resp, _, err := tp.PredictWithTools(context.Background(), hiRequest(), tools, "auto")

	require.NoError(t, err)
	assert.Equal(t, "OK", resp.Content)
	assert.Equal(t, []string{"/chat/completions", "/responses"}, h.takePaths())
	sent := h.bodies[len(h.bodies)-1]["tools"].([]any)[0].(map[string]any)
	assert.Equal(t, "lookup", sent["name"], "Responses tools carry the name at the top level")
}

// The streaming path switches too.
func TestLearnedResponses_PredictStreamSwitches(t *testing.T) {
	h := newResponsesOnlyHost(t, chatRejectOnlyResponses)
	p := proxyProvider(t, h, "pro-alias", nil)

	ch, err := p.PredictStream(context.Background(), hiRequest())
	require.NoError(t, err)
	var text string
	for c := range ch {
		require.NoError(t, c.Error)
		text = c.Content
	}

	assert.Equal(t, "OK", text)
	assert.Equal(t, []string{"/chat/completions", "/responses"}, h.takePaths())
}

// The tool streaming path switches too.
func TestLearnedResponses_PredictStreamWithToolsSwitches(t *testing.T) {
	h := newResponsesOnlyHost(t, chatRejectOnlyResponses)
	tp := proxyProvider(t, h, "pro-alias", nil).(providers.ToolSupport)
	ch, err := tp.PredictStreamWithTools(context.Background(), hiRequest(), nil, "")
	require.NoError(t, err)
	var text string
	for c := range ch {
		require.NoError(t, c.Error)
		text = c.Content
	}

	assert.Equal(t, "OK", text)
	assert.Equal(t, []string{"/chat/completions", "/responses"}, h.takePaths())
}

// A declared api_mode is never second-guessed: the 404 is returned as is.
func TestLearnedResponses_DeclaredCompletionsIsKept(t *testing.T) {
	h := newResponsesOnlyHost(t, chatRejectOnlyResponses)
	p := proxyProvider(t, h, "gpt-5-pro-2025-10-06", map[string]any{"api_mode": "completions"})

	_, err := p.Predict(context.Background(), hiRequest())

	require.Error(t, err)
	assert.Contains(t, err.Error(), "only supported in v1/responses")
	assert.Equal(t, []string{"/chat/completions"}, h.takePaths())
}

// Any other 404 is not a reason to switch.
func TestLearnedResponses_OtherNotFoundIsReturned(t *testing.T) {
	h := newResponsesOnlyHost(t, chatRejectModelNotFound)
	p := proxyProvider(t, h, "nope", nil)

	_, err := p.Predict(context.Background(), hiRequest())

	require.Error(t, err)
	assert.Contains(t, err.Error(), "does not exist")
	assert.Equal(t, []string{"/chat/completions"}, h.takePaths())
}

// On Azure the provider always uses Chat Completions, so the rejection is
// returned rather than resent to a Responses route.
func TestLearnedResponses_NotOnAzure(t *testing.T) {
	h := newResponsesOnlyHost(t, chatRejectOnlyResponses)
	p, err := providers.CreateProviderFromSpec(providers.ProviderSpec{
		ID: "az", Type: "openai", Model: "my-pro", Credential: &tokenCredential{},
		Platform:       "azure",
		PlatformConfig: &providers.PlatformConfig{Type: "azure", Endpoint: h.srv.URL},
		Defaults:       providers.ProviderDefaults{MaxTokens: 32},
	})
	require.NoError(t, err)

	_, err = p.Predict(context.Background(), hiRequest())

	require.Error(t, err)
	assert.Len(t, h.takePaths(), 1, "one request, not resent")
}

// A provider already on Responses (here by the -pro name rule) has nowhere to
// switch to, so a rejection that names Responses is returned, not resent.
func TestLearnedResponses_AlreadyOnResponsesIsNotResent(t *testing.T) {
	var paths []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		paths = append(paths, r.URL.Path)
		w.WriteHeader(http.StatusNotFound)
		_, _ = io.WriteString(w, chatRejectOnlyResponses)
	}))
	t.Cleanup(srv.Close)
	t.Setenv("OPENAI_API_KEY", "test-key")
	p, err := providers.CreateProviderFromSpec(providers.ProviderSpec{
		ID: "proxy", Type: "openai", Model: "gpt-5-pro", BaseURL: srv.URL,
		Defaults: providers.ProviderDefaults{MaxTokens: 32},
	})
	require.NoError(t, err)

	_, err = p.Predict(context.Background(), hiRequest())

	require.Error(t, err)
	assert.Equal(t, []string{"/responses"}, paths)
}
