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
	// KindVideo is reserved for future use (YAGNI — not implemented).
	KindVideo
)

// Format describes the encoding of the media payload.
// Audio fields are present now; video fields (Width, Height, Codec) will be added later.
type Format struct {
	// SampleRate is the number of audio samples per second (e.g. 16000, 24000, 48000).
	SampleRate int
	// Channels is 1 for mono, 2 for stereo.
	Channels int
}

// MediaFrame is a single unit of captured or synthesized media.
// PTS (presentation timestamp) is measured from the session clock and is the
// load-bearing field for AEC delay estimation and A/V sync.
type MediaFrame struct {
	// Kind identifies the media type.
	Kind MediaKind
	// Data is the raw payload — PCM16 little-endian for audio.
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
	// Note: hardware sinks fix the playback sample rate at session construction
	// (WithPlaybackRate, default 24 kHz); MediaFrame.Format is informational and
	// is NOT resampled. Callers must write frames at the session's playback rate.
	// (Phase 3 adds resample-at-sink.)
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
