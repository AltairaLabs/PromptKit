package providers

import (
	"errors"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/AltairaLabs/PromptKit/runtime/v2/types"
)

// imageProvider takes PNG images up to 1 MB and no audio.
func imageProvider() *mockMultimodalProvider {
	return &mockMultimodalProvider{
		mockProvider: mockProvider{id: "images"},
		capabilities: MultimodalCapabilities{
			SupportsImages: true, ImageFormats: []string{types.MIMETypeImagePNG}, MaxImageSizeMB: 1,
		},
	}
}

func toolResultRequest(parts ...types.ContentPart) *PredictionRequest {
	return &PredictionRequest{Messages: []types.Message{
		{Role: "user", Content: "look"},
		{Role: "tool", ToolResult: &types.MessageToolResult{ID: "call-1", Name: "snapshot", Parts: parts}},
	}}
}

// A tool result's image in a format the provider does not take fails before
// the request, naming the format, as a user's image does.
func TestValidateMultimodalRequest_ToolResultImageFormat(t *testing.T) {
	err := ValidateMultimodalRequest(imageProvider(),
		toolResultRequest(types.NewImagePartFromData("AAAA", "image/bmp", nil)))
	var unsupported *UnsupportedContentError
	require.True(t, errors.As(err, &unsupported), "got %v", err)
	assert.Equal(t, "image/bmp", unsupported.MIMEType)
	assert.Equal(t, "images: unsupported image format: image/bmp (part 0)", err.Error())
}

// A tool result's inline image over the provider's size limit fails before
// the request instead of at the provider.
func TestValidateMultimodalRequest_ToolResultImageSize(t *testing.T) {
	big := strings.Repeat("A", 2*1024*1024) // 1.5 MB decoded
	err := ValidateMultimodalRequest(imageProvider(),
		toolResultRequest(types.NewImagePartFromData(big, types.MIMETypeImagePNG, nil)))
	var unsupported *UnsupportedContentError
	require.True(t, errors.As(err, &unsupported), "got %v", err)
	assert.Equal(t, "images: image is 1.5 MB, over the provider's 1 MB limit (part 0)", err.Error())

	small := strings.Repeat("A", 1024)
	assert.NoError(t, ValidateMultimodalRequest(imageProvider(),
		toolResultRequest(types.NewImagePartFromData(small, types.MIMETypeImagePNG, nil))))
}

// Media of a kind the provider does not take at all is not refused in a tool
// result: the provider describes it to the model instead.
func TestValidateMultimodalRequest_ToolResultAudioIsLeftToTheProvider(t *testing.T) {
	audio := types.NewAudioPartFromData("AAAA", types.MIMETypeAudioWAV)
	inToolResult := ValidateMultimodalRequest(imageProvider(), toolResultRequest(audio))
	inUserMessage := ValidateMultimodalRequest(imageProvider(),
		&PredictionRequest{Messages: []types.Message{{Role: "user", Parts: []types.ContentPart{audio}}}})

	assert.Nil(t, inToolResult, "the provider describes a tool's audio instead")
	var unsupported *UnsupportedContentError
	require.True(t, errors.As(inUserMessage, &unsupported), "a user's audio is still refused")
	assert.Equal(t, "audio", unsupported.ContentType)
}

// A declared size counts when the bytes are not inline (a stored image).
func TestValidateMultimodalRequest_ToolResultDeclaredSize(t *testing.T) {
	ref := "media/huge.png"
	kb := int64(3 * 1024)
	err := ValidateMultimodalRequest(imageProvider(), toolResultRequest(types.ContentPart{
		Type: types.ContentTypeImage, Media: &types.MediaContent{StorageReference: &ref, MIMEType: types.MIMETypeImagePNG, SizeKB: &kb},
	}))
	require.Error(t, err)
	assert.Equal(t, "images: image is 3.0 MB, over the provider's 1 MB limit (part 0)", err.Error())
}
