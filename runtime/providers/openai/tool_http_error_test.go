package openai

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/AltairaLabs/PromptKit/runtime/v2/providers"
)

// A non-200 on the tool path is the same typed error the other request paths
// return, so callers can read the status (errors.As) and classify it
// (IsTransient), and a sampling-parameter rejection is still learned.
func TestToolPath_Non200IsAProviderHTTPError(t *testing.T) {
	const body = `{"error":{"message":"bad things","type":"invalid_request_error"}}`
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		_, _ = io.WriteString(w, body)
	}))
	t.Cleanup(srv.Close)
	t.Setenv("OPENAI_API_KEY", "test-key")
	p, err := providers.CreateProviderFromSpec(providers.ProviderSpec{
		ID: "tool-err", Type: "openai", Model: "gpt-x", BaseURL: srv.URL,
		Defaults:         providers.ProviderDefaults{MaxTokens: 32},
		AdditionalConfig: map[string]any{"api_mode": "completions"},
	})
	require.NoError(t, err)

	_, _, err = p.(providers.ToolSupport).PredictWithTools(context.Background(), hiRequest(), nil, "")

	var httpErr *providers.ProviderHTTPError
	require.True(t, errors.As(err, &httpErr), "got %T: %v", err, err)
	assert.Equal(t, http.StatusBadRequest, httpErr.StatusCode)
	assert.Equal(t, body, httpErr.Body)
	assert.Equal(t, srv.URL+"/chat/completions", httpErr.URL)
	assert.Equal(t, "tool-err", httpErr.Provider)
	assert.False(t, providers.IsTransient(err), "a 400 is not transient")
}

// The tool path still learns a rejected sampling parameter from the typed error.
func TestToolPath_RejectedParamStillLearned(t *testing.T) {
	var bodies []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		bodies = append(bodies, string(raw))
		w.Header().Set("Content-Type", "application/json")
		if len(bodies) == 1 {
			w.WriteHeader(http.StatusBadRequest)
			_, _ = io.WriteString(w, `{"error":{"message":"Unsupported parameter: 'temperature' is not supported with this model."}}`)
			return
		}
		_, _ = io.WriteString(w, `{"id":"c1","choices":[{"index":0,"message":{"role":"assistant","content":"OK"},`+
			`"finish_reason":"stop"}],"usage":{"prompt_tokens":1,"completion_tokens":1}}`)
	}))
	t.Cleanup(srv.Close)
	t.Setenv("OPENAI_API_KEY", "test-key")
	p, err := providers.CreateProviderFromSpec(providers.ProviderSpec{
		ID: "tool-err", Type: "openai", Model: "gpt-x", BaseURL: srv.URL,
		Defaults:         providers.ProviderDefaults{MaxTokens: 32, Temperature: 0.5},
		AdditionalConfig: map[string]any{"api_mode": "completions"},
	})
	require.NoError(t, err)
	req := hiRequest()
	req.Temperature = 0.5

	resp, _, err := p.(providers.ToolSupport).PredictWithTools(context.Background(), req, nil, "")

	require.NoError(t, err)
	assert.Equal(t, "OK", resp.Content)
	require.Len(t, bodies, 2)
	assert.Contains(t, bodies[0], `"temperature"`)
	assert.NotContains(t, bodies[1], `"temperature"`)
}
