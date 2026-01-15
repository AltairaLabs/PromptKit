// Package main demonstrates audio analysis capabilities with the PromptKit SDK.
//
// This example shows:
//   - Sending audio files with text prompts using WithAudioFile
//   - Using Gemini 2.5 for audio analysis and transcription
//   - Streaming audio analysis responses
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
	"os"
	"path/filepath"

	"github.com/AltairaLabs/PromptKit/sdk"
)

func main() {
	// Get API key - check both GEMINI_API_KEY and GOOGLE_API_KEY
	apiKey := os.Getenv("GEMINI_API_KEY")
	if apiKey == "" {
		apiKey = os.Getenv("GOOGLE_API_KEY")
	}
	if apiKey == "" {
		log.Fatal("Please set GEMINI_API_KEY or GOOGLE_API_KEY environment variable")
	}

	// Get the audio file path - default to harvard.wav in Downloads
	audioPath := os.Getenv("AUDIO_FILE")
	if audioPath == "" {
		homeDir, _ := os.UserHomeDir()
		audioPath = filepath.Join(homeDir, "Downloads", "harvard.wav")
	}

	// Check if the audio file exists
	if _, err := os.Stat(audioPath); os.IsNotExist(err) {
		log.Fatalf("Audio file not found: %s\nSet AUDIO_FILE env var to specify a different path", audioPath)
	}

	// Open a conversation with the audio analyst prompt using Gemini 2.5
	conv, err := sdk.Open("./audio-analysis.pack.json", "audio-analyst",
		sdk.WithModel("gemini-3-flash-preview"),
		sdk.WithAPIKey(apiKey),
	)
	if err != nil {
		log.Fatalf("Failed to open pack: %v", err)
	}
	defer conv.Close()

	ctx := context.Background()

	// Example 1: Transcribe and analyze the audio with streaming
	fmt.Println("=== Audio Analysis with Gemini 2.5 ===")
	fmt.Println()
	fmt.Printf("Analyzing: %s\n", audioPath)
	fmt.Println()

	fmt.Println("--- Transcription ---")
	fmt.Println()

	for chunk := range conv.Stream(ctx,
		"Please transcribe this audio file. Provide the exact words spoken.",
		sdk.WithAudioFile(audioPath),
	) {
		if chunk.Error != nil {
			log.Printf("Error: %v", chunk.Error)
			break
		}
		if chunk.Type == sdk.ChunkDone {
			fmt.Println()
			fmt.Println()
			break
		}
		fmt.Print(chunk.Text)
	}

	// Example 2: Follow-up analysis (conversation maintains context)
	fmt.Println("--- Audio Quality Analysis ---")
	fmt.Println()

	for chunk := range conv.Stream(ctx,
		"Now analyze the audio quality: What is the sample rate? Is there any background noise? How clear is the speech?",
	) {
		if chunk.Error != nil {
			log.Printf("Error: %v", chunk.Error)
			break
		}
		if chunk.Type == sdk.ChunkDone {
			fmt.Println()
			fmt.Println()
			break
		}
		fmt.Print(chunk.Text)
	}

	// Example 3: Speaker analysis
	fmt.Println("--- Speaker Analysis ---")
	fmt.Println()

	resp, err := conv.Send(ctx,
		"Based on the audio, can you identify any characteristics of the speaker(s)? Consider accent, gender, speaking pace, and tone.",
	)
	if err != nil {
		log.Printf("Error: %v", err)
	} else {
		fmt.Println(resp.Text())
	}

	fmt.Println()
	fmt.Println("=== Analysis Complete ===")
}
