package ollama

import (
	"context"
	"testing"
	"time"

	"github.com/AltairaLabs/PromptKit/runtime/providers"
	"github.com/AltairaLabs/PromptKit/runtime/storage"
	"github.com/AltairaLabs/PromptKit/runtime/types"
)

// refStore is a fake MediaStorageService that resolves any reference to a fixed
// remote URL (and to fixed bytes on retrieval).
type refStore struct{ url, bytes string }

func (s refStore) StoreMedia(
	context.Context, *types.MediaContent, *storage.MediaMetadata,
) (storage.Reference, error) {
	return "", nil
}

func (s refStore) RetrieveMedia(context.Context, storage.Reference) (*types.MediaContent, error) {
	d := s.bytes
	return &types.MediaContent{Data: &d, MIMEType: "image/png"}, nil
}

func (s refStore) DeleteMedia(context.Context, storage.Reference) error { return nil }

func (s refStore) GetURL(context.Context, storage.Reference, time.Duration) (string, error) {
	return s.url, nil
}

// TestOllama_ImageStorageRef_UsesURL asserts a StorageReference image is resolved
// to a URL via the injected MediaStorageService (URL-first, no base64 fallback).
func TestOllama_ImageStorageRef_UsesURL(t *testing.T) {
	p := NewProvider("test", "llava", "http://localhost:11434",
		providers.ProviderDefaults{}, false, nil)
	p.SetMediaStorageService(refStore{url: "https://s3/img?sig=1"})

	ref := "s3://b/k"
	part := types.ContentPart{
		Type: types.ContentTypeImage,
		Media: &types.MediaContent{
			StorageReference: &ref,
			MIMEType:         "image/png",
		},
	}

	result, err := p.convertImagePartToOllama(context.Background(), part)
	if err != nil {
		t.Fatalf("convertImagePartToOllama returned error: %v", err)
	}

	imageURL, ok := result["image_url"].(map[string]any)
	if !ok {
		t.Fatalf("expected image_url to be map[string]any, got %T", result["image_url"])
	}
	if got := imageURL["url"]; got != "https://s3/img?sig=1" {
		t.Errorf("expected resolved storage URL, got %v", got)
	}
}
