//go:build integration

package gemini

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/AltairaLabs/PromptKit/runtime/providers"
	"github.com/AltairaLabs/PromptKit/runtime/types"
)

// TestDiagnostic_AudioModalityRawMessages dumps all raw JSON when AUDIO modality is requested
// This will show us EXACTLY what the API is sending back
func TestDiagnostic_AudioModalityRawMessages(t *testing.T) {
	apiKey := os.Getenv("GEMINI_API_KEY")
	if apiKey == "" {
		t.Skip("GEMINI_API_KEY not set")
	}

	separator := strings.Repeat("=", 80)
	fmt.Println("\n" + separator)
	fmt.Println("🔍 DIAGNOSTIC: Raw Messages with AUDIO Modality")
	fmt.Println(separator)
	fmt.Println("Requesting responseModalities: [\"AUDIO\"]")
	fmt.Println("This will show ALL raw JSON from the API")
	fmt.Println()

	provider := NewGeminiProvider(
		"gemini-diagnostic",
		"gemini-2.0-flash-exp",
		"https://generativelanguage.googleapis.com",
		providers.ProviderDefaults{Temperature: 0.7},
		false,
	)

	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()

	config := types.StreamingMediaConfig{
		Type:       types.ContentTypeAudio,
		ChunkSize:  3200,
		SampleRate: 16000,
		Channels:   1,
		BitDepth:   16,
		Encoding:   "pcm_linear16",
	}

	// Request AUDIO modality
	req := providers.StreamInputRequest{
		Config:    config,
		SystemMsg: "You are a helpful assistant.",
		Metadata: map[string]interface{}{
			"response_modalities": []string{"AUDIO"}, // 🎯 AUDIO only
		},
	}

	fmt.Println("📡 Creating session with AUDIO modality...")
	session, err := provider.CreateStreamSession(ctx, &req)
	if err != nil {
		if strings.Contains(err.Error(), "API key not valid") ||
			strings.Contains(err.Error(), "policy violation") ||
			strings.Contains(err.Error(), "invalid argument") {
			t.Skipf("⚠️  Skipping: %v", err)
		}
		t.Fatalf("Failed to create stream session: %v", err)
	}
	defer session.Close()
	fmt.Println("✅ Session created!")
	fmt.Println()

	// Cast to GeminiStreamSession so we can intercept raw messages
	geminiSession, ok := session.(*GeminiStreamSession)
	if !ok {
		t.Fatal("Session is not a GeminiStreamSession")
	}

	chunkCount := 0
	rawMessageCount := 0
	hasAudioData := false
	done := make(chan bool)

	// Intercept and log raw websocket messages before they're processed
	// We'll do this by creating a goroutine that reads directly from websocket
	go func() {
		defer close(done)

		time.Sleep(1 * time.Second) // Give receiver loop time to start

		// Instead of competing with receiveLoop, let's just consume from Response() channel
		// But also enable verbose logging to see what we're actually receiving
		for chunk := range session.Response() {
			chunkCount++

			fmt.Printf("\n%s\n", strings.Repeat("=", 80))
			fmt.Printf("📦 Chunk #%d\n", chunkCount)
			fmt.Printf("%s\n", strings.Repeat("=", 80))

			// Log the raw chunk structure
			fmt.Printf("Content length: %d\n", len(chunk.Content))
			fmt.Printf("Content: %q\n", chunk.Content)
			fmt.Printf("Delta: %q\n", chunk.Delta)

			// Check for audio in MediaDelta (first-class field)
			if chunk.MediaDelta != nil {
				hasAudioData = true
				fmt.Println("🎵🎵🎵 AUDIO DATA FOUND! 🎵🎵🎵")
				fmt.Printf("   MIME Type: %s\n", chunk.MediaDelta.MIMEType)
				if chunk.MediaDelta.Data != nil {
					audioData := *chunk.MediaDelta.Data
					fmt.Printf("   Data length: %d bytes\n", len(audioData))
					if len(audioData) > 0 {
						fmt.Printf("   First 50 chars: %q\n", audioData[:min(50, len(audioData))])
					}
				}
				if chunk.MediaDelta.Channels != nil {
					fmt.Printf("   Channels: %d\n", *chunk.MediaDelta.Channels)
				}
				if chunk.MediaDelta.BitRate != nil {
					fmt.Printf("   Sample Rate: %d Hz\n", *chunk.MediaDelta.BitRate)
				}
			}

			if chunk.Metadata != nil {
				fmt.Printf("Metadata keys: %d\n", len(chunk.Metadata))
				for key, val := range chunk.Metadata {
					fmt.Printf("   - %s: %v\n", key, val)
				}
			} else {
				fmt.Println("Metadata: nil")
			}

			if chunk.FinishReason != nil {
				fmt.Printf("🏁 Finish Reason: %s\n", *chunk.FinishReason)
				break
			}
		}

		if err := session.Error(); err != nil {
			fmt.Printf("\n❌ Session error: %v\n", err)
		}
	}() // Also tap into the raw websocket to see what's REALLY coming through
	go func() {
		// Wait a bit for session to be ready
		time.Sleep(2 * time.Second)

		// Try to read one raw message to see what the API is actually sending
		fmt.Println("\n🔍 Attempting to capture raw WebSocket message...")
		fmt.Println("(Note: This may fail if receiveLoop already consumed it)")

		var rawMsg json.RawMessage
		readCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
		defer cancel()

		if err := geminiSession.ws.Receive(readCtx, &rawMsg); err != nil {
			fmt.Printf("⚠️  Could not capture raw message (expected - receiveLoop got it): %v\n", err)
		} else {
			rawMessageCount++
			var prettyJSON map[string]interface{}
			if err := json.Unmarshal(rawMsg, &prettyJSON); err == nil {
				prettyBytes, _ := json.MarshalIndent(prettyJSON, "", "  ")
				fmt.Printf("\n🔍 RAW MESSAGE CAPTURED:\n%s\n", string(prettyBytes))
			}
		}
	}() // Wait for setup
	time.Sleep(500 * time.Millisecond)

	// Send text message
	fmt.Println("\n📤 Sending text: 'What is 2+2? Please answer briefly.'")

	if err := session.SendText(ctx, "What is 2+2? Please answer briefly."); err != nil {
		t.Fatalf("Failed to send text: %v", err)
	}

	// Wait for completion
	select {
	case <-done:
		fmt.Println("\n✅ Response stream completed")
	case <-time.After(35 * time.Second):
		fmt.Println("\n⏱️  Timeout (may have captured some messages)")
	}

	fmt.Println("\n" + separator)
	fmt.Printf("📊 Total chunks received: %d\n", chunkCount)
	if hasAudioData {
		fmt.Println("🎵 AUDIO DATA WAS FOUND!")
	} else {
		fmt.Println("⚠️  No audio data found in any chunks")
	}
	fmt.Println(separator)
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
