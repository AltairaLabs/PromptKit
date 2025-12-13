// Package main demonstrates bidirectional streaming with the SDK.
//
// This example shows:
//   - Creating a bidirectional streaming session
//   - Sending text input in real-time
//   - Receiving streaming responses
//   - Interactive conversation flow
//
// Requirements:
//   - OpenAI API key (or other provider supporting StreamInputSupport)
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
	"time"

	"github.com/AltairaLabs/PromptKit/sdk"
)

func main() {
	fmt.Println("🔄 Bidirectional Streaming Example")
	fmt.Println("===================================")
	fmt.Println()

	ctx := context.Background()

	// Open conversation
	// Note: This requires a pack with a provider that supports bidirectional streaming
	conv, err := sdk.Open("../voice-chat/voice-assistant.pack.json", "chat")
	if err != nil {
		log.Fatalf("Failed to open pack: %v", err)
	}
	defer conv.Close()

	// Create bidirectional streaming session
	bidi, err := conv.StreamBiDi(ctx)
	if err != nil {
		log.Fatalf("Failed to create bidirectional stream: %v", err)
	}
	defer bidi.Close()

	fmt.Println("✅ Bidirectional stream established")
	fmt.Println()

	// Send messages in a goroutine
	go func() {
		messages := []string{
			"Hello! Can you hear me?",
			"What's the weather like today?",
			"Tell me a short joke.",
		}

		for i, msg := range messages {
			// Wait a bit between messages
			if i > 0 {
				time.Sleep(5 * time.Second)
			}

			fmt.Printf("\n📤 Sending: %s\n", msg)
			if err := bidi.SendText(ctx, msg); err != nil {
				log.Printf("Error sending message: %v", err)
				return
			}
			fmt.Println("📥 Receiving response...")
		}
	}()

	// Receive responses
	responseCount := 0
	for chunk := range bidi.Output() {
		if chunk.Error != nil {
			log.Printf("❌ Error: %v", chunk.Error)
			break
		}

		switch chunk.Type {
		case sdk.ChunkText:
			fmt.Print(chunk.Text)

		case sdk.ChunkToolCall:
			fmt.Printf("\n🔧 Tool call: %s\n", chunk.ToolCall.Name)

		case sdk.ChunkDone:
			fmt.Println()
			fmt.Println("✅ Response complete")
			responseCount++

			// Exit after receiving 3 responses
			if responseCount >= 3 {
				fmt.Println("\n✅ All messages processed!")
				return
			}
		}
	}

	// Check for any errors
	if err := bidi.Error(); err != nil {
		log.Printf("Stream error: %v", err)
	}

	fmt.Println("\n👋 Done!")
}
