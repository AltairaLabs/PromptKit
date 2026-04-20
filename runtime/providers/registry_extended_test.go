package providers

import (
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/AltairaLabs/PromptKit/runtime/httputil"
	"github.com/AltairaLabs/PromptKit/runtime/types"
)

// timeoutAwareProvider embeds BaseProvider so it gains SetHTTPTimeout and
// SetStreamIdleTimeout via method promotion, satisfying the
// timeoutConfigurable interface used by CreateProviderFromSpec.
type timeoutAwareProvider struct {
	*BaseProvider
}

func (t *timeoutAwareProvider) Model() string { return testModelName }
func (t *timeoutAwareProvider) CalculateCost(_, _, _ int) types.CostInfo {
	return types.CostInfo{}
}
func (t *timeoutAwareProvider) Predict(_ context.Context, _ PredictionRequest) (PredictionResponse, error) {
	return PredictionResponse{}, nil
}
func (t *timeoutAwareProvider) PredictStream(_ context.Context, _ PredictionRequest) (<-chan StreamChunk, error) {
	return nil, nil
}

// TestCreateProviderFromSpec_AppliesTimeouts verifies that the registry
// entry point uniformly applies RequestTimeout and StreamIdleTimeout to
// any provider that embeds BaseProvider (via the timeoutConfigurable
// interface), so individual provider factories do not have to thread the
// durations through by hand.
func TestCreateProviderFromSpec_AppliesTimeouts(t *testing.T) {
	const typeName = "test-timeout-provider"

	originalFactory := providerFactories[typeName]
	t.Cleanup(func() {
		if originalFactory != nil {
			providerFactories[typeName] = originalFactory
		} else {
			delete(providerFactories, typeName)
		}
	})

	RegisterProviderFactory(typeName, func(spec ProviderSpec) (Provider, error) {
		base := NewBaseProvider(spec.ID, false, &http.Client{
			Timeout:   httputil.DefaultProviderTimeout,
			Transport: NewInstrumentedTransport(NewPooledTransport()),
		})
		return &timeoutAwareProvider{BaseProvider: &base}, nil
	})

	t.Run("applies both timeouts when set", func(t *testing.T) {
		prov, err := CreateProviderFromSpec(ProviderSpec{
			ID:                "t",
			Type:              typeName,
			RequestTimeout:    3 * time.Minute,
			StreamIdleTimeout: 90 * time.Second,
		})
		if err != nil {
			t.Fatalf("CreateProviderFromSpec returned error: %v", err)
		}
		tp := prov.(*timeoutAwareProvider)
		if got := tp.GetHTTPClient().Timeout; got != 3*time.Minute {
			t.Errorf("request client timeout = %v, want 3m", got)
		}
		if got := tp.GetStreamingHTTPClient().Timeout; got != 0 {
			t.Errorf("streaming client timeout = %v, want 0 (no wall-clock cap)", got)
		}
		if got := tp.StreamIdleTimeout(); got != 90*time.Second {
			t.Errorf("StreamIdleTimeout() = %v, want 90s", got)
		}
	})

	t.Run("leaves defaults when both zero", func(t *testing.T) {
		prov, err := CreateProviderFromSpec(ProviderSpec{
			ID:   "t2",
			Type: typeName,
		})
		if err != nil {
			t.Fatalf("CreateProviderFromSpec returned error: %v", err)
		}
		tp := prov.(*timeoutAwareProvider)
		if got := tp.GetHTTPClient().Timeout; got != httputil.DefaultProviderTimeout {
			t.Errorf("request client timeout = %v, want default %v", got, httputil.DefaultProviderTimeout)
		}
		if got := tp.StreamIdleTimeout(); got != DefaultStreamIdleTimeout {
			t.Errorf("StreamIdleTimeout() = %v, want default %v", got, DefaultStreamIdleTimeout)
		}
	})

	t.Run("applies only the set timeout when the other is zero", func(t *testing.T) {
		prov, err := CreateProviderFromSpec(ProviderSpec{
			ID:             "t3",
			Type:           typeName,
			RequestTimeout: 2 * time.Minute,
		})
		if err != nil {
			t.Fatalf("CreateProviderFromSpec returned error: %v", err)
		}
		tp := prov.(*timeoutAwareProvider)
		if got := tp.GetHTTPClient().Timeout; got != 2*time.Minute {
			t.Errorf("request client timeout = %v, want 2m", got)
		}
		if got := tp.StreamIdleTimeout(); got != DefaultStreamIdleTimeout {
			t.Errorf("StreamIdleTimeout() = %v, want default when unset", got)
		}
	})
}

const testModelName = "test-model"

// mockProviderForTest is a minimal provider implementation for registry testing
type mockProviderForTest struct {
	id string
}

func (m *mockProviderForTest) ID() string                   { return m.id }
func (m *mockProviderForTest) Model() string                { return testModelName }
func (m *mockProviderForTest) Close() error                 { return nil }
func (m *mockProviderForTest) ShouldIncludeRawOutput() bool { return false }
func (m *mockProviderForTest) SupportsStreaming() bool      { return false }
func (m *mockProviderForTest) CalculateCost(_, _, _ int) types.CostInfo {
	return types.CostInfo{}
}
func (m *mockProviderForTest) Predict(_ context.Context, _ PredictionRequest) (PredictionResponse, error) {
	return PredictionResponse{}, nil
}
func (m *mockProviderForTest) PredictStream(_ context.Context, _ PredictionRequest) (<-chan StreamChunk, error) {
	return nil, nil
}

// Register test factories in init to test default base URL assignment
func init() {
	// Register a test factory to help with coverage
	RegisterProviderFactory("test-provider", func(spec ProviderSpec) (Provider, error) {
		return &mockProviderForTest{id: spec.ID}, nil
	})
}

func TestCreateProviderFromSpecDefaultBaseURLs(t *testing.T) {
	tests := []struct {
		name            string
		providerType    string
		expectedBaseURL string
	}{
		{"openai default", "openai", "https://api.openai.com/v1"},
		{"gemini default", "gemini", "https://generativelanguage.googleapis.com/v1beta"},
		{"claude default", "claude", "https://api.anthropic.com"},
		{"imagen default", "imagen", "https://generativelanguage.googleapis.com/v1beta"},
		{"ollama default", "ollama", "http://localhost:11434"},
		{"vllm default", "vllm", "http://localhost:8000"},
		{"mock default", "mock", ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Register a temporary factory for this type if not already registered
			originalFactory := providerFactories[tt.providerType]
			capturedBaseURL := ""
			providerFactories[tt.providerType] = func(spec ProviderSpec) (Provider, error) {
				capturedBaseURL = spec.BaseURL
				return &mockProviderForTest{id: spec.ID}, nil
			}
			defer func() {
				if originalFactory != nil {
					providerFactories[tt.providerType] = originalFactory
				} else {
					delete(providerFactories, tt.providerType)
				}
			}()

			spec := ProviderSpec{
				ID:    "test-" + tt.providerType,
				Type:  tt.providerType,
				Model: testModelName,
				// No BaseURL - should use default
			}

			_, err := CreateProviderFromSpec(spec)
			if err != nil {
				t.Fatalf("Expected no error, got %v", err)
			}

			if capturedBaseURL != tt.expectedBaseURL {
				t.Errorf("Expected baseURL %q, got %q", tt.expectedBaseURL, capturedBaseURL)
			}
		})
	}
}

// TestCreateProviderFromSpecOpenAIAzureSkipsDefault is a regression test for
// issue #1010: when spec.Type=="openai" and spec.Platform=="azure", the
// registry must NOT default BaseURL to https://api.openai.com/v1. The openai
// factory builds the deployment URL from PlatformConfig and that branch is
// gated on baseURL=="" — so clobbering it makes the Azure path unreachable.
func TestCreateProviderFromSpecOpenAIAzureSkipsDefault(t *testing.T) {
	originalFactory := providerFactories["openai"]
	capturedBaseURL := "sentinel"
	providerFactories["openai"] = func(spec ProviderSpec) (Provider, error) {
		capturedBaseURL = spec.BaseURL
		return &mockProviderForTest{id: spec.ID}, nil
	}
	defer func() {
		if originalFactory != nil {
			providerFactories["openai"] = originalFactory
		} else {
			delete(providerFactories, "openai")
		}
	}()

	spec := ProviderSpec{
		ID:       "azure-openai",
		Type:     "openai",
		Model:    testModelName,
		Platform: "azure",
		// No BaseURL — the factory must see "" so it can build the
		// deployment URL from PlatformConfig.
	}

	if _, err := CreateProviderFromSpec(spec); err != nil {
		t.Fatalf("Expected no error, got %v", err)
	}

	if capturedBaseURL != "" {
		t.Errorf("Expected empty BaseURL for openai+azure, got %q (issue #1010)", capturedBaseURL)
	}
}

// TestCreateProviderFromSpecClaudeVertexSkipsDefault is the matching
// regression test for the claude+vertex cell: api.anthropic.com must not
// clobber spec.BaseURL when Platform=="vertex". The claude factory derives
// the publishers/anthropic/models URL from PlatformConfig in that case.
func TestCreateProviderFromSpecClaudeVertexSkipsDefault(t *testing.T) {
	originalFactory := providerFactories["claude"]
	capturedBaseURL := "sentinel"
	providerFactories["claude"] = func(spec ProviderSpec) (Provider, error) {
		capturedBaseURL = spec.BaseURL
		return &mockProviderForTest{id: spec.ID}, nil
	}
	defer func() {
		if originalFactory != nil {
			providerFactories["claude"] = originalFactory
		} else {
			delete(providerFactories, "claude")
		}
	}()

	spec := ProviderSpec{
		ID:       "vertex-claude",
		Type:     "claude",
		Model:    testModelName,
		Platform: "vertex",
	}

	if _, err := CreateProviderFromSpec(spec); err != nil {
		t.Fatalf("Expected no error, got %v", err)
	}

	if capturedBaseURL != "" {
		t.Errorf("Expected empty BaseURL for claude+vertex, got %q (regression of #1010-class bug)", capturedBaseURL)
	}
}

func TestCreateProviderFromSpecCustomBaseURL(t *testing.T) {
	customURL := "https://custom.api.example.com"
	spec := ProviderSpec{
		ID:      "test",
		Type:    "test-provider",
		Model:   testModelName,
		BaseURL: customURL,
	}

	provider, err := CreateProviderFromSpec(spec)
	if err != nil {
		t.Fatalf("Expected no error, got %v", err)
	}
	if provider == nil {
		t.Fatal("Expected provider but got nil")
	}
}

func TestCreateProviderFromSpecUnsupported(t *testing.T) {
	spec := ProviderSpec{
		ID:    "test",
		Type:  "unsupported-type",
		Model: testModelName,
	}

	provider, err := CreateProviderFromSpec(spec)
	if err == nil {
		t.Error("Expected error but got none")
	}
	if provider != nil {
		t.Errorf("Expected nil provider but got %v", provider)
	}
}

func TestCreateProviderFromSpecEmptyType(t *testing.T) {
	spec := ProviderSpec{
		ID:    "test",
		Type:  "",
		Model: testModelName,
	}

	provider, err := CreateProviderFromSpec(spec)
	if err == nil {
		t.Error("Expected error but got none")
	}
	if provider != nil {
		t.Errorf("Expected nil provider but got %v", provider)
	}
}

func TestProviderSpec_HasCredential(t *testing.T) {
	tests := []struct {
		name     string
		spec     ProviderSpec
		expected bool
	}{
		{
			name:     "nil credential",
			spec:     ProviderSpec{},
			expected: false,
		},
		{
			name:     "none type credential",
			spec:     ProviderSpec{Credential: &mockCredForRegistry{credType: "none"}},
			expected: false,
		},
		{
			name:     "api_key credential",
			spec:     ProviderSpec{Credential: &mockCredForRegistry{credType: "api_key"}},
			expected: true,
		},
		{
			name:     "bearer credential",
			spec:     ProviderSpec{Credential: &mockCredForRegistry{credType: "bearer"}},
			expected: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.spec.HasCredential()
			if got != tt.expected {
				t.Errorf("HasCredential() = %v, want %v", got, tt.expected)
			}
		})
	}
}

func TestCredentialFactory(t *testing.T) {
	withCredCalled := false
	withoutCredCalled := false

	factory := CredentialFactory(
		func(_ ProviderSpec) (Provider, error) {
			withCredCalled = true
			return &mockProviderForTest{id: "cred"}, nil
		},
		func(_ ProviderSpec) (Provider, error) {
			withoutCredCalled = true
			return &mockProviderForTest{id: "env"}, nil
		},
	)

	// Test with credential
	p, err := factory(ProviderSpec{Credential: &mockCredForRegistry{credType: "api_key"}})
	if err != nil {
		t.Fatalf("Expected no error, got %v", err)
	}
	if !withCredCalled {
		t.Error("Expected withCred factory to be called")
	}
	if p.ID() != "cred" {
		t.Errorf("Expected provider ID 'cred', got %q", p.ID())
	}

	// Reset
	withCredCalled = false
	withoutCredCalled = false

	// Test without credential
	p, err = factory(ProviderSpec{})
	if err != nil {
		t.Fatalf("Expected no error, got %v", err)
	}
	if !withoutCredCalled {
		t.Error("Expected withoutCred factory to be called")
	}
	if p.ID() != "env" {
		t.Errorf("Expected provider ID 'env', got %q", p.ID())
	}
}

// mockCredForRegistry implements Credential for registry tests.
type mockCredForRegistry struct {
	credType string
}

func (m *mockCredForRegistry) Apply(_ context.Context, _ *http.Request) error { return nil }
func (m *mockCredForRegistry) Type() string                                   { return m.credType }
