package classify

import (
	"context"
	"time"
)

// AudioClassifier classifies a single audio clip into one or more
// labeled scores. Required interface for every backend that supports
// audio (HF base, Hume, Deepgram, ONNX).
//
// Implementations must be safe for concurrent use; the registry hands
// the same instance to multiple eval handlers.
type AudioClassifier interface {
	ClassifyAudio(ctx context.Context, audio []byte, opts AudioOptions) ([]LabelScore, error)
}

// AudioChunk is a single packet in a streaming audio classification
// request. Backends that stream consume these from a channel until
// it closes.
type AudioChunk struct {
	// PCM is the raw PCM audio bytes for this chunk. Format
	// declared by the parent AudioStreamOptions.
	PCM []byte
	// Final marks the last chunk; backends may flush state.
	Final bool
}

// AudioStreamOptions carries the framing contract for a streaming
// audio classification request.
type AudioStreamOptions struct {
	AudioOptions
	// ChunkInterval is the source's emit cadence; backends use it
	// to size analysis windows and stamp output timestamps.
	ChunkInterval time.Duration
}

// LabelScoreEvent is one window's worth of classification output
// from a streaming backend. Timestamp is relative to the start of
// the input stream.
type LabelScoreEvent struct {
	Timestamp time.Duration
	Window    time.Duration
	Scores    []LabelScore
}

// StreamingAudioClassifier is the optional capability for live
// classification — Hume, Deepgram, and locally-windowed ONNX
// implement it; HF Inference API base does not. Eval handlers that
// only need end-of-turn classification depend on AudioClassifier;
// hooks and guardrails that react mid-call type-assert to this.
type StreamingAudioClassifier interface {
	AudioClassifier
	ClassifyAudioStream(
		ctx context.Context,
		chunks <-chan AudioChunk,
		opts AudioStreamOptions,
	) (<-chan LabelScoreEvent, error)
}
