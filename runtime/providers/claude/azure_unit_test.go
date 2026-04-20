package claude

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/AltairaLabs/PromptKit/runtime/providers"
	"github.com/AltairaLabs/PromptKit/runtime/types"
)

func TestAzureAnthropicDeploymentEndpoint(t *testing.T) {
	tests := []struct {
		name, endpoint, deployment, want string
	}{
		{
			name:       "trailing slash trimmed",
			endpoint:   "https://my-resource.services.ai.azure.com/",
			deployment: "claude-haiku-4-5",
			want:       "https://my-resource.services.ai.azure.com/openai/deployments/claude-haiku-4-5",
		},
		{
			name:       "no trailing slash",
			endpoint:   "https://my-resource.services.ai.azure.com",
			deployment: "claude-haiku-4-5",
			want:       "https://my-resource.services.ai.azure.com/openai/deployments/claude-haiku-4-5",
		},
		{
			name:       "deployment name with hyphens preserved",
			endpoint:   "https://my-resource.services.ai.azure.com",
			deployment: "claude-sonnet-4-6",
			want:       "https://my-resource.services.ai.azure.com/openai/deployments/claude-sonnet-4-6",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := azureAnthropicDeploymentEndpoint(tt.endpoint, tt.deployment)
			if got != tt.want {
				t.Errorf("got %q, want %q", got, tt.want)
			}
		})
	}
}

func TestProvider_IsAzure(t *testing.T) {
	tests := []struct {
		platform string
		want     bool
	}{
		{azurePlatform, true},
		{bedrockPlatform, false},
		{vertexPlatform, false},
		{"", false},
	}
	for _, tt := range tests {
		t.Run(tt.platform, func(t *testing.T) {
			p := &Provider{platform: tt.platform}
			if got := p.isAzure(); got != tt.want {
				t.Errorf("isAzure platform=%q got %v want %v", tt.platform, got, tt.want)
			}
		})
	}
}

func TestProvider_UsesCredentialAuth(t *testing.T) {
	tests := []struct {
		platform string
		want     bool
	}{
		{bedrockPlatform, true},
		{vertexPlatform, true},
		{azurePlatform, true},
		{"", false},
	}
	for _, tt := range tests {
		t.Run(tt.platform, func(t *testing.T) {
			p := &Provider{platform: tt.platform}
			if got := p.usesCredentialAuth(); got != tt.want {
				t.Errorf("usesCredentialAuth platform=%q got %v want %v", tt.platform, got, tt.want)
			}
		})
	}
}

// Azure must NOT be in isPartnerHosted — it uses the direct API body shape,
// not the Bedrock/Vertex partner shape.
func TestProvider_AzureNotPartnerHosted(t *testing.T) {
	p := &Provider{platform: azurePlatform}
	if p.isPartnerHosted() {
		t.Error("Azure must not be classified as partner-hosted (direct API body shape, no anthropic_version body field)")
	}
}

func TestProvider_AzureAPIVersion(t *testing.T) {
	t.Run("default when not configured", func(t *testing.T) {
		p := &Provider{platform: azurePlatform, platformConfig: &providers.PlatformConfig{}}
		if got := p.azureAPIVersion(); got != defaultAzureAPIVersion {
			t.Errorf("got %q, want default %q", got, defaultAzureAPIVersion)
		}
	})
	t.Run("override via PlatformConfig.AdditionalConfig", func(t *testing.T) {
		p := &Provider{
			platform: azurePlatform,
			platformConfig: &providers.PlatformConfig{
				AdditionalConfig: map[string]any{"api_version": "2025-04-01-preview"},
			},
		}
		if got := p.azureAPIVersion(); got != "2025-04-01-preview" {
			t.Errorf("got %q, want override", got)
		}
	})
	t.Run("nil platformConfig falls back to default", func(t *testing.T) {
		p := &Provider{platform: azurePlatform, platformConfig: nil}
		if got := p.azureAPIVersion(); got != defaultAzureAPIVersion {
			t.Errorf("got %q, want default %q", got, defaultAzureAPIVersion)
		}
	})
}

func TestProvider_MessagesURL_Azure(t *testing.T) {
	p := &Provider{
		platform: azurePlatform,
		baseURL:  "https://my-resource.services.ai.azure.com/openai/deployments/claude-haiku-4-5",
		model:    "claude-haiku-4-5",
		platformConfig: &providers.PlatformConfig{
			AdditionalConfig: map[string]any{"api_version": "2024-12-01-preview"},
		},
	}
	t.Run("messagesURL appends api-version", func(t *testing.T) {
		got := p.messagesURL()
		want := "https://my-resource.services.ai.azure.com/openai/deployments/claude-haiku-4-5/messages?api-version=2024-12-01-preview"
		if got != want {
			t.Errorf("messagesURL = %q, want %q", got, want)
		}
	})
	t.Run("messagesStreamURL same path as non-streaming (Azure uses single endpoint)", func(t *testing.T) {
		if p.messagesURL() != p.messagesStreamURL() {
			t.Errorf("Azure messagesURL and messagesStreamURL should match — they share the deployment path; got %q vs %q",
				p.messagesURL(), p.messagesStreamURL())
		}
	})
	t.Run("URL must not contain partner-style segments", func(t *testing.T) {
		got := p.messagesURL()
		for _, bad := range []string{"/model/", ":rawPredict", ":streamRawPredict"} {
			if strings.Contains(got, bad) {
				t.Errorf("Azure URL must not contain %q, got %s", bad, got)
			}
		}
	})
}

func TestNewProviderWithCredential_DerivesAzureURL(t *testing.T) {
	cred := &mockBearerCredential{}

	t.Run("empty BaseURL with azure platform derives deployment URL from PlatformConfig.Endpoint", func(t *testing.T) {
		pc := &providers.PlatformConfig{
			Type:     azurePlatform,
			Endpoint: "https://my-resource.services.ai.azure.com",
		}
		p := NewProviderWithCredential(
			"test", "claude-haiku-4-5", "",
			providers.ProviderDefaults{},
			false, cred, azurePlatform, pc,
		)
		want := azureAnthropicDeploymentEndpoint(pc.Endpoint, "claude-haiku-4-5")
		if p.baseURL != want {
			t.Errorf("baseURL = %q, want %q", p.baseURL, want)
		}
	})

	t.Run("explicit BaseURL is preserved on azure", func(t *testing.T) {
		pc := &providers.PlatformConfig{
			Type:     azurePlatform,
			Endpoint: "https://my-resource.services.ai.azure.com",
		}
		custom := "https://custom.azure.example/openai/deployments/foo"
		p := NewProviderWithCredential(
			"test", "claude-haiku-4-5", custom,
			providers.ProviderDefaults{},
			false, cred, azurePlatform, pc,
		)
		if p.baseURL != custom {
			t.Errorf("explicit baseURL must win, got %q", p.baseURL)
		}
	})

	t.Run("missing endpoint leaves BaseURL empty", func(t *testing.T) {
		pc := &providers.PlatformConfig{Type: azurePlatform}
		p := NewProviderWithCredential(
			"test", "claude-haiku-4-5", "",
			providers.ProviderDefaults{},
			false, cred, azurePlatform, pc,
		)
		if p.baseURL != "" {
			t.Errorf("incomplete PlatformConfig should not derive URL, got %q", p.baseURL)
		}
	})
}

// TestProvider_PredictStream_AzureSendsCorrectWireFormat exercises the
// streaming path through PredictStream against a mock httptest server,
// and asserts the wire format the provider produces:
//   - URL ends in /messages?api-version=...
//   - body includes model field (direct API shape, not partner shape)
//   - Bearer auth via credential, no x-api-key, no anthropic-version header
func TestProvider_PredictStream_AzureSendsCorrectWireFormat(t *testing.T) {
	var capturedReq *http.Request
	var capturedBody []byte
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedReq = r.Clone(context.Background())
		body := make([]byte, r.ContentLength)
		_, _ = r.Body.Read(body)
		capturedBody = body
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("data: {\"type\":\"message_stop\"}\n\n"))
	}))
	t.Cleanup(server.Close)

	cred := &mockBearerCredential{}
	p := &Provider{
		BaseProvider: providers.NewBaseProvider("test-azure", false, server.Client()),
		model:        "claude-haiku-4-5",
		baseURL:      server.URL,
		platform:     azurePlatform,
		credential:   cred,
		defaults:     providers.ProviderDefaults{MaxTokens: 64},
		platformConfig: &providers.PlatformConfig{
			Type:             azurePlatform,
			Endpoint:         server.URL,
			AdditionalConfig: map[string]any{"api_version": "2024-12-01-preview"},
		},
	}

	ch, err := p.PredictStream(context.Background(), providers.PredictionRequest{
		Messages: []types.Message{{Role: "user", Content: "hi"}},
	})
	if err != nil {
		t.Fatalf("PredictStream: %v", err)
	}
	for range ch {
	}

	if capturedReq == nil {
		t.Fatal("server never received a request")
	}
	if !strings.HasSuffix(capturedReq.URL.Path+"?"+capturedReq.URL.RawQuery, "/messages?api-version=2024-12-01-preview") {
		t.Errorf("URL = %s?%s, want suffix /messages?api-version=2024-12-01-preview",
			capturedReq.URL.Path, capturedReq.URL.RawQuery)
	}
	if h := capturedReq.Header.Get(anthropicVersionKey); h != "" {
		t.Errorf("Azure must not set %s header (api-version is in URL), got %q", anthropicVersionKey, h)
	}
	if h := capturedReq.Header.Get("X-API-Key"); h != "" {
		t.Errorf("Azure must not set X-API-Key, got %q", h)
	}
	if !cred.applied {
		t.Error("Azure must invoke credential.Apply")
	}
	if !strings.Contains(string(capturedBody), `"model":"claude-haiku-4-5"`) {
		t.Errorf("Azure body must include model field (direct API shape), got: %s", string(capturedBody))
	}
}
