//go:build portaudio

// Package main demonstrates OpenAI Realtime API with bidirectional audio streaming.
//
// This example shows:
//   - Using OpenDuplex() with OpenAI Realtime API
//   - Real-time bidirectional audio streaming at 24kHz
//   - Interactive audio input via microphone with server-side VAD
//   - Sending audio chunks in real-time to OpenAI
//   - Receiving streaming audio responses
//   - Function/tool calling during streaming sessions
//
// Requirements:
//   - OpenAI API key with Realtime API access
//   - Model: gpt-4o-realtime-preview
//   - Microphone input (for interactive mode)
//   - PortAudio library (brew install portaudio on macOS)
//
// Run with:
//
//	export OPENAI_API_KEY=your-key
//	go run -tags portaudio .
//
// Note: The OpenAI Realtime API is in preview and requires special access.
// Visit https://platform.openai.com/docs/guides/realtime to learn more.
package main

import (
	"context"
	"encoding/json"
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
	// OpenAI Realtime uses 24kHz for both input and output
	sampleRate   = 24000
	channels     = 1
	framesPerBuf = 2400 // 100ms at 24kHz
)

func main() {
	fmt.Println("OpenAI Realtime API - Voice Chat")
	fmt.Println("=================================")
	fmt.Println()

	// Check for API key
	apiKey := os.Getenv("OPENAI_API_KEY")
	if apiKey == "" {
		log.Fatal("OPENAI_API_KEY environment variable is required")
	}

	// Determine mode from args
	mode := "interactive"
	if len(os.Args) > 1 {
		mode = strings.ToLower(os.Args[1])
	}

	ctx := context.Background()

	switch mode {
	case "interactive", "voice", "audio":
		if err := runInteractiveMode(ctx, apiKey); err != nil {
			log.Fatalf("Interactive mode failed: %v", err)
		}
	case "tools":
		if err := runToolsDemo(ctx, apiKey); err != nil {
			log.Fatalf("Tools demo failed: %v", err)
		}
	case "translator":
		if err := runTranslatorMode(ctx, apiKey); err != nil {
			log.Fatalf("Translator mode failed: %v", err)
		}
	default:
		fmt.Printf("Unknown mode: %s\n", mode)
		fmt.Println("Available modes:")
		fmt.Println("  interactive - Voice chat with audio input/output (default)")
		fmt.Println("  tools       - Demo function calling during streaming")
		fmt.Println("  translator  - Real-time translation demo")
		os.Exit(1)
	}
}

// runInteractiveMode runs the main voice chat mode
func runInteractiveMode(ctx context.Context, apiKey string) error {
	// Initialize PortAudio
	if err := portaudio.Initialize(); err != nil {
		return fmt.Errorf("failed to initialize PortAudio: %w", err)
	}
	defer portaudio.Terminate()

	// Open duplex conversation with OpenAI Realtime
	conv, err := sdk.OpenDuplex(
		"./openai-realtime.pack.json",
		"assistant",
		sdk.WithModel("gpt-4o-realtime-preview"),
		sdk.WithAPIKey(apiKey),
		sdk.WithStreamingConfig(&providers.StreamingInputConfig{
			Config: types.StreamingMediaConfig{
				Type:       types.ContentTypeAudio,
				SampleRate: sampleRate,
				Channels:   channels,
				Encoding:   "pcm16",
				BitDepth:   16,
				ChunkSize:  4800, // 100ms at 24kHz
			},
			Metadata: map[string]interface{}{
				"voice":               "alloy",
				"modalities":          []string{"text", "audio"},
				"input_transcription": true,
			},
		}),
	)
	if err != nil {
		return fmt.Errorf("failed to open duplex conversation: %w", err)
	}
	defer conv.Close()

	fmt.Println("Connected to OpenAI Realtime API!")
	fmt.Println()

	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	// Create audio handler
	handler := &AudioHandler{
		conv:       conv,
		audioQueue: make(chan []byte, 100),
	}

	// Set up signal handling
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)

	// Start goroutines
	var wg sync.WaitGroup

	wg.Add(1)
	go func() {
		defer wg.Done()
		handler.processResponses(ctx)
	}()

	wg.Add(1)
	go func() {
		defer wg.Done()
		if err := handler.playAudioOutput(ctx); err != nil {
			log.Printf("Audio playback error: %v", err)
		}
	}()

	wg.Add(1)
	go func() {
		defer wg.Done()
		if err := handler.captureAndStreamAudio(ctx); err != nil {
			log.Printf("Audio capture error: %v", err)
		}
	}()

	fmt.Println("Listening... Speak into your microphone.")
	fmt.Println("OpenAI's server-side VAD will detect when you're done speaking.")
	fmt.Println("Press Ctrl+C to exit.")
	fmt.Println()

	// Wait for interrupt
	<-sigChan
	fmt.Println("\n\nShutting down...")
	cancel()
	close(handler.audioQueue)
	wg.Wait()

	return nil
}

// runToolsDemo demonstrates function calling during realtime streaming
func runToolsDemo(ctx context.Context, apiKey string) error {
	if err := portaudio.Initialize(); err != nil {
		return fmt.Errorf("failed to initialize PortAudio: %w", err)
	}
	defer portaudio.Terminate()

	// Open with tools configuration
	conv, err := sdk.OpenDuplex(
		"./openai-realtime.pack.json",
		"assistant",
		sdk.WithModel("gpt-4o-realtime-preview"),
		sdk.WithAPIKey(apiKey),
		sdk.WithStreamingConfig(&providers.StreamingInputConfig{
			Config: types.StreamingMediaConfig{
				Type:       types.ContentTypeAudio,
				SampleRate: sampleRate,
				Channels:   channels,
				Encoding:   "pcm16",
				BitDepth:   16,
				ChunkSize:  4800,
			},
			Tools: []providers.StreamingToolDefinition{
				{
					Name:        "get_weather",
					Description: "Get the current weather for a location",
					Parameters: map[string]interface{}{
						"type": "object",
						"properties": map[string]interface{}{
							"location": map[string]interface{}{
								"type":        "string",
								"description": "The city and state, e.g. San Francisco, CA",
							},
							"unit": map[string]interface{}{
								"type":        "string",
								"enum":        []string{"celsius", "fahrenheit"},
								"description": "The temperature unit",
							},
						},
						"required": []string{"location"},
					},
				},
			},
			Metadata: map[string]interface{}{
				"voice":               "echo",
				"modalities":          []string{"text", "audio"},
				"input_transcription": true,
			},
		}),
	)
	if err != nil {
		return fmt.Errorf("failed to open duplex conversation: %w", err)
	}
	defer conv.Close()

	fmt.Println("Connected to OpenAI Realtime API with Tools!")
	fmt.Println("Try asking: 'What's the weather in San Francisco?'")
	fmt.Println()

	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	handler := &AudioHandler{
		conv:       conv,
		audioQueue: make(chan []byte, 100),
	}

	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)

	var wg sync.WaitGroup

	wg.Add(1)
	go func() {
		defer wg.Done()
		handler.processResponsesWithTools(ctx)
	}()

	wg.Add(1)
	go func() {
		defer wg.Done()
		if err := handler.playAudioOutput(ctx); err != nil {
			log.Printf("Audio playback error: %v", err)
		}
	}()

	wg.Add(1)
	go func() {
		defer wg.Done()
		if err := handler.captureAndStreamAudio(ctx); err != nil {
			log.Printf("Audio capture error: %v", err)
		}
	}()

	fmt.Println("Listening... Ask about the weather!")
	fmt.Println("Press Ctrl+C to exit.")
	fmt.Println()

	<-sigChan
	fmt.Println("\n\nShutting down...")
	cancel()
	close(handler.audioQueue)
	wg.Wait()

	return nil
}

// runTranslatorMode demonstrates real-time translation
func runTranslatorMode(ctx context.Context, apiKey string) error {
	if err := portaudio.Initialize(); err != nil {
		return fmt.Errorf("failed to initialize PortAudio: %w", err)
	}
	defer portaudio.Terminate()

	conv, err := sdk.OpenDuplex(
		"./openai-realtime.pack.json",
		"translator",
		sdk.WithModel("gpt-4o-realtime-preview"),
		sdk.WithAPIKey(apiKey),
		sdk.WithVariables(map[string]string{
			"target_language": "Spanish",
		}),
		sdk.WithStreamingConfig(&providers.StreamingInputConfig{
			Config: types.StreamingMediaConfig{
				Type:       types.ContentTypeAudio,
				SampleRate: sampleRate,
				Channels:   channels,
				Encoding:   "pcm16",
				BitDepth:   16,
				ChunkSize:  4800,
			},
			Metadata: map[string]interface{}{
				"voice":               "shimmer",
				"modalities":          []string{"text", "audio"},
				"input_transcription": true,
			},
		}),
	)
	if err != nil {
		return fmt.Errorf("failed to open duplex conversation: %w", err)
	}
	defer conv.Close()

	fmt.Println("Connected to OpenAI Realtime Translator!")
	fmt.Println("Speak in English and hear the Spanish translation.")
	fmt.Println()

	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	handler := &AudioHandler{
		conv:       conv,
		audioQueue: make(chan []byte, 100),
	}

	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)

	var wg sync.WaitGroup

	wg.Add(1)
	go func() {
		defer wg.Done()
		handler.processResponses(ctx)
	}()

	wg.Add(1)
	go func() {
		defer wg.Done()
		if err := handler.playAudioOutput(ctx); err != nil {
			log.Printf("Audio playback error: %v", err)
		}
	}()

	wg.Add(1)
	go func() {
		defer wg.Done()
		if err := handler.captureAndStreamAudio(ctx); err != nil {
			log.Printf("Audio capture error: %v", err)
		}
	}()

	fmt.Println("Listening... Speak and hear the translation!")
	fmt.Println("Press Ctrl+C to exit.")
	fmt.Println()

	<-sigChan
	fmt.Println("\n\nShutting down...")
	cancel()
	close(handler.audioQueue)
	wg.Wait()

	return nil
}

// AudioHandler manages audio capture, streaming, and playback
type AudioHandler struct {
	conv       *sdk.Conversation
	audioQueue chan []byte
	mu         sync.Mutex
	speaking   bool
}

// captureAndStreamAudio captures microphone input and streams to OpenAI
func (ah *AudioHandler) captureAndStreamAudio(ctx context.Context) error {
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

	fmt.Println("Audio capture started...")

	for {
		select {
		case <-ctx.Done():
			return nil
		default:
			if err := stream.Read(); err != nil {
				log.Printf("Audio read error: %v", err)
				continue
			}

			// Convert int16 to bytes (PCM16 little-endian)
			audioBytes := int16ToBytes(in)

			// Create audio chunk
			audioData := string(audioBytes)
			chunk := &providers.StreamChunk{
				MediaDelta: &types.MediaContent{
					MIMEType: "audio/pcm",
					Data:     &audioData,
				},
			}

			// Send to OpenAI Realtime
			if err := ah.conv.SendChunk(ctx, chunk); err != nil {
				log.Printf("Failed to send audio chunk: %v", err)
				continue
			}

			// Visual feedback for audio level
			if hasAudioEnergy(in) {
				fmt.Print("\033[32m|\033[0m") // Green bar for speech
			}
		}
	}
}

// processResponses handles streaming responses from OpenAI Realtime
func (ah *AudioHandler) processResponses(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		default:
		}

		respCh, err := ah.conv.Response()
		if err != nil {
			if ctx.Err() != nil {
				return
			}
			log.Printf("Response error: %v", err)
			time.Sleep(100 * time.Millisecond)
			continue
		}

		hasText := false
		for {
			select {
			case <-ctx.Done():
				return
			case chunk, ok := <-respCh:
				if !ok {
					goto nextResponse
				}

				if chunk.Error != nil {
					log.Printf("Chunk error: %v", chunk.Error)
					goto nextResponse
				}

				// Handle audio response
				if chunk.MediaDelta != nil && chunk.MediaDelta.Data != nil {
					audioData := []byte(*chunk.MediaDelta.Data)
					select {
					case ah.audioQueue <- audioData:
					case <-ctx.Done():
						return
					}
				}

				// Handle text response (transcript)
				if chunk.Delta != "" {
					if !hasText {
						fmt.Print("\n\033[34mAssistant:\033[0m ")
						hasText = true
					}
					fmt.Print(chunk.Delta)
				}

				// Handle input transcription
				if chunk.Metadata != nil {
					if transcript, ok := chunk.Metadata["input_transcript"].(string); ok && transcript != "" {
						fmt.Printf("\n\033[33m[You said: %s]\033[0m\n", transcript)
					}
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
		select {
		case <-ctx.Done():
			return
		case <-time.After(10 * time.Millisecond):
		}
	}
}

// processResponsesWithTools handles responses including tool calls
func (ah *AudioHandler) processResponsesWithTools(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		default:
		}

		respCh, err := ah.conv.Response()
		if err != nil {
			if ctx.Err() != nil {
				return
			}
			time.Sleep(100 * time.Millisecond)
			continue
		}

		hasText := false
		for {
			select {
			case <-ctx.Done():
				return
			case chunk, ok := <-respCh:
				if !ok {
					goto nextResponse
				}

				if chunk.Error != nil {
					goto nextResponse
				}

				// Handle audio
				if chunk.MediaDelta != nil && chunk.MediaDelta.Data != nil {
					audioData := []byte(*chunk.MediaDelta.Data)
					select {
					case ah.audioQueue <- audioData:
					case <-ctx.Done():
						return
					}
				}

				// Handle text
				if chunk.Delta != "" {
					if !hasText {
						fmt.Print("\n\033[34mAssistant:\033[0m ")
						hasText = true
					}
					fmt.Print(chunk.Delta)
				}

				// Handle tool calls
				if chunk.ToolCalls != nil {
					for _, tc := range chunk.ToolCalls {
						argsStr := string(tc.Args)
						fmt.Printf("\n\033[35m[Tool call: %s(%s)]\033[0m\n", tc.Name, argsStr)

						// Simulate tool execution
						result := ah.executeToolCall(tc.Name, argsStr)
						fmt.Printf("\033[35m[Tool result: %s]\033[0m\n", result)

						// Note: In a real implementation, you would send the tool result back
						// to the streaming session. This requires the session to implement
						// ToolResponseSupport interface. For now, we just log it.
						// TODO: Add conv.SendToolResult() when SDK supports streaming tool responses
						log.Printf("Tool result for %s: %s", tc.ID, result)
					}
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
		select {
		case <-ctx.Done():
			return
		case <-time.After(10 * time.Millisecond):
		}
	}
}

// executeToolCall simulates executing a tool call
func (ah *AudioHandler) executeToolCall(name, arguments string) string {
	switch name {
	case "get_weather":
		var args struct {
			Location string `json:"location"`
			Unit     string `json:"unit"`
		}
		if err := json.Unmarshal([]byte(arguments), &args); err != nil {
			return fmt.Sprintf("Error parsing arguments: %v", err)
		}

		// Simulated weather response
		unit := args.Unit
		if unit == "" {
			unit = "fahrenheit"
		}
		temp := 72
		if unit == "celsius" {
			temp = 22
		}
		return fmt.Sprintf("The weather in %s is currently sunny with a temperature of %d degrees %s.",
			args.Location, temp, unit)

	default:
		return fmt.Sprintf("Unknown tool: %s", name)
	}
}

// playAudioOutput plays audio responses through speakers
func (ah *AudioHandler) playAudioOutput(ctx context.Context) error {
	out := make([]int16, framesPerBuf)
	stream, err := portaudio.OpenDefaultStream(0, channels, float64(sampleRate), framesPerBuf, out)
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
				for i := 0; i < len(out); i++ {
					if i*2+1 < len(buffer) {
						out[i] = int16(buffer[i*2]) | int16(buffer[i*2+1])<<8
					}
				}

				if err := stream.Write(); err != nil {
					log.Printf("Audio write error: %v", err)
				}

				buffer = buffer[len(out)*2:]
			}
		}
	}
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
