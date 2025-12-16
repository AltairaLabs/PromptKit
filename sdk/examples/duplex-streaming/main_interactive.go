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

	"github.com/gordonklaus/portaudio"

	"github.com/AltairaLabs/PromptKit/runtime/providers"
	"github.com/AltairaLabs/PromptKit/runtime/types"
	"github.com/AltairaLabs/PromptKit/sdk"
)

const (
	sampleRate       = 16000 // 16kHz for speech input
	outputSampleRate = 24000 // 24kHz for audio output
	channels         = 1     // Mono
	framesPerBuf     = 1600  // 100ms at 16kHz for input
	outputFramesBuf  = 960   // 40ms at 24kHz for output
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

	// Initialize PortAudio
	if err := portaudio.Initialize(); err != nil {
		return fmt.Errorf("failed to initialize PortAudio: %w", err)
	}
	defer portaudio.Terminate()

	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	// Create audio handler
	audioHandler := &AudioHandler{
		conv:        conv,
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
	mu          sync.Mutex
	audioQueue  chan []byte // Queue for audio playback
	audioBuffer []byte
	speaking    bool
}

// captureAndStreamAudio captures microphone input and streams it to the conversation
func (ah *AudioHandler) captureAndStreamAudio(ctx context.Context) error {
	// Open input stream
	in := make([]int16, framesPerBuf)
	stream, err := portaudio.OpenDefaultStream(channels, 0, sampleRate, framesPerBuf, in)
	if err != nil {
		return fmt.Errorf("failed to open input stream: %w", err)
	}
	defer stream.Close()

	if err := stream.Start(); err != nil {
		return fmt.Errorf("failed to start input stream: %w", err)
	}
	defer stream.Stop()

	fmt.Println("Starting continuous audio streaming...")

	// Stream audio continuously in chunks
	for {
		select {
		case <-ctx.Done():
			return nil
		default:
			// Read audio frame
			if err := stream.Read(); err != nil {
				log.Printf("Audio read error: %v", err)
				continue
			}

			// Convert int16 to bytes (PCM16)
			audioBytes := int16ToBytes(in)

			// Stream audio continuously to the model
			// Gemini ASM can handle continuous bidirectional audio
			audioData := string(audioBytes)
			chunk := &providers.StreamChunk{
				MediaDelta: &types.MediaContent{
					MIMEType: types.MIMETypeAudioWAV,
					Data:     &audioData,
				},
			}

			if err := ah.conv.SendChunk(ctx, chunk); err != nil {
				log.Printf("Failed to send audio chunk: %v", err)
				continue
			}

			// Visual feedback
			if hasAudioEnergy(in) {
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
				if chunk.MediaDelta != nil && chunk.MediaDelta.Data != nil {
					// Queue audio for playback
					audioData := []byte(*chunk.MediaDelta.Data)
					select {
					case ah.audioQueue <- audioData:
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
	// Open output stream
	out := make([]int16, outputFramesBuf)
	stream, err := portaudio.OpenDefaultStream(0, channels, float64(outputSampleRate), outputFramesBuf, out)
	if err != nil {
		return fmt.Errorf("failed to open output stream: %w", err)
	}
	defer stream.Close()

	if err := stream.Start(); err != nil {
		return fmt.Errorf("failed to start output stream: %w", err)
	}
	defer stream.Stop()

	buffer := []byte{}

	for {
		select {
		case <-ctx.Done():
			return nil
		case audioData, ok := <-ah.audioQueue:
			if !ok {
				return nil
			}

			buffer = append(buffer, audioData...)

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

// setSpeaking updates the speaking state
func (ah *AudioHandler) setSpeaking(speaking bool) {
	ah.mu.Lock()
	defer ah.mu.Unlock()
	ah.speaking = speaking
}

// hasAudioEnergy checks if audio frame has significant energy
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

// int16ToBytes converts int16 audio samples to bytes (little-endian PCM16)
func int16ToBytes(samples []int16) []byte {
	bytes := make([]byte, len(samples)*2)
	for i, s := range samples {
		bytes[i*2] = byte(s & 0xFF)
		bytes[i*2+1] = byte((s >> 8) & 0xFF)
	}
	return bytes
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
