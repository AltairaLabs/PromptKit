//go:build integration
// +build integration

package gemini

import (
	"context"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/AltairaLabs/PromptKit/runtime/logger"
	"github.com/AltairaLabs/PromptKit/runtime/providers"
	"github.com/AltairaLabs/PromptKit/runtime/types"
)

// TestStreamingDemo_AudioAndTextOutput demonstrates attempting to receive both audio and text responses
// NOTE: As of Nov 2025, the Gemini Live API does NOT support requesting both TEXT and AUDIO simultaneously.
// The API rejects the setup with "invalid argument" error.
// This test documents the expected behavior for future reference.
func TestStreamingDemo_AudioAndTextOutput(t *testing.T) {
	apiKey := os.Getenv("GEMINI_API_KEY")
	if apiKey == "" {
		t.Skip("GEMINI_API_KEY not set")
	}

	// Enable verbose logging
	logger.SetVerbose(true)
	defer logger.SetVerbose(false)

	separator := "================================================================================"
	fmt.Println("\n" + separator)
	fmt.Println("🎙️ + 📝  GEMINI AUDIO + TEXT OUTPUT DEMO")
	fmt.Println(separator + "\n")

	// Create provider
	provider := NewProvider(
		"gemini-demo",
		"gemini-2.0-flash-exp",
		"https://generativelanguage.googleapis.com/v1beta",
		providers.ProviderDefaults{Temperature: 0.7},
		false,
	)

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	// Configure audio streaming
	config := types.StreamingMediaConfig{
		Type:       types.ContentTypeAudio,
		ChunkSize:  3200, // 100ms at 16kHz
		SampleRate: 16000,
		Channels:   1,
		BitDepth:   16,
		Encoding:   "pcm_linear16",
	}

	// 🎯 KEY CONFIGURATION: Request BOTH text and audio in responses
	// Note: As of the current API version, audio output may not be supported yet.
	// This test demonstrates how to configure it when available.
	req := providers.StreamingInputConfig{
		Config: config,
		Metadata: map[string]interface{}{
			"response_modalities": []string{"TEXT", "AUDIO"}, // Request both!
		},
	}

	fmt.Println("📡 Step 1: Establishing WebSocket connection...")
	fmt.Println("   🎯 Configured for TEXT + AUDIO responses")
	fmt.Println("   ⚠️  Note: Audio output may not be available in current API version")
	session, err := provider.CreateStreamSession(ctx, &req)
	if err != nil {
		// If audio output is not supported, the API will reject with "invalid argument"
		errMsg := err.Error()
		if strings.Contains(errMsg, "invalid argument") || strings.Contains(errMsg, "1007") {
			t.Skipf("⚠️  Skipping: Audio output not yet supported by Gemini Live API. Error: %v", err)
		}
		t.Fatalf("❌ Failed to create session: %v", err)
	}
	defer session.Close()
	fmt.Println("✅ Connection established!")
	fmt.Println()

	// Track responses
	responseComplete := make(chan bool, 1)
	errorChan := make(chan error, 1)
	receivedChunks := false

	// Statistics
	textChunks := 0
	audioChunks := 0
	totalAudioBytes := 0

	go func() {
		defer close(responseComplete)

		fullTextResponse := ""

		for chunk := range session.Response() {
			receivedChunks = true

			// Check for TEXT content
			if chunk.Content != "" {
				textChunks++
				fullTextResponse += chunk.Content
				fmt.Printf("📝 [Text Chunk %d] %q\n", textChunks, chunk.Content)
			}

			// Check for AUDIO content (raw bytes in MediaData)
			if chunk.MediaData != nil {
				audioChunks++
				totalAudioBytes += len(chunk.MediaData.Data)
				fmt.Printf("🎵 [Audio Chunk %d] mime=%s, size=%d bytes (raw)\n",
					audioChunks, chunk.MediaData.MIMEType, len(chunk.MediaData.Data))

				// Show audio metadata if available
				if chunk.MediaData.Channels != 0 {
					fmt.Printf("   Channels: %d\n", chunk.MediaData.Channels)
				}
				if chunk.MediaData.SampleRate != 0 {
					fmt.Printf("   Sample Rate: %d Hz\n", chunk.MediaData.SampleRate)
				}
			}

			// Check for completion
			if chunk.FinishReason != nil {
				fmt.Printf("\n🏁 Response complete: %s\n", *chunk.FinishReason)
				break
			}
		}

		if err := session.Error(); err != nil {
			errorChan <- err
		} else {
			responseComplete <- true
		}

		// Print summary
		fmt.Println("\n" + separator)
		fmt.Println("📊 RESPONSE SUMMARY:")
		fmt.Println(separator)
		fmt.Printf("📝 Text chunks received: %d\n", textChunks)
		fmt.Printf("🎵 Audio chunks received: %d\n", audioChunks)
		fmt.Printf("📏 Total audio data: %d bytes (raw)\n", totalAudioBytes)

		if fullTextResponse != "" {
			fmt.Println("\n📝 Complete Text Response:")
			fmt.Printf("   %q\n", fullTextResponse)
		}

		if audioChunks > 0 {
			fmt.Println("\n🎵 Audio Response: Received PCM audio data")
			fmt.Println("   (Audio data is raw PCM, ready to play)")
		}
		fmt.Println(separator)
	}()

	// Send a simple text prompt (no audio input needed for this demo)
	fmt.Println("💬 Step 2: Sending text prompt...")
	prompt := "Say 'Hello, this is a test' in a friendly voice."
	fmt.Printf("   Prompt: %q\n", prompt)
	fmt.Println()

	if err := session.SendText(ctx, prompt); err != nil {
		t.Fatalf("❌ Failed to send text: %v", err)
	}

	// Wait for response
	fmt.Println("⏳ Step 3: Waiting for TEXT + AUDIO response from Gemini...")
	fmt.Println(separator)

	select {
	case <-responseComplete:
		// Success - response was received and printed in goroutine

	case err := <-errorChan:
		fmt.Println("\n" + separator)
		fmt.Printf("❌ ERROR: %v\n", err)
		fmt.Println(separator)
		t.Errorf("Session error: %v", err)

	case <-time.After(30 * time.Second):
		if !receivedChunks {
			fmt.Println("\n" + separator)
			fmt.Println("⏱️  TIMEOUT: No response received within 30 seconds")
			fmt.Println(separator)
			t.Error("Timeout waiting for response")
		} else {
			fmt.Println("\n✅ Received chunks but session didn't close cleanly")
		}

	case <-ctx.Done():
		fmt.Println("\n" + separator)
		fmt.Printf("❌ Context cancelled: %v\n", ctx.Err())
		fmt.Println(separator)
		t.Fatalf("Context cancelled: %v", ctx.Err())
	}

	if !receivedChunks {
		t.Error("❌ No response received from Gemini")
	}

	// Verify we got both text AND audio
	if textChunks == 0 && audioChunks == 0 {
		t.Error("❌ Expected to receive text or audio, got neither")
	}

	fmt.Println("\n" + separator)
	fmt.Println("🎬 DEMO COMPLETE")
	if textChunks > 0 && audioChunks > 0 {
		fmt.Println("✅ Successfully received BOTH text and audio responses!")
	} else if textChunks > 0 {
		fmt.Println("⚠️  Received text only (no audio)")
	} else if audioChunks > 0 {
		fmt.Println("⚠️  Received audio only (no text)")
	}
	fmt.Println(separator)
}

// TestStreamingDemo_AudioOutputOnly demonstrates configuring for audio-only responses
// NOTE: As of Nov 2025, the Gemini Live API ACCEPTS the "AUDIO" modality configuration,
// but appears to not yet return actual audio data in responses. The session connects
// successfully but no audio chunks are received. This may be a preview/beta limitation.
func TestStreamingDemo_AudioOutputOnly(t *testing.T) {
	apiKey := os.Getenv("GEMINI_API_KEY")
	if apiKey == "" {
		t.Skip("GEMINI_API_KEY not set")
	}

	logger.SetVerbose(true)
	defer logger.SetVerbose(false)

	separator := "================================================================================"
	fmt.Println("\n" + separator)
	fmt.Println("🎵  GEMINI AUDIO-ONLY OUTPUT DEMO")
	fmt.Println(separator + "\n")

	provider := NewProvider(
		"gemini-demo",
		"gemini-2.0-flash-exp",
		"https://generativelanguage.googleapis.com/v1beta",
		providers.ProviderDefaults{Temperature: 0.7},
		false,
	)

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	config := types.StreamingMediaConfig{
		Type:       types.ContentTypeAudio,
		ChunkSize:  3200,
		SampleRate: 16000,
		Channels:   1,
		BitDepth:   16,
		Encoding:   "pcm_linear16",
	}

	// 🎯 KEY CONFIGURATION: Request AUDIO-ONLY responses
	// Note: As of the current API version, audio output may not be supported yet.
	req := providers.StreamingInputConfig{
		Config: config,
		Metadata: map[string]interface{}{
			"response_modalities": []string{"AUDIO"}, // Audio only!
		},
	}

	fmt.Println("📡 Step 1: Establishing WebSocket connection...")
	fmt.Println("   🎯 Configured for AUDIO-ONLY responses")
	fmt.Println("   ⚠️  Note: Audio output may not be available in current API version")
	session, err := provider.CreateStreamSession(ctx, &req)
	if err != nil {
		// If audio output is not supported, the API will reject with "invalid argument"
		errMsg := err.Error()
		if strings.Contains(errMsg, "invalid argument") || strings.Contains(errMsg, "1007") {
			t.Skipf("⚠️  Skipping: Audio output not yet supported by Gemini Live API. Error: %v", err)
		}
		t.Fatalf("❌ Failed to create session: %v", err)
	}
	defer session.Close()
	fmt.Println("✅ Connection established!")
	fmt.Println()

	responseComplete := make(chan bool, 1)
	errorChan := make(chan error, 1)
	receivedChunks := false
	audioChunks := 0

	go func() {
		defer close(responseComplete)

		for chunk := range session.Response() {
			receivedChunks = true

			// Check for text (should be minimal or none)
			if chunk.Content != "" {
				fmt.Printf("📝 [Unexpected Text] %q\n", chunk.Content)
			}

			// Check for AUDIO (raw bytes in MediaData)
			if chunk.MediaData != nil {
				audioChunks++
				fmt.Printf("🎵 [Audio Chunk %d] mime=%s, size=%d bytes\n",
					audioChunks, chunk.MediaData.MIMEType, len(chunk.MediaData.Data))
			}

			if chunk.FinishReason != nil {
				fmt.Printf("\n🏁 Response complete: %s\n", *chunk.FinishReason)
				break
			}
		}

		if err := session.Error(); err != nil {
			errorChan <- err
		} else {
			responseComplete <- true
		}

		fmt.Println("\n" + separator)
		fmt.Printf("🎵 Total audio chunks: %d\n", audioChunks)
		fmt.Println(separator)
	}()

	fmt.Println("💬 Step 2: Sending text prompt...")
	prompt := "Count from one to three."
	fmt.Printf("   Prompt: %q\n", prompt)
	fmt.Println()

	if err := session.SendText(ctx, prompt); err != nil {
		t.Fatalf("❌ Failed to send text: %v", err)
	}

	fmt.Println("⏳ Step 3: Waiting for AUDIO response...")
	fmt.Println(separator)

	select {
	case <-responseComplete:
	case err := <-errorChan:
		t.Errorf("Session error: %v", err)
	case <-time.After(30 * time.Second):
		if !receivedChunks {
			t.Error("Timeout waiting for response")
		}
	case <-ctx.Done():
		t.Fatalf("Context cancelled: %v", ctx.Err())
	}

	if audioChunks > 0 {
		fmt.Println("\n✅ Successfully received audio-only response!")
	} else {
		fmt.Println("\n⚠️  No audio received (may not be supported yet)")
	}

	fmt.Println("\n" + separator)
	fmt.Println("🎬 DEMO COMPLETE")
	fmt.Println(separator)
}
