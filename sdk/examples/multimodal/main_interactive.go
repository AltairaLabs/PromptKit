// Package main demonstrates multimodal capabilities with the PromptKit SDK.
//
// This example shows:
//   - Sending images with text prompts using WithImageURL
//   - Streaming multimodal responses
//   - Using Gemini provider for vision analysis
//
// Run with:
//
//	export GEMINI_API_KEY=your-key
//	go run .
package main

import (
	"context"
	"fmt"
	"log"

	"github.com/AltairaLabs/PromptKit/sdk"
)

func main() {
	// Open a conversation with the vision analyst prompt
	conv, err := sdk.Open("./multimodal.pack.json", "vision-analyst")
	if err != nil {
		log.Fatalf("Failed to open pack: %v", err)
	}
	defer conv.Close()

	ctx := context.Background()

	// Example 1: Analyze an image with streaming response
	fmt.Println("=== Image Analysis with Streaming ===")
	fmt.Println()

	// Using a public domain image URL for the example
	imageURL := "https://upload.wikimedia.org/wikipedia/commons/thumb/3/3a/Cat03.jpg/1200px-Cat03.jpg"

	fmt.Println("Analyzing image...")
	fmt.Println()

	for chunk := range conv.Stream(ctx, "What do you see in this image? Describe it in detail.",
		sdk.WithImageURL(imageURL),
	) {
		if chunk.Error != nil {
			log.Printf("Error: %v", chunk.Error)
			break
		}
		if chunk.Type == sdk.ChunkDone {
			fmt.Println("\n\n[Analysis Complete]")
			break
		}
		fmt.Print(chunk.Text)
	}

	// Example 2: Follow-up question about the image (conversation maintains context)
	fmt.Println("\n=== Follow-up Question ===")
	fmt.Println()

	for chunk := range conv.Stream(ctx, "What colors are prominent in the image?") {
		if chunk.Error != nil {
			log.Printf("Error: %v", chunk.Error)
			break
		}
		if chunk.Type == sdk.ChunkDone {
			fmt.Println("\n\n[Complete]")
			break
		}
		fmt.Print(chunk.Text)
	}

	// Example 3: Non-streaming multimodal request
	fmt.Println("\n=== Non-Streaming Analysis ===")
	fmt.Println()

	// Analyze a different image without streaming
	architectureURL := "https://upload.wikimedia.org/wikipedia/commons/thumb/1/10/Empire_State_Building_%28aerial_view%29.jpg/800px-Empire_State_Building_%28aerial_view%29.jpg"

	resp, err := conv.Send(ctx, "Identify the building in this image and provide a brief history.",
		sdk.WithImageURL(architectureURL),
	)
	if err != nil {
		log.Printf("Error: %v", err)
	} else {
		fmt.Println(resp.Text())
	}
}
