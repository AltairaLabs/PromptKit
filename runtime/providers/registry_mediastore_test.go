package providers

import (
	"context"
	"net/http"
	"testing"

	"github.com/AltairaLabs/PromptKit/runtime/httputil"
	"github.com/AltairaLabs/PromptKit/runtime/types"
)

// TestCreateProviderFromSpec_AppliesStorageService verifies the registry entry
// point injects spec.StorageService into any provider embedding BaseProvider
// (via MediaStorageConfigurable), so StorageReference resolution works uniformly.
func TestCreateProviderFromSpec_AppliesStorageService(t *testing.T) {
	const typeName = "test-mediastore-provider"

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

	store := resolveURLFakeStore{url: "https://s3/x?sig=1"}
	p, err := CreateProviderFromSpec(ProviderSpec{ID: "m", Type: typeName, StorageService: store})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	ref := "s3://b/k"
	url, ok, err := p.(*timeoutAwareProvider).MediaLoader().ResolveURL(context.Background(),
		&types.MediaContent{StorageReference: &ref, MIMEType: "image/png"})
	if err != nil || !ok || url != "https://s3/x?sig=1" {
		t.Fatalf("store not applied to provider: (%q,%v,%v)", url, ok, err)
	}

	// Nil store must be a no-op (no panic, ResolveURL falls back).
	p2, err := CreateProviderFromSpec(ProviderSpec{ID: "m2", Type: typeName})
	if err != nil {
		t.Fatalf("create nil-store: %v", err)
	}
	if _, ok, _ := p2.(*timeoutAwareProvider).MediaLoader().ResolveURL(context.Background(),
		&types.MediaContent{StorageReference: &ref, MIMEType: "image/png"}); ok {
		t.Fatal("expected ok=false with no store")
	}
}
