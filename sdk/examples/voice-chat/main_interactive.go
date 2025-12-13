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
//	go run .
package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"os/signal"
	"sync"
	"syscall"

	"github.com/AltairaLabs/PromptKit/sdk"

	"github.com/gordonklaus/portaudio"
)

const (
	sampleRate   = 16000 // 16kHz for VAD
	channels     = 1     // Mono
	framesPerBuf = 160   // 10ms at 16kHz
)

func main() {
	fmt.Println("🎤 Voice Chat with SDK Pipeline")
	fmt.Println("=================================")
	fmt.Println()

	// Initialize PortAudio
	if err := portaudio.Initialize(); err != nil {
		log.Fatalf("Failed to initialize PortAudio: %v", err)
	}
	defer portaudio.Terminate()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Open conversation using SDK (uses OpenAI by default)
	conv, err := sdk.Open("./voice-assistant.pack.json", "chat")
	if err != nil {
		log.Fatalf("Failed to open pack: %v", err)
	}
	defer conv.Close()

	// Create voice chat handler
	voiceChat := &VoiceChat{
		conv:       conv,
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
	// Open input stream
	in := make([]int16, framesPerBuf)
	stream, err := portaudio.OpenDefaultStream(channels, 0, sampleRate, framesPerBuf, in)
	if err != nil {
		log.Printf("Failed to open input stream: %v", err)
		return
	}
	defer stream.Close()

	if err := stream.Start(); err != nil {
		log.Printf("Failed to start input stream: %v", err)
		return
	}
	defer stream.Stop()

	fmt.Println("🎙️  Listening... (Speak for 2 seconds, then pause)")

	const silenceThreshold = 32 // ~2 seconds of silence at 10ms frames
	const minAudioFrames = 10   // Minimum frames to consider valid speech

	for {
		select {
		case <-ctx.Done():
			return
		default:
			// Read audio frame
			if err := stream.Read(); err != nil {
				log.Printf("Audio read error: %v", err)
				continue
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

			// Convert int16 to bytes
			audioBytes := int16ToBytes(in)

			// Simple energy-based voice detection
			hasEnergy := hasAudioEnergy(in)

			if hasEnergy {
				vc.audioBuffer = append(vc.audioBuffer, audioBytes...)
				vc.silenceFrames = 0
				fmt.Print("█")
			} else if len(vc.audioBuffer) > 0 {
				vc.silenceFrames++
				fmt.Print("░")

				if vc.silenceFrames >= silenceThreshold && len(vc.audioBuffer) > minAudioFrames*len(audioBytes) {
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
	// Open output stream (24kHz for typical TTS output)
	out := make([]int16, 1920) // 80ms buffer at 24kHz
	stream, err := portaudio.OpenDefaultStream(0, channels, 24000, len(out), out)
	if err != nil {
		log.Printf("Failed to open output stream: %v", err)
		return
	}
	defer stream.Close()

	if err := stream.Start(); err != nil {
		log.Printf("Failed to start output stream: %v", err)
		return
	}
	defer stream.Stop()

	buffer := []byte{}

	for {
		select {
		case <-ctx.Done():
			return
		case chunk, ok := <-vc.audioQueue:
			if !ok {
				return
			}

			buffer = append(buffer, chunk...)

			// Play when we have enough samples
			for len(buffer) >= len(out)*2 {
				// Convert bytes to int16
				for i := 0; i < len(out); i++ {
					if i*2+1 < len(buffer) {
						out[i] = int16(buffer[i*2]) | int16(buffer[i*2+1])<<8
					}
				}

				// Write to output
				if err := stream.Write(); err != nil {
					log.Printf("Audio write error: %v", err)
				}

				// Remove played samples
				buffer = buffer[len(out)*2:]
			}
		}
	}
}

// int16ToBytes converts int16 audio samples to bytes (little-endian).
func int16ToBytes(samples []int16) []byte {
	bytes := make([]byte, len(samples)*2)
	for i, s := range samples {
		bytes[i*2] = byte(s & 0xFF)
		bytes[i*2+1] = byte((s >> 8) & 0xFF)
	}
	return bytes
}
