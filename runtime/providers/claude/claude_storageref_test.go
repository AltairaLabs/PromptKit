package claude

import (
	"context"
	"encoding/base64"
	"strings"
	"testing"
	"time"

	"github.com/AltairaLabs/PromptKit/runtime/storage"
	"github.com/AltairaLabs/PromptKit/runtime/types"
)

// refStore is a fake MediaStorageService that returns a canned URL and/or bytes
// so the Claude conversion path can be exercised without a real backend.
type refStore struct{ url, bytes string }

func (s refStore) StoreMedia(context.Context, *types.MediaContent, *storage.MediaMetadata) (storage.Reference, error) {
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

// TestClaude_ImageStorageRef_UsesURL asserts a StorageReference image resolves to
// a model-fetchable URL (URL-first) rather than inlined bytes.
func TestClaude_ImageStorageRef_UsesURL(t *testing.T) {
	p := testClaudeProvider()
	p.SetMediaStorageService(refStore{url: "https://s3/img?sig=1"})

	part := types.NewImagePartFromStorageRef("s3://b/k", types.MIMETypeImagePNG, nil)
	block, err := p.convertImagePartToClaude(context.Background(), part)
	if err != nil {
		t.Fatalf("convertImagePartToClaude returned error: %v", err)
	}
	if block.Source.Type != "url" {
		t.Fatalf("expected source type url, got %q", block.Source.Type)
	}
	if block.Source.URL != "https://s3/img?sig=1" {
		t.Errorf("expected presigned URL, got %q", block.Source.URL)
	}
}

// TestClaude_DocumentStorageRef_UsesBytes asserts a StorageReference document
// falls back to base64 bytes, since Claude documents do not support URL sources.
func TestClaude_DocumentStorageRef_UsesBytes(t *testing.T) {
	encoded := base64.StdEncoding.EncodeToString([]byte("PDFBYTES"))
	p := testClaudeProvider()
	p.SetMediaStorageService(refStore{bytes: encoded})

	part := types.NewDocumentPartFromStorageRef("s3://b/doc", types.MIMETypePDF)
	block, err := p.convertDocumentPartToClaude(context.Background(), part)
	if err != nil {
		t.Fatalf("convertDocumentPartToClaude returned error: %v", err)
	}
	if block.Source.Type != "base64" {
		t.Fatalf("expected source type base64, got %q", block.Source.Type)
	}
	if !strings.Contains(block.Source.Data, encoded) {
		t.Errorf("expected data to contain %q, got %q", encoded, block.Source.Data)
	}
}
