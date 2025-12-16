//go:build portaudio

// Package audio provides audio capture and playback using PortAudio.
package audio

import (
	"context"
	"encoding/binary"
	"fmt"
	"sync"
	"time"

	"github.com/gordonklaus/portaudio"
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
	mu          sync.Mutex
	stream      *portaudio.Stream
	audioOut    chan []byte
	energyOut   chan float64
	running     bool
	initialized bool

	// Drop detection metrics
	chunksSent    uint64
	chunksDropped uint64
	lastDropLog   time.Time
}

// AudioPlayback handles speaker output
type AudioPlayback struct {
	mu          sync.Mutex
	stream      *portaudio.Stream
	audioIn     chan []byte
	running     bool
	initialized bool
}

// AudioSystem manages both capture and playback
type AudioSystem struct {
	capture  *AudioCapture
	playback *AudioPlayback
	paInit   bool
}

// NewAudioSystem creates a new audio system
func NewAudioSystem() (*AudioSystem, error) {
	if err := portaudio.Initialize(); err != nil {
		return nil, fmt.Errorf("failed to initialize PortAudio: %w", err)
	}

	return &AudioSystem{
		capture: &AudioCapture{
			audioOut:  make(chan []byte, 100),
			energyOut: make(chan float64, 100),
		},
		playback: &AudioPlayback{
			audioIn: make(chan []byte, 500),
		},
		paInit: true,
	}, nil
}

// Close shuts down the audio system
func (as *AudioSystem) Close() error {
	if as.capture != nil {
		as.capture.Stop()
	}
	if as.playback != nil {
		as.playback.Stop()
	}
	if as.paInit {
		portaudio.Terminate()
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
	ac.mu.Unlock()

	fmt.Printf("[AUDIO] Starting capture: %dHz, %d channels, %d frames/buffer\n",
		InputSampleRate, Channels, InputFramesPerBuffer)

	// Open input stream
	in := make([]int16, InputFramesPerBuffer)
	stream, err := portaudio.OpenDefaultStream(Channels, 0, InputSampleRate, InputFramesPerBuffer, in)
	if err != nil {
		fmt.Printf("[AUDIO ERROR] Failed to open input stream: %v\n", err)
		return fmt.Errorf("failed to open input stream: %w", err)
	}

	if err := stream.Start(); err != nil {
		stream.Close()
		fmt.Printf("[AUDIO ERROR] Failed to start input stream: %v\n", err)
		return fmt.Errorf("failed to start input stream: %w", err)
	}

	ac.mu.Lock()
	ac.stream = stream
	ac.running = true
	ac.initialized = true
	ac.mu.Unlock()

	fmt.Println("[AUDIO] Microphone stream opened successfully")

	// Start capture goroutine
	go ac.captureLoop(ctx, in)

	return nil
}

// Stop stops audio capture
func (ac *AudioCapture) Stop() {
	ac.mu.Lock()
	defer ac.mu.Unlock()

	if ac.stream != nil {
		ac.stream.Stop()
		ac.stream.Close()
		ac.stream = nil
	}
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

func (ac *AudioCapture) captureLoop(ctx context.Context, in []int16) {
	fmt.Println("[AUDIO] Capture loop started - listening for microphone input")

	for {
		select {
		case <-ctx.Done():
			ac.logFinalStats()
			ac.Stop()
			return
		default:
		}

		ac.mu.Lock()
		if !ac.running || ac.stream == nil {
			ac.mu.Unlock()
			ac.logFinalStats()
			return
		}
		stream := ac.stream
		ac.mu.Unlock()

		// Read audio frame
		if err := stream.Read(); err != nil {
			time.Sleep(10 * time.Millisecond)
			continue
		}

		// Convert int16 to bytes (PCM16 little-endian)
		audioBytes := int16ToBytes(in)

		// Calculate energy level (0.0 - 1.0)
		energy := calculateEnergy(in)

		// Send audio data with drop detection
		select {
		case ac.audioOut <- audioBytes:
			ac.chunksSent++
			// Log first chunk and periodic progress
			if ac.chunksSent == 1 {
				fmt.Printf("[AUDIO] First audio chunk captured: %d bytes, energy=%.3f\n", len(audioBytes), energy)
			} else if ac.chunksSent%500 == 0 {
				fmt.Printf("[AUDIO] Captured %d chunks (dropped %d, %.1f%% loss)\n",
					ac.chunksSent, ac.chunksDropped,
					float64(ac.chunksDropped)/float64(ac.chunksSent+ac.chunksDropped)*100)
			}
		default:
			ac.chunksDropped++
			// Log drops (throttled to avoid spam)
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
	ap.mu.Unlock()

	// Open output stream
	out := make([]int16, OutputFramesPerBuffer)
	stream, err := portaudio.OpenDefaultStream(0, Channels, float64(OutputSampleRate), OutputFramesPerBuffer, out)
	if err != nil {
		return fmt.Errorf("failed to open output stream: %w", err)
	}

	if err := stream.Start(); err != nil {
		stream.Close()
		return fmt.Errorf("failed to start output stream: %w", err)
	}

	ap.mu.Lock()
	ap.stream = stream
	ap.running = true
	ap.initialized = true
	ap.mu.Unlock()

	// Start playback goroutine
	go ap.playbackLoop(ctx, out)

	return nil
}

// Stop stops audio playback
func (ap *AudioPlayback) Stop() {
	ap.mu.Lock()
	defer ap.mu.Unlock()

	if ap.stream != nil {
		ap.stream.Stop()
		ap.stream.Close()
		ap.stream = nil
	}
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

func (ap *AudioPlayback) playbackLoop(ctx context.Context, out []int16) {
	buffer := make([]byte, 0, OutputFramesPerBuffer*4)

	for {
		select {
		case <-ctx.Done():
			ap.Stop()
			return
		case audioData, ok := <-ap.audioIn:
			if !ok {
				return
			}

			buffer = append(buffer, audioData...)

			// Play when we have enough samples
			for len(buffer) >= len(out)*2 {
				// Convert bytes to int16
				for i := 0; i < len(out); i++ {
					if i*2+1 < len(buffer) {
						out[i] = int16(binary.LittleEndian.Uint16(buffer[i*2:]))
					}
				}

				ap.mu.Lock()
				if ap.stream != nil {
					ap.stream.Write()
				}
				ap.mu.Unlock()

				// Remove played samples
				buffer = buffer[len(out)*2:]
			}
		}
	}
}

// HasVoiceActivity checks if audio has significant energy
func HasVoiceActivity(samples []int16) bool {
	return calculateEnergy(samples) > 0.1
}

// calculateEnergy returns normalized energy level (0.0 - 1.0)
func calculateEnergy(samples []int16) float64 {
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

// int16ToBytes converts int16 audio samples to bytes (little-endian PCM16)
func int16ToBytes(samples []int16) []byte {
	bytes := make([]byte, len(samples)*2)
	for i, s := range samples {
		binary.LittleEndian.PutUint16(bytes[i*2:], uint16(s))
	}
	return bytes
}

// BytesToInt16 converts bytes to int16 audio samples (little-endian PCM16)
func BytesToInt16(data []byte) []int16 {
	samples := make([]int16, len(data)/2)
	for i := range samples {
		samples[i] = int16(binary.LittleEndian.Uint16(data[i*2:]))
	}
	return samples
}
