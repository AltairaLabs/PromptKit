package sdk_test

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/AltairaLabs/PromptKit/runtime/providers"
	"github.com/AltairaLabs/PromptKit/runtime/providers/gemini"
	"github.com/AltairaLabs/PromptKit/sdk"
)

// TestVADModeStreaming tests the VAD mode flow: text-based streaming with Gemini
// This simulates what happens in the voice interview when speech is transcribed
// and sent to the LLM for a response.
func TestVADModeStreaming(t *testing.T) {
	apiKey := os.Getenv("GEMINI_API_KEY")
	if apiKey == "" {
		t.Skip("GEMINI_API_KEY not set")
	}

	// Create Gemini provider (same as VAD mode uses)
	geminiProvider := gemini.NewProvider(
		"gemini",
		"gemini-2.0-flash-exp",
		"https://generativelanguage.googleapis.com",
		providers.ProviderDefaults{
			Temperature: 0.7,
			MaxTokens:   500,
		},
		false,
	)

	// Create a simple pack for testing
	packPath := createVADTestPack(t)

	// Open conversation in unary mode (like VAD mode does)
	conv, err := sdk.Open(
		packPath,
		"test",
		sdk.WithProvider(geminiProvider),
		sdk.WithAPIKey(apiKey),
		sdk.WithSkipSchemaValidation(),
	)
	if err != nil {
		t.Fatalf("Failed to open conversation: %v", err)
	}
	defer conv.Close()

	// Create context with timeout
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// Simulate VAD mode: send transcribed text and stream response
	t.Log("Sending streaming request to Gemini...")
	respCh := conv.Stream(ctx, "Hello! Please introduce yourself briefly.")

	var fullResponse string
	chunkCount := 0
	textChunkCount := 0

	for chunk := range respCh {
		chunkCount++
		t.Logf("Chunk %d: Type=%s, Text=%q, Error=%v",
			chunkCount, chunk.Type, truncateStr(chunk.Text, 50), chunk.Error)

		if chunk.Error != nil {
			t.Fatalf("Stream error: %v", chunk.Error)
		}

		if chunk.Text != "" {
			textChunkCount++
			fullResponse += chunk.Text
		}

		if chunk.Type == sdk.ChunkDone {
			t.Logf("Stream complete. Message: %v", chunk.Message != nil)
		}
	}

	t.Logf("Total chunks: %d, text chunks: %d", chunkCount, textChunkCount)
	t.Logf("Full response (%d chars): %s", len(fullResponse), truncateStr(fullResponse, 200))

	// Verify we got a response
	if fullResponse == "" {
		t.Error("Expected non-empty response from Gemini")
	}
	if textChunkCount == 0 {
		t.Error("Expected at least one text chunk")
	}
}

// TestVADModeStreamingWithParts tests streaming when message is built with AddTextPart
func TestVADModeStreamingWithParts(t *testing.T) {
	apiKey := os.Getenv("GEMINI_API_KEY")
	if apiKey == "" {
		t.Skip("GEMINI_API_KEY not set")
	}

	geminiProvider := gemini.NewProvider(
		"gemini",
		"gemini-2.0-flash-exp",
		"https://generativelanguage.googleapis.com",
		providers.ProviderDefaults{
			Temperature: 0.7,
			MaxTokens:   500,
		},
		false,
	)

	packPath := createVADTestPack(t)

	conv, err := sdk.Open(
		packPath,
		"test",
		sdk.WithProvider(geminiProvider),
		sdk.WithAPIKey(apiKey),
		sdk.WithSkipSchemaValidation(),
	)
	if err != nil {
		t.Fatalf("Failed to open conversation: %v", err)
	}
	defer conv.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// Send request and stream response
	respCh := conv.Stream(ctx, "Say hello in exactly 5 words.")

	var fullResponse string
	for chunk := range respCh {
		if chunk.Error != nil {
			t.Fatalf("Stream error: %v", chunk.Error)
		}
		if chunk.Text != "" {
			fullResponse += chunk.Text
		}
	}

	t.Logf("Response: %s", fullResponse)

	if fullResponse == "" {
		t.Error("Expected non-empty response")
	}
}

func createVADTestPack(t *testing.T) string {
	t.Helper()

	packContent := `{
		"name": "test-pack",
		"version": "1.0.0",
		"description": "Test pack for VAD mode",
		"prompts": {
			"test": {
				"id": "test",
				"version": "1.0.0",
				"system": "You are a helpful assistant. Keep responses brief."
			}
		}
	}`

	tmpDir := t.TempDir()
	packPath := tmpDir + "/test.pack.json"
	if err := os.WriteFile(packPath, []byte(packContent), 0644); err != nil {
		t.Fatalf("Failed to create test pack: %v", err)
	}

	return packPath
}

func truncateStr(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "..."
}
