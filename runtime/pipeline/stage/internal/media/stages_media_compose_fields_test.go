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

// A composed media part carries only the fields its source knew: no data,
// dimensions, format, frame rate or duration when those are empty or zero.
func TestMediaComposeStage_ComposedPartsCarryKnownFieldsOnly(t *testing.T) {
	t.Parallel()
	assert.Equal(t, &types.MediaContent{MIMEType: "image/png"},
		imageDataToMediaContent(&core.ImageData{MIMEType: "image/png"}))
	assert.Equal(t, &types.MediaContent{MIMEType: "video/mp4"},
		videoDataToMediaContent(&core.VideoData{MIMEType: "video/mp4"}))
}

// A composed media part carries every field its source knew, exactly.
func TestMediaComposeStage_ComposedPartsCarryEveryKnownField(t *testing.T) {
	t.Parallel()
	data := base64.StdEncoding.EncodeToString([]byte{1, 2, 3})
	w, h := 1, 1
	format := "png"
	assert.Equal(t,
		&types.MediaContent{MIMEType: "image/png", Data: &data, Width: &w, Height: &h, Format: &format},
		imageDataToMediaContent(&core.ImageData{MIMEType: "image/png", Data: []byte{1, 2, 3},
			Width: 1, Height: 1, Format: "png"}))

	vw, vh, fps, dur := 1, 1, 1, 1
	vformat := "h264"
	assert.Equal(t,
		&types.MediaContent{MIMEType: "video/mp4", Data: &data, Width: &vw, Height: &vh, Format: &vformat,
			FPS: &fps, Duration: &dur},
		videoDataToMediaContent(&core.VideoData{MIMEType: "video/mp4", Data: []byte{1, 2, 3},
			Width: 1, Height: 1, Format: "h264", FrameRate: 1, Duration: time.Second}))
}

// A message still waiting for parts is not composed early by the timeout
// check: before its CompletionTimeout passes, the checks leave it pending, and
// it is composed whole when its last part arrives.
func TestMediaComposeStage_PartialMessageWaitsForItsTimeout(t *testing.T) {
	t.Parallel()
	part := func(idx int, tag byte) core.StreamElement {
		elem := core.NewImageElement(&core.ImageData{Data: []byte{tag}, MIMEType: "image/jpeg"})
		elem.Meta.MediaExtract = &core.MediaExtractInfo{MessageID: "m1", PartIndex: idx, TotalParts: 2,
			MediaType: types.ContentTypeImage}
		return elem
	}
	stage := NewMediaComposeStage(MediaComposeConfig{CompletionTimeout: time.Hour})
	stage.timeoutCheckInterval = time.Millisecond
	input := make(chan core.StreamElement, 2)
	output := make(chan core.StreamElement, 2)
	done := make(chan error, 1)
	go func() { done <- stage.Process(context.Background(), input, output) }()

	input <- part(0, 'a')
	select {
	case got := <-output:
		t.Fatalf("partial message emitted before its timeout: %+v", got)
	case <-time.After(50 * time.Millisecond): // ~50 timeout checks
	}
	input <- part(1, 'b')
	close(input)
	require.NoError(t, <-done)

	var out []core.StreamElement
	for e := range output {
		out = append(out, e)
	}
	require.Len(t, out, 1)
	assert.Equal(t, []byte{'a', 'b'}, partTags(t, out[0].Message))
}

// When the input closes before a message's parts are all in, the parts that
// did arrive are composed, in part order, into one message.
func TestMediaComposeStage_IncompleteMessageIsFlushedOnClose(t *testing.T) {
	t.Parallel()
	part := func(idx int, tag byte) core.StreamElement {
		elem := core.NewImageElement(&core.ImageData{Data: []byte{tag}, MIMEType: "image/jpeg"})
		elem.Meta.MediaExtract = &core.MediaExtractInfo{MessageID: "m1", PartIndex: idx, TotalParts: 3,
			MediaType: types.ContentTypeImage}
		return elem
	}
	input := make(chan core.StreamElement, 2)
	output := make(chan core.StreamElement, 2)
	input <- part(2, 'c')
	input <- part(0, 'a')
	close(input)
	require.NoError(t, NewMediaComposeStage(DefaultMediaComposeConfig()).Process(context.Background(), input, output))

	var out []core.StreamElement
	for e := range output {
		out = append(out, e)
	}
	require.Len(t, out, 1)
	require.NotNil(t, out[0].Message)
	assert.Equal(t, core.RoleUser, out[0].Message.Role)
	assert.Equal(t, []byte{'a', 'c'}, partTags(t, out[0].Message))
}

// An accumulating stage whose context is canceled stops checking for timed-out
// work but still drains its input: when the input closes it returns nil and
// closes its output, having emitted nothing.
func TestAccumulatingStages_CanceledContextStillDrainsInput(t *testing.T) {
	t.Parallel()
	stages := map[string]interface {
		Process(context.Context, <-chan core.StreamElement, chan<- core.StreamElement) error
	}{
		"media compose":     NewMediaComposeStage(DefaultMediaComposeConfig()),
		"frames to message": NewFramesToMessageStage(DefaultFramesToMessageConfig()),
	}
	for name, stage := range stages {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			ctx, cancel := context.WithCancel(context.Background())
			cancel()
			input := make(chan core.StreamElement)
			output := make(chan core.StreamElement, 1)
			done := make(chan error, 1)
			go func() { done <- stage.Process(ctx, input, output) }()

			// The timeout checker sees the cancellation, not the end of input.
			time.Sleep(20 * time.Millisecond)
			close(input)
			require.NoError(t, <-done)
			_, open := <-output
			assert.False(t, open, "output is closed with nothing emitted")
		})
	}
}
