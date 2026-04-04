package stage_test

import (
	"testing"

	"github.com/AltairaLabs/PromptKit/runtime/pipeline/stage"
	"github.com/AltairaLabs/PromptKit/runtime/providers"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestStreamMediaToElement_Audio(t *testing.T) {
	media := &providers.StreamMediaData{
		Data:       []byte{0x01, 0x02, 0x03},
		MIMEType:   "audio/pcm",
		SampleRate: 24000,
		Channels:   1,
	}

	elem := stage.StreamMediaToElement(media)

	require.NotNil(t, elem.Audio)
	assert.Equal(t, media.Data, elem.Audio.Samples)
	assert.Equal(t, 24000, elem.Audio.SampleRate)
	assert.Equal(t, 1, elem.Audio.Channels)
	assert.Equal(t, stage.AudioFormatPCM16, elem.Audio.Format)
	assert.Nil(t, elem.Video)
	assert.Nil(t, elem.Image)
}

func TestStreamMediaToElement_AudioDefaults(t *testing.T) {
	media := &providers.StreamMediaData{
		Data:     []byte{0x01, 0x02},
		MIMEType: "audio/wav",
		// SampleRate and Channels are zero — should default
	}

	elem := stage.StreamMediaToElement(media)

	require.NotNil(t, elem.Audio)
	assert.Equal(t, 16000, elem.Audio.SampleRate)
	assert.Equal(t, 1, elem.Audio.Channels)
}

func TestStreamMediaToElement_Video(t *testing.T) {
	media := &providers.StreamMediaData{
		Data:       []byte{0xAB, 0xCD},
		MIMEType:   "video/h264",
		Width:      1920,
		Height:     1080,
		FrameRate:  30.0,
		IsKeyFrame: true,
		FrameNum:   42,
	}

	elem := stage.StreamMediaToElement(media)

	require.NotNil(t, elem.Video)
	assert.Equal(t, media.Data, elem.Video.Data)
	assert.Equal(t, "video/h264", elem.Video.MIMEType)
	assert.Equal(t, 1920, elem.Video.Width)
	assert.Equal(t, 1080, elem.Video.Height)
	assert.Equal(t, 30.0, elem.Video.FrameRate)
	assert.True(t, elem.Video.IsKeyFrame)
	assert.Equal(t, int64(42), elem.Video.FrameNum)
	assert.Equal(t, stage.PriorityHigh, elem.Priority)
	assert.Nil(t, elem.Audio)
	assert.Nil(t, elem.Image)
}

func TestStreamMediaToElement_Image(t *testing.T) {
	media := &providers.StreamMediaData{
		Data:     []byte{0xFF, 0xD8},
		MIMEType: "image/jpeg",
		Width:    800,
		Height:   600,
		FrameNum: 7,
	}

	elem := stage.StreamMediaToElement(media)

	require.NotNil(t, elem.Image)
	assert.Equal(t, media.Data, elem.Image.Data)
	assert.Equal(t, "image/jpeg", elem.Image.MIMEType)
	assert.Equal(t, 800, elem.Image.Width)
	assert.Equal(t, 600, elem.Image.Height)
	assert.Equal(t, int64(7), elem.Image.FrameNum)
	assert.Nil(t, elem.Audio)
	assert.Nil(t, elem.Video)
}
