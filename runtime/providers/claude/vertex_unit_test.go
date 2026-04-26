package claude

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/AltairaLabs/PromptKit/runtime/providers"
)

// mockBearerCredential records whether Apply was invoked. Used to verify the
// Vertex auth path attaches the credential and the direct path leaves the
// request unauthenticated by the credential (it uses x-api-key instead).
type mockBearerCredential struct{ applied bool }

func (m *mockBearerCredential) Type() string { return "bearer" }
func (m *mockBearerCredential) Apply(_ context.Context, _ *http.Request) error {
	m.applied = true
	return nil
}

func TestVertexAnthropicEndpoint(t *testing.T) {
	got := vertexAnthropicEndpoint("us-east5", "my-project")
	want := "https://us-east5-aiplatform.googleapis.com/v1/projects/my-project/locations/us-east5/publishers/anthropic/models"
	if got != want {
		t.Errorf("vertexAnthropicEndpoint = %q, want %q", got, want)
	}
}

func TestProvider_PlatformPredicates(t *testing.T) {
	tests := []struct {
		platform                             string
		wantBedrock, wantVertex, wantPartner bool
		wantVersion                          string
	}{
		{bedrockPlatform, true, false, true, bedrockVersionValue},
		{vertexPlatform, false, true, true, vertexVersionValue},
		{"azure", false, false, false, ""},
		{"", false, false, false, ""},
	}
	for _, tt := range tests {
		t.Run(tt.platform, func(t *testing.T) {
			p := &Provider{platform: tt.platform}
			if got := p.isBedrock(); got != tt.wantBedrock {
				t.Errorf("isBedrock platform=%q got %v want %v", tt.platform, got, tt.wantBedrock)
			}
			if got := p.isVertex(); got != tt.wantVertex {
				t.Errorf("isVertex platform=%q got %v want %v", tt.platform, got, tt.wantVertex)
			}
			if got := p.isPartnerHosted(); got != tt.wantPartner {
				t.Errorf("isPartnerHosted platform=%q got %v want %v", tt.platform, got, tt.wantPartner)
			}
			if got := p.platformAnthropicVersion(); got != tt.wantVersion {
				t.Errorf("platformAnthropicVersion platform=%q got %q want %q",
					tt.platform, got, tt.wantVersion)
			}
		})
	}
}

func TestProvider_MessagesURL_Vertex(t *testing.T) {
	p := &Provider{
		platform: vertexPlatform,
		baseURL:  "https://us-east5-aiplatform.googleapis.com/v1/projects/p/locations/us-east5/publishers/anthropic/models",
		model:    "claude-haiku-4-5@20251001",
	}
	t.Run("rawPredict", func(t *testing.T) {
		got := p.messagesURL()
		want := "https://us-east5-aiplatform.googleapis.com/v1/projects/p/locations/us-east5/publishers/anthropic/models/claude-haiku-4-5@20251001:rawPredict"
		if got != want {
			t.Errorf("messagesURL = %q, want %q", got, want)
		}
	})
	t.Run("streamRawPredict", func(t *testing.T) {
		got := p.messagesStreamURL()
		want := "https://us-east5-aiplatform.googleapis.com/v1/projects/p/locations/us-east5/publishers/anthropic/models/claude-haiku-4-5@20251001:streamRawPredict"
		if got != want {
			t.Errorf("messagesStreamURL = %q, want %q", got, want)
		}
	})
}

func TestProvider_MarshalBedrockRequest_VertexVersion(t *testing.T) {
	// marshalBedrockRequest is shared by Bedrock and Vertex; the
	// anthropic_version body field must reflect the active platform.
	p := &Provider{platform: vertexPlatform, model: "claude-haiku-4-5@20251001"}
	req := &claudeRequest{
		MaxTokens: 100,
		Messages: []claudeMessage{
			{Role: "user", Content: []claudeContentBlock{{Type: "text", Text: "hi"}}},
		},
	}
	body, err := p.marshalBedrockRequest(req)
	if err != nil {
		t.Fatalf("marshalBedrockRequest: %v", err)
	}
	var parsed map[string]any
	if err := json.Unmarshal(body, &parsed); err != nil {
		t.Fatalf("response body not valid JSON: %v", err)
	}
	if parsed[bedrockVersionBodyKey] != vertexVersionValue {
		t.Errorf("anthropic_version = %v, want %q", parsed[bedrockVersionBodyKey], vertexVersionValue)
	}
	if _, hasModel := parsed["model"]; hasModel {
		t.Errorf("partner body must not include model field, got: %v", parsed["model"])
	}
}

func TestProvider_MarshalBedrockStreamingRequest_VertexVersion(t *testing.T) {
	p := &Provider{platform: vertexPlatform, model: "claude-haiku-4-5@20251001"}
	reqMap := map[string]any{
		"model":      "claude-haiku-4-5@20251001",
		"max_tokens": 100,
		"messages":   []any{},
		"stream":     true,
	}
	body, err := p.marshalBedrockStreamingRequest(reqMap)
	if err != nil {
		t.Fatalf("marshalBedrockStreamingRequest: %v", err)
	}
	var parsed map[string]any
	if err := json.Unmarshal(body, &parsed); err != nil {
		t.Fatalf("body not valid JSON: %v", err)
	}
	if parsed[bedrockVersionBodyKey] != vertexVersionValue {
		t.Errorf("anthropic_version = %v, want %q", parsed[bedrockVersionBodyKey], vertexVersionValue)
	}
	if _, hasModel := parsed["model"]; hasModel {
		t.Error("vertex streaming body must not include model field")
	}
	if _, hasStream := parsed["stream"]; hasStream {
		t.Error("vertex streaming body must not include stream field (URL action signals streaming)")
	}
}

func TestNewProviderWithCredential_DerivesVertexURL(t *testing.T) {
	cred := &mockBearerCredential{}

	t.Run("empty BaseURL with vertex platform derives publisher-models URL", func(t *testing.T) {
		pc := &providers.PlatformConfig{
			Type: vertexPlatform, Region: "us-east5", Project: "my-proj",
		}
		p := NewProviderWithCredential(
			"test", "claude-haiku-4-5@20251001", "",
			providers.ProviderDefaults{},
			false, cred, vertexPlatform, pc,
		)
		want := vertexAnthropicEndpoint("us-east5", "my-proj")
		if p.baseURL != want {
			t.Errorf("baseURL = %q, want %q", p.baseURL, want)
		}
	})

	t.Run("explicit BaseURL is preserved on vertex", func(t *testing.T) {
		pc := &providers.PlatformConfig{
			Type: vertexPlatform, Region: "us-east5", Project: "my-proj",
		}
		custom := "https://custom.vertex.example/v1/.../publishers/anthropic/models"
		p := NewProviderWithCredential(
			"test", "claude-haiku-4-5@20251001", custom,
			providers.ProviderDefaults{},
			false, cred, vertexPlatform, pc,
		)
		if p.baseURL != custom {
			t.Errorf("explicit baseURL must win, got %q", p.baseURL)
		}
	})

	t.Run("missing project leaves BaseURL empty", func(t *testing.T) {
		pc := &providers.PlatformConfig{Type: vertexPlatform, Region: "us-east5"}
		p := NewProviderWithCredential(
			"test", "claude-haiku-4-5@20251001", "",
			providers.ProviderDefaults{},
			false, cred, vertexPlatform, pc,
		)
		if p.baseURL != "" {
			t.Errorf("incomplete PlatformConfig should not derive URL, got %q", p.baseURL)
		}
	})

	t.Run("unrecognized platform does not derive URL", func(t *testing.T) {
		// Note: bedrock used to be exercised here as the "non-vertex"
		// example, but Bedrock URL derivation was added in #1029. Use
		// a synthetic unsupported platform name instead so the test
		// continues to assert "platforms outside the supported set
		// fall through to an empty derived URL".
		pc := &providers.PlatformConfig{Type: "unknown-platform", Region: "us-west-2"}
		p := NewProviderWithCredential(
			"test", "claude-haiku-4-5", "",
			providers.ProviderDefaults{},
			false, cred, "unknown-platform", pc,
		)
		if p.baseURL != "" {
			t.Errorf("unrecognized platform should not derive URL, got %q", p.baseURL)
		}
	})
}

// TestVertex_DirectAPIPathStillUsesAPIKeyHeader sanity-checks that the
// direct-API tool request path is not regressed by the partner-hosted
// branching — direct requests must still set x-api-key + anthropic-version
// (verified via the ToolProvider's applyToolRequestHeaders).
func TestProvider_ApplyToolRequestHeaders_VertexUsesCredential(t *testing.T) {
	cred := &mockBearerCredential{}
	tp := &ToolProvider{
		Provider: &Provider{
			platform:   vertexPlatform,
			credential: cred,
			apiKey:     "should-not-be-used",
		},
	}
	req, _ := http.NewRequestWithContext(context.Background(), http.MethodPost, "https://example.com", http.NoBody)
	if err := tp.applyToolRequestHeaders(context.Background(), req); err != nil {
		t.Fatalf("applyToolRequestHeaders: %v", err)
	}
	if !cred.applied {
		t.Error("vertex path must invoke credential.Apply (Bearer)")
	}
	if h := req.Header.Get(apiKeyHeader); h != "" {
		t.Errorf("vertex path must not set %s header, got %q", apiKeyHeader, h)
	}
	if h := req.Header.Get(anthropicVersionKey); h != "" {
		t.Errorf("vertex path must not set %s header (version is in body), got %q", anthropicVersionKey, h)
	}
	if got := req.Header.Get("Content-Type"); got != "application/json" {
		t.Errorf("Content-Type = %q, want application/json", got)
	}
}

func TestProvider_ApplyToolRequestHeaders_DirectStillUsesAPIKey(t *testing.T) {
	tp := &ToolProvider{
		Provider: &Provider{
			platform: "",
			apiKey:   "direct-key",
		},
	}
	req, _ := http.NewRequestWithContext(context.Background(), http.MethodPost, "https://example.com", http.NoBody)
	if err := tp.applyToolRequestHeaders(context.Background(), req); err != nil {
		t.Fatalf("applyToolRequestHeaders: %v", err)
	}
	if got := req.Header.Get(apiKeyHeader); got != "direct-key" {
		t.Errorf("direct path must set %s, got %q", apiKeyHeader, got)
	}
	if got := req.Header.Get(anthropicVersionKey); got == "" {
		t.Errorf("direct path must set %s header", anthropicVersionKey)
	}
}

// Catch a doubled segment regression like /models/models/{model}.
func TestProvider_MessagesURL_NoDoubledSegments(t *testing.T) {
	p := &Provider{
		platform: vertexPlatform,
		baseURL:  vertexAnthropicEndpoint("us-east5", "p"),
		model:    "claude-haiku-4-5",
	}
	for _, fn := range []func() string{p.messagesURL, p.messagesStreamURL} {
		got := fn()
		if strings.Contains(got, "/models/models/") {
			t.Errorf("doubled /models/ segment in URL: %s", got)
		}
		if strings.Contains(got, "?key=") {
			t.Errorf("vertex URL must not embed an API key: %s", got)
		}
	}
}
