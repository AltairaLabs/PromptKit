//go:build !portaudio

// Package main demonstrates OpenAI Realtime API streaming with text mode.
//
// This example shows:
//   - Setting up OpenAI Realtime API connection
//   - Sending text messages to the realtime session
//   - Receiving streaming text responses
//   - Function/tool calling during realtime sessions
//
// For full audio streaming with microphone input, see main_interactive.go
// (requires portaudio build tag).
//
// Requirements:
//   - OpenAI API key with Realtime API access
//   - Model: gpt-4o-realtime-preview or gpt-4o-realtime-preview-2024-12-17
//
// Run with:
//
//	export OPENAI_API_KEY=your-key
//	go run .
//
// Note: The OpenAI Realtime API is in preview and requires special access.
// Visit https://platform.openai.com/docs/guides/realtime to learn more.
package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"time"

	"github.com/AltairaLabs/PromptKit/runtime/providers"
	"github.com/AltairaLabs/PromptKit/runtime/types"
	"github.com/AltairaLabs/PromptKit/sdk"
)

func main() {
	fmt.Println("OpenAI Realtime API - Text Mode Demo")
	fmt.Println("=====================================")
	fmt.Println()
	fmt.Println("Note: For full audio streaming, build with: go build -tags portaudio")
	fmt.Println()

	// Check for API key
	apiKey := os.Getenv("OPENAI_API_KEY")
	if apiKey == "" {
		log.Fatal("OPENAI_API_KEY environment variable is required")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	// Open duplex conversation with OpenAI Realtime
	conv, err := sdk.OpenDuplex(
		"./openai-realtime.pack.json",
		"assistant",
		sdk.WithModel("gpt-4o-realtime-preview"),
		sdk.WithAPIKey(apiKey),
		sdk.WithStreamingConfig(&providers.StreamingInputConfig{
			Config: types.StreamingMediaConfig{
				Type:       types.ContentTypeAudio,
				SampleRate: 24000, // OpenAI Realtime uses 24kHz
				Channels:   1,
				Encoding:   "pcm16",
				BitDepth:   16,
				ChunkSize:  4800, // 100ms of 16-bit PCM audio at 24kHz
			},
			Metadata: map[string]interface{}{
				"voice":               "alloy", // OpenAI voice options: alloy, echo, shimmer, ash, ballad, coral, sage, verse
				"modalities":          []string{"text", "audio"},
				"input_transcription": true,
			},
		}),
	)
	if err != nil {
		log.Fatalf("Failed to open duplex conversation: %v", err)
	}
	defer conv.Close()

	fmt.Println("Connected to OpenAI Realtime API!")
	fmt.Println()

	// Send a text message
	fmt.Println("Sending: 'Hello! Can you tell me a short joke?'")
	fmt.Println()

	// Use SendText for text-based input
	if err := conv.SendText(ctx, "Hello! Can you tell me a short joke?"); err != nil {
		log.Fatalf("Failed to send text: %v", err)
	}

	// Get streaming response
	respCh, err := conv.Response()
	if err != nil {
		log.Fatalf("Failed to get response channel: %v", err)
	}

	fmt.Print("Assistant: ")
	for chunk := range respCh {
		if chunk.Error != nil {
			log.Printf("Error: %v", chunk.Error)
			break
		}

		// Handle text response
		if chunk.Delta != "" {
			fmt.Print(chunk.Delta)
		}

		// Handle transcription (if input_transcription is enabled)
		if chunk.Metadata != nil {
			if transcript, ok := chunk.Metadata["input_transcript"].(string); ok {
				fmt.Printf("\n[Input transcript: %s]", transcript)
			}
		}

		if chunk.FinishReason != nil {
			fmt.Println()
			break
		}
	}

	fmt.Println()
	fmt.Println("Demo completed!")
	fmt.Println()
	fmt.Println("Features available in OpenAI Realtime API:")
	fmt.Println("  - Bidirectional audio streaming (24kHz PCM16)")
	fmt.Println("  - Server-side voice activity detection (VAD)")
	fmt.Println("  - Function/tool calling during streaming")
	fmt.Println("  - Input and output audio transcription")
	fmt.Println("  - Multiple voice options (alloy, echo, shimmer, etc.)")
}
