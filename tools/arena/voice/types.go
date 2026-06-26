// Package voice provides live microphone/speaker audio for the interactive
// console. Hardware I/O is backed by PortAudio (cgo); the driver depends only
// on the AudioIO interface so it stays testable without audio hardware.
package voice

import (
	"context"
)

const (
	// CaptureSampleRate is the mic capture rate (16 kHz mono PCM16), matching
	// the VAD/STT pipeline default.
	CaptureSampleRate = 16000
	// PlaybackSampleRate is the speaker playback rate (24 kHz mono PCM16),
	// matching TTS / realtime-provider output.
	PlaybackSampleRate = 24000
)

// AudioIO is hardware mic capture + speaker playback as PCM16 byte streams.
// Implementations are platform audio backends; the driver depends only on this
// interface so it is testable without audio hardware.
type AudioIO interface {
	// Start opens the mic and speaker devices.
	Start(ctx context.Context) error
	// CaptureChunks yields mic PCM16 frames (CaptureSampleRate, mono).
	CaptureChunks() <-chan []byte
	// Play enqueues a PCM16 frame (PlaybackSampleRate, mono) for the speaker.
	Play(frame []byte)
	// Close stops devices and releases resources.
	Close() error
}
