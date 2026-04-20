package gemini

import (
	"context"
	"net/http"
	"strings"
	"testing"

	"github.com/AltairaLabs/PromptKit/runtime/providers"
)

// mockBearerCredential records whether Apply was invoked. Used to verify the
// Vertex auth branch attaches the credential while the AI Studio branch
// leaves the request unauthenticated.
type mockBearerCredential struct{ applied bool }

func (m *mockBearerCredential) Type() string { return "bearer" }
func (m *mockBearerCredential) Apply(_ context.Context, _ *http.Request) error {
	m.applied = true
	return nil
}

func TestVertexGeminiEndpoint(t *testing.T) {
	got := vertexGeminiEndpoint("us-central1", "my-project")
	want := "https://us-central1-aiplatform.googleapis.com/v1/projects/my-project/locations/us-central1/publishers/google/models"
	if got != want {
		t.Errorf("vertexGeminiEndpoint = %q, want %q", got, want)
	}
}

func TestProvider_IsVertex(t *testing.T) {
	tests := []struct {
		platform string
		want     bool
	}{
		{vertexPlatform, true},
		{"bedrock", false},
		{"azure", false},
		{"", false},
	}
	for _, tt := range tests {
		t.Run(tt.platform, func(t *testing.T) {
			p := &Provider{platform: tt.platform}
			if got := p.isVertex(); got != tt.want {
				t.Errorf("isVertex() platform=%q got %v, want %v", tt.platform, got, tt.want)
			}
		})
	}
}

func TestProvider_GenerateContentURL(t *testing.T) {
	t.Run("vertex omits api key and /models prefix", func(t *testing.T) {
		p := &Provider{
			platform: vertexPlatform,
			baseURL:  "https://us-central1-aiplatform.googleapis.com/v1/projects/p/locations/us-central1/publishers/google/models",
			model:    "gemini-2.5-flash",
		}
		got := p.generateContentURL("generateContent")
		want := "https://us-central1-aiplatform.googleapis.com/v1/projects/p/locations/us-central1/publishers/google/models/gemini-2.5-flash:generateContent"
		if got != want {
			t.Errorf("vertex URL = %q, want %q", got, want)
		}
		if strings.Contains(got, "?key=") {
			t.Errorf("vertex URL must not embed an API key: %s", got)
		}
		if strings.Contains(got, "/models/models/") {
			t.Errorf("vertex URL must not double up /models/: %s", got)
		}
	})

	t.Run("vertex stream action", func(t *testing.T) {
		p := &Provider{
			platform: vertexPlatform,
			baseURL:  "https://us-central1-aiplatform.googleapis.com/v1/projects/p/locations/us-central1/publishers/google/models",
			model:    "gemini-2.5-flash",
		}
		got := p.generateContentURL("streamGenerateContent")
		if !strings.HasSuffix(got, "/gemini-2.5-flash:streamGenerateContent") {
			t.Errorf("vertex stream URL did not end with model:action — got %s", got)
		}
	})

	t.Run("ai studio embeds api key in query string", func(t *testing.T) {
		p := &Provider{
			platform: "",
			baseURL:  "https://generativelanguage.googleapis.com/v1beta",
			apiKey:   "test-api-key",
			model:    "gemini-2.5-flash",
		}
		got := p.generateContentURL("generateContent")
		want := "https://generativelanguage.googleapis.com/v1beta/models/gemini-2.5-flash:generateContent?key=test-api-key"
		if got != want {
			t.Errorf("ai studio URL = %q, want %q", got, want)
		}
	})
}

func TestProvider_ApplyAuth(t *testing.T) {
	t.Run("vertex applies bearer credential", func(t *testing.T) {
		cred := &mockBearerCredential{}
		p := &Provider{platform: vertexPlatform, credential: cred}
		req, _ := http.NewRequestWithContext(context.Background(), http.MethodPost, "https://example.com", http.NoBody)
		if err := p.applyAuth(context.Background(), req); err != nil {
			t.Fatalf("applyAuth returned error: %v", err)
		}
		if !cred.applied {
			t.Error("vertex applyAuth did not invoke credential.Apply")
		}
	})

	t.Run("vertex with nil credential is a no-op", func(t *testing.T) {
		p := &Provider{platform: vertexPlatform, credential: nil}
		req, _ := http.NewRequestWithContext(context.Background(), http.MethodPost, "https://example.com", http.NoBody)
		if err := p.applyAuth(context.Background(), req); err != nil {
			t.Errorf("applyAuth with nil credential should not error, got %v", err)
		}
	})

	t.Run("ai studio does not invoke credential", func(t *testing.T) {
		cred := &mockBearerCredential{}
		p := &Provider{platform: "", credential: cred}
		req, _ := http.NewRequestWithContext(context.Background(), http.MethodPost, "https://example.com", http.NoBody)
		if err := p.applyAuth(context.Background(), req); err != nil {
			t.Fatalf("applyAuth returned error: %v", err)
		}
		if cred.applied {
			t.Error("ai studio applyAuth must not call credential.Apply (key is in URL)")
		}
	})
}

func TestNewProviderWithCredential_DerivesVertexURL(t *testing.T) {
	cred := &mockBearerCredential{}

	t.Run("empty BaseURL with vertex platform derives publisher-models URL", func(t *testing.T) {
		pc := &providers.PlatformConfig{
			Type: vertexPlatform, Region: "us-central1", Project: "my-proj",
		}
		p := NewProviderWithCredential(
			"test", "gemini-2.5-flash", "",
			providers.ProviderDefaults{},
			false, cred, vertexPlatform, pc,
		)
		want := vertexGeminiEndpoint("us-central1", "my-proj")
		if p.baseURL != want {
			t.Errorf("baseURL = %q, want %q", p.baseURL, want)
		}
	})

	t.Run("explicit BaseURL is preserved on vertex", func(t *testing.T) {
		pc := &providers.PlatformConfig{
			Type: vertexPlatform, Region: "us-central1", Project: "my-proj",
		}
		custom := "https://custom.vertex.example/v1/.../models"
		p := NewProviderWithCredential(
			"test", "gemini-2.5-flash", custom,
			providers.ProviderDefaults{},
			false, cred, vertexPlatform, pc,
		)
		if p.baseURL != custom {
			t.Errorf("explicit baseURL must win, got %q", p.baseURL)
		}
	})

	t.Run("missing project leaves BaseURL empty", func(t *testing.T) {
		pc := &providers.PlatformConfig{Type: vertexPlatform, Region: "us-central1"}
		p := NewProviderWithCredential(
			"test", "gemini-2.5-flash", "",
			providers.ProviderDefaults{},
			false, cred, vertexPlatform, pc,
		)
		if p.baseURL != "" {
			t.Errorf("incomplete PlatformConfig should not derive URL, got %q", p.baseURL)
		}
	})

	t.Run("non-vertex platform does not derive URL", func(t *testing.T) {
		pc := &providers.PlatformConfig{Type: "bedrock", Region: "us-west-2"}
		p := NewProviderWithCredential(
			"test", "gemini-2.5-flash", "",
			providers.ProviderDefaults{},
			false, cred, "bedrock", pc,
		)
		if p.baseURL != "" {
			t.Errorf("non-vertex platform should not derive URL, got %q", p.baseURL)
		}
	})
}
