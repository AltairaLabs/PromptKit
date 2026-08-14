package providers

import (
	"context"
	"net/http"
	"strings"
	"testing"
)

// stubEmbeddingProvider is a minimal EmbeddingProvider for factory tests.
type stubEmbeddingProvider struct{ *BaseEmbeddingProvider }

func (s *stubEmbeddingProvider) Embed(_ context.Context, _ EmbeddingRequest) (EmbeddingResponse, error) {
	return EmbeddingResponse{}, nil
}

func newStubEmbedding() *stubEmbeddingProvider {
	return &stubEmbeddingProvider{
		BaseEmbeddingProvider: NewBaseEmbeddingProvider("stub", "m", "", 8, 4, 0),
	}
}

func TestEmbeddingWiringFrom_CarriesTransportAndDimensions(t *testing.T) {
	client := &http.Client{}
	w := EmbeddingWiringFrom(
		EmbeddingProviderSpec{
			Model:            "m1",
			AdditionalConfig: map[string]any{"dimensions": 256},
		},
		EmbeddingTransport{BaseURL: "https://host", Client: client, PlatformAuth: true},
	)

	if w.Model != "m1" || w.BaseURL != "https://host" || w.Client != client || !w.PlatformAuth {
		t.Fatalf("wiring = %+v, want the spec model and the transport's fields", w)
	}
	if w.Dimensions != 256 {
		t.Fatalf("Dimensions = %d, want 256", w.Dimensions)
	}
}

// Dimensions must stay 0 when unset: that zero is what tells a provider to keep
// its model-family default rather than reporting a size nobody asked for.
func TestEmbeddingWiringFrom_LeavesDimensionsZeroWhenAbsent(t *testing.T) {
	w := EmbeddingWiringFrom(EmbeddingProviderSpec{Model: "m"}, EmbeddingTransport{})
	if w.Dimensions != 0 {
		t.Fatalf("Dimensions = %d, want 0 for an unset spec", w.Dimensions)
	}
}

func TestApplyWiring_AppliesEveryFieldAndReportsExplicitDimensions(t *testing.T) {
	p := newStubEmbedding()
	client := &http.Client{}

	explicit := p.ApplyWiring(EmbeddingWiring{
		Model: "m2", BaseURL: "https://h", Client: client, PlatformAuth: true, Dimensions: 512,
	})

	if !explicit {
		t.Fatal("dimsExplicit = false, want true when Dimensions is set")
	}
	if p.Model() != "m2" || p.BaseURL != "https://h" || p.HTTPClient != client || !p.PlatformAuth {
		t.Fatalf("provider = %+v, want every wiring field applied", p.BaseEmbeddingProvider)
	}
	if p.EmbeddingDimensions() != 512 {
		t.Fatalf("Dimensions = %d, want 512", p.EmbeddingDimensions())
	}
}

// An empty wiring must not blank out constructor defaults — the factory applies
// wiring after construction, so overwriting with zero values would erase them.
func TestApplyWiring_EmptyWiringLeavesDefaults(t *testing.T) {
	p := newStubEmbedding()
	p.BaseURL = "https://default"

	explicit := p.ApplyWiring(EmbeddingWiring{})

	if explicit {
		t.Fatal("dimsExplicit = true, want false for an empty wiring")
	}
	if p.Model() != "m" || p.BaseURL != "https://default" || p.EmbeddingDimensions() != 8 {
		t.Fatalf("provider = %+v, want defaults preserved", p.BaseEmbeddingProvider)
	}
	if p.PlatformAuth {
		t.Fatal("PlatformAuth = true, want it left alone")
	}
}

// The platform block is what supplies the signer or token source, so a spec
// without one must fail at construction rather than once per request.
func TestRegisterPlatformEmbeddingProvider_RequiresPlatform(t *testing.T) {
	RegisterPlatformEmbeddingProvider("stub-platform-required", PlatformEmbeddingSpec{
		PlatformHint: "platform.type: stub, platform.region: <region>",
		Build: func(EmbeddingProviderSpec, EmbeddingTransport) (EmbeddingProvider, error) {
			t.Fatal("Build must not run without a platform block")
			return nil, nil
		},
	})

	_, err := CreateEmbeddingProviderFromSpec(EmbeddingProviderSpec{Type: "stub-platform-required"})
	if err == nil {
		t.Fatal("expected an error for a spec with no platform block")
	}
	// The hint has to name the fields, or the error tells the caller nothing
	// they can act on.
	if !strings.Contains(err.Error(), "platform.region") {
		t.Fatalf("err = %v, want the hint naming the required fields", err)
	}
}

// A transport failure must surface rather than be swallowed into a provider
// built with no credentials.
func TestRegisterPlatformEmbeddingProvider_PropagatesTransportError(t *testing.T) {
	RegisterPlatformEmbeddingProvider("stub-bad-transport", PlatformEmbeddingSpec{
		PlatformHint: "platform.type: stub",
		Build: func(EmbeddingProviderSpec, EmbeddingTransport) (EmbeddingProvider, error) {
			t.Fatal("Build must not run when the transport fails to resolve")
			return nil, nil
		},
	})

	_, err := CreateEmbeddingProviderFromSpec(EmbeddingProviderSpec{
		Type:     "stub-bad-transport",
		Platform: "nonexistent-platform",
	})
	if err == nil {
		t.Fatal("expected the transport resolution error to surface")
	}
}

// The success path — Build receiving a resolved transport — cannot be tested
// with a stub type here: every platform's transport guards that the provider
// type is its own native one (azure→openai, bedrock→bedrock, vertex→vertex), so
// a made-up type can never resolve a transport, and registering the stub under
// a real type would clobber that provider's factory for the rest of the package.
//
// It is covered where it is real instead, end to end through the registry:
// TestFactoryBuildsProviderFromSpec in runtime/providers/bedrock and
// runtime/providers/vertex, each asserting the constructed provider's ID.
