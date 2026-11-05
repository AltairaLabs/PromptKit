package audio

import (
	"context"
	"time"
)

// MediaKind identifies the type of media carried in a MediaFrame.
type MediaKind int

const (
	// KindAudio is a PCM16 little-endian audio frame.
	KindAudio MediaKind = iota
	// KindVideo is reserved. No Source or Sink in this package produces or consumes it.
	KindVideo
)

// Format describes the encoding of the media payload.
// It carries the audio fields only.
type Format struct {
	// SampleRate is the number of audio samples per second (e.g. 16000, 24000, 48000).
	SampleRate int
	// Channels is 1 for mono, 2 for stereo.
	Channels int
}

// MediaFrame is a single unit of captured or synthesized media.
// PTS (presentation timestamp) is measured from the session clock.
type MediaFrame struct {
	// Kind identifies the media type.
	Kind MediaKind
	// Data is the raw payload: PCM16 little-endian for audio.
	Data []byte
	// PTS is the presentation timestamp from the session clock.
	PTS time.Duration
	// Format describes the encoding of Data.
	Format Format
}

// Source is a read-only media stream. Implementations include hardware capture
// devices (microphone), file readers, and the in-memory MemSource test double.
type Source interface {
	// Frames returns a channel that delivers captured MediaFrames.
	// The channel is closed when the source ends or Close is called.
	Frames() <-chan MediaFrame
	// Kind returns the MediaKind produced by this source.
	Kind() MediaKind
	// Close stops the source and releases any underlying resources.
	Close() error
}

// Sink is a write-only media stream. Implementations include hardware playback
// devices (speaker), file writers, and the in-memory MemSink test double.
type Sink interface {
	// Write enqueues a MediaFrame for playback/storage.
	// Note: a hardware sink fixes its playback sample rate when it is
	// constructed. MediaFrame.Format is informational and is not resampled, so
	// callers must write frames at the sink's playback rate.
	Write(MediaFrame)
	// Flush drops all queued output immediately (e.g. on barge-in).
	Flush()
	// Kind returns the MediaKind consumed by this sink.
	Kind() MediaKind
	// Close stops the sink and releases any underlying resources.
	Close() error
}

// Session groups the Sources and Sinks that belong to one audio session
// (e.g. a single call leg: one microphone source + one speaker sink).
type Session interface {
	// Start begins media flow. It runs until ctx is canceled or Close is called.
	Start(ctx context.Context) error
	// Sources returns all Sources in this session.
	Sources() []Source
	// Sinks returns all Sinks in this session.
	Sinks() []Sink
	// Close stops all Sources and Sinks and releases session resources.
	Close() error
}
