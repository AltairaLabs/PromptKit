package openai

import (
	"context"
	"encoding/base64"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/AltairaLabs/PromptKit/runtime/v2/providers"
	"github.com/AltairaLabs/PromptKit/runtime/v2/types"
)

// The Responses API path loads images and rejects unsupported parts as the
// Chat Completions path does. It used to read only an image's URL or inline
// data, so an image given as a file (or a storage reference) was dropped from
// the request without a word, as was any audio or video part.
func TestPrepareResponsesMessages_LoadsImagesAndRejectsWhatItCannotSend(t *testing.T) {
	p := NewProviderWithConfig("responses-media", "gpt-6-luna", "https://api.openai.com/v1",
		providers.ProviderDefaults{}, false, nil)
	require.EqualValues(t, APIModeResponses, p.apiMode)

	png := []byte("\x89PNG\r\n\x1a\nnot-really")
	path := filepath.Join(t.TempDir(), "pic.png")
	require.NoError(t, os.WriteFile(path, png, 0o600))
	detail := "low"
	image := types.ContentPart{Type: types.ContentTypeImage,
		Media: &types.MediaContent{FilePath: &path, MIMEType: "image/png", Detail: &detail}}
	text := "describe it"
	req := providers.PredictionRequest{Messages: []types.Message{{Role: "user", Parts: []types.ContentPart{
		{Type: types.ContentTypeText, Text: &text}, image,
	}}}}

	prepared, err := p.prepareResponsesMessages(context.Background(), req)
	require.NoError(t, err)
	assert.Nil(t, req.Messages[0].Parts[1].Media.URL, "the caller's message is not modified")

	input := p.buildResponsesRequest(prepared, nil, "")["input"].([]any)
	content := input[0].(map[string]any)[keyContent].([]map[string]any)
	require.Len(t, content, 2, "the image reaches the request")
	assert.Equal(t, "input_image", content[1][keyType])
	assert.Equal(t, "data:image/png;base64,"+base64.StdEncoding.EncodeToString(png), content[1][keyImageURL])
	assert.Equal(t, "low", content[1]["detail"])

	for _, tc := range []struct {
		part types.ContentPart
		want string
	}{
		{types.ContentPart{Type: types.ContentTypeAudio, Media: &types.MediaContent{FilePath: &path}},
			"audio content requires audio model"},
		{types.ContentPart{Type: types.ContentTypeVideo, Media: &types.MediaContent{FilePath: &path}},
			"video content not supported"},
		{types.ContentPart{Type: types.ContentTypeImage}, "image part missing media content"},
	} {
		bad := providers.PredictionRequest{Messages: []types.Message{{Role: "user", Parts: []types.ContentPart{tc.part}}}}
		_, err := p.prepareResponsesMessages(context.Background(), bad)
		assert.ErrorContains(t, err, tc.want)
	}
}
