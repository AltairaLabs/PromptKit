// Package main demonstrates bidirectional streaming with OpenDuplex.
//
// This example shows:
//   - Using OpenDuplex() for real-time bidirectional streaming
//   - Sending text and audio chunks to the model
//   - Receiving streaming responses
//   - Handling duplex session lifecycle
//
// Requirements:
//   - Gemini API key with Live API access enabled
//   - Model: gemini-2.0-flash-exp (supports streaming input)
//
// Run with:
//
//	export GEMINI_API_KEY=your-key
//	go run .
//
// Note: The Gemini Live API is in preview and requires special access.
// Visit https://ai.google.dev/ to request access if needed.
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
	fmt.Println("🎙️  Duplex Streaming with OpenDuplex")
	fmt.Println("=====================================")
	fmt.Println()

	// Check for API key
	apiKey := os.Getenv("GEMINI_API_KEY")
	if apiKey == "" {
		log.Fatal("GEMINI_API_KEY environment variable is required")
	}

	ctx := context.Background()

	// Open a duplex streaming conversation
	// This requires a provider that implements StreamInputSupport (e.g., Gemini)
	conv, err := sdk.OpenDuplex(
		"./duplex.pack.json",
		"assistant",
		sdk.WithModel("gemini-2.0-flash-exp"),
		sdk.WithAPIKey(apiKey),
	)
	if err != nil {
		log.Fatalf("Failed to open duplex conversation: %v", err)
	}
	defer conv.Close()

	fmt.Println("✅ Duplex session created")
	fmt.Println()

	// Example 1: Send text and receive streaming response
	fmt.Println("=== Example 1: Text Streaming ===")
	if err := textStreamingExample(ctx, conv); err != nil {
		log.Printf("Text streaming example failed: %v", err)
	}

	fmt.Println()
	time.Sleep(1 * time.Second)

	// Example 2: Send multiple text chunks
	fmt.Println("=== Example 2: Multiple Text Chunks ===")
	if err := multipleChunksExample(ctx, conv); err != nil {
		log.Printf("Multiple chunks example failed: %v", err)
	}

	fmt.Println()
	fmt.Println("All examples completed!")
}

// textStreamingExample demonstrates basic text streaming
func textStreamingExample(ctx context.Context, conv *sdk.Conversation) error {
	fmt.Println("Sending: 'Hello, tell me a short joke'")
	fmt.Println()

	// Send text to the model
	if err := conv.SendText(ctx, "Hello, tell me a short joke"); err != nil {
		return fmt.Errorf("failed to send text: %w", err)
	}

	// Get the response channel
	respCh, err := conv.Response()
	if err != nil {
		return fmt.Errorf("failed to get response channel: %w", err)
	}

	// Receive and print streaming response
	fmt.Print("Response: ")
	for chunk := range respCh {
		if chunk.Error != nil {
			return fmt.Errorf("received error chunk: %w", chunk.Error)
		}

		// Print content as it arrives
		if chunk.Content != "" {
			fmt.Print(chunk.Content)
		}
		if chunk.Delta != "" {
			fmt.Print(chunk.Delta)
		}

		// Check if done
		if chunk.FinishReason != nil {
			break
		}
	}
	fmt.Println()

	return nil
}

// multipleChunksExample demonstrates sending multiple chunks
func multipleChunksExample(ctx context.Context, conv *sdk.Conversation) error {
	// Send multiple text chunks to build up a complete message
	chunks := []string{
		"Can you ",
		"count from ",
		"one to five?",
	}

	fmt.Println("Sending message in chunks:")
	for _, chunk := range chunks {
		fmt.Printf("  - '%s'\n", chunk)
		if err := conv.SendText(ctx, chunk); err != nil {
			return fmt.Errorf("failed to send chunk: %w", err)
		}
		time.Sleep(100 * time.Millisecond) // Small delay between chunks
	}
	fmt.Println()

	// Get the response channel
	respCh, err := conv.Response()
	if err != nil {
		return fmt.Errorf("failed to get response channel: %w", err)
	}

	// Receive and print streaming response
	fmt.Print("Response: ")
	timeout := time.After(10 * time.Second)

loop:
	for {
		select {
		case chunk, ok := <-respCh:
			if !ok {
				// Channel closed
				break loop
			}

			if chunk.Error != nil {
				return fmt.Errorf("received error chunk: %w", chunk.Error)
			}

			// Print content as it arrives
			if chunk.Content != "" {
				fmt.Print(chunk.Content)
			}
			if chunk.Delta != "" {
				fmt.Print(chunk.Delta)
			}

			// Check if done
			if chunk.FinishReason != nil {
				break loop
			}

		case <-timeout:
			return fmt.Errorf("response timeout")
		}
	}
	fmt.Println()

	return nil
}

// sendAudioExample demonstrates sending audio chunks (optional, requires audio data)
func sendAudioExample(ctx context.Context, conv *sdk.Conversation) error {
	fmt.Println("Sending audio chunk...")

	// Example: Send an audio chunk
	// In a real application, you would get audio data from a microphone
	audioData := "sample audio data" // This would be actual PCM audio bytes

	chunk := &providers.StreamChunk{
		MediaDelta: &types.MediaContent{
			MIMEType: types.MIMETypeAudioWAV,
			Data:     &audioData,
		},
	}

	if err := conv.SendChunk(ctx, chunk); err != nil {
		return fmt.Errorf("failed to send audio chunk: %w", err)
	}

	// Get response
	respCh, err := conv.Response()
	if err != nil {
		return fmt.Errorf("failed to get response channel: %w", err)
	}

	// Receive streaming response
	fmt.Print("Response: ")
	for chunk := range respCh {
		if chunk.Error != nil {
			return fmt.Errorf("received error chunk: %w", chunk.Error)
		}

		if chunk.Content != "" {
			fmt.Print(chunk.Content)
		}

		if chunk.FinishReason != nil {
			break
		}
	}
	fmt.Println()

	return nil
}
