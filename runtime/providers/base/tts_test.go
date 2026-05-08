package base_test

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/AltairaLabs/PromptKit/runtime/audio"
	"github.com/AltairaLabs/PromptKit/runtime/providers/base"
	"github.com/AltairaLabs/PromptKit/runtime/types"
)

// fakeTTSStream is a test implementation of base.TTSStream.
type fakeTTSStream struct {
	chunks   chan audio.Chunk
	cost     *types.CostInfo
	closed   bool
	closeErr error
}

func newFakeTTSStream(chunks []audio.Chunk, cost *types.CostInfo) *fakeTTSStream {
	ch := make(chan audio.Chunk, len(chunks))
	for _, c := range chunks {
		ch <- c
	}
	close(ch)
	return &fakeTTSStream{chunks: ch, cost: cost}
}

func (f *fakeTTSStream) Chunks() <-chan audio.Chunk { return f.chunks }
func (f *fakeTTSStream) Cost() *types.CostInfo      { return f.cost }
func (f *fakeTTSStream) Close() error {
	f.closed = true
	return f.closeErr
}

func TestReadAllAudio_HappyPath(t *testing.T) {
	chunks := []audio.Chunk{
		{Data: []byte("hello"), Index: 0},
		{Data: []byte(" world"), Index: 1, Final: true},
	}
	wantCost := &types.CostInfo{TotalCost: 0.001}
	stream := newFakeTTSStream(chunks, wantCost)

	data, cost, err := base.ReadAllAudio(stream)
	require.NoError(t, err)
	assert.Equal(t, []byte("hello world"), data)
	assert.Equal(t, wantCost, cost)
	assert.True(t, stream.closed, "Close should have been called")
}

func TestReadAllAudio_ErrorChunk(t *testing.T) {
	syntheticErr := errors.New("synthesis failed")
	chunks := []audio.Chunk{
		{Data: []byte("ok"), Index: 0},
		{Error: syntheticErr, Index: 1},
	}
	stream := newFakeTTSStream(chunks, nil)

	_, _, err := base.ReadAllAudio(stream)
	assert.ErrorIs(t, err, syntheticErr)
	assert.True(t, stream.closed, "Close should have been called even on error")
}

func TestReadAllAudio_EmptyStream(t *testing.T) {
	stream := newFakeTTSStream(nil, nil)

	data, cost, err := base.ReadAllAudio(stream)
	require.NoError(t, err)
	assert.Nil(t, data)
	assert.Nil(t, cost)
}

func TestTTSRequest_Fields(t *testing.T) {
	req := base.TTSRequest{
		Text:       "hello",
		Voice:      "alloy",
		Speed:      1.0,
		Format:     "pcm",
		SampleRate: 24000,
		Hints:      map[string]string{"k": "v"},
	}
	assert.Equal(t, "hello", req.Text)
	assert.Equal(t, "alloy", req.Voice)
	assert.Equal(t, float32(1.0), req.Speed)
	assert.Equal(t, "pcm", req.Format)
	assert.Equal(t, 24000, req.SampleRate)
	assert.Equal(t, "v", req.Hints["k"])
}
