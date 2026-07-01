//go:build portaudio

// Package main demonstrates interactive voice chat using the SDK.
//
// This example shows:
//   - Using SDK Conversation for streaming responses
//   - Real-time audio input via PortAudio
//   - Energy-based turn detection
//   - Interactive conversation flow
//
// Requirements:
//   - Microphone input (system default)
//   - Audio output/speakers
//   - OpenAI API key
//
// Run with:
//
//	export OPENAI_API_KEY=your-key
//	go run -tags portaudio .
package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"os/signal"
	"sync"
	"syscall"

	rtaudio "github.com/AltairaLabs/PromptKit/runtime/audio"
	"github.com/AltairaLabs/PromptKit/sdk"
	"github.com/AltairaLabs/PromptKit/sdk/examples/audiohelper"
)

const (
	captureRate  = 16000 // 16kHz mic capture for VAD
	playbackRate = 24000 // 24kHz speaker playback (typical TTS output)
	channels     = 1     // Mono
)

func main() {
	fmt.Println("🎤 Voice Chat with SDK Pipeline")
	fmt.Println("=================================")
	fmt.Println()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Open the shared pure-Go audio session (16 kHz mic + 24 kHz speaker).
	session, err := audiohelper.NewSession(
		audiohelper.WithCaptureRate(captureRate),
		audiohelper.WithPlaybackRate(playbackRate),
	)
	if err != nil {
		log.Fatalf("Failed to open audio session: %v", err)
	}
	defer session.Close()
	if err := session.Start(ctx); err != nil {
		log.Fatalf("Failed to start audio session: %v", err)
	}

	// Open conversation using SDK (uses OpenAI by default)
	conv, err := sdk.Open("./voice-assistant.pack.json", "chat")
	if err != nil {
		log.Fatalf("Failed to open pack: %v", err)
	}
	defer conv.Close()

	// Create voice chat handler
	voiceChat := &VoiceChat{
		conv:       conv,
		session:    session,
		audioQueue: make(chan []byte, 100),
		turnQueue:  make(chan string, 10),
	}

	// Set up signal handling for graceful shutdown
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)

	// Start the voice chat
	if err := voiceChat.Start(ctx); err != nil {
		log.Fatalf("Failed to start voice chat: %v", err)
	}

	fmt.Println("Voice chat started! Speak into your microphone.")
	fmt.Println("Press Ctrl+C to exit.")
	fmt.Println()

	// Wait for interrupt
	<-sigChan
	fmt.Println("\n\nShutting down...")
	cancel()
	voiceChat.Stop()
}

// VoiceChat manages the full voice conversation flow through the SDK.
type VoiceChat struct {
	conv          *sdk.Conversation
	session       rtaudio.Session
	audioQueue    chan []byte
	turnQueue     chan string // Queue of completed transcribed turns
	mu            sync.Mutex
	speaking      bool
	audioBuffer   []byte
	silenceFrames int
}

// Start begins the voice chat session.
func (vc *VoiceChat) Start(ctx context.Context) error {
	// Start audio input processing
	go vc.processAudioInput(ctx)

	// Start conversation handler - reads turns and calls SDK
	go vc.handleConversation(ctx)

	// Start audio output playback
	go vc.playAudioOutput(ctx)

	return nil
}

// Stop ends the voice chat session.
func (vc *VoiceChat) Stop() {
	close(vc.turnQueue)
	close(vc.audioQueue)
}

// processAudioInput captures microphone input and detects turns.
func (vc *VoiceChat) processAudioInput(ctx context.Context) {
	fmt.Println("🎙️  Listening... (Speak for 2 seconds, then pause)")

	// The shared audio Session yields ~100 ms PCM16 frames (captureRate/10),
	// so the frame-count thresholds are tuned for that window.
	const silenceThreshold = 15 // ~1.5 seconds of silence at 100ms frames
	const minSpeechBytes = 3200 // ~100ms of 16kHz PCM16 speech before a turn counts

	frames := vc.session.Sources()[0].Frames()
	for {
		select {
		case <-ctx.Done():
			return
		case frame, ok := <-frames:
			if !ok {
				return
			}

			// Don't record while bot is speaking
			vc.mu.Lock()
			isSpeaking := vc.speaking
			vc.mu.Unlock()

			if isSpeaking {
				vc.audioBuffer = nil
				vc.silenceFrames = 0
				continue
			}

			// frame.Data is already PCM16 little-endian bytes.
			audioBytes := frame.Data

			// Simple energy-based voice detection
			hasEnergy := hasAudioEnergy(bytesToInt16(audioBytes))

			if hasEnergy {
				vc.audioBuffer = append(vc.audioBuffer, audioBytes...)
				vc.silenceFrames = 0
				fmt.Print("█")
			} else if len(vc.audioBuffer) > 0 {
				vc.silenceFrames++
				fmt.Print("░")

				if vc.silenceFrames >= silenceThreshold && len(vc.audioBuffer) > minSpeechBytes {
					// Turn complete - send for processing
					fmt.Println(" ✓")
					turnText := fmt.Sprintf("[Audio turn: %d bytes]", len(vc.audioBuffer))

					select {
					case vc.turnQueue <- turnText:
						vc.audioBuffer = nil
						vc.silenceFrames = 0
					case <-ctx.Done():
						return
					}
				}
			}
		}
	}
}

// hasAudioEnergy checks if audio frame has significant energy.
func hasAudioEnergy(samples []int16) bool {
	if len(samples) == 0 {
		return false
	}
	const threshold = 500
	var sum int64
	for _, s := range samples {
		if s < 0 {
			sum -= int64(s)
		} else {
			sum += int64(s)
		}
	}
	avg := sum / int64(len(samples))
	return avg > threshold
}

// handleConversation processes turns and uses SDK to get responses.
func (vc *VoiceChat) handleConversation(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		case turn, ok := <-vc.turnQueue:
			if !ok {
				return
			}

			fmt.Printf("\nYou: %s\n", turn)
			fmt.Println("🤔 Thinking...")

			// Use SDK to get streaming response
			vc.setSpeaking(true)
			fmt.Print("Assistant: ")

			for chunk := range vc.conv.Stream(ctx, turn) {
				if chunk.Error != nil {
					log.Printf("Error: %v", chunk.Error)
					break
				}

				if chunk.Text != "" {
					fmt.Print(chunk.Text)
				}

				if chunk.Type == sdk.ChunkDone {
					fmt.Println()
					break
				}
			}

			vc.setSpeaking(false)
			fmt.Println("🎙️  Listening...")
		}
	}
}

// setSpeaking updates the speaking state.
func (vc *VoiceChat) setSpeaking(speaking bool) {
	vc.mu.Lock()
	defer vc.mu.Unlock()
	vc.speaking = speaking
}

// processStreamingResponses handles streaming text responses from SDK.
func (vc *VoiceChat) processStreamingResponses(ctx context.Context) {
	// This would stream responses from conv.Stream()
	// For now, placeholder for full implementation
}

// processResponses handles streaming responses from the session.
func (vc *VoiceChat) processResponses(ctx context.Context) {
	// Process text streaming responses via SDK
	// For audio responses, would need provider streaming session

	// Example: using SDK Stream() method
	// respChan := vc.conv.Stream(ctx, userMessage)
	// for chunk := range respChan {
	//     if chunk.Error != nil {
	//         log.Printf("Error: %v", chunk.Error)
	//         continue
	//     }
	//     if chunk.Text != "" {
	//         fmt.Print(chunk.Text)
	//     }
	// }
}

// playAudioOutput plays queued audio through speakers.
func (vc *VoiceChat) playAudioOutput(ctx context.Context) {
	sink := vc.session.Sinks()[0]
	for {
		select {
		case <-ctx.Done():
			return
		case chunk, ok := <-vc.audioQueue:
			if !ok {
				return
			}

			sink.Write(rtaudio.MediaFrame{
				Kind:   rtaudio.KindAudio,
				Data:   chunk,
				Format: rtaudio.Format{SampleRate: playbackRate, Channels: channels},
			})
		}
	}
}

// bytesToInt16 converts little-endian PCM16 bytes to int16 audio samples.
func bytesToInt16(data []byte) []int16 {
	samples := make([]int16, len(data)/2)
	for i := range samples {
		samples[i] = int16(data[i*2]) | int16(data[i*2+1])<<8
	}
	return samples
}
