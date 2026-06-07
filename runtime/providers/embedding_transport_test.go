package providers

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/AltairaLabs/PromptKit/runtime/credentials"
)

func TestEmbeddingProviderSpec_CarriesPlatform(t *testing.T) {
	spec := EmbeddingProviderSpec{
		Type:           "openai",
		Platform:       "azure",
		PlatformConfig: &PlatformConfig{Type: "azure", Endpoint: "https://example.openai.azure.com/"},
	}
	if spec.Platform != "azure" {
		t.Fatalf("Platform = %q, want azure", spec.Platform)
	}
	if spec.PlatformConfig == nil || spec.PlatformConfig.Endpoint == "" {
		t.Fatal("PlatformConfig not carried")
	}
}

func TestBaseEmbeddingProvider_PlatformAuthFlag(t *testing.T) {
	b := &BaseEmbeddingProvider{}
	b.PlatformAuth = true
	if !b.PlatformAuth {
		t.Fatal("PlatformAuth not settable")
	}
}

// fakeCred records that Apply ran and injects a static bearer token,
// standing in for AzureCredential without needing real Azure auth.
type fakeCred struct{ applied bool }

func (f *fakeCred) Apply(_ context.Context, req *http.Request) error {
	f.applied = true
	req.Header.Set("Authorization", "Bearer faketoken")
	return nil
}
func (f *fakeCred) Type() string { return "fake" }

func TestResolveEmbeddingTransport_StaticKey(t *testing.T) {
	tr, err := ResolveEmbeddingTransport(EmbeddingProviderSpec{
		Type:       "openai",
		BaseURL:    "https://custom.example/v1",
		Credential: credentials.NewAPIKeyCredential("sk-static"),
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if tr.Client != nil {
		t.Fatal("static path must not build an HTTP client")
	}
	if tr.PlatformAuth {
		t.Fatal("static path must not set PlatformAuth")
	}
	if tr.BaseURL != "https://custom.example/v1" {
		t.Fatalf("BaseURL = %q", tr.BaseURL)
	}
	if tr.APIKey != "sk-static" {
		t.Fatalf("APIKey = %q", tr.APIKey)
	}
}

func TestResolveEmbeddingTransport_Azure(t *testing.T) {
	var gotAuth, gotPath, gotQuery string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		gotPath = r.URL.Path
		gotQuery = r.URL.RawQuery
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{}`))
	}))
	defer srv.Close()

	cred := &fakeCred{}
	tr, err := ResolveEmbeddingTransport(EmbeddingProviderSpec{
		Type:           "openai",
		Model:          "text-embedding-3-small",
		Platform:       "azure",
		PlatformConfig: &PlatformConfig{Type: "azure", Endpoint: srv.URL},
		Credential:     cred,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if tr.Client == nil {
		t.Fatal("azure path must build an HTTP client")
	}
	if !tr.PlatformAuth {
		t.Fatal("azure path must set PlatformAuth")
	}
	if tr.APIKey != "" {
		t.Fatalf("azure path must not carry a static key, got %q", tr.APIKey)
	}
	wantBase := srv.URL + "/openai/deployments/text-embedding-3-small"
	if tr.BaseURL != wantBase {
		t.Fatalf("BaseURL = %q, want %q", tr.BaseURL, wantBase)
	}

	// Drive a request through the client; the RoundTripper must inject
	// the bearer token and api-version.
	req, _ := http.NewRequest(http.MethodPost, tr.BaseURL+"/embeddings", strings.NewReader("{}"))
	resp, err := tr.Client.Do(req)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	_ = resp.Body.Close()
	if !cred.applied {
		t.Fatal("credential.Apply was not called")
	}
	if gotAuth != "Bearer faketoken" {
		t.Fatalf("Authorization = %q", gotAuth)
	}
	if gotPath != "/openai/deployments/text-embedding-3-small/embeddings" {
		t.Fatalf("path = %q", gotPath)
	}
	if !strings.Contains(gotQuery, "api-version="+credentials.DefaultAzureAPIVersion) {
		t.Fatalf("query = %q, want api-version=%s", gotQuery, credentials.DefaultAzureAPIVersion)
	}
}

func TestResolveEmbeddingTransport_AzureCustomAPIVersion(t *testing.T) {
	tr, err := ResolveEmbeddingTransport(EmbeddingProviderSpec{
		Type:     "openai",
		Model:    "text-embedding-3-small",
		Platform: "azure",
		PlatformConfig: &PlatformConfig{
			Type:             "azure",
			Endpoint:         "https://x.openai.azure.com/",
			AdditionalConfig: map[string]interface{}{"api_version": "2099-01-01"},
		},
		Credential: &fakeCred{},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	rt, ok := tr.Client.Transport.(*platformEmbeddingRoundTripper)
	if !ok {
		t.Fatalf("transport type = %T", tr.Client.Transport)
	}
	if rt.apiVersion != "2099-01-01" {
		t.Fatalf("apiVersion = %q, want 2099-01-01", rt.apiVersion)
	}
}

func TestResolveEmbeddingTransport_AzureRequiresEndpoint(t *testing.T) {
	_, err := ResolveEmbeddingTransport(EmbeddingProviderSpec{
		Type:           "openai",
		Platform:       "azure",
		PlatformConfig: &PlatformConfig{Type: "azure"},
		Credential:     &fakeCred{},
	})
	if err == nil || !strings.Contains(err.Error(), "endpoint") {
		t.Fatalf("err = %v, want endpoint error", err)
	}
}

func TestResolveEmbeddingTransport_AzureRequiresOpenAIWire(t *testing.T) {
	_, err := ResolveEmbeddingTransport(EmbeddingProviderSpec{
		Type:           "gemini",
		Platform:       "azure",
		PlatformConfig: &PlatformConfig{Type: "azure", Endpoint: "https://x"},
		Credential:     &fakeCred{},
	})
	if err == nil || !strings.Contains(err.Error(), "OpenAI-wire") {
		t.Fatalf("err = %v, want OpenAI-wire guard error", err)
	}
}

func TestResolveEmbeddingTransport_BedrockVertexGated(t *testing.T) {
	for _, p := range []string{"bedrock", "vertex"} {
		_, err := ResolveEmbeddingTransport(EmbeddingProviderSpec{
			Type:           "openai",
			Platform:       p,
			PlatformConfig: &PlatformConfig{Type: p},
			Credential:     &fakeCred{},
		})
		if err == nil || !strings.Contains(err.Error(), "not yet supported") {
			t.Fatalf("platform %s: err = %v, want 'not yet supported'", p, err)
		}
	}
}
