package sdk

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/AltairaLabs/PromptKit/runtime/providers"
	"github.com/AltairaLabs/PromptKit/runtime/providers/mock"
	"github.com/AltairaLabs/PromptKit/runtime/storage"
	"github.com/AltairaLabs/PromptKit/runtime/types"
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
