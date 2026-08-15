package providers

import (
	"errors"
	"strings"
	"testing"
)

func TestRejectPlatforms_RejectsListedPlatform(t *testing.T) {
	innerCalled := false
	wrapped := RejectPlatforms(
		map[string]bool{"vertex": true},
		func(spec ProviderSpec) (Provider, error) {
			innerCalled = true
			return &mockProviderForTest{id: spec.ID}, nil
		},
	)

	_, err := wrapped(ProviderSpec{Type: "openai", Platform: "vertex"})
	if err == nil {
		t.Fatal("expected error for rejected platform, got nil")
	}
	if innerCalled {
		t.Error("inner factory must not be invoked for rejected platforms")
	}

	var typed *UnsupportedProviderPlatformError
	if !errors.As(err, &typed) {
		t.Fatalf("expected UnsupportedProviderPlatformError, got %T: %v", err, err)
	}
	if typed.ProviderType != "openai" || typed.Platform != "vertex" {
		t.Errorf("error fields = (%q, %q), want (openai, vertex)", typed.ProviderType, typed.Platform)
	}
}

func TestRejectPlatforms_AllowsUnlistedPlatform(t *testing.T) {
	wrapped := RejectPlatforms(
		map[string]bool{"vertex": true},
		func(spec ProviderSpec) (Provider, error) {
			return &mockProviderForTest{id: spec.ID}, nil
		},
	)

	tests := []string{"", "azure", "bedrock"}
	for _, platform := range tests {
		t.Run("platform="+platform, func(t *testing.T) {
			p, err := wrapped(ProviderSpec{ID: "x", Type: "openai", Platform: platform})
			if err != nil {
				t.Fatalf("unexpected error for allowed platform %q: %v", platform, err)
			}
			if p == nil {
				t.Fatal("expected non-nil provider for allowed platform")
			}
		})
	}
}

func TestUnsupportedProviderPlatformError_Message(t *testing.T) {
	err := &UnsupportedProviderPlatformError{ProviderType: "gemini", Platform: "bedrock"}
	msg := err.Error()
	for _, want := range []string{"gemini", "bedrock", "not offered by the platform vendor"} {
		if !strings.Contains(msg, want) {
			t.Errorf("error message %q missing %q", msg, want)
		}
	}
}

// (openai, vertex) is no longer rejected. #1009 blocked it because Vertex
// hosted no OpenAI-shaped endpoint, so the pair could only 404; #1768 added the
// OpenAI-compatible Model Garden route, so the combination is real and the
// rejection was lifted. This replaces the old rejection test.
//
// The base URL is the assertion that matters: the api.openai.com default must
// NOT be applied, or the provider constructs happily and talks to the wrong
// host — a silent failure the old error at least made loud.
func TestCreateProviderFromSpec_AllowsOpenAIVertex(t *testing.T) {
	if _, ok := providerFactories["openai"]; !ok {
		t.Skip("openai factory not registered; subpackage not imported in this test build")
	}
	p, err := CreateProviderFromSpec(ProviderSpec{
		ID:       "x",
		Type:     "openai",
		Model:    testModelName,
		Platform: "vertex",
		PlatformConfig: &PlatformConfig{
			Type: "vertex", Project: "my-project", Region: "us-central1",
		},
	})
	if err != nil {
		t.Fatalf("openai+vertex must construct, got %v", err)
	}
	if p == nil {
		t.Fatal("provider = nil")
	}
}

// The default base URL must be skipped for every platform whose factory builds
// its own, or the platform branch is unreachable (#1010) — the defect that made
// #1768 look implemented while it was not.
func TestOpenAIBuildsPlatformBaseURL(t *testing.T) {
	for _, platform := range []string{"azure", "bedrock", "vertex"} {
		if !openAIBuildsPlatformBaseURL(platform) {
			t.Errorf("openAIBuildsPlatformBaseURL(%q) = false, want true", platform)
		}
	}
	for _, platform := range []string{"", "none", "openai"} {
		if openAIBuildsPlatformBaseURL(platform) {
			t.Errorf("openAIBuildsPlatformBaseURL(%q) = true, want false", platform)
		}
	}
}

// TestCreateProviderFromSpec_RejectsGeminiOnHyperscalers verifies the
// registered gemini factory rejects both bedrock and azure (neither AWS
// nor Azure hosts Gemini natively).
func TestCreateProviderFromSpec_RejectsGeminiOnHyperscalers(t *testing.T) {
	if _, ok := providerFactories["gemini"]; !ok {
		t.Skip("gemini factory not registered; subpackage not imported in this test build")
	}

	for _, platform := range []string{"bedrock", "azure"} {
		t.Run("platform="+platform, func(t *testing.T) {
			_, err := CreateProviderFromSpec(ProviderSpec{
				ID:       "x",
				Type:     "gemini",
				Model:    testModelName,
				Platform: platform,
			})
			if err == nil {
				t.Fatalf("expected error for gemini+%s, got nil", platform)
			}
			var typed *UnsupportedProviderPlatformError
			if !errors.As(err, &typed) {
				t.Fatalf("expected UnsupportedProviderPlatformError, got %T: %v", err, err)
			}
			if typed.Platform != platform {
				t.Errorf("error platform = %q, want %q", typed.Platform, platform)
			}
		})
	}
}
