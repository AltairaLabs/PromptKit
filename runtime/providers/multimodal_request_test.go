package providers

import (
	"errors"
	"testing"

	"github.com/AltairaLabs/PromptKit/runtime/v2/types"
)

// Text carried in Parts is not media: a provider that declares no multimodal
// capabilities accepts it.
func TestValidateMultimodalMessage_TextPartsNeedNoCapabilities(t *testing.T) {
	msg := types.Message{Role: "user", Parts: []types.ContentPart{types.NewTextPart("hello")}}
	if err := ValidateMultimodalMessage(&mockProvider{id: "text-only"}, msg); err != nil {
		t.Fatalf("text-only parts rejected: %v", err)
	}
}

// A part with no MIME type is checked for its modality only, since there is
// no format to compare.
func TestValidateMultimodalMessage_EmptyMIMETypeChecksModalityOnly(t *testing.T) {
	url := "https://example.com/picture"
	part := types.ContentPart{Type: types.ContentTypeImage, Media: &types.MediaContent{URL: &url}}
	msg := types.Message{Role: "user", Parts: []types.ContentPart{part}}

	images := &mockMultimodalProvider{
		mockProvider: mockProvider{id: "images"},
		capabilities: MultimodalCapabilities{SupportsImages: true, ImageFormats: []string{types.MIMETypeImagePNG}},
	}
	if err := ValidateMultimodalMessage(images, msg); err != nil {
		t.Fatalf("image without a MIME type rejected by an image provider: %v", err)
	}

	noImages := &mockMultimodalProvider{mockProvider: mockProvider{id: "no-images"}}
	var unsupported *UnsupportedContentError
	if err := ValidateMultimodalMessage(noImages, msg); !errors.As(err, &unsupported) {
		t.Fatalf("image accepted by a provider without image support: %v", err)
	}
}

// A format entry ending in "/*" admits every subtype of that type, and only
// that type.
func TestIsFormatSupported_TypeWildcard(t *testing.T) {
	p := &mockMultimodalProvider{
		mockProvider: mockProvider{id: "docs"},
		capabilities: MultimodalCapabilities{SupportsDocuments: true, DocumentFormats: []string{"text/*"}},
	}
	for mime, want := range map[string]bool{
		"text/plain":      true,
		"text/markdown":   true,
		"application/pdf": false,
		"textual/plain":   false,
	} {
		if got := IsFormatSupported(p, types.ContentTypeDocument, mime); got != want {
			t.Errorf("IsFormatSupported(%q) = %v, want %v", mime, got, want)
		}
	}
}

// Assistant replies are model output, not input the capabilities describe;
// user messages are still checked.
func TestValidateMultimodalRequest_SkipsAssistantMessages(t *testing.T) {
	p := &mockMultimodalProvider{
		mockProvider: mockProvider{id: "images"},
		capabilities: MultimodalCapabilities{SupportsImages: true},
	}
	audio := types.NewAudioPartFromData("AAAA", types.MIMETypeAudioWAV)

	withAssistantAudio := &PredictionRequest{Messages: []types.Message{
		{Role: "user", Content: "speak"},
		{Role: "assistant", Parts: []types.ContentPart{audio}},
	}}
	if err := ValidateMultimodalRequest(p, withAssistantAudio); err != nil {
		t.Fatalf("assistant audio was validated: %v", err)
	}

	withUserAudio := &PredictionRequest{Messages: []types.Message{
		{Role: "user", Parts: []types.ContentPart{audio}},
	}}
	var unsupported *UnsupportedContentError
	if err := ValidateMultimodalRequest(p, withUserAudio); !errors.As(err, &unsupported) {
		t.Fatalf("user audio accepted by a provider without audio: %v", err)
	}
}
