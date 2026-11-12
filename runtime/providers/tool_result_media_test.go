package providers_test

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/AltairaLabs/PromptKit/runtime/v2/providers"
	"github.com/AltairaLabs/PromptKit/runtime/v2/types"
)

func TestUnsentToolMedia_NamesWhatTheToolReturnedAndWhereItIs(t *testing.T) {
	result := &types.MessageToolResult{ID: "call-1", Name: "camera"}
	url := "https://cdn.example.com/clip.mp4"
	signed := "https://bucket.s3.amazonaws.com/clip.mp4?X-Amz-Signature=abc123&X-Amz-Date=20261111"
	ref := "media/clip-7"
	const why = "Claude does not accept video input"
	cases := map[string]struct {
		part types.ContentPart
		want string
	}{
		"stored video": {
			types.ContentPart{Type: types.ContentTypeVideo, Media: &types.MediaContent{StorageReference: &ref, MIMEType: "video/mp4"}},
			"[This tool returned a video (video/mp4, stored as media/clip-7) that was not sent: Claude does not accept video input.]",
		},
		"video at a URL": {
			types.ContentPart{Type: types.ContentTypeVideo, Media: &types.MediaContent{URL: &url, MIMEType: "video/mp4"}},
			"[This tool returned a video (video/mp4, at https://cdn.example.com/clip.mp4) that was not sent: " +
				"Claude does not accept video input.]",
		},
		"a presigned URL keeps its signature out of the prompt": {
			types.ContentPart{Type: types.ContentTypeVideo, Media: &types.MediaContent{URL: &signed, MIMEType: "video/mp4"}},
			"[This tool returned a video (video/mp4, at https://bucket.s3.amazonaws.com/clip.mp4?X-Amz-Signature=REDACTED" +
				"&X-Amz-Date=20261111) that was not sent: Claude does not accept video input.]",
		},
		"inline audio": {
			types.NewAudioPartFromData("AAAA", types.MIMETypeAudioMP3),
			"[This tool returned an audio (audio/mpeg) that was not sent: Claude does not accept video input.]",
		},
		"no MIME type": {
			types.ContentPart{Type: types.ContentTypeAudio, Media: &types.MediaContent{}},
			"[This tool returned an audio that was not sent: Claude does not accept video input.]",
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			assert.Equal(t, tc.want, providers.UnsentToolMedia("claude", result, &tc.part, why))
		})
	}
}

// toolCaps take PNG images up to 1 MB and PDF documents, and no audio.
var toolCaps = providers.MultimodalCapabilities{
	SupportsImages: true, ImageFormats: []string{types.MIMETypeImagePNG}, MaxImageSizeMB: 1,
	SupportsDocuments: true, DocumentFormats: []string{types.MIMETypePDF},
}

func TestToolMediaRefusal_SaysWhyAPartCannotBeSent(t *testing.T) {
	ref := "media/huge.png"
	declared := int64(3 * 1024)
	cases := map[string]struct {
		part types.ContentPart
		want string
	}{
		"accepted image":    {types.NewImagePartFromData("AAAA", types.MIMETypeImagePNG, nil), ""},
		"accepted document": {types.NewDocumentPartFromData("AAAA", types.MIMETypePDF), ""},
		"kind not taken": {
			types.NewAudioPartFromData("AAAA", types.MIMETypeAudioWAV),
			"OpenAI Responses does not accept audio input",
		},
		"format not taken": {
			types.NewImagePartFromData("AAAA", "image/bmp", nil),
			"OpenAI Responses does not accept image/bmp images",
		},
		"inline image over the limit": {
			types.NewImagePartFromData(strings.Repeat("A", 2*1024*1024), types.MIMETypeImagePNG, nil),
			"it is 1.5 MB, over OpenAI Responses' 1 MB limit for images",
		},
		"declared size over the limit": {
			types.ContentPart{Type: types.ContentTypeImage, Media: &types.MediaContent{
				StorageReference: &ref, MIMEType: types.MIMETypeImagePNG, SizeKB: &declared}},
			"it is 3.0 MB, over OpenAI Responses' 1 MB limit for images",
		},
		"inline image under the limit": {
			types.NewImagePartFromData(strings.Repeat("A", 1024), types.MIMETypeImagePNG, nil), "",
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			assert.Equal(t, tc.want, providers.ToolMediaRefusal("OpenAI Responses", toolCaps, &tc.part))
		})
	}
}

func TestToolResultTextWithUnsentMedia(t *testing.T) {
	result := &types.MessageToolResult{ID: "call-1", Name: "camera", Parts: []types.ContentPart{
		types.NewTextPart("camera 3"),
		types.NewAudioPartFromData("AAAA", types.MIMETypeAudioMP3),
		{Type: types.ContentTypeImage},
	}}
	assert.Equal(t, "camera 3\n[This tool returned an audio (audio/mpeg) that was not sent: Ollama does not send media "+
		"in a tool result.]", providers.ToolResultTextWithUnsentMedia("ollama", "Ollama", result),
		"a part with no media is skipped")
	text := types.NewTextToolResult("call-2", "status", "all clear")
	assert.Equal(t, "all clear", providers.ToolResultTextWithUnsentMedia("ollama", "Ollama", &text))
}
