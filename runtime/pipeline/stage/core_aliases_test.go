package stage

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/AltairaLabs/PromptKit/runtime/v2/types"
)

// The facade's wrappers of internal/core functions that stage's own code does
// not call: each must hand back what core gives, so a stage.X caller sees no
// difference from before the move (#2876).

func TestFacade_CapabilityConstructors(t *testing.T) {
	assert.Equal(t, Capabilities{ContentTypes: []ContentType{ContentTypeText}}, TextCapabilities())
	assert.Equal(t, Capabilities{ContentTypes: []ContentType{ContentTypeMessage}}, MessageCapabilities())
	assert.Equal(t, "pass: no format capabilities declared (accepts any)",
		DescribeCapabilities(NewPassthroughStage("pass")))
}

func TestFacade_PooledElements(t *testing.T) {
	elem := GetElement()
	assert.True(t, elem.IsEmpty(), "a pooled element comes back reset")
	PutElement(elem)

	text := GetTextElement("hi")
	assert.Equal(t, "hi", *text.Text)
	assert.Equal(t, PriorityNormal, text.Priority)

	msg := &types.Message{Role: "user", Content: "hello"}
	assert.Same(t, msg, GetMessageElement(msg).Message)

	audio := &AudioData{SampleRate: 16000}
	gotAudio := GetAudioElement(audio)
	assert.Same(t, audio, gotAudio.Audio)
	assert.Equal(t, PriorityHigh, gotAudio.Priority)

	video := &VideoData{Width: 640}
	assert.Same(t, video, GetVideoElement(video).Video)

	image := &ImageData{Width: 32}
	assert.Same(t, image, GetImageElement(image).Image)

	boom := errors.New("boom")
	gotErr := GetErrorElement(boom)
	assert.Same(t, boom, gotErr.Error)
	assert.Equal(t, PriorityCritical, gotErr.Priority)

	eos := GetEndOfStreamElement()
	assert.True(t, eos.EndOfStream)
	assert.Equal(t, PriorityCritical, eos.Priority)
}

func TestFacade_NewFilterStage(t *testing.T) {
	stage := NewFilterStage("keep-text", func(e StreamElement) bool { return e.Text != nil })
	assert.Equal(t, "keep-text", stage.Name())
	assert.Equal(t, StageTypeTransform, stage.Type())
}
