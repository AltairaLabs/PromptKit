//go:build !portaudio

// Package main demonstrates bidirectional streaming with OpenDuplex.
//
// This is the text-mode fallback that builds without the portaudio tag (no
// microphone/speaker access). For full interactive voice streaming with a
// microphone, build the interactive variant in main_interactive.go:
//
//	export GEMINI_API_KEY=your-key
//	go run -tags portaudio . interactive
//
// Run the text demo with:
//
//	export GEMINI_API_KEY=your-key
//	go run .
package main

import (
	"context"
	"fmt"
	"log"
	"os"

	"github.com/AltairaLabs/PromptKit/sdk"
)

func main() {
	fmt.Println("🎙️  Duplex Streaming with OpenDuplex (text mode)")
	fmt.Println("================================================")
	fmt.Println()
	fmt.Println("Note: For interactive voice streaming, build with: go run -tags portaudio . interactive")
	fmt.Println()

	apiKey := os.Getenv("GEMINI_API_KEY")
	if apiKey == "" {
		log.Fatal("GEMINI_API_KEY environment variable is required")
	}

	ctx := context.Background()

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
	fmt.Println("Sending: 'Hello, tell me a short joke'")
	fmt.Println()

	resp, err := conv.Send(ctx, "Hello, tell me a short joke")
	if err != nil {
		log.Fatalf("Failed to send: %v", err)
	}

	fmt.Printf("Response: %s\n", resp.Text())
}
