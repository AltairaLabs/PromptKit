package stream

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// mockStreamer implements Streamer for testing.
type mockStreamer struct {
	chunks []Chunk
	err    error
}

func (m *mockStreamer) StreamRaw(_ context.Context, _ any) (<-chan Chunk, error) {
	if m.err != nil {
		return nil, m.err
	}
	ch := make(chan Chunk, len(m.chunks))
	for _, chunk := range m.chunks {
		ch <- chunk
	}
	close(ch)
	return ch, nil
}

func TestProcess(t *testing.T) {
	t.Run("processes all chunks", func(t *testing.T) {
		streamer := &mockStreamer{
			chunks: []Chunk{
				{Type: ChunkText, Text: "Hello "},
				{Type: ChunkText, Text: "World"},
				{Type: ChunkText, Text: "!", Done: true},
			},
		}
		var collected string
		err := Process(context.Background(), streamer, "test", func(chunk Chunk) error {
			collected += chunk.Text
			return nil
		})
		require.NoError(t, err)
		assert.Equal(t, "Hello World!", collected)
	})

	t.Run("stops on handler error", func(t *testing.T) {
		streamer := &mockStreamer{
			chunks: []Chunk{
				{Type: ChunkText, Text: "Hello "},
				{Type: ChunkText, Text: "World"},
				{Type: ChunkText, Text: "!", Done: true},
			},
		}
		handlerErr := errors.New("handler error")
		callCount := 0
		err := Process(context.Background(), streamer, "test", func(_ Chunk) error {
			callCount++
			if callCount == 2 {
				return handlerErr
			}
			return nil
		})
		assert.Equal(t, handlerErr, err)
		assert.Equal(t, 2, callCount)
	})

	t.Run("returns chunk error", func(t *testing.T) {
		chunkErr := errors.New("chunk error")
		streamer := &mockStreamer{
			chunks: []Chunk{
				{Type: ChunkText, Text: "Hello"},
				{Error: chunkErr},
			},
		}
		err := Process(context.Background(), streamer, "test", func(_ Chunk) error {
			return nil
		})
		assert.Equal(t, chunkErr, err)
	})

	t.Run("returns streamer error", func(t *testing.T) {
		streamErr := errors.New("stream error")
		streamer := &mockStreamer{
			err: streamErr,
		}
		err := Process(context.Background(), streamer, "test", func(_ Chunk) error {
			return nil
		})
		assert.Equal(t, streamErr, err)
	})

	t.Run("stops on done chunk", func(t *testing.T) {
		streamer := &mockStreamer{
			chunks: []Chunk{
				{Type: ChunkText, Text: "Hello", Done: true},
				{Type: ChunkText, Text: "World"}, // Should not be processed
			},
		}
		var collected string
		err := Process(context.Background(), streamer, "test", func(chunk Chunk) error {
			collected += chunk.Text
			return nil
		})
		require.NoError(t, err)
		assert.Equal(t, "Hello", collected)
	})
}

func TestCollectText(t *testing.T) {
	t.Run("collects all text chunks", func(t *testing.T) {
		streamer := &mockStreamer{
			chunks: []Chunk{
				{Type: ChunkText, Text: "Hello "},
				{Type: ChunkText, Text: "World"},
				{Type: ChunkText, Text: "!", Done: true},
			},
		}
		result, err := CollectText(context.Background(), streamer, "test")
		require.NoError(t, err)
		assert.Equal(t, "Hello World!", result)
	})

	t.Run("ignores non-text chunks", func(t *testing.T) {
		streamer := &mockStreamer{
			chunks: []Chunk{
				{Type: ChunkText, Text: "Hello"},
				{Type: ChunkToolCall, Text: "ignored"},
				{Type: ChunkText, Text: " World", Done: true},
			},
		}
		result, err := CollectText(context.Background(), streamer, "test")
		require.NoError(t, err)
		assert.Equal(t, "Hello World", result)
	})

	t.Run("returns error from streamer", func(t *testing.T) {
		streamErr := errors.New("stream error")
		streamer := &mockStreamer{err: streamErr}
		_, err := CollectText(context.Background(), streamer, "test")
		assert.Equal(t, streamErr, err)
	})

	t.Run("returns empty string for no text chunks", func(t *testing.T) {
		streamer := &mockStreamer{
			chunks: []Chunk{
				{Type: ChunkToolCall, Done: true},
			},
		}
		result, err := CollectText(context.Background(), streamer, "test")
		require.NoError(t, err)
		assert.Equal(t, "", result)
	})
}

func TestChunkType(t *testing.T) {
	// Verify chunk type constants
	assert.Equal(t, ChunkType(0), ChunkText)
	assert.Equal(t, ChunkType(1), ChunkToolCall)
	assert.Equal(t, ChunkType(2), ChunkMedia)
}
