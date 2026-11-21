package ollama

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/AltairaLabs/PromptKit/runtime/v2/providers"
	"github.com/AltairaLabs/PromptKit/runtime/v2/types"
)

// A non-200 on the Ollama tool path is a typed ProviderHTTPError carrying the
// status, the body, the URL and the provider ID.
func TestToolPath_Non200IsAProviderHTTPError(t *testing.T) {
	const body = `{"error":"model 'nope' not found"}`
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		_, _ = io.WriteString(w, body)
	}))
	t.Cleanup(srv.Close)
	p, err := providers.CreateProviderFromSpec(providers.ProviderSpec{
		ID: "ollama-tools", Type: "ollama", Model: "nope", BaseURL: srv.URL,
	})
	if err != nil {
		t.Fatalf("CreateProviderFromSpec: %v", err)
	}

	_, _, err = p.(providers.ToolSupport).PredictWithTools(context.Background(), providers.PredictionRequest{
		Messages: []types.Message{{Role: "user", Content: "hi"}},
	}, nil, "")

	var httpErr *providers.ProviderHTTPError
	if !errors.As(err, &httpErr) {
		t.Fatalf("got %T (%v), want *providers.ProviderHTTPError", err, err)
	}
	want := providers.ProviderHTTPError{
		StatusCode: http.StatusNotFound, URL: srv.URL + ollamaChatCompletionsPath,
		Body: body, Provider: "ollama-tools",
	}
	if *httpErr != want {
		t.Errorf("got %+v, want %+v", *httpErr, want)
	}
}
