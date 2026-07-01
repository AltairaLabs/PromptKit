//go:build portaudio

// Package main demonstrates bidirectional streaming with OpenDuplex.
//
// This example shows:
//   - Using OpenDuplex() with ASM (Audio Streaming Model) mode
//   - Real-time bidirectional audio streaming with gemini-2.5-pro-tts (supports bidiGenerateContent)
//   - Text mode uses gemini-2.5-flash (regular unary API)
//   - Interactive audio input via microphone with voice activity detection
//   - Sending audio chunks in real-time to Gemini
//   - Receiving streaming audio responses
//   - Handling duplex session lifecycle
//
// Requirements:
//   - Gemini API key with Live API access enabled
//   - Model: gemini-2.5-pro-tts for audio (supports streaming input), gemini-2.5-flash for text
//   - Microphone input (for interactive mode)
//
// Run with:
//
//	export GEMINI_API_KEY=your-key
//	go run . [mode]
//
// Modes:
//   - interactive: Voice input with microphone (default)
//   - text: Text streaming example
//   - chunks: Multiple chunk sending example
//
// Note: The Gemini Live API is in preview and requires special access.
// Visit https://ai.google.dev/ to request access if needed.
package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"os/signal"
	"strings"
	"sync"
	"syscall"
	"time"

	rtaudio "github.com/AltairaLabs/PromptKit/runtime/audio"
	"github.com/AltairaLabs/PromptKit/runtime/providers"
	"github.com/AltairaLabs/PromptKit/runtime/types"
	"github.com/AltairaLabs/PromptKit/sdk"
	"github.com/AltairaLabs/PromptKit/sdk/examples/audiohelper"
)

const (
	sampleRate       = 16000 // 16kHz for speech input
	outputSampleRate = 24000 // 24kHz for audio output
	channels         = 1     // Mono
)

func main() {
	fmt.Println("🎙️  Duplex Streaming with OpenDuplex")
	fmt.Println("=====================================")
	fmt.Println()

	// Check for API key
	apiKey := os.Getenv("GEMINI_API_KEY")
	if apiKey == "" {
		log.Fatal("GEMINI_API_KEY environment variable is required")
	}

	// Determine mode from args
	mode := "interactive"
	if len(os.Args) > 1 {
		mode = strings.ToLower(os.Args[1])
	}

	ctx := context.Background()

	// Run based on mode
	switch mode {
	case "interactive", "voice", "audio":
		// ASM mode: continuous bidirectional audio streaming
		conv, err := sdk.OpenDuplex(
			"./duplex.pack.json",
			"assistant",
			sdk.WithModel("gemini-2.0-flash-exp"),
			sdk.WithAPIKey(apiKey),
			sdk.WithStreamingConfig(&providers.StreamingInputConfig{
				Config: types.StreamingMediaConfig{
					Type:       types.ContentTypeAudio,
					SampleRate: sampleRate,
					Channels:   channels,
					Encoding:   "pcm",
					BitDepth:   16,
					ChunkSize:  3200, // 100ms of 16-bit PCM audio at 16kHz
				},
			}),
		)
		if err != nil {
			log.Fatalf("Failed to open duplex conversation: %v", err)
		}
		defer conv.Close()

		fmt.Println("✅ Duplex session created with ASM mode (continuous audio)")
		fmt.Println()

		if err := interactiveAudioExample(ctx, conv); err != nil {
			log.Fatalf("Interactive audio example failed: %v", err)
		}

	case "text":
		// For text mode, use regular unary conversation (not duplex)
		conv, err := sdk.Open(
			"./duplex.pack.json",
			"assistant",
			sdk.WithModel("gemini-2.5-flash"),
			sdk.WithAPIKey(apiKey),
		)
		if err != nil {
			log.Fatalf("Failed to open conversation: %v", err)
		}
		defer conv.Close()

		fmt.Println("✅ Conversation created (unary mode)")
		fmt.Println()
		fmt.Println("=== Text Streaming Example ===")

		if err := textStreamingExample(ctx, conv); err != nil {
			log.Printf("Text streaming example failed: %v", err)
		}

	case "chunks":
		// For chunks mode, use regular unary conversation
		conv, err := sdk.Open(
			"./duplex.pack.json",
			"assistant",
			sdk.WithModel("gemini-2.5-flash"),
			sdk.WithAPIKey(apiKey),
		)
		if err != nil {
			log.Fatalf("Failed to open conversation: %v", err)
		}
		defer conv.Close()

		fmt.Println("✅ Conversation created (unary mode)")
		fmt.Println()
		fmt.Println("=== Multiple Text Chunks Example ===")

		if err := multipleChunksExample(ctx, conv); err != nil {
			log.Printf("Multiple chunks example failed: %v", err)
		}

	default:
		log.Fatalf("Unknown mode: %s. Use 'interactive', 'text', or 'chunks'", mode)
	}

	fmt.Println()
	fmt.Println("Example completed!")
}

// interactiveAudioExample demonstrates real-time audio capture and streaming
func interactiveAudioExample(ctx context.Context, conv *sdk.Conversation) error {
	fmt.Println("🎤 Interactive Voice Mode")
	fmt.Println("=========================")

	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	// Open the shared pure-Go audio session (16 kHz mic + 24 kHz speaker defaults).
	session, err := audiohelper.NewSession(
		audiohelper.WithCaptureRate(sampleRate),
		audiohelper.WithPlaybackRate(outputSampleRate),
	)
	if err != nil {
		return fmt.Errorf("failed to open audio session: %w", err)
	}
	defer session.Close()
	if err := session.Start(ctx); err != nil {
		return fmt.Errorf("failed to start audio session: %w", err)
	}

	// Create audio handler
	audioHandler := &AudioHandler{
		conv:        conv,
		session:     session,
		audioBuffer: make([]byte, 0),
		audioQueue:  make(chan []byte, 100),
	}

	// Set up signal handling for graceful shutdown
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)

	// Start response processor in background
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		audioHandler.processResponses(ctx)
	}()

	// Start audio playback
	wg.Add(1)
	go func() {
		defer wg.Done()
		if err := audioHandler.playAudioOutput(ctx); err != nil {
			log.Printf("Audio playback error: %v", err)
		}
	}()

	// Start audio capture
	wg.Add(1)
	go func() {
		defer wg.Done()
		if err := audioHandler.captureAndStreamAudio(ctx); err != nil {
			log.Printf("Audio capture error: %v", err)
		}
	}()

	fmt.Println()
	fmt.Println("🎙️  Listening... Speak into your microphone.")
	fmt.Println("🔊  Audio responses will play through speakers.")
	fmt.Println("Press Ctrl+C to exit.")
	fmt.Println()

	// Wait for interrupt
	<-sigChan
	fmt.Println("\n\nShutting down...")
	cancel()

	// Close the audio queue to signal playback to stop
	close(audioHandler.audioQueue)

	wg.Wait()

	return nil
}

// AudioHandler manages audio capture and streaming
type AudioHandler struct {
	conv        *sdk.Conversation
	session     rtaudio.Session
	mu          sync.Mutex
	audioQueue  chan []byte // Queue for audio playback
	audioBuffer []byte
	speaking    bool
}

// captureAndStreamAudio captures microphone input and streams it to the conversation
func (ah *AudioHandler) captureAndStreamAudio(ctx context.Context) error {
	fmt.Println("Starting continuous audio streaming...")

	// Stream audio continuously in chunks
	frames := ah.session.Sources()[0].Frames()
	for {
		select {
		case <-ctx.Done():
			return nil
		case frame, ok := <-frames:
			if !ok {
				return nil
			}

			// frame.Data is already PCM16 little-endian bytes.
			// Gemini ASM can handle continuous bidirectional audio.
			chunk := &providers.StreamChunk{
				MediaData: &providers.StreamMediaData{
					MIMEType: "audio/pcm",
					Data:     frame.Data,
				},
			}

			if err := ah.conv.SendChunk(ctx, chunk); err != nil {
				log.Printf("Failed to send audio chunk: %v", err)
				continue
			}

			// Visual feedback
			if hasAudioEnergy(bytesToInt16(frame.Data)) {
				fmt.Print("█")
			} else {
				fmt.Print("░")
			}
		}
	}
}

// processResponses handles streaming responses from the model
func (ah *AudioHandler) processResponses(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		default:
		}

		// Get response from duplex conversation
		// Use a timeout to avoid blocking forever
		respCh, err := ah.conv.Response()
		if err != nil {
			if ctx.Err() != nil {
				return // Context canceled, exit gracefully
			}
			log.Printf("Response error: %v", err)
			time.Sleep(100 * time.Millisecond)
			continue
		}

		// Process streaming response
		hasText := false
		for {
			select {
			case <-ctx.Done():
				return
			case chunk, ok := <-respCh:
				if !ok {
					// Channel closed, get next response
					goto nextResponse
				}

				if chunk.Error != nil {
					log.Printf("Chunk error: %v", chunk.Error)
					goto nextResponse
				}

				// Handle audio response
				if chunk.MediaData != nil && len(chunk.MediaData.Data) > 0 {
					// Queue audio for playback
					select {
					case ah.audioQueue <- chunk.MediaData.Data:
					case <-ctx.Done():
						return
					}
				}

				// Handle text response (for debugging/transcription)
				if chunk.Delta != "" {
					if !hasText {
						fmt.Print("\n🤖 Assistant: ")
						hasText = true
					}
					fmt.Print(chunk.Delta)
				}

				if chunk.FinishReason != nil {
					if hasText {
						fmt.Println()
					}
					goto nextResponse
				}
			}
		}

	nextResponse:
		hasText = false
		// Small delay before getting next response
		select {
		case <-ctx.Done():
			return
		case <-time.After(10 * time.Millisecond):
		}
	}
}

// playAudioOutput plays audio responses through speakers
func (ah *AudioHandler) playAudioOutput(ctx context.Context) error {
	sink := ah.session.Sinks()[0]
	for {
		select {
		case <-ctx.Done():
			return nil
		case audioData, ok := <-ah.audioQueue:
			if !ok {
				return nil
			}

			sink.Write(rtaudio.MediaFrame{
				Kind:   rtaudio.KindAudio,
				Data:   audioData,
				Format: rtaudio.Format{SampleRate: outputSampleRate, Channels: channels},
			})
		}
	}
}

// setSpeaking updates the speaking state
func (ah *AudioHandler) setSpeaking(speaking bool) {
	ah.mu.Lock()
	defer ah.mu.Unlock()
	ah.speaking = speaking
}

// hasAudioEnergy checks if audio frame has significant energy
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

// bytesToInt16 converts little-endian PCM16 bytes to int16 audio samples.
func bytesToInt16(data []byte) []int16 {
	samples := make([]int16, len(data)/2)
	for i := range samples {
		samples[i] = int16(data[i*2]) | int16(data[i*2+1])<<8
	}
	return samples
}

// textStreamingExample demonstrates basic text streaming
func textStreamingExample(ctx context.Context, conv *sdk.Conversation) error {
	fmt.Println("Sending: 'Hello, tell me a short joke'")
	fmt.Println()

	// Use regular Send
	resp, err := conv.Send(ctx, "Hello, tell me a short joke")
	if err != nil {
		return fmt.Errorf("failed to send: %w", err)
	}

	// Print the response
	fmt.Printf("Response: %s\n", resp.Text())

	return nil
}

// multipleChunksExample demonstrates sending a message
func multipleChunksExample(ctx context.Context, conv *sdk.Conversation) error {
	// Build message
	message := "Can you count from one to five?"

	fmt.Println("Sending: '" + message + "'")
	fmt.Println()

	// Use regular Send
	resp, err := conv.Send(ctx, message)
	if err != nil {
		return fmt.Errorf("failed to send: %w", err)
	}

	// Print the response
	fmt.Printf("Response: %s\n", resp.Text())

	return nil
}
