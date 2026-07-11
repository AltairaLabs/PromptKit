package providers

import (
	"context"
	"testing"
	"time"

	"github.com/AltairaLabs/PromptKit/runtime/storage"
	"github.com/AltairaLabs/PromptKit/runtime/types"
)

// nilMediaStore is a misbehaving store whose RetrieveMedia returns (nil, nil).
type nilMediaStore struct{}

func (nilMediaStore) StoreMedia(context.Context, *types.MediaContent, *storage.MediaMetadata) (storage.Reference, error) {
	return "", nil
}
func (nilMediaStore) RetrieveMedia(context.Context, storage.Reference) (*types.MediaContent, error) {
	return nil, nil //nolint:nilnil // deliberately misbehaving store for the guard test
}
func (nilMediaStore) DeleteMedia(context.Context, storage.Reference) error { return nil }
func (nilMediaStore) GetURL(context.Context, storage.Reference, time.Duration) (string, error) {
	return "", nil
}

// TestGetBase64Data_NilMediaFromStore guards against a segfault when a storage
// backend returns a nil *MediaContent with no error: it must surface an error,
// not panic. Regression for the nil-deref found by the live e2e suite.
func TestGetBase64Data_NilMediaFromStore(t *testing.T) {
	loader := NewMediaLoader(MediaLoaderConfig{StorageService: nilMediaStore{}})
	ref := "s3://bucket/key"
	_, err := loader.GetBase64Data(context.Background(),
		&types.MediaContent{StorageReference: &ref, MIMEType: "image/png"})
	if err == nil {
		t.Fatal("expected an error when the store returns nil media, got nil")
	}
}
