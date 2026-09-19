package providers_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/AltairaLabs/PromptKit/runtime/v2/providers"
	_ "github.com/AltairaLabs/PromptKit/runtime/v2/providers/openai"
	"github.com/AltairaLabs/PromptKit/runtime/v2/types"
)

// azureFakeCredential stands in for a real Azure AD credential. It has to be
// non-nil: openai's CredentialFactory takes a no-credential branch that does not
// forward Platform/PlatformConfig at all, which would mask what these tests
// check.
type azureFakeCredential struct{}

func (azureFakeCredential) Apply(context.Context, *http.Request) error { return nil }
func (azureFakeCredential) Type() string                               { return "azure" }

// azureRequestPath builds a provider from spec, issues one prediction against a
// local server, and reports the request path that server actually received.
//
// Asserting the path is the point: the failure this guards against is one where
// provider construction succeeds and only the request URL is wrong, so a test
// that stops at "a provider was returned" would pass while every call 404s.
func azureRequestPath(t *testing.T, spec providers.ProviderSpec) string {
	t.Helper()

	var got string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = r.URL.Path
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"choices":[{"message":{"role":"assistant","content":"ok"}}]}`))
	}))
	defer srv.Close()

	if spec.BaseURL != "" {
		spec.BaseURL = srv.URL
	}
	spec.PlatformConfig.Endpoint = srv.URL

	p, err := providers.CreateProviderFromSpec(spec)
	if err != nil {
		t.Fatalf("CreateProviderFromSpec: %v", err)
	}
	_, _ = p.Predict(context.Background(), providers.PredictionRequest{
		Messages: []types.Message{{Role: "user", Content: "hi"}},
	})
	return got
}

func azureSpec(baseURL string) providers.ProviderSpec {
	return providers.ProviderSpec{
		ID:             "openai",
		Type:           "openai",
		Model:          "gpt-4-1-mini",
		BaseURL:        baseURL,
		Credential:     azureFakeCredential{},
		Platform:       "azure",
		PlatformConfig: &providers.PlatformConfig{Type: "azure", Endpoint: "https://placeholder"},
	}
}

// This is the shape sdk.WithAzure now produces: an empty BaseURL, with the
// account host carried on PlatformConfig.Endpoint. The factory must turn that
// into the per-deployment path, because Azure has no route at /chat/completions.
func TestAzureEmptyBaseURLUsesDeploymentPath(t *testing.T) {
	got := azureRequestPath(t, azureSpec(""))

	want := "/openai/deployments/gpt-4-1-mini/chat/completions"
	if got != want {
		t.Errorf("request path = %q, want %q", got, want)
	}
}

// A BaseURL that is set still wins, so an operator pointing at a gateway or a
// recorded fixture is not overridden.
func TestAzureExplicitBaseURLIsRespected(t *testing.T) {
	got := azureRequestPath(t, azureSpec("https://placeholder"))

	if got != "/chat/completions" {
		t.Errorf("request path = %q, want an explicit BaseURL to be used verbatim", got)
	}
}
