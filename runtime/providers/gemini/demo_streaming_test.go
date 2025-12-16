//go:build integration
// +build integration

package gemini

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/AltairaLabs/PromptKit/runtime/logger"
	"github.com/AltairaLabs/PromptKit/runtime/providers"
	"github.com/AltairaLabs/PromptKit/runtime/types"
)

// TestStreamingDemo_RealAPI demonstrates the complete streaming audio pipeline
// INPUT → TRANSFORMATION → OUTPUT
func TestStreamingDemo_RealAPI(t *testing.T) {
	apiKey := os.Getenv("GEMINI_API_KEY")
	if apiKey == "" {
		t.Skip("GEMINI_API_KEY not set")
	}

	// Enable verbose logging to see everything
	logger.SetVerbose(true)
	defer logger.SetVerbose(false)

	separator := "================================================================================"
	fmt.Println("\n" + separator)
	fmt.Println("🎙️  GEMINI LIVE API STREAMING DEMO")
	fmt.Println(separator + "\n")

	// Create provider
	provider := NewProvider(
		"gemini-demo",
		"gemini-2.0-flash-exp",
		"https://generativelanguage.googleapis.com",
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

	req := providers.StreamInputRequest{
		Config: config,
	}

	fmt.Println("📡 Step 1: Establishing WebSocket connection to Gemini Live API...")
	session, err := provider.CreateStreamSession(ctx, &req)
	if err != nil {
		t.Fatalf("❌ Failed to create session: %v", err)
	}
	defer session.Close()
	fmt.Println("✅ Connection established!")
	fmt.Println()

	// Start receiving responses in a goroutine
	responseComplete := make(chan bool, 1)
	errorChan := make(chan error, 1)
	receivedChunks := false

	go func() {
		defer close(responseComplete)

		fullResponse := ""
		chunkCount := 0
		audioChunks := 0

		for chunk := range session.Response() {
			chunkCount++
			fullResponse += chunk.Content
			receivedChunks = true

			// Check for audio in MediaDelta (first-class field)
			if chunk.MediaDelta != nil {
				audioChunks++
				audioData := *chunk.MediaDelta.Data // Base64 string
				fmt.Printf("🎵 [Chunk %d] Received AUDIO: mime=%s, size=%d bytes (base64)\n",
					chunkCount, chunk.MediaDelta.MIMEType, len(audioData))
			}

			if chunk.Content != "" {
				fmt.Printf("📥 [Chunk %d] Received TEXT: %q\n", chunkCount, chunk.Content)
			}

			if chunk.FinishReason != nil {
				fmt.Printf("🏁 Finish reason: %s\n", *chunk.FinishReason)
				if audioChunks > 0 {
					fmt.Printf("🎵 Total audio chunks received: %d\n", audioChunks)
				}
				break
			}
		}

		if err := session.Error(); err != nil {
			errorChan <- err
		} else {
			responseComplete <- true
		}

		// Print final response
		if fullResponse != "" {
			fmt.Println("\n" + separator)
			fmt.Println("✅ SUCCESS! Complete response from Gemini:")
			fmt.Println(separator)
			fmt.Printf("\n%s\n", fullResponse)
			fmt.Println(separator)
		}
	}() // INPUT: Generate test audio (440Hz sine wave = musical note A)
	fmt.Println("🎵 Step 2: Generating INPUT audio...")
	fmt.Println("   - Frequency: 440 Hz (musical note A)")
	fmt.Println("   - Duration: 1 second")
	fmt.Println("   - Format: 16-bit PCM, 16kHz mono")

	encoder := NewAudioEncoder()
	testAudio := encoder.GenerateSineWave(440.0, 1000, 0.5)
	fmt.Printf("   - Generated: %d bytes of PCM data\n\n", len(testAudio))

	// TRANSFORMATION: Create chunks and send to Gemini
	fmt.Println("📤 Step 3: TRANSFORMATION - Chunking and sending to Gemini...")
	chunks, err := encoder.CreateChunks(ctx, testAudio)
	if err != nil {
		t.Fatalf("❌ Failed to create chunks: %v", err)
	}
	fmt.Printf("   - Split into %d chunks\n", len(chunks))

	for i, chunk := range chunks {
		// Show what we're sending
		base64Data, _ := encoder.EncodePCM(chunk.Data)
		fmt.Printf("   [%d/%d] Sending chunk: %d bytes PCM → %d bytes base64\n",
			i+1, len(chunks), len(chunk.Data), len(base64Data))

		if err := session.SendChunk(ctx, chunk); err != nil {
			t.Fatalf("❌ Failed to send chunk %d: %v", i, err)
		}

		time.Sleep(50 * time.Millisecond) // Simulate real-time streaming
	}
	fmt.Println("   ✅ All audio chunks sent!")
	fmt.Println()

	// Send a text prompt to get a response
	fmt.Println("💬 Step 4: Sending text prompt to trigger response...")
	prompt := "I just sent you audio. Please describe what you heard in one sentence."
	fmt.Printf("   Prompt: %q\n", prompt)
	fmt.Println()

	if err := session.SendText(ctx, prompt); err != nil {
		t.Fatalf("❌ Failed to send text: %v", err)
	}

	// OUTPUT: Wait for response
	fmt.Println("⏳ Step 5: Waiting for OUTPUT from Gemini...")
	fmt.Println(separator)

	// Simple wait for completion or error
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
	} // Final status
	if err := session.Error(); err != nil {
		fmt.Printf("\n⚠️  Session had errors: %v\n", err)
	}

	fmt.Println("\n" + separator)
	fmt.Println("🎬 DEMO COMPLETE")
	fmt.Println(separator)
}
