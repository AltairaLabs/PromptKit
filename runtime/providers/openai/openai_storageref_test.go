package openai

import (
	"context"
	"encoding/base64"
	"testing"
	"time"

	"github.com/AltairaLabs/PromptKit/runtime/providers"
	"github.com/AltairaLabs/PromptKit/runtime/storage"
	"github.com/AltairaLabs/PromptKit/runtime/types"
)

// refStore is a minimal MediaStorageService for storage-reference tests.
// GetURL returns a fixed URL; RetrieveMedia returns bytes as inline Data.
type refStore struct {
	url   string
	bytes string
}

func (s refStore) StoreMedia(
	context.Context, *types.MediaContent, *storage.MediaMetadata,
) (storage.Reference, error) {
	return "", nil
}

func (s refStore) RetrieveMedia(context.Context, storage.Reference) (*types.MediaContent, error) {
	d := s.bytes
	return &types.MediaContent{Data: &d, MIMEType: "audio/mpeg"}, nil
}

func (s refStore) DeleteMedia(context.Context, storage.Reference) error { return nil }

func (s refStore) GetURL(context.Context, storage.Reference, time.Duration) (string, error) {
	return s.url, nil
}

// TestOpenAI_ImageStorageRef_UsesURL verifies an image carried by a storage
// reference is resolved to a model-fetchable URL (not inlined as a data: URL).
func TestOpenAI_ImageStorageRef_UsesURL(t *testing.T) {
	p := NewProvider("test", "gpt-4o", "https://api.openai.com/v1", providers.ProviderDefaults{}, false)
	p.SetMediaStorageService(refStore{url: "https://s3/img?sig=1"})

	ref := "s3://b/k"
	part := types.ContentPart{
		Type: types.ContentTypeImage,
		Media: &types.MediaContent{
			StorageReference: &ref,
			MIMEType:         types.MIMETypeImagePNG,
		},
	}

	out, err := p.convertImagePartToOpenAI(context.Background(), part)
	if err != nil {
		t.Fatalf("convertImagePartToOpenAI returned error: %v", err)
	}

	imageURL, ok := out["image_url"].(map[string]interface{})
	if !ok {
		t.Fatalf("expected image_url map, got %T", out["image_url"])
	}
	if got := imageURL["url"]; got != "https://s3/img?sig=1" {
		t.Fatalf("expected resolved URL, got %v", got)
	}
}

// TestOpenAI_AudioStorageRef_UsesBytes verifies audio carried by a storage
// reference is loaded as base64 bytes via the store (OpenAI has no audio URL path).
func TestOpenAI_AudioStorageRef_UsesBytes(t *testing.T) {
	p := NewProvider("test", "gpt-4o", "https://api.openai.com/v1", providers.ProviderDefaults{}, false)
	want := base64.StdEncoding.EncodeToString([]byte("AUDIO"))
	p.SetMediaStorageService(refStore{bytes: want})

	ref := "s3://b/audio"
	part := types.ContentPart{
		Type: types.ContentTypeAudio,
		Media: &types.MediaContent{
			StorageReference: &ref,
			MIMEType:         types.MIMETypeAudioMP3,
		},
	}

	out, err := p.convertAudioPartToOpenAI(context.Background(), part)
	if err != nil {
		t.Fatalf("convertAudioPartToOpenAI returned error: %v", err)
	}

	inputAudio, ok := out["input_audio"].(map[string]interface{})
	if !ok {
		t.Fatalf("expected input_audio map, got %T", out["input_audio"])
	}
	if got := inputAudio["data"]; got != want {
		t.Fatalf("expected base64 %q, got %v", want, got)
	}
}
