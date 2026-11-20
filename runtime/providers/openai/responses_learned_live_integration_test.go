//go:build integration

package openai

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/http/httputil"
	"net/url"
	"os"
	"strings"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/AltairaLabs/PromptKit/runtime/v2/providers"
	"github.com/AltairaLabs/PromptKit/runtime/v2/types"
)

// TestOpenAI_LearnsResponsesBehindProxy_Live is the live check for the
// Responses fallback. A local reverse proxy fronts api.openai.com, so the
// provider sees a non-OpenAI host and starts on Chat Completions, as it does
// behind any OpenAI-compatible gateway. Each model is served by OpenAI on
// v1/responses only and is named so that the -pro rule does not route it
// there. The first turn must be resent to Responses and answered; the second
// must go to Responses directly.
func TestOpenAI_LearnsResponsesBehindProxy_Live(t *testing.T) {
	if os.Getenv("OPENAI_API_KEY") == "" {
		t.Skip("OPENAI_API_KEY not set")
	}
	upstream, err := url.Parse("https://api.openai.com")
	require.NoError(t, err)
	var mu sync.Mutex
	var paths []string
	proxy := httputil.NewSingleHostReverseProxy(upstream)
	base := proxy.Director
	proxy.Director = func(r *http.Request) {
		mu.Lock()
		paths = append(paths, r.URL.Path)
		mu.Unlock()
		base(r)
		r.Host = upstream.Host
	}
	srv := httptest.NewServer(proxy)
	defer srv.Close()
	take := func() []string {
		mu.Lock()
		defer mu.Unlock()
		got := paths
		paths = nil
		return got
	}

	// gpt-5.3-codex: "Use the v1/responses endpoint instead";
	// gpt-5.4-pro-2026-03-05: "not a chat model".
	for _, model := range []string{"gpt-5.3-codex", "gpt-5.4-pro-2026-03-05"} {
		p, err := providers.CreateProviderFromSpec(providers.ProviderSpec{
			ID: "proxy-live", Type: "openai", Model: model, BaseURL: srv.URL + "/v1",
			Defaults: providers.ProviderDefaults{MaxTokens: 2048},
		})
		require.NoError(t, err)
		req := providers.PredictionRequest{
			Messages: []types.Message{{Role: "user", Content: "Reply with the single word OK"}},
		}

		resp, err := p.Predict(context.Background(), req)
		require.NoError(t, err, model)
		assert.Contains(t, strings.ToUpper(resp.Content), "OK", model)
		assert.Equal(t, []string{"/v1/chat/completions", "/v1/responses"}, take(), model)

		resp, err = p.Predict(context.Background(), req)
		require.NoError(t, err, model)
		assert.Contains(t, strings.ToUpper(resp.Content), "OK", model)
		assert.Equal(t, []string{"/v1/responses"}, take(), model)
	}
}

// The tool path learns the same way: its 404 is the typed ProviderHTTPError.
func TestOpenAI_ToolPathLearnsResponsesBehindProxy_Live(t *testing.T) {
	if os.Getenv("OPENAI_API_KEY") == "" {
		t.Skip("OPENAI_API_KEY not set")
	}
	upstream, err := url.Parse("https://api.openai.com")
	require.NoError(t, err)
	var mu sync.Mutex
	var paths []string
	proxy := httputil.NewSingleHostReverseProxy(upstream)
	base := proxy.Director
	proxy.Director = func(r *http.Request) {
		mu.Lock()
		paths = append(paths, r.URL.Path)
		mu.Unlock()
		base(r)
		r.Host = upstream.Host
	}
	srv := httptest.NewServer(proxy)
	defer srv.Close()

	p, err := providers.CreateProviderFromSpec(providers.ProviderSpec{
		ID: "proxy-live", Type: "openai", Model: "gpt-5.3-codex", BaseURL: srv.URL + "/v1",
		Defaults: providers.ProviderDefaults{MaxTokens: 2048},
	})
	require.NoError(t, err)
	tp := p.(providers.ToolSupport)
	tools, err := tp.BuildTooling([]*providers.ToolDescriptor{{
		Name: "get_time", Description: "Returns the current time",
		InputSchema: []byte(`{"type":"object","properties":{}}`),
	}})
	require.NoError(t, err)

	resp, _, err := tp.PredictWithTools(context.Background(), providers.PredictionRequest{
		Messages: []types.Message{{Role: "user", Content: "Reply with the single word OK. Do not call any tool."}},
	}, tools, "auto")

	require.NoError(t, err)
	assert.Contains(t, strings.ToUpper(resp.Content), "OK")
	mu.Lock()
	defer mu.Unlock()
	assert.Equal(t, []string{"/v1/chat/completions", "/v1/responses"}, paths)
}
