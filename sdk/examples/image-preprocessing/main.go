// Package main demonstrates image preprocessing capabilities with the PromptKit SDK.
//
// This example shows:
//   - Automatic image resizing with WithAutoResize
//   - Custom image preprocessing with WithImagePreprocessing
//   - Quality optimization for LLM vision models
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

	"github.com/AltairaLabs/PromptKit/runtime/pipeline/stage"
	"github.com/AltairaLabs/PromptKit/sdk"
)

func main() {
	ctx := context.Background()

	// Example 1: Using WithAutoResize for simple resizing
	fmt.Println("=== Example 1: Auto-Resize (1024x1024 max) ===")
	fmt.Println()

	conv1, err := sdk.Open(
		"./image-preprocessing.pack.json",
		"vision-analyst",
		sdk.WithAutoResize(1024, 1024), // Resize large images to max 1024x1024
	)
	if err != nil {
		log.Fatalf("Failed to open pack: %v", err)
	}
	defer conv1.Close()

	// Using a high-resolution image that will be automatically resized
	imageURL := "https://upload.wikimedia.org/wikipedia/commons/thumb/e/ea/Van_Gogh_-_Starry_Night_-_Google_Art_Project.jpg/1280px-Van_Gogh_-_Starry_Night_-_Google_Art_Project.jpg"

	fmt.Println("Analyzing high-resolution image (will be auto-resized)...")
	fmt.Println()

	resp, err := conv1.Send(ctx, "Describe this famous painting. What artistic techniques do you notice?",
		sdk.WithImageURL(imageURL),
	)
	if err != nil {
		log.Printf("Error: %v", err)
	} else {
		fmt.Println(resp.Text())
	}

	// Example 2: Using WithImagePreprocessing for full control
	fmt.Println("\n=== Example 2: Custom Preprocessing ===")
	fmt.Println()

	conv2, err := sdk.Open(
		"./image-preprocessing.pack.json",
		"vision-analyst",
		sdk.WithImagePreprocessing(&stage.ImagePreprocessConfig{
			Resize: stage.ImageResizeStageConfig{
				MaxWidth:  800,
				MaxHeight: 600,
				Quality:   90, // Higher quality JPEG
			},
			EnableResize: true,
		}),
	)
	if err != nil {
		log.Fatalf("Failed to open pack: %v", err)
	}
	defer conv2.Close()

	// Another high-resolution image
	photoURL := "https://upload.wikimedia.org/wikipedia/commons/thumb/1/1e/Sunrise_over_the_sea.jpg/1280px-Sunrise_over_the_sea.jpg"

	fmt.Println("Analyzing with custom preprocessing (800x600 max, 90% quality)...")
	fmt.Println()

	resp2, err := conv2.Send(ctx, "What time of day is shown in this photograph? Describe the lighting.",
		sdk.WithImageURL(photoURL),
	)
	if err != nil {
		log.Printf("Error: %v", err)
	} else {
		fmt.Println(resp2.Text())
	}

	// Example 3: Streaming with preprocessing
	fmt.Println("\n=== Example 3: Streaming with Auto-Resize ===")
	fmt.Println()

	fmt.Println("Streaming analysis of preprocessed image...")
	fmt.Println()

	for chunk := range conv1.Stream(ctx, "What emotions does this artwork evoke? Be specific about visual elements that create those feelings.") {
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

	fmt.Println("\n=== Examples Complete ===")
	fmt.Println()
	fmt.Println("Image preprocessing automatically:")
	fmt.Println("  - Resizes large images to reduce API costs and latency")
	fmt.Println("  - Maintains aspect ratio during resizing")
	fmt.Println("  - Optimizes JPEG quality for vision models")
	fmt.Println("  - Works seamlessly with both Send() and Stream()")
}
