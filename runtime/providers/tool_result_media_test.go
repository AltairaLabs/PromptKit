package providers_test

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/AltairaLabs/PromptKit/runtime/v2/providers"
	"github.com/AltairaLabs/PromptKit/runtime/v2/types"
)

func TestUnsentToolMedia_NamesWhatTheToolReturnedAndWhereItIs(t *testing.T) {
	result := &types.MessageToolResult{ID: "call-1", Name: "camera"}
	url := "https://cdn.example.com/clip.mp4"
	ref := "media/clip-7"
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
		"inline audio": {
			types.NewAudioPartFromData("AAAA", types.MIMETypeAudioMP3),
			"[This tool returned an audio (audio/mpeg) that was not sent: Claude does not accept audio input.]",
		},
		"no MIME type": {
			types.ContentPart{Type: types.ContentTypeAudio, Media: &types.MediaContent{}},
			"[This tool returned an audio that was not sent: Claude does not accept audio input.]",
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			assert.Equal(t, tc.want, providers.UnsentToolMedia("claude", "Claude", result, &tc.part))
		})
	}
}
