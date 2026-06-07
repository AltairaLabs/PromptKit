package providers

import "testing"

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
