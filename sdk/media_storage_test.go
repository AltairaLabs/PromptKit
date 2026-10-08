package sdk

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/AltairaLabs/PromptKit/runtime/v2/providers"
	"github.com/AltairaLabs/PromptKit/runtime/v2/providers/mock"
	"github.com/AltairaLabs/PromptKit/runtime/v2/storage"
	"github.com/AltairaLabs/PromptKit/runtime/v2/types"
)

// fakeMediaStore is a no-op MediaStorageService used to verify injection.
type fakeMediaStore struct{}

func (fakeMediaStore) StoreMedia(
	context.Context, *types.MediaContent, *storage.MediaMetadata,
) (storage.Reference, error) {
	return "", nil
}

func (fakeMediaStore) RetrieveMedia(context.Context, storage.Reference) (*types.MediaContent, error) {
	return nil, nil
}

func (fakeMediaStore) DeleteMedia(context.Context, storage.Reference) error { return nil }

func (fakeMediaStore) GetURL(context.Context, storage.Reference, time.Duration) (string, error) {
	return "", nil
}

// storageSpyProvider embeds a mock provider (a full providers.Provider) and
// records SetMediaStorageService calls, so it satisfies
// providers.MediaStorageConfigurable. The runtime mock provider embeds
// base.Implementation, not BaseProvider, so it does not implement the
// interface on its own — this spy adds the seam.
type storageSpyProvider struct {
	*mock.Provider
	mu    sync.Mutex
	store storage.MediaStorageService
}

func (s *storageSpyProvider) SetMediaStorageService(st storage.MediaStorageService) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.store = st
}

func (s *storageSpyProvider) injectedStore() storage.MediaStorageService {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.store
}

func TestWithMediaStorage_InjectsIntoPooledProvider(t *testing.T) {
	spy := &storageSpyProvider{Provider: mock.NewProvider("spy", "mock-model", false)}
	// Compile-time proof the spy implements the configurable interface.
	var _ providers.MediaStorageConfigurable = spy

	fake := fakeMediaStore{}
	conv, err := Open("./testdata/packs/eval-test.pack.json", "assistant",
		WithProvider(spy),
		WithMediaStorage(fake),
		WithSkipSchemaValidation(),
	)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer func() { _ = conv.Close() }()

	if spy.injectedStore() == nil {
		t.Fatal("expected WithMediaStorage to inject the store into the pooled provider")
	}
}

func TestWithMediaStorage_NoStoreIsNoOp(t *testing.T) {
	spy := &storageSpyProvider{Provider: mock.NewProvider("spy", "mock-model", false)}

	conv, err := Open("./testdata/packs/eval-test.pack.json", "assistant",
		WithProvider(spy),
		WithSkipSchemaValidation(),
	)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer func() { _ = conv.Close() }()

	if spy.injectedStore() != nil {
		t.Fatal("expected no store injection when WithMediaStorage is not set")
	}
}

// scopedSpy is a MediaScoped provider: WithMediaSettings returns a view
// carrying the settings, and any in-place write is recorded.
type scopedSpy struct {
	*mock.Provider
	settings providers.MediaSettings
	mutated  bool
}

func (s *scopedSpy) WithMediaSettings(m providers.MediaSettings) providers.Provider {
	return &scopedSpy{Provider: s.Provider, settings: m}
}

func (s *scopedSpy) SetMediaStorageService(storage.MediaStorageService) { s.mutated = true }

func (s *scopedSpy) SetAllowPrivateNetworkMedia(bool) { s.mutated = true }

// Two conversations over one shared provider keep their own media settings
// (#2216). Writing them onto the shared provider let the last Open decide for
// every conversation, and one WithUnsafePrivateNetworkMedia switched off the
// private-network guard for all of them.
func TestMediaSettings_AreScopedPerConversation(t *testing.T) {
	shared := &scopedSpy{Provider: mock.NewProvider("shared", "mock-model", false)}
	store := fakeMediaStore{}

	trusting, err := Open("./testdata/packs/eval-test.pack.json", "assistant", WithProvider(shared),
		WithMediaStorage(store), WithUnsafePrivateNetworkMedia(), WithSkipSchemaValidation())
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer func() { _ = trusting.Close() }()
	plain, err := Open("./testdata/packs/eval-test.pack.json", "assistant", WithProvider(shared),
		WithSkipSchemaValidation())
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer func() { _ = plain.Close() }()

	if shared.mutated {
		t.Fatal("a conversation's media settings were written onto the shared provider")
	}
	view, ok := trusting.callProvider().(*scopedSpy)
	if !ok || view == shared {
		t.Fatalf("the trusting conversation runs on %T %p, not its own view", trusting.callProvider(), view)
	}
	if view.settings.Storage != store || !view.settings.AllowPrivateNetworks {
		t.Errorf("the view carries %+v, not the conversation's settings", view.settings)
	}
	if plain.callProvider() != providers.Provider(shared) {
		t.Error("a conversation without media options should run on the shared provider unchanged")
	}
}
