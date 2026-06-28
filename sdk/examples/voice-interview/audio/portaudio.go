// Package audio provides microphone capture and speaker playback for the
// voice-interview example. It is a thin adapter over the shared pure-Go
// runtime/audio Session (purego/dlopen PortAudio — no CGO), preserving the
// capture/playback API the interview controller depends on.
package audio

import (
	"context"
	"encoding/binary"
	"fmt"
	"sync"
	"time"

	rtaudio "github.com/AltairaLabs/PromptKit/runtime/audio"
)

const (
	// InputSampleRate is the sample rate for microphone input (16kHz for speech)
	InputSampleRate = 16000
	// OutputSampleRate is the sample rate for speaker output (24kHz for TTS)
	OutputSampleRate = 24000
	// Channels is mono audio
	Channels = 1
	// InputFramesPerBuffer is 100ms of audio at 16kHz
	InputFramesPerBuffer = 1600
	// OutputFramesPerBuffer is 40ms of audio at 24kHz
	OutputFramesPerBuffer = 960
	// EnergyThreshold for voice activity detection
	EnergyThreshold = 500
)

// AudioCapture handles microphone input
type AudioCapture struct {
	sys       *AudioSystem
	mu        sync.Mutex
	audioOut  chan []byte
	energyOut chan float64
	running   bool

	// Drop detection metrics
	chunksSent    uint64
	chunksDropped uint64
	lastDropLog   time.Time
}

// AudioPlayback handles speaker output
type AudioPlayback struct {
	sys     *AudioSystem
	mu      sync.Mutex
	audioIn chan []byte
	running bool
}

// AudioSystem manages both capture and playback over one shared audio session.
type AudioSystem struct {
	session  rtaudio.Session
	capture  *AudioCapture
	playback *AudioPlayback

	startOnce sync.Once
	startErr  error
}

// NewAudioSystem creates a new audio system backed by the shared runtime/audio
// Session (16kHz capture, 24kHz playback).
func NewAudioSystem() (*AudioSystem, error) {
	session, err := rtaudio.NewPortAudioSession(
		rtaudio.WithCaptureRate(InputSampleRate),
		rtaudio.WithPlaybackRate(OutputSampleRate),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to initialize audio session: %w", err)
	}

	as := &AudioSystem{session: session}
	as.capture = &AudioCapture{
		sys:       as,
		audioOut:  make(chan []byte, 100),
		energyOut: make(chan float64, 100),
	}
	as.playback = &AudioPlayback{
		sys:     as,
		audioIn: make(chan []byte, 500),
	}
	return as, nil
}

// startSession starts the underlying session exactly once. Both Capture.Start
// and Playback.Start call it; the shared session drives both streams.
func (as *AudioSystem) startSession(ctx context.Context) error {
	as.startOnce.Do(func() {
		as.startErr = as.session.Start(ctx)
	})
	return as.startErr
}

// Close shuts down the audio system
func (as *AudioSystem) Close() error {
	if as.capture != nil {
		as.capture.Stop()
	}
	if as.playback != nil {
		as.playback.Stop()
	}
	if as.session != nil {
		return as.session.Close()
	}
	return nil
}

// Capture returns the audio capture component
func (as *AudioSystem) Capture() *AudioCapture {
	return as.capture
}

// Playback returns the audio playback component
func (as *AudioSystem) Playback() *AudioPlayback {
	return as.playback
}

// Start begins audio capture
func (ac *AudioCapture) Start(ctx context.Context) error {
	ac.mu.Lock()
	if ac.running {
		ac.mu.Unlock()
		fmt.Println("[AUDIO] Capture already running")
		return nil
	}
	ac.running = true
	ac.mu.Unlock()

	fmt.Printf("[AUDIO] Starting capture: %dHz, %d channels, %d frames/buffer\n",
		InputSampleRate, Channels, InputFramesPerBuffer)

	if err := ac.sys.startSession(ctx); err != nil {
		ac.mu.Lock()
		ac.running = false
		ac.mu.Unlock()
		fmt.Printf("[AUDIO ERROR] Failed to start audio session: %v\n", err)
		return fmt.Errorf("failed to start audio session: %w", err)
	}

	fmt.Println("[AUDIO] Microphone stream opened successfully")

	go ac.captureLoop(ctx)

	return nil
}

// Stop stops audio capture
func (ac *AudioCapture) Stop() {
	ac.mu.Lock()
	defer ac.mu.Unlock()
	ac.running = false
}

// AudioChunks returns the channel for receiving audio data
func (ac *AudioCapture) AudioChunks() <-chan []byte {
	return ac.audioOut
}

// EnergyLevels returns the channel for receiving audio energy levels
func (ac *AudioCapture) EnergyLevels() <-chan float64 {
	return ac.energyOut
}

func (ac *AudioCapture) captureLoop(ctx context.Context) {
	fmt.Println("[AUDIO] Capture loop started - listening for microphone input")

	frames := ac.sys.session.Sources()[0].Frames()
	for {
		select {
		case <-ctx.Done():
			ac.logFinalStats()
			ac.Stop()
			return
		case frame, ok := <-frames:
			if !ok {
				ac.logFinalStats()
				ac.Stop()
				return
			}

			// frame.Data is already PCM16 little-endian bytes.
			audioBytes := frame.Data

			// Calculate energy level (0.0 - 1.0)
			energy := calculateEnergy(BytesToInt16(audioBytes))

			// Send audio data with drop detection
			select {
			case ac.audioOut <- audioBytes:
				ac.chunksSent++
				if ac.chunksSent == 1 {
					fmt.Printf("[AUDIO] First audio chunk captured: %d bytes, energy=%.3f\n", len(audioBytes), energy)
				} else if ac.chunksSent%500 == 0 {
					fmt.Printf("[AUDIO] Captured %d chunks (dropped %d, %.1f%% loss)\n",
						ac.chunksSent, ac.chunksDropped,
						float64(ac.chunksDropped)/float64(ac.chunksSent+ac.chunksDropped)*100)
				}
			default:
				ac.chunksDropped++
				if time.Since(ac.lastDropLog) > time.Second {
					fmt.Printf("[AUDIO WARNING] Dropping audio chunks! Sent=%d, Dropped=%d (channel full)\n",
						ac.chunksSent, ac.chunksDropped)
					ac.lastDropLog = time.Now()
				}
			}

			// Send energy level
			select {
			case ac.energyOut <- energy:
			default:
				// Energy drops are less critical, don't log
			}
		}
	}
}

// logFinalStats logs capture statistics when stopping
func (ac *AudioCapture) logFinalStats() {
	total := ac.chunksSent + ac.chunksDropped
	if total > 0 {
		fmt.Printf("[AUDIO] Capture stopped - Total: %d chunks, Sent: %d, Dropped: %d (%.1f%% loss)\n",
			total, ac.chunksSent, ac.chunksDropped,
			float64(ac.chunksDropped)/float64(total)*100)
	}
}

// Start begins audio playback
func (ap *AudioPlayback) Start(ctx context.Context) error {
	ap.mu.Lock()
	if ap.running {
		ap.mu.Unlock()
		return nil
	}
	ap.running = true
	ap.mu.Unlock()

	if err := ap.sys.startSession(ctx); err != nil {
		ap.mu.Lock()
		ap.running = false
		ap.mu.Unlock()
		return fmt.Errorf("failed to start audio session: %w", err)
	}

	go ap.playbackLoop(ctx)

	return nil
}

// Stop stops audio playback
func (ap *AudioPlayback) Stop() {
	ap.mu.Lock()
	defer ap.mu.Unlock()
	ap.running = false
}

// Write sends audio data for playback
func (ap *AudioPlayback) Write(data []byte) error {
	select {
	case ap.audioIn <- data:
		return nil
	default:
		return fmt.Errorf("playback buffer full")
	}
}

// AudioInput returns the channel for sending audio data to play
func (ap *AudioPlayback) AudioInput() chan<- []byte {
	return ap.audioIn
}

func (ap *AudioPlayback) playbackLoop(ctx context.Context) {
	sink := ap.sys.session.Sinks()[0]
	for {
		select {
		case <-ctx.Done():
			ap.Stop()
			return
		case audioData, ok := <-ap.audioIn:
			if !ok {
				return
			}

			// The sink buffers and paces playback internally; hand it the raw
			// PCM16 little-endian bytes.
			sink.Write(rtaudio.MediaFrame{
				Kind:   rtaudio.KindAudio,
				Data:   audioData,
				Format: rtaudio.Format{SampleRate: OutputSampleRate, Channels: Channels},
			})
		}
	}
}

// HasVoiceActivity checks if audio has significant energy
func HasVoiceActivity(samples []int16) bool {
	return calculateEnergy(samples) > 0.1
}

// calculateEnergy returns normalized energy level (0.0 - 1.0)
func calculateEnergy(samples []int16) float64 {
	if len(samples) == 0 {
		return 0
	}
	var sum int64
	for _, s := range samples {
		if s < 0 {
			sum -= int64(s)
		} else {
			sum += int64(s)
		}
	}
	avg := float64(sum) / float64(len(samples))
	// Normalize to 0-1 range (assuming max 16-bit audio is ~10000 average energy for speech)
	normalized := avg / 10000.0
	if normalized > 1.0 {
		normalized = 1.0
	}
	return normalized
}

// BytesToInt16 converts bytes to int16 audio samples (little-endian PCM16)
func BytesToInt16(data []byte) []int16 {
	samples := make([]int16, len(data)/2)
	for i := range samples {
		samples[i] = int16(binary.LittleEndian.Uint16(data[i*2:]))
	}
	return samples
}
