package streaming

import (
	"context"
	"time"

	"github.com/AltairaLabs/PromptKit/runtime/logger"
	"github.com/AltairaLabs/PromptKit/runtime/pipeline/stage"
)

// Default audio configuration constants
const (
	// DefaultChunkSize is the default audio chunk size in bytes.
	// 640 bytes = 20ms at 16kHz 16-bit mono (16000 * 2 * 0.02)
	DefaultChunkSize = 640

	// DefaultSampleRate is the default audio sample rate in Hz.
	// 16kHz is required by Gemini Live API.
	DefaultSampleRate = 16000

	// DefaultChunkIntervalMs is the default interval between chunks in milliseconds
	// when streaming in real-time mode.
	DefaultChunkIntervalMs = 20
)

// AudioStreamer provides utilities for streaming audio data through a pipeline.
type AudioStreamer struct {
	// ChunkSize is the number of bytes per chunk.
	ChunkSize int

	// ChunkIntervalMs is the interval between chunks in milliseconds
	// when streaming in real-time mode.
	ChunkIntervalMs int
}

// NewAudioStreamer creates a new audio streamer with default settings.
func NewAudioStreamer() *AudioStreamer {
	return &AudioStreamer{
		ChunkSize:       DefaultChunkSize,
		ChunkIntervalMs: DefaultChunkIntervalMs,
	}
}

// StreamBurst sends all audio data as fast as possible without pacing.
// This is preferred for pre-recorded audio to avoid false turn detections
// from natural speech pauses.
//
// The provider receives all audio before detecting any turn boundaries,
// which prevents "user interrupted" signals from arriving mid-utterance.
func (a *AudioStreamer) StreamBurst(
	ctx context.Context,
	audioData []byte,
	sampleRate int,
	inputChan chan<- stage.StreamElement,
) error {
	chunkSize := a.getChunkSize()
	totalChunks := (len(audioData) + chunkSize - 1) / chunkSize

	logger.Debug("Streaming audio in BURST MODE",
		"chunk_size", chunkSize,
		"sample_rate", sampleRate,
		"total_bytes", len(audioData),
		"total_chunks", totalChunks,
	)

	streamStart := time.Now()
	for offset := 0; offset < len(audioData); offset += chunkSize {
		chunk, chunkIdx := a.getChunk(audioData, offset, chunkSize)

		if err := a.SendChunk(ctx, chunk, sampleRate, inputChan); err != nil {
			return err
		}

		a.logProgress(chunkIdx, totalChunks, streamStart, len(chunk))
	}

	return nil
}

// StreamRealtime sends audio data paced to match real-time playback.
// Each chunk is sent with a delay matching its duration.
//
// Note: This mode can cause issues with some providers (like Gemini) that
// detect speech pauses mid-utterance. Use StreamBurst for pre-recorded audio.
func (a *AudioStreamer) StreamRealtime(
	ctx context.Context,
	audioData []byte,
	sampleRate int,
	inputChan chan<- stage.StreamElement,
) error {
	chunkSize := a.getChunkSize()
	totalChunks := (len(audioData) + chunkSize - 1) / chunkSize
	chunkInterval := time.Duration(a.getChunkIntervalMs()) * time.Millisecond

	logger.Debug("Streaming audio in REALTIME MODE",
		"chunk_size", chunkSize,
		"sample_rate", sampleRate,
		"total_bytes", len(audioData),
		"total_chunks", totalChunks,
		"chunk_interval_ms", a.getChunkIntervalMs(),
	)

	streamStart := time.Now()
	for offset := 0; offset < len(audioData); offset += chunkSize {
		chunk, chunkIdx := a.getChunk(audioData, offset, chunkSize)

		if err := a.SendChunk(ctx, chunk, sampleRate, inputChan); err != nil {
			return err
		}

		a.logProgress(chunkIdx, totalChunks, streamStart, len(chunk))

		// Pace chunks to match real-time playback
		if offset+chunkSize < len(audioData) {
			select {
			case <-time.After(chunkInterval):
			case <-ctx.Done():
				return ctx.Err()
			}
		}
	}

	return nil
}

// SendChunk sends a single audio chunk through the pipeline.
func (a *AudioStreamer) SendChunk(
	ctx context.Context,
	chunk []byte,
	sampleRate int,
	inputChan chan<- stage.StreamElement,
) error {
	elem := stage.StreamElement{
		Audio: &stage.AudioData{
			Samples:    chunk,
			SampleRate: sampleRate,
			Channels:   1,
			Format:     stage.AudioFormatPCM16,
		},
		Metadata: map[string]interface{}{
			"passthrough": true,
		},
	}

	select {
	case inputChan <- elem:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// SendEndOfStream signals that audio input is complete for the current turn.
// This triggers the provider to generate a response.
func SendEndOfStream(
	ctx context.Context,
	inputChan chan<- stage.StreamElement,
) error {
	logger.Debug("Sending EndOfStream signal to trigger response")
	endOfTurn := stage.StreamElement{EndOfStream: true}
	select {
	case inputChan <- endOfTurn:
		logger.Debug("EndOfStream signal sent")
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// getChunkSize returns the configured chunk size or default.
func (a *AudioStreamer) getChunkSize() int {
	if a.ChunkSize <= 0 {
		return DefaultChunkSize
	}
	return a.ChunkSize
}

// getChunkIntervalMs returns the configured chunk interval or default.
func (a *AudioStreamer) getChunkIntervalMs() int {
	if a.ChunkIntervalMs <= 0 {
		return DefaultChunkIntervalMs
	}
	return a.ChunkIntervalMs
}

// getChunk extracts a chunk from audio data at the given offset.
func (a *AudioStreamer) getChunk(audioData []byte, offset, chunkSize int) (chunk []byte, chunkIdx int) {
	end := offset + chunkSize
	if end > len(audioData) {
		end = len(audioData)
	}
	return audioData[offset:end], offset / chunkSize
}

// logProgress logs progress for first, middle, and last chunks.
func (a *AudioStreamer) logProgress(chunkIdx, totalChunks int, streamStart time.Time, chunkBytes int) {
	if chunkIdx == 0 || chunkIdx == totalChunks/2 || chunkIdx == totalChunks-1 {
		logger.Debug("Audio chunk sent",
			"chunk_idx", chunkIdx,
			"total_chunks", totalChunks,
			"elapsed_ms", time.Since(streamStart).Milliseconds(),
			"chunk_bytes", chunkBytes,
		)
	}
}
