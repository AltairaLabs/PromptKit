// Package main demonstrates streaming responses with SDK v2.
//
// This example shows:
//   - Using Stream() for real-time response streaming
//   - Processing chunks as they arrive
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

	"github.com/AltairaLabs/PromptKit/sdk"
)

func main() {
	// Open a conversation
	conv, err := sdk.Open("./streaming.pack.json", "storyteller")
	if err != nil {
		log.Fatalf("Failed to open pack: %v", err)
	}
	defer conv.Close()

	ctx := context.Background()

	// Method 1: Basic streaming with Stream()
	fmt.Println("=== Basic Streaming ===")
	fmt.Println()

	for chunk := range conv.Stream(ctx, "Tell me a very short story about a robot") {
		if chunk.Error != nil {
			log.Printf("Error: %v", chunk.Error)
			break
		}
		if chunk.Type == sdk.ChunkDone {
			fmt.Println("\n\n[Complete]")
			break
		}
		// Print text chunks as they arrive
		fmt.Print(chunk.Text)
	}

	// Method 2: Stream with character counting
	fmt.Println("\n=== Stream with Progress ===")
	fmt.Println()

	charCount := 0
	chunks := conv.Stream(ctx, "Write a short poem about AI")

	for chunk := range chunks {
		if chunk.Error != nil {
			log.Printf("Error: %v", chunk.Error)
			break
		}
		if chunk.Type == sdk.ChunkDone {
			fmt.Printf("\n\n[Complete - %d characters received]\n", charCount)
			break
		}
		fmt.Print(chunk.Text)
		charCount += len(chunk.Text)
	}

	// Method 3: Collect all text (when you want streaming progress but need full text)
	fmt.Println("\n=== Collecting Text ===")
	fmt.Println()

	var fullText string
	for chunk := range conv.Stream(ctx, "Now tell me a haiku about programming") {
		if chunk.Error != nil {
			log.Printf("Error: %v", chunk.Error)
			break
		}
		if chunk.Type == sdk.ChunkDone {
			break
		}
		fmt.Print(".") // Show progress
		fullText += chunk.Text
	}
	fmt.Println()
	fmt.Println("Collected text:")
	fmt.Println(fullText)
}
