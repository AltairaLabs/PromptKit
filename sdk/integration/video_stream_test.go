package integration

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/AltairaLabs/PromptKit/sdk"
)

// TestStreamingVideo_PipelineWithFrameRateLimit verifies that WithStreamingVideo
// configures a frame rate limiter in the streaming pipeline without breaking
// the normal streaming flow.
func TestStreamingVideo_PipelineWithFrameRateLimit(t *testing.T) {
	cfg := sdk.DefaultVideoStreamConfig()
	cfg.TargetFPS = 2.0

	conv := openTestConv(t, sdk.WithStreamingVideo(cfg))
	ctx := context.Background()

	ch := conv.Stream(ctx, "Hello with video config")

	var chunks []sdk.StreamChunk
	for chunk := range ch {
		chunks = append(chunks, chunk)
	}

	require.NotEmpty(t, chunks, "expected chunks from stream")

	// Verify no errors
	for i, chunk := range chunks {
		require.NoError(t, chunk.Error, "chunk %d had error", i)
	}

	// Should still get text and done chunks
	hasText := false
	hasDone := false
	for _, chunk := range chunks {
		if chunk.Type == sdk.ChunkText && chunk.Text != "" {
			hasText = true
		}
		if chunk.Type == sdk.ChunkDone {
			hasDone = true
		}
	}
	assert.True(t, hasText, "expected text chunks")
	assert.True(t, hasDone, "expected done chunk")
}

// TestStreamingVideo_SendStillWorks verifies that WithStreamingVideo
// doesn't break unary Send (non-streaming) path.
func TestStreamingVideo_SendStillWorks(t *testing.T) {
	cfg := sdk.DefaultVideoStreamConfig()
	cfg.TargetFPS = 5.0

	conv := openTestConv(t, sdk.WithStreamingVideo(cfg))

	resp, err := conv.Send(context.Background(), "Hello")
	require.NoError(t, err)
	assert.NotEmpty(t, resp.Text())
}
