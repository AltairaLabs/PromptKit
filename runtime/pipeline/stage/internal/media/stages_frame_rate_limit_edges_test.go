package media

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/AltairaLabs/PromptKit/runtime/v2/pipeline/stage/internal/core"
	"github.com/AltairaLabs/PromptKit/runtime/v2/types"
)

// limitFrames runs elems through a 1 FPS FrameRateLimitStage whose clock
// advances by step per rate-limited frame, and returns the tag of each element
// it emitted: the first byte of its image data, or 'x' for an element with no
// image data.
func limitFrames(t *testing.T, step time.Duration, elems ...core.StreamElement) []byte {
	t.Helper()
	stage := NewFrameRateLimitStage(DefaultFrameRateLimitConfig())
	stage.clock = &steppingFrameClock{at: time.Unix(0, 0), step: step}

	input := make(chan core.StreamElement, len(elems))
	output := make(chan core.StreamElement, len(elems))
	for _, e := range elems {
		input <- e
	}
	close(input)
	require.NoError(t, stage.Process(context.Background(), input, output))

	var got []byte
	for elem := range output {
		if elem.Image != nil && len(elem.Image.Data) > 0 {
			got = append(got, elem.Image.Data[0])
		} else {
			got = append(got, 'x')
		}
	}
	return got
}

func imageFrame(tag byte) core.StreamElement {
	return core.StreamElement{Image: &core.ImageData{Data: []byte{tag}, MIMEType: "image/jpeg"}}
}

// A frame that arrives exactly one interval after the last one emitted is
// emitted: the interval is a minimum gap, not a gap to exceed.
func TestFrameRateLimitStage_FrameExactlyOneIntervalLaterIsEmitted(t *testing.T) {
	t.Parallel()
	got := limitFrames(t, time.Second, imageFrame(1), imageFrame(2), imageFrame(3))
	assert.Equal(t, []byte{1, 2, 3}, got)
}

// Image frames carrying an error, text, a message, a tool call or a part are
// passed through, never rate limited, even when they arrive at the same
// instant as the frame before them. A plain image frame at that instant is
// dropped.
func TestFrameRateLimitStage_ControlAndNonMediaFramesBypassTheLimit(t *testing.T) {
	t.Parallel()
	text := "caption"
	cases := map[string]func(*core.StreamElement){
		"error":     func(e *core.StreamElement) { e.Error = errors.New("upstream failed") },
		"text":      func(e *core.StreamElement) { e.Text = &text },
		"message":   func(e *core.StreamElement) { e.Message = &types.Message{Role: "user"} },
		"tool call": func(e *core.StreamElement) { e.ToolCall = &types.MessageToolCall{ID: "call-1"} },
		"part":      func(e *core.StreamElement) { e.Part = &types.ContentPart{Type: types.ContentTypeText} },
	}
	for name, mark := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			second := imageFrame(2)
			mark(&second)
			got := limitFrames(t, 0, imageFrame(1), second, imageFrame(3))
			assert.Equal(t, []byte{1, 2}, got, "the marked frame passes, the plain one after it is dropped")
		})
	}
}

// A video or image element with no data is not a frame: it passes and does
// not use up the interval, so the next real frame is still dropped only by
// the first one.
func TestFrameRateLimitStage_EmptyMediaIsNotRateLimited(t *testing.T) {
	t.Parallel()
	emptyVideo := core.StreamElement{Video: &core.VideoData{MIMEType: "video/mp4"}}
	emptyImage := core.StreamElement{Image: &core.ImageData{MIMEType: "image/jpeg"}}
	got := limitFrames(t, 0, imageFrame(1), emptyVideo, emptyImage, imageFrame(2))
	assert.Equal(t, []byte{1, 'x', 'x'}, got)
}
