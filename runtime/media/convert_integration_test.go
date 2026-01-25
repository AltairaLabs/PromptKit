package media

import (
	"context"
	"testing"

	"github.com/AltairaLabs/PromptKit/runtime/types"
)

func TestNewContentConverter(t *testing.T) {
	config := DefaultAudioConverterConfig()
	conv := NewContentConverter(config)

	if conv == nil {
		t.Fatal("expected non-nil converter")
	}
	if conv.audioConverter == nil {
		t.Fatal("expected non-nil audio converter")
	}
}

func TestContentConverter_ConvertMessageForProvider_NilMessage(t *testing.T) {
	conv := NewContentConverter(DefaultAudioConverterConfig())

	result, err := conv.ConvertMessageForProvider(context.Background(), nil, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != nil {
		t.Error("expected nil result for nil message")
	}
}

func TestContentConverter_ConvertMessageForProvider_EmptyParts(t *testing.T) {
	conv := NewContentConverter(DefaultAudioConverterConfig())

	msg := &types.Message{
		Role:  "user",
		Parts: []types.ContentPart{},
	}

	result, err := conv.ConvertMessageForProvider(context.Background(), msg, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != msg {
		t.Error("expected same message returned for empty parts")
	}
}

func TestContentConverter_ConvertMessageForProvider_TextOnly(t *testing.T) {
	conv := NewContentConverter(DefaultAudioConverterConfig())

	textContent := "Hello, world!"
	msg := &types.Message{
		Role: "user",
		Parts: []types.ContentPart{
			{
				Type: types.ContentTypeText,
				Text: &textContent,
			},
		},
	}

	result, err := conv.ConvertMessageForProvider(context.Background(), msg, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result.Parts) != 1 {
		t.Errorf("expected 1 part, got %d", len(result.Parts))
	}
	if *result.Parts[0].Text != textContent {
		t.Error("text content should be unchanged")
	}
}

func TestContentConverter_ConvertMediaContentIfNeeded_NilMedia(t *testing.T) {
	conv := NewContentConverter(DefaultAudioConverterConfig())

	result, err := conv.ConvertMediaContentIfNeeded(context.Background(), nil, types.ContentTypeAudio, []string{MIMETypeAudioWAV})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != nil {
		t.Error("expected nil result for nil media")
	}
}

func TestContentConverter_ConvertMediaContentIfNeeded_EmptyTargetFormats(t *testing.T) {
	conv := NewContentConverter(DefaultAudioConverterConfig())

	data := "base64audiodata"
	media := &types.MediaContent{
		Data:     &data,
		MIMEType: MIMETypeAudioMP3,
	}

	result, err := conv.ConvertMediaContentIfNeeded(context.Background(), media, types.ContentTypeAudio, []string{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != media {
		t.Error("expected same media returned when no target formats")
	}
}

func TestContentConverter_ConvertMediaContentIfNeeded_AlreadySupported(t *testing.T) {
	conv := NewContentConverter(DefaultAudioConverterConfig())

	data := "base64audiodata"
	media := &types.MediaContent{
		Data:     &data,
		MIMEType: MIMETypeAudioWAV,
	}

	result, err := conv.ConvertMediaContentIfNeeded(
		context.Background(),
		media,
		types.ContentTypeAudio,
		[]string{MIMETypeAudioWAV, MIMETypeAudioMP3},
	)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != media {
		t.Error("expected same media returned when already in supported format")
	}
}

func TestContentConverter_ConvertMediaContentIfNeeded_UnsupportedContentType(t *testing.T) {
	conv := NewContentConverter(DefaultAudioConverterConfig())

	data := "base64videodata"
	media := &types.MediaContent{
		Data:     &data,
		MIMEType: "video/mp4",
	}

	// Video conversion not implemented, should return as-is
	result, err := conv.ConvertMediaContentIfNeeded(
		context.Background(),
		media,
		types.ContentTypeVideo,
		[]string{"video/webm"},
	)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != media {
		t.Error("expected same media returned for unsupported content type")
	}
}

func TestGetMediaData_WithBase64Data(t *testing.T) {
	// Valid base64 encoded "hello"
	base64Data := "aGVsbG8="
	media := &types.MediaContent{
		Data: &base64Data,
	}

	data, err := getMediaData(media)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if string(data) != "hello" {
		t.Errorf("expected 'hello', got %q", string(data))
	}
}

func TestGetMediaData_EmptyData(t *testing.T) {
	emptyData := ""
	media := &types.MediaContent{
		Data: &emptyData,
	}

	_, err := getMediaData(media)
	if err == nil {
		t.Error("expected error for empty data")
	}
}

func TestGetMediaData_NilData(t *testing.T) {
	media := &types.MediaContent{
		Data: nil,
	}

	_, err := getMediaData(media)
	if err == nil {
		t.Error("expected error for nil data")
	}
}

func TestGetMediaData_InvalidBase64(t *testing.T) {
	invalidData := "not-valid-base64!!!"
	media := &types.MediaContent{
		Data: &invalidData,
	}

	_, err := getMediaData(media)
	if err == nil {
		t.Error("expected error for invalid base64")
	}
}

// mockMultimodalProvider implements providers.MultimodalSupport for testing
type mockMultimodalProvider struct {
	audioFormats []string
	imageFormats []string
	videoFormats []string
}

func (m *mockMultimodalProvider) GetMultimodalCapabilities() mockMultimodalCapabilities {
	return mockMultimodalCapabilities{
		AudioFormats: m.audioFormats,
		ImageFormats: m.imageFormats,
		VideoFormats: m.videoFormats,
	}
}

type mockMultimodalCapabilities struct {
	AudioFormats []string
	ImageFormats []string
	VideoFormats []string
}

func TestProviderCaps_Extraction(t *testing.T) {
	// Test with nil provider
	caps := getProviderCapabilities(nil)
	if caps == nil {
		t.Fatal("expected non-nil caps even for nil provider")
	}
	if len(caps.audioFormats) != 0 {
		t.Error("expected empty audio formats for nil provider")
	}
}

func TestConvertPartIfNeeded_TextPart(t *testing.T) {
	conv := NewContentConverter(DefaultAudioConverterConfig())

	textContent := "Hello"
	part := types.ContentPart{
		Type: types.ContentTypeText,
		Text: &textContent,
	}

	caps := &providerCaps{
		audioFormats: []string{MIMETypeAudioWAV},
	}

	result, err := conv.convertPartIfNeeded(context.Background(), part, caps)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Type != types.ContentTypeText {
		t.Error("text part type should be unchanged")
	}
}

func TestConvertPartIfNeeded_NilMedia(t *testing.T) {
	conv := NewContentConverter(DefaultAudioConverterConfig())

	part := types.ContentPart{
		Type:  types.ContentTypeAudio,
		Media: nil,
	}

	caps := &providerCaps{
		audioFormats: []string{MIMETypeAudioWAV},
	}

	result, err := conv.convertPartIfNeeded(context.Background(), part, caps)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Should return the part unchanged when media is nil
	if result.Media != nil {
		t.Error("expected nil media in result")
	}
}

func TestConvertPartIfNeeded_NoFormatRestrictions(t *testing.T) {
	conv := NewContentConverter(DefaultAudioConverterConfig())

	data := "base64audio"
	part := types.ContentPart{
		Type: types.ContentTypeAudio,
		Media: &types.MediaContent{
			Data:     &data,
			MIMEType: MIMETypeAudioFLAC,
		},
	}

	caps := &providerCaps{
		audioFormats: []string{}, // No restrictions
	}

	result, err := conv.convertPartIfNeeded(context.Background(), part, caps)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Should pass through unchanged when no format restrictions
	if result.Media.MIMEType != MIMETypeAudioFLAC {
		t.Error("media should be unchanged when no format restrictions")
	}
}

func TestConvertPartIfNeeded_AlreadySupported(t *testing.T) {
	conv := NewContentConverter(DefaultAudioConverterConfig())

	data := "base64audio"
	part := types.ContentPart{
		Type: types.ContentTypeAudio,
		Media: &types.MediaContent{
			Data:     &data,
			MIMEType: MIMETypeAudioWAV,
		},
	}

	caps := &providerCaps{
		audioFormats: []string{MIMETypeAudioWAV, MIMETypeAudioMP3},
	}

	result, err := conv.convertPartIfNeeded(context.Background(), part, caps)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Should pass through unchanged when already supported
	if result.Media.MIMEType != MIMETypeAudioWAV {
		t.Error("media should be unchanged when already in supported format")
	}
}

func TestConvertPartIfNeeded_ImagePart(t *testing.T) {
	conv := NewContentConverter(DefaultAudioConverterConfig())

	data := "base64image"
	part := types.ContentPart{
		Type: types.ContentTypeImage,
		Media: &types.MediaContent{
			Data:     &data,
			MIMEType: "image/png",
		},
	}

	caps := &providerCaps{
		imageFormats: []string{"image/jpeg"},
	}

	// Image conversion not implemented, should pass through
	result, err := conv.convertPartIfNeeded(context.Background(), part, caps)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Currently no image conversion, so should be unchanged
	if result.Media.MIMEType != "image/png" {
		t.Error("image should pass through (conversion not implemented)")
	}
}

func TestConvertPartIfNeeded_VideoPart(t *testing.T) {
	conv := NewContentConverter(DefaultAudioConverterConfig())

	data := "base64video"
	part := types.ContentPart{
		Type: types.ContentTypeVideo,
		Media: &types.MediaContent{
			Data:     &data,
			MIMEType: "video/webm",
		},
	}

	caps := &providerCaps{
		videoFormats: []string{"video/mp4"},
	}

	// Video conversion not implemented, should pass through
	result, err := conv.convertPartIfNeeded(context.Background(), part, caps)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Currently no video conversion, so should be unchanged
	if result.Media.MIMEType != "video/webm" {
		t.Error("video should pass through (conversion not implemented)")
	}
}

func TestConvertPartIfNeeded_UnknownContentType(t *testing.T) {
	conv := NewContentConverter(DefaultAudioConverterConfig())

	data := "base64data"
	part := types.ContentPart{
		Type: "unknown",
		Media: &types.MediaContent{
			Data:     &data,
			MIMEType: "application/octet-stream",
		},
	}

	caps := &providerCaps{}

	result, err := conv.convertPartIfNeeded(context.Background(), part, caps)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Unknown type should pass through
	if result.Type != "unknown" {
		t.Error("unknown content type should pass through unchanged")
	}
}

func TestConvertPartIfNeeded_ImageAlreadySupported(t *testing.T) {
	conv := NewContentConverter(DefaultAudioConverterConfig())

	data := "base64image"
	part := types.ContentPart{
		Type: types.ContentTypeImage,
		Media: &types.MediaContent{
			Data:     &data,
			MIMEType: "image/jpeg",
		},
	}

	caps := &providerCaps{
		imageFormats: []string{"image/jpeg", "image/png"},
	}

	result, err := conv.convertPartIfNeeded(context.Background(), part, caps)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Media.MIMEType != "image/jpeg" {
		t.Error("image should be unchanged when already supported")
	}
}

func TestConvertPartIfNeeded_VideoAlreadySupported(t *testing.T) {
	conv := NewContentConverter(DefaultAudioConverterConfig())

	data := "base64video"
	part := types.ContentPart{
		Type: types.ContentTypeVideo,
		Media: &types.MediaContent{
			Data:     &data,
			MIMEType: "video/mp4",
		},
	}

	caps := &providerCaps{
		videoFormats: []string{"video/mp4", "video/webm"},
	}

	result, err := conv.convertPartIfNeeded(context.Background(), part, caps)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Media.MIMEType != "video/mp4" {
		t.Error("video should be unchanged when already supported")
	}
}

func TestContentConverter_ConvertMessageForProvider_MultipleParts(t *testing.T) {
	conv := NewContentConverter(DefaultAudioConverterConfig())

	textContent := "Hello"
	audioData := "base64audio"
	msg := &types.Message{
		Role: "user",
		Parts: []types.ContentPart{
			{
				Type: types.ContentTypeText,
				Text: &textContent,
			},
			{
				Type: types.ContentTypeAudio,
				Media: &types.MediaContent{
					Data:     &audioData,
					MIMEType: MIMETypeAudioWAV,
				},
			},
		},
	}

	result, err := conv.ConvertMessageForProvider(context.Background(), msg, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result.Parts) != 2 {
		t.Errorf("expected 2 parts, got %d", len(result.Parts))
	}
	// Original should not be modified
	if msg == result {
		t.Error("expected a copy of the message, not the same instance")
	}
}

func TestContentConverter_ConvertMediaContentIfNeeded_ImageType(t *testing.T) {
	conv := NewContentConverter(DefaultAudioConverterConfig())

	data := "base64imagedata"
	media := &types.MediaContent{
		Data:     &data,
		MIMEType: "image/png",
	}

	// Image conversion not implemented, should return as-is
	result, err := conv.ConvertMediaContentIfNeeded(
		context.Background(),
		media,
		types.ContentTypeImage,
		[]string{"image/jpeg"},
	)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != media {
		t.Error("expected same media returned for unsupported conversion")
	}
}

func TestProviderCaps_NilProvider(t *testing.T) {
	caps := getProviderCapabilities(nil)

	if caps == nil {
		t.Fatal("expected non-nil caps")
	}
	if len(caps.audioFormats) != 0 {
		t.Error("expected empty audio formats")
	}
	if len(caps.imageFormats) != 0 {
		t.Error("expected empty image formats")
	}
	if len(caps.videoFormats) != 0 {
		t.Error("expected empty video formats")
	}
}
