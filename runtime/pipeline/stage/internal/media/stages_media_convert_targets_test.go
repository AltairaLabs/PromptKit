package media

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/AltairaLabs/PromptKit/runtime/v2/pipeline/stage/internal/core"
	"github.com/AltairaLabs/PromptKit/runtime/v2/types"
)

// convertOne runs elem through a MediaConvertStage that reports conversion
// errors on the element, and returns what it emitted.
func convertOne(t *testing.T, config MediaConvertConfig, elem core.StreamElement) core.StreamElement {
	t.Helper()
	config.PassthroughOnError = false
	input := make(chan core.StreamElement, 1)
	output := make(chan core.StreamElement, 1)
	input <- elem
	close(input)
	require.NoError(t, NewMediaConvertStage(&config).Process(context.Background(), input, output))
	return <-output
}

// A media kind with no target formats configured is left alone, whatever its
// format and however unconvertible its data: no conversion is attempted, so
// no error is reported and the element comes out as it went in.
func TestMediaConvertStage_NoTargetsForAKindLeavesItAlone(t *testing.T) {
	t.Parallel()
	bad := "!!! not base64 !!!"
	cases := map[string]func() core.StreamElement{
		"audio element": func() core.StreamElement {
			return core.StreamElement{Audio: &core.AudioData{Format: core.AudioFormatMP3}}
		},
		"image element": func() core.StreamElement {
			return core.StreamElement{Image: &core.ImageData{MIMEType: "image/gif", Data: []byte("not an image")}}
		},
		"video element": func() core.StreamElement {
			return core.StreamElement{Video: &core.VideoData{MIMEType: "video/webm", Data: []byte("not a video")}}
		},
		"audio part": func() core.StreamElement {
			return core.StreamElement{Message: &types.Message{Role: "user", Parts: []types.ContentPart{
				{Type: types.ContentTypeAudio, Media: &types.MediaContent{MIMEType: "audio/mpeg", Data: &bad}}}}}
		},
		"image part": func() core.StreamElement {
			return core.StreamElement{Message: &types.Message{Role: "user", Parts: []types.ContentPart{
				{Type: types.ContentTypeImage, Media: &types.MediaContent{MIMEType: "image/gif", Data: &bad}}}}}
		},
	}
	for name, build := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			got := convertOne(t, DefaultMediaConvertConfig(), build())
			assert.Equal(t, build(), got)
		})
	}
}

// An image part with no data is skipped, not converted.
func TestMediaConvertStage_ImagePartWithoutDataIsSkipped(t *testing.T) {
	t.Parallel()
	config := DefaultMediaConvertConfig()
	config.TargetImageFormats = []string{"image/jpeg"}
	elem := core.StreamElement{Message: &types.Message{Role: "user", Parts: []types.ContentPart{
		{Type: types.ContentTypeImage, Media: &types.MediaContent{MIMEType: "image/gif"}}}}}

	got := convertOne(t, config, elem)
	assert.NoError(t, got.Error)
	assert.Equal(t, &types.MediaContent{MIMEType: "image/gif"}, got.Message.Parts[0].Media)
}

// An image is converted to the one target format configured, not to the
// JPEG fallback, and keeps its dimensions.
func TestMediaConvertStage_ImageConvertsToTheConfiguredTarget(t *testing.T) {
	t.Parallel()
	config := DefaultMediaConvertConfig()
	config.TargetImageFormats = []string{"image/png"}
	elem := core.StreamElement{Image: &core.ImageData{MIMEType: "image/jpeg", Data: createTestImageData(8, 6)}}

	got := convertOne(t, config, elem)
	require.NoError(t, got.Error)
	assert.Equal(t, "image/png", got.Image.MIMEType)
	assert.Equal(t, []int{8, 6}, []int{got.Image.Width, got.Image.Height})
	assert.Equal(t, []byte{0x89, 'P', 'N', 'G'}, got.Image.Data[:4])
}
