//go:build e2e

package sdk

import (
	"context"
	"encoding/base64"
	"testing"
	"time"

	"github.com/AltairaLabs/PromptKit/runtime/storage"
	"github.com/AltairaLabs/PromptKit/runtime/storage/local"
	"github.com/AltairaLabs/PromptKit/runtime/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// =============================================================================
// Storage-Reference Resolution E2E Tests
//
// These tests prove that media sent by durable StorageReference resolves
// end-to-end against real vision providers, exercising both resolution paths:
//
//   - URL path:   ref -> store.GetURL -> remote URL -> model fetches the URL
//   - Bytes path: ref -> file:// URL (non-remote) -> provider falls back to
//                 downloading bytes -> model receives inline image data
//
// They are opt-in and require a live provider API key:
//
//	go test -tags=e2e ./sdk/... -run TestE2E_StorageRef
//
// A live-S3 presigned-URL test is intentionally out of scope here: the
// URL-path test uses a fake store returning a stable PUBLIC image URL, which
// exercises exactly the same GetURL -> remote-URL -> model code path a real
// S3/GCS presigned URL would, without the credential and network coupling.
// =============================================================================

// publicStorageRefImageURL is a stable, provider-fetchable public PNG. It stands
// in for a presigned S3/GCS URL: the point is that the model fetches a remote URL
// our code produced from a StorageReference. GitHub raw is used (not Wikipedia,
// which bot-blocks provider fetchers with a 400 "unable to download").
const publicStorageRefImageURL = "https://raw.githubusercontent.com/opencv/opencv/master/samples/data/opencv-logo.png"

// urlFakeStore is a minimal MediaStorageService whose GetURL always returns a
// stable, public, remote image URL. It lets the URL-resolution path be tested
// without any cloud credentials. URL-capable providers (Claude, OpenAI) hand
// the model that URL; base64-only providers (Gemini) instead call RetrieveMedia,
// so it also serves real image bytes.
type urlFakeStore struct {
	url      string
	bytesB64 string // base64 image bytes, served by RetrieveMedia for base64-only providers
}

func (s *urlFakeStore) StoreMedia(
	_ context.Context, _ *types.MediaContent, _ *storage.MediaMetadata,
) (storage.Reference, error) {
	return storage.Reference("ref-1"), nil
}

func (s *urlFakeStore) RetrieveMedia(
	_ context.Context, _ storage.Reference,
) (*types.MediaContent, error) {
	data := s.bytesB64
	return &types.MediaContent{Data: &data, MIMEType: "image/png"}, nil
}

func (s *urlFakeStore) DeleteMedia(_ context.Context, _ storage.Reference) error {
	return nil
}

func (s *urlFakeStore) GetURL(
	_ context.Context, _ storage.Reference, _ time.Duration,
) (string, error) {
	return s.url, nil
}

// TestE2E_StorageRef_URLPath proves ref -> GetURL -> remote URL -> real model.
func TestE2E_StorageRef_URLPath(t *testing.T) {
	EnsureTestPacks(t)

	RunForProviders(t, CapVision, func(t *testing.T, provider ProviderConfig) {
		if provider.ID == "mock" {
			t.Skip("Mock provider doesn't support real vision")
		}

		store := &urlFakeStore{
			url:      publicStorageRefImageURL,
			bytesB64: base64.StdEncoding.EncodeToString(getTestImage(t)),
		}

		conv := NewVisionConversation(t, provider, WithMediaStorage(store))
		defer conv.Close()

		ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
		defer cancel()

		resp, err := conv.Send(ctx,
			"What do you see in this image? Answer in one sentence.",
			WithImageStorageRef("ref-1", "image/png"))
		require.NoError(t, err)
		require.NotNil(t, resp)
		assert.NotEmpty(t, resp.Text(), "Should return a description via URL path")

		t.Logf("Provider %s storage-ref URL path: %s", provider.ID, truncate(resp.Text(), 150))
	})
}

// TestE2E_StorageRef_BytesPath proves ref -> file:// -> bytes fallback -> real
// model. The local file store's GetURL returns a non-remote file:// URL, so
// providers fetch the stored bytes instead of handing the model a URL.
func TestE2E_StorageRef_BytesPath(t *testing.T) {
	EnsureTestPacks(t)

	RunForProviders(t, CapVision, func(t *testing.T, provider ProviderConfig) {
		if provider.ID == "mock" {
			t.Skip("Mock provider doesn't support real vision")
		}

		store, err := local.NewFileStore(local.FileStoreConfig{BaseDir: t.TempDir()})
		require.NoError(t, err)

		data := base64.StdEncoding.EncodeToString(getTestImage(t))
		ref, err := store.StoreMedia(context.Background(),
			&types.MediaContent{Data: &data, MIMEType: "image/png"},
			&storage.MediaMetadata{SessionID: "storage-ref-e2e", MIMEType: "image/png"})
		require.NoError(t, err)

		conv := NewVisionConversation(t, provider, WithMediaStorage(store))
		defer conv.Close()

		ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
		defer cancel()

		resp, err := conv.Send(ctx,
			"What do you see in this image? Answer in one sentence.",
			WithImageStorageRef(string(ref), "image/png"))
		require.NoError(t, err)
		require.NotNil(t, resp)
		assert.NotEmpty(t, resp.Text(), "Should return a description via bytes path")

		t.Logf("Provider %s storage-ref bytes path: %s", provider.ID, truncate(resp.Text(), 150))
	})
}
