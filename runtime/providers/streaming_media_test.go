package providers

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestStreamMediaData_AudioFields(t *testing.T) {
	media := &StreamMediaData{
		Data:       []byte{0x01, 0x02, 0x03, 0x04},
		MIMEType:   "audio/pcm",
		SampleRate: 16000,
		Channels:   1,
	}
	assert.Equal(t, "audio/pcm", media.MIMEType)
	assert.Equal(t, 16000, media.SampleRate)
	assert.Equal(t, 1, media.Channels)
	assert.Len(t, media.Data, 4)
	assert.Zero(t, media.Width)
	assert.Zero(t, media.Height)
}

func TestStreamMediaData_VideoFields(t *testing.T) {
	media := &StreamMediaData{
		Data:       []byte{0xFF, 0xD8},
		MIMEType:   "video/h264",
		Width:      1920,
		Height:     1080,
		FrameRate:  30.0,
		IsKeyFrame: true,
		FrameNum:   42,
	}
	assert.Equal(t, "video/h264", media.MIMEType)
	assert.Equal(t, 1920, media.Width)
	assert.Equal(t, 1080, media.Height)
	assert.Equal(t, 30.0, media.FrameRate)
	assert.True(t, media.IsKeyFrame)
	assert.Equal(t, int64(42), media.FrameNum)
	assert.Zero(t, media.SampleRate)
	assert.Zero(t, media.Channels)
}

func TestStreamMediaData_ImageFields(t *testing.T) {
	media := &StreamMediaData{
		Data:     []byte{0x89, 0x50, 0x4E, 0x47},
		MIMEType: "image/png",
		Width:    800,
		Height:   600,
		FrameNum: 7,
	}
	assert.Equal(t, "image/png", media.MIMEType)
	assert.Equal(t, 800, media.Width)
	assert.Equal(t, 600, media.Height)
	assert.Equal(t, int64(7), media.FrameNum)
}

func TestStreamChunk_MediaDataField(t *testing.T) {
	chunk := StreamChunk{
		Delta: "hello",
		MediaData: &StreamMediaData{
			Data:     []byte{0x01},
			MIMEType: "audio/pcm",
		},
	}
	assert.NotNil(t, chunk.MediaData)
	assert.Equal(t, "audio/pcm", chunk.MediaData.MIMEType)
	assert.Nil(t, chunk.MediaDelta)
}
