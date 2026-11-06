package providers

import (
	"errors"
	"strings"
	"testing"

	"github.com/AltairaLabs/PromptKit/runtime/v2/types"
)

func documentMessage(mimeType string) types.Message {
	text := "summarize"
	return types.Message{Role: "user", Parts: []types.ContentPart{
		{Type: types.ContentTypeText, Text: &text},
		{Type: types.ContentTypeDocument, Media: &types.MediaContent{MIMEType: mimeType}},
	}}
}

func TestValidateMultimodalMessage_Documents(t *testing.T) {
	tests := []struct {
		name     string
		caps     MultimodalCapabilities
		mimeType string
		wantErr  string // "" means valid
	}{
		{
			name:     "documents not supported",
			caps:     MultimodalCapabilities{SupportsImages: true},
			mimeType: types.MIMETypePDF,
			wantErr:  "provider does not support document inputs (part 1)",
		},
		{
			name:     "format not listed",
			caps:     MultimodalCapabilities{SupportsDocuments: true, DocumentFormats: []string{types.MIMETypePDF}},
			mimeType: "text/csv",
			wantErr:  "unsupported document format: text/csv (part 1)",
		},
		{
			name:     "listed format",
			caps:     MultimodalCapabilities{SupportsDocuments: true, DocumentFormats: []string{types.MIMETypePDF}},
			mimeType: types.MIMETypePDF,
		},
		{
			name:     "no format list accepts any",
			caps:     MultimodalCapabilities{SupportsDocuments: true},
			mimeType: "text/csv",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := &mockMultimodalProvider{mockProvider: mockProvider{id: "p"}, capabilities: tt.caps}
			err := ValidateMultimodalMessage(p, documentMessage(tt.mimeType))
			if tt.wantErr == "" {
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
				return
			}
			var ce *UnsupportedContentError
			if !errors.As(err, &ce) {
				t.Fatalf("got %v, want *UnsupportedContentError", err)
			}
			if ce.ContentType != "document" || ce.PartIndex != 1 {
				t.Errorf("ContentType=%q PartIndex=%d, want document/1", ce.ContentType, ce.PartIndex)
			}
			if !strings.HasSuffix(err.Error(), tt.wantErr) {
				t.Errorf("Error() = %q, want suffix %q", err.Error(), tt.wantErr)
			}
		})
	}
}

// An error about the provider, not a part, must not name a part.
func TestValidateMultimodalMessage_ProviderLevelErrorNamesNoPart(t *testing.T) {
	err := ValidateMultimodalMessage(&mockProvider{id: "text-only"}, documentMessage(types.MIMETypePDF))
	var ce *UnsupportedContentError
	if !errors.As(err, &ce) {
		t.Fatalf("got %v, want *UnsupportedContentError", err)
	}
	if want := "text-only: provider does not support multimodal content"; err.Error() != want {
		t.Errorf("Error() = %q, want %q", err.Error(), want)
	}
	if ce.PartIndex != -1 {
		t.Errorf("PartIndex = %d, want -1", ce.PartIndex)
	}
}
