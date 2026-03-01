package streaming

import (
	"context"
	"time"

	"github.com/AltairaLabs/PromptKit/runtime/logger"
	"github.com/AltairaLabs/PromptKit/runtime/pipeline/stage"
)

// Default video streaming configuration constants.
const (
	// DefaultChunkDurationMs is the default video chunk duration in milliseconds.
	// 1000ms (1 second) chunks provide good balance between latency and efficiency.
	DefaultChunkDurationMs = 1000
)

// VideoStreamer provides utilities for streaming video chunks through a pipeline.
// Use this for encoded video segments (H.264, VP8, etc.) rather than individual frames.
// For individual image frames, use ImageStreamer instead.
type VideoStreamer struct {
	// ChunkDurationMs is the target duration of each video chunk in milliseconds.
	// Default: 1000 (1 second).
	ChunkDurationMs int
}

// NewVideoStreamer creates a new video streamer with the specified chunk duration.
// Use chunkDurationMs of 0 or less for default (1000ms).
func NewVideoStreamer(chunkDurationMs int) *VideoStreamer {
	if chunkDurationMs <= 0 {
		chunkDurationMs = DefaultChunkDurationMs
	}
	return &VideoStreamer{
		ChunkDurationMs: chunkDurationMs,
	}
}

// SendChunk sends a single video chunk through the pipeline.
//
// Parameters:
//   - data: Encoded video data (H.264, VP8, etc.)
//   - mimeType: MIME type of the video (e.g., "video/h264", "video/webm")
//   - chunkIndex: Sequence number for ordering
//   - isKeyFrame: True if this chunk contains a keyframe (important for decoding)
//   - timestamp: When the chunk was captured/created
//   - output: Pipeline input channel
func (s *VideoStreamer) SendChunk(
	ctx context.Context,
	data []byte,
	mimeType string,
	chunkIndex int,
	isKeyFrame bool,
	timestamp time.Time,
	output chan<- stage.StreamElement,
) error {
	elem := stage.StreamElement{
		Video: &stage.VideoData{
			Data:       data,
			MIMEType:   mimeType,
			IsKeyFrame: isKeyFrame,
			FrameNum:   int64(chunkIndex),
			Timestamp:  timestamp,
		},
		Priority: stage.PriorityHigh,
		Metadata: map[string]any{
			"passthrough":  true,
			"is_key_frame": isKeyFrame,
		},
	}

	select {
	case output <- elem:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// SendChunkWithDimensions sends a video chunk with explicit dimensions and frame rate.
// Use this when video metadata is known to avoid parsing overhead downstream.
func (s *VideoStreamer) SendChunkWithDimensions(
	ctx context.Context,
	data []byte,
	mimeType string,
	width, height int,
	frameRate float64,
	chunkIndex int,
	isKeyFrame bool,
	timestamp time.Time,
	duration time.Duration,
	output chan<- stage.StreamElement,
) error {
	elem := stage.StreamElement{
		Video: &stage.VideoData{
			Data:       data,
			MIMEType:   mimeType,
			Width:      width,
			Height:     height,
			FrameRate:  frameRate,
			Duration:   duration,
			IsKeyFrame: isKeyFrame,
			FrameNum:   int64(chunkIndex),
			Timestamp:  timestamp,
		},
		Priority: stage.PriorityHigh,
		Metadata: map[string]any{
			"passthrough":  true,
			"is_key_frame": isKeyFrame,
		},
	}

	select {
	case output <- elem:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// VideoChunk represents a video chunk with metadata for batch streaming.
type VideoChunk struct {
	Data       []byte
	IsKeyFrame bool
	Timestamp  time.Time
	Duration   time.Duration
}

// StreamChunksBurst sends all video chunks as fast as possible without pacing.
// Use this for pre-recorded video where real-time pacing isn't needed.
func (s *VideoStreamer) StreamChunksBurst(
	ctx context.Context,
	chunks []VideoChunk,
	mimeType string,
	output chan<- stage.StreamElement,
) error {
	totalChunks := len(chunks)
	if totalChunks == 0 {
		return nil
	}

	logger.Debug("Streaming video chunks",
		"mode", "burst",
		"total_chunks", totalChunks,
		"mime_type", mimeType,
	)

	streamStart := time.Now()
	for i, chunk := range chunks {
		if err := s.SendChunk(ctx, chunk.Data, mimeType, i, chunk.IsKeyFrame, chunk.Timestamp, output); err != nil {
			return err
		}

		s.logProgress(i, totalChunks, streamStart, len(chunk.Data), chunk.IsKeyFrame)
	}

	return nil
}

// StreamChunksRealtime sends video chunks paced according to their duration.
// Use this for simulating real-time playback of pre-recorded video.
func (s *VideoStreamer) StreamChunksRealtime(
	ctx context.Context,
	chunks []VideoChunk,
	mimeType string,
	output chan<- stage.StreamElement,
) error {
	totalChunks := len(chunks)
	if totalChunks == 0 {
		return nil
	}

	logger.Debug("Streaming video chunks",
		"mode", "realtime",
		"total_chunks", totalChunks,
		"mime_type", mimeType,
		"target_chunk_duration_ms", s.getChunkDurationMs(),
	)

	streamStart := time.Now()
	for i, chunk := range chunks {
		if err := s.SendChunk(ctx, chunk.Data, mimeType, i, chunk.IsKeyFrame, chunk.Timestamp, output); err != nil {
			return err
		}

		s.logProgress(i, totalChunks, streamStart, len(chunk.Data), chunk.IsKeyFrame)

		// Pace chunks according to their duration (or default if not specified)
		if i < totalChunks-1 {
			delay := chunk.Duration
			if delay <= 0 {
				delay = time.Duration(s.getChunkDurationMs()) * time.Millisecond
			}
			select {
			case <-time.After(delay):
			case <-ctx.Done():
				return ctx.Err()
			}
		}
	}

	return nil
}

// SendVideoEndOfStream signals that video input is complete for the current turn.
// This triggers the provider to generate a response.
func SendVideoEndOfStream(
	ctx context.Context,
	output chan<- stage.StreamElement,
) error {
	logger.Debug("Sending video EndOfStream signal to trigger response")
	elem := stage.StreamElement{
		EndOfStream: true,
		Metadata: map[string]any{
			"media_type": "video",
		},
	}
	select {
	case output <- elem:
		logger.Debug("Video EndOfStream signal sent")
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// getChunkDurationMs returns the configured chunk duration or default.
func (s *VideoStreamer) getChunkDurationMs() int {
	if s.ChunkDurationMs <= 0 {
		return DefaultChunkDurationMs
	}
	return s.ChunkDurationMs
}

// logProgress logs progress for first, middle, and last chunks.
func (s *VideoStreamer) logProgress(chunkIdx, totalChunks int, streamStart time.Time, chunkBytes int, isKeyFrame bool) {
	if chunkIdx == 0 || chunkIdx == totalChunks/2 || chunkIdx == totalChunks-1 {
		logger.Debug("Video chunk sent",
			"chunk_idx", chunkIdx,
			"total_chunks", totalChunks,
			"elapsed_ms", time.Since(streamStart).Milliseconds(),
			"chunk_bytes", chunkBytes,
			"is_key_frame", isKeyFrame,
		)
	}
}
