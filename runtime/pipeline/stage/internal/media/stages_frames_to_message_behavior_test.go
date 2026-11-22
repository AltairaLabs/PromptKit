package media

import (
	"context"
	"encoding/base64"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/AltairaLabs/PromptKit/runtime/v2/pipeline/stage/internal/core"
	"github.com/AltairaLabs/PromptKit/runtime/v2/types"
)

// taggedFrame is frame idx of total from video "v1": its image data is the
// single byte tag, so a composed message says which frames it holds.
func taggedFrame(idx, total int, tag byte) core.StreamElement {
	return core.StreamElement{
		Image: &core.ImageData{Data: []byte{tag}, MIMEType: "image/jpeg"},
		Meta:  core.ElementMetadata{VideoFrames: &core.VideoFramesInfo{VideoID: "v1", FrameIndex: idx, TotalFrames: total}},
	}
}

// partTags returns the tag byte of each image part of msg, in order.
func partTags(t *testing.T, msg *types.Message) []byte {
	t.Helper()
	var tags []byte
	for _, p := range msg.Parts {
		require.NotNil(t, p.Media)
		require.NotNil(t, p.Media.Data)
		raw, err := base64.StdEncoding.DecodeString(*p.Media.Data)
		require.NoError(t, err)
		require.Len(t, raw, 1)
		tags = append(tags, raw[0])
	}
	return tags
}

// composeFrames runs elems through a FramesToMessageStage, closes its input
// and returns everything it emitted.
func composeFrames(t *testing.T, config FramesToMessageConfig, elems ...core.StreamElement) []core.StreamElement {
	t.Helper()
	input := make(chan core.StreamElement, len(elems))
	output := make(chan core.StreamElement, len(elems)+1)
	for _, e := range elems {
		input <- e
	}
	close(input)
	require.NoError(t, NewFramesToMessageStage(config).Process(context.Background(), input, output))
	var out []core.StreamElement
	for e := range output {
		out = append(out, e)
	}
	return out
}

// A video's frames, over the per-message limit, are thinned by the selection
// strategy, and the message holds the chosen frames in frame order.
func TestFramesToMessageStage_SelectionStrategyPicksFrames(t *testing.T) {
	t.Parallel()
	cases := map[string]struct {
		strategy FrameSelectionStrategy
		want     []byte
	}{
		"uniform": {FrameSelectionUniform, []byte{0, 3, 6, 9}},
		"first":   {FrameSelectionFirst, []byte{0, 1, 2, 3}},
		"last":    {FrameSelectionLast, []byte{6, 7, 8, 9}},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			config := DefaultFramesToMessageConfig()
			config.MaxFramesPerMessage = 4
			config.FrameSelectionStrategy = tc.strategy
			var elems []core.StreamElement
			for i := range 10 {
				elems = append(elems, taggedFrame(i, 10, byte(i)))
			}

			out := composeFrames(t, config, elems...)
			require.Len(t, out, 1)
			require.NotNil(t, out[0].Message)
			assert.Equal(t, tc.want, partTags(t, out[0].Message))
		})
	}
}

// Uniform selection spreads the frames from the first to the last, whatever
// the ratio of frames to the limit.
func TestFramesToMessageStage_UniformSelectionSpansTheVideo(t *testing.T) {
	t.Parallel()
	config := DefaultFramesToMessageConfig()
	config.MaxFramesPerMessage = 3
	var elems []core.StreamElement
	for i := range 7 {
		elems = append(elems, taggedFrame(i, 7, byte(i)))
	}

	out := composeFrames(t, config, elems...)
	require.Len(t, out, 1)
	assert.Equal(t, []byte{0, 3, 6}, partTags(t, out[0].Message))
}

// With a limit of one frame, uniform selection keeps the first frame, the
// frame it always starts from, as it does for any larger limit.
func TestFramesToMessageStage_UniformSelectionOfOneFrame(t *testing.T) {
	t.Parallel()
	config := DefaultFramesToMessageConfig()
	config.MaxFramesPerMessage = 1
	config.FrameSelectionStrategy = FrameSelectionUniform

	out := composeFrames(t, config, taggedFrame(0, 3, 'a'), taggedFrame(1, 3, 'b'), taggedFrame(2, 3, 'c'))
	require.Len(t, out, 1)
	require.NotNil(t, out[0].Message)
	assert.Equal(t, []byte{'a'}, partTags(t, out[0].Message))
}

// The message for a video is emitted as soon as its last frame arrives, not
// when the input closes.
func TestFramesToMessageStage_EmitsWhenTheLastFrameArrives(t *testing.T) {
	t.Parallel()
	input := make(chan core.StreamElement, 2)
	output := make(chan core.StreamElement, 2)
	done := make(chan error, 1)
	go func() {
		done <- NewFramesToMessageStage(DefaultFramesToMessageConfig()).Process(context.Background(), input, output)
	}()

	input <- taggedFrame(0, 2, 'a')
	input <- taggedFrame(1, 2, 'b')
	select {
	case got := <-output:
		require.NotNil(t, got.Message)
		assert.Equal(t, []byte{'a', 'b'}, partTags(t, got.Message))
	case <-time.After(5 * time.Second):
		t.Fatal("no message while the input was still open")
	}
	close(input)
	require.NoError(t, <-done)
	_, open := <-output
	assert.False(t, open, "nothing more is emitted")
}

// When the input closes before a video's frames are all in, the frames that
// did arrive are composed into a message.
func TestFramesToMessageStage_IncompleteVideoIsFlushedOnClose(t *testing.T) {
	t.Parallel()
	out := composeFrames(t, DefaultFramesToMessageConfig(), taggedFrame(0, 3, 'a'), taggedFrame(1, 3, 'b'))
	require.Len(t, out, 1)
	require.NotNil(t, out[0].Message)
	assert.Equal(t, core.RoleUser, out[0].Message.Role)
	assert.Equal(t, []byte{'a', 'b'}, partTags(t, out[0].Message))
}

// A frame's image part carries its dimensions only when they are known, and
// its data only when there is some.
func TestFramesToMessageStage_ImagePartCarriesKnownFieldsOnly(t *testing.T) {
	t.Parallel()
	sized := taggedFrame(0, 2, 'a')
	sized.Image.Width, sized.Image.Height = 64, 48
	empty := taggedFrame(1, 2, 0)
	empty.Image.Data = nil

	out := composeFrames(t, DefaultFramesToMessageConfig(), sized, empty)
	require.Len(t, out, 1)
	require.Len(t, out[0].Message.Parts, 2)

	w, h := 64, 48
	data := base64.StdEncoding.EncodeToString([]byte{'a'})
	assert.Equal(t, &types.MediaContent{MIMEType: "image/jpeg", Data: &data, Width: &w, Height: &h},
		out[0].Message.Parts[0].Media)
	assert.Equal(t, &types.MediaContent{MIMEType: "image/jpeg"}, out[0].Message.Parts[1].Media)
}

// The FFmpeg arguments for a configuration, exactly: no frame cap when
// MaxFrames is 0, and JPEG quality 1-100 mapped onto FFmpeg's 31-2 scale.
func TestVideoToFramesStage_BuildFFmpegArgsExactly(t *testing.T) {
	t.Parallel()
	cases := map[int]string{1: "31", 50: "17", 85: "7", 100: "2"}
	for quality, qscale := range cases {
		config := DefaultVideoToFramesConfig()
		config.Mode = FrameExtractionInterval
		config.Interval = 2 * time.Second
		config.MaxFrames = 0
		config.OutputQuality = quality

		got := NewVideoToFramesStage(config).buildFFmpegArgs("/in.mp4", "/out_%04d.jpg")
		assert.Equal(t, []string{"-y", "-i", "/in.mp4", "-vf", "fps=0.5000", "-q:v", qscale, "/out_%04d.jpg"}, got,
			"quality %d", quality)
	}
}

// A canceled stage whose output nobody reads returns context.Canceled for a
// video it could not extract, and for an element it would pass through.
func TestVideoToFramesStage_CanceledWhileOutputBlocked(t *testing.T) {
	t.Parallel()
	cases := map[string]core.StreamElement{
		"failed video": core.NewVideoElement(&core.VideoData{MIMEType: "video/mp4", Data: []byte("not a video")}),
		"passthrough":  core.NewTextElement("hello"),
	}
	for name, elem := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			config := DefaultVideoToFramesConfig()
			config.FFmpegPath = "/nonexistent/ffmpeg"
			ctx, cancel := context.WithCancel(context.Background())
			cancel()
			input := make(chan core.StreamElement, 1)
			input <- elem
			close(input)
			output := make(chan core.StreamElement) // never read

			err := NewVideoToFramesStage(config).Process(ctx, input, output)
			assert.ErrorIs(t, err, context.Canceled)
		})
	}
}
