// Package main demonstrates text-to-speech (TTS) capabilities in the PromptKit SDK.
//
// This example shows:
//   - Setting up a TTS service with the SDK
//   - Converting LLM responses to speech
//   - Saving audio output to files
//   - Using different voices and formats
//
// Run with:
//
//	export OPENAI_API_KEY=your-key
//	go run .
package main

import (
	"context"
	"fmt"
	"io"
	"log"
	"os"

	"github.com/AltairaLabs/PromptKit/runtime/tts"
	"github.com/AltairaLabs/PromptKit/sdk"
)

func main() {
	// Check for API key
	if os.Getenv("OPENAI_API_KEY") == "" {
		fmt.Println("⚠️  Set OPENAI_API_KEY environment variable to run this example")
		fmt.Println("   export OPENAI_API_KEY=your-key")
		os.Exit(1)
	}

	// Create TTS service (OpenAI's TTS API)
	ttsService := tts.NewOpenAI(os.Getenv("OPENAI_API_KEY"))

	// Open conversation with TTS enabled
	conv, err := sdk.Open("./assistant.pack.json", "storyteller",
		sdk.WithTTS(ttsService),
	)
	if err != nil {
		log.Fatalf("Failed to open pack: %v", err)
	}
	defer conv.Close()

	ctx := context.Background()

	fmt.Println("=== Text-to-Speech Demo ===")
	fmt.Println()

	// Ask for a short story
	fmt.Println("Requesting a short story from the LLM...")
	resp, err := conv.Send(ctx, "Tell me a very short story about a robot learning to paint. Keep it under 50 words.")
	if err != nil {
		log.Fatalf("Failed to get response: %v", err)
	}

	fmt.Println("\n📝 Story text:")
	fmt.Println(resp.Text())
	fmt.Println()

	// Convert response to speech using default voice
	fmt.Println("🔊 Converting to speech with default voice (alloy)...")
	audio, err := conv.SpeakResponse(ctx, resp)
	if err != nil {
		log.Fatalf("Failed to synthesize speech: %v", err)
	}

	// Save to file
	if err := saveAudio(audio, "story_alloy.mp3"); err != nil {
		log.Fatalf("Failed to save audio: %v", err)
	}
	fmt.Println("   Saved to: story_alloy.mp3")

	// Try a different voice - Nova (female voice)
	fmt.Println("🔊 Converting with Nova voice...")
	audio, err = conv.SpeakResponse(ctx, resp,
		sdk.WithTTSVoice(tts.VoiceNova),
	)
	if err != nil {
		log.Fatalf("Failed to synthesize speech: %v", err)
	}

	if err := saveAudio(audio, "story_nova.mp3"); err != nil {
		log.Fatalf("Failed to save audio: %v", err)
	}
	fmt.Println("   Saved to: story_nova.mp3")

	// Try with different speed
	fmt.Println("🔊 Converting with slower speed (0.8x)...")
	audio, err = conv.SpeakResponse(ctx, resp,
		sdk.WithTTSVoice(tts.VoiceOnyx),
		sdk.WithTTSSpeed(0.8),
	)
	if err != nil {
		log.Fatalf("Failed to synthesize speech: %v", err)
	}

	if err := saveAudio(audio, "story_slow.mp3"); err != nil {
		log.Fatalf("Failed to save audio: %v", err)
	}
	fmt.Println("   Saved to: story_slow.mp3")

	// High-definition model
	fmt.Println("🔊 Converting with HD model...")
	audio, err = conv.SpeakResponse(ctx, resp,
		sdk.WithTTSModel(tts.ModelTTS1HD),
		sdk.WithTTSVoice(tts.VoiceShimmer),
	)
	if err != nil {
		log.Fatalf("Failed to synthesize speech: %v", err)
	}

	if err := saveAudio(audio, "story_hd.mp3"); err != nil {
		log.Fatalf("Failed to save audio: %v", err)
	}
	fmt.Println("   Saved to: story_hd.mp3")

	fmt.Println()
	fmt.Println("✅ Done! Generated 4 audio files with different voices and settings.")
	fmt.Println("   Play them with: afplay story_alloy.mp3")
}

// saveAudio writes audio data to a file.
func saveAudio(reader io.ReadCloser, filename string) error {
	defer reader.Close()

	file, err := os.Create(filename)
	if err != nil {
		return err
	}
	defer file.Close()

	_, err = io.Copy(file, reader)
	return err
}
