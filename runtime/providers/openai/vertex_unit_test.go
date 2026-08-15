package openai

import (
	"strings"
	"testing"

	"github.com/AltairaLabs/PromptKit/runtime/providers"
)

func vertexPlatformConfig(endpointID string) *providers.PlatformConfig {
	return &providers.PlatformConfig{
		Type:     vertexPlatform,
		Project:  "my-project",
		Region:   "us-central1",
		Endpoint: endpointID,
	}
}

// Vertex Model Garden is reachable only if the platform switch maps it to a
// base URL. The credential half was already wired — this is the missing half
// (#1768).
func TestNewProviderFromConfig_Vertex(t *testing.T) {
	cred := &mockSigningCredential{}

	t.Run("derives the shared MaaS endpoint from project and region", func(t *testing.T) {
		p := NewProviderFromConfig(&ProviderConfig{
			ID:             "llama-maas",
			Model:          "meta/llama-3.3-70b-instruct-maas",
			Platform:       vertexPlatform,
			PlatformConfig: vertexPlatformConfig(""),
			Credential:     cred,
		})
		want := "https://us-central1-aiplatform.googleapis.com/v1beta1/projects/my-project" +
			"/locations/us-central1/endpoints/openapi"
		if p.baseURL != want {
			t.Errorf("baseURL = %q, want %q", p.baseURL, want)
		}
	})

	t.Run("targets a dedicated self-deployed endpoint by ID", func(t *testing.T) {
		p := NewProviderFromConfig(&ProviderConfig{
			ID:             "qwen-self-hosted",
			Model:          "qwen2.5-7b",
			Platform:       vertexPlatform,
			PlatformConfig: vertexPlatformConfig("4812379461283840"),
			Credential:     cred,
		})
		if !strings.HasSuffix(p.baseURL, "/endpoints/4812379461283840") {
			t.Errorf("baseURL = %q, want it to target the dedicated endpoint", p.baseURL)
		}
	})

	// The request URL is what actually reaches Vertex; the provider appends the
	// path itself, so a helper that included it would double up.
	t.Run("request URL appends chat/completions exactly once", func(t *testing.T) {
		p := NewProviderFromConfig(&ProviderConfig{
			ID:             "llama-maas",
			Model:          "meta/llama-3.3-70b-instruct-maas",
			Platform:       vertexPlatform,
			PlatformConfig: vertexPlatformConfig(""),
			Credential:     cred,
		})
		got := p.chatCompletionsURL()
		want := "https://us-central1-aiplatform.googleapis.com/v1beta1/projects/my-project" +
			"/locations/us-central1/endpoints/openapi/chat/completions"
		if got != want {
			t.Errorf("chatCompletionsURL() = %q, want %q", got, want)
		}
		if strings.Count(got, "chat/completions") != 1 {
			t.Errorf("chat/completions appears %d times in %q", strings.Count(got, "chat/completions"), got)
		}
	})

	t.Run("explicit BaseURL is preserved", func(t *testing.T) {
		custom := "https://custom.vertex.example/v1beta1/endpoints/openapi"
		p := NewProviderFromConfig(&ProviderConfig{
			ID:             "test",
			Model:          "qwen2.5-7b",
			BaseURL:        custom,
			Platform:       vertexPlatform,
			PlatformConfig: vertexPlatformConfig(""),
			Credential:     cred,
		})
		if p.baseURL != custom {
			t.Errorf("baseURL = %q, want the explicit %q", p.baseURL, custom)
		}
	})

	// Both appear twice in the URL and neither has a sane default, so an
	// incomplete platform block must not produce a half-built URL.
	t.Run("incomplete platform config derives no base URL", func(t *testing.T) {
		for _, tc := range []struct {
			name string
			pc   *providers.PlatformConfig
		}{
			{"no project", &providers.PlatformConfig{Type: vertexPlatform, Region: "us-central1"}},
			{"no region", &providers.PlatformConfig{Type: vertexPlatform, Project: "my-project"}},
			{"nil config", nil},
		} {
			t.Run(tc.name, func(t *testing.T) {
				p := NewProviderFromConfig(&ProviderConfig{
					ID:             "test",
					Model:          "qwen2.5-7b",
					Platform:       vertexPlatform,
					PlatformConfig: tc.pc,
					Credential:     cred,
				})
				if p.baseURL != "" {
					t.Errorf("baseURL = %q, want empty for an incomplete platform block", p.baseURL)
				}
			})
		}
	})
}

// The registry is where this was actually unreachable. (openai, vertex) was
// rejected outright by RejectPlatforms under #1009, so fixing the platform
// switch alone would have left the feature inert — the constructor is never
// called for a spec the factory refuses.
func TestCreateProviderFromSpec_VertexIsNoLongerRejected(t *testing.T) {
	p, err := providers.CreateProviderFromSpec(providers.ProviderSpec{
		ID:             "llama-maas",
		Type:           "openai",
		Model:          "meta/llama-3.3-70b-instruct-maas",
		Platform:       vertexPlatform,
		PlatformConfig: vertexPlatformConfig(""),
		Credential:     &mockSigningCredential{},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	tp, ok := p.(*ToolProvider)
	if !ok {
		t.Fatalf("provider = %T, want *ToolProvider", p)
	}
	want := "https://us-central1-aiplatform.googleapis.com/v1beta1/projects/my-project" +
		"/locations/us-central1/endpoints/openapi"
	if tp.baseURL != want {
		t.Errorf("baseURL = %q, want %q", tp.baseURL, want)
	}
}
