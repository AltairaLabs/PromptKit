package gemini

import (
	"context"
	"encoding/base64"
	"testing"
	"time"

	"github.com/AltairaLabs/PromptKit/runtime/providers"
	"github.com/AltairaLabs/PromptKit/runtime/storage"
	"github.com/AltairaLabs/PromptKit/runtime/types"
)

// refStore is a minimal MediaStorageService that resolves any storage reference
// to a fixed base64 payload, letting tests assert that Gemini's conversion path
// routes StorageReference media through the injected store.
type refStore struct{ bytes string }

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
	return "", nil
}

// convProvider returns a bare Gemini provider for exercising the (now-method)
// conversion helpers in tests.
func convProvider() *Provider {
	return NewProvider("test", "gemini-1.5-flash", "https://test.com", providers.ProviderDefaults{}, false)
}

func TestGeminiConvertMediaPart_ResolvesStorageReference(t *testing.T) {
	want := base64.StdEncoding.EncodeToString([]byte("IMGBYTES"))

	p := convProvider()
	p.SetMediaStorageService(refStore{bytes: want})

	ref := "s3://b/k"
	part := types.ContentPart{
		Type: types.ContentTypeImage,
		Media: &types.MediaContent{
			StorageReference: &ref,
			MIMEType:         "image/png",
		},
	}

	got, err := p.convertMediaPartToGemini(context.Background(), part)
	if err != nil {
		t.Fatalf("convertMediaPartToGemini returned error: %v", err)
	}
	if got.InlineData == nil {
		t.Fatalf("expected inline data on converted part, got nil")
	}
	if got.InlineData.Data != want {
		t.Errorf("inline data mismatch: got %q, want %q", got.InlineData.Data, want)
	}
	if got.InlineData.MimeType != "image/png" {
		t.Errorf("mime type mismatch: got %q, want image/png", got.InlineData.MimeType)
	}
}

// TestGeminiToolPath_ResolvesStorageReference verifies the tool-request media
// builder (convertMediaPartToMap) also routes StorageReference through the store,
// fixing the prior bug where only Data/URL were handled.
func TestGeminiToolPath_ResolvesStorageReference(t *testing.T) {
	want := base64.StdEncoding.EncodeToString([]byte("IMGBYTES"))

	p := convProvider()
	p.SetMediaStorageService(refStore{bytes: want})

	ref := "s3://b/k"
	part := types.ContentPart{
		Type: types.ContentTypeImage,
		Media: &types.MediaContent{
			StorageReference: &ref,
			MIMEType:         "image/png",
		},
	}

	got := p.convertMediaPartToMap(context.Background(), part)
	if got == nil {
		t.Fatalf("convertMediaPartToMap returned nil for storage-reference media")
	}
	inline, ok := got["inlineData"].(map[string]any)
	if !ok {
		t.Fatalf("expected inlineData map, got %#v", got)
	}
	if inline["data"] != want {
		t.Errorf("inline data mismatch: got %v, want %q", inline["data"], want)
	}
}
