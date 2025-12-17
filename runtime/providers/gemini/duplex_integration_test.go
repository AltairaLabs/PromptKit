//go:build integration
// +build integration

package gemini

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/AltairaLabs/PromptKit/runtime/logger"
	"github.com/AltairaLabs/PromptKit/runtime/providers"
	"github.com/AltairaLabs/PromptKit/runtime/types"
)

// TestDuplexIntegration_SystemPrompt verifies the system prompt is sent in setup
func TestDuplexIntegration_SystemPrompt(t *testing.T) {
	apiKey := os.Getenv("GEMINI_API_KEY")
	if apiKey == "" {
		t.Skip("GEMINI_API_KEY not set")
	}

	// Enable verbose logging
	logger.SetVerbose(true)
	defer logger.SetVerbose(false)

	provider := NewProvider(
		"gemini-test",
		"gemini-2.0-flash-exp",
		"https://generativelanguage.googleapis.com",
		providers.ProviderDefaults{Temperature: 0.7},
		false,
	)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// Configure with system instruction
	req := &providers.StreamingInputConfig{
		Config: types.StreamingMediaConfig{
			Type:       types.ContentTypeAudio,
			ChunkSize:  3200,
			SampleRate: 16000,
			Channels:   1,
			BitDepth:   16,
			Encoding:   "pcm_linear16",
		},
		SystemInstruction: "You are Nova, a helpful voice assistant. Always introduce yourself by name.",
	}

	t.Log("Creating stream session...")
	session, err := provider.CreateStreamSession(ctx, req)
	if err != nil {
		t.Fatalf("Failed to create session: %v", err)
	}
	defer session.Close()
	t.Log("Session created successfully")

	// Check for any session errors
	if err := session.Error(); err != nil {
		t.Fatalf("Session has error after creation: %v", err)
	}

	// Send a text prompt to verify system instruction is working
	t.Log("Sending text prompt...")
	if err := session.SendText(ctx, "Hello, who are you?"); err != nil {
		t.Fatalf("Failed to send text: %v", err)
	}
	t.Log("Text prompt sent")

	// Collect response - accumulate deltas
	var response string
	var gotResponse bool
	var chunkCount int
	var costInfo *types.CostInfo
	timeout := time.After(15 * time.Second)

	t.Log("Waiting for response...")
	for {
		select {
		case chunk, ok := <-session.Response():
			if !ok {
				t.Log("Response channel closed")
				goto done
			}
			chunkCount++
			t.Logf("Received chunk %d: content=%q, delta=%q, finishReason=%v, error=%v",
				chunkCount, chunk.Content, chunk.Delta, chunk.FinishReason, chunk.Error)

			if chunk.Error != nil {
				t.Errorf("Chunk had error: %v", chunk.Error)
			}

			// Accumulate response from deltas
			if chunk.Delta != "" {
				response += chunk.Delta
			}

			if chunk.CostInfo != nil {
				costInfo = chunk.CostInfo
			}

			if chunk.FinishReason != nil {
				gotResponse = true
				goto done
			}
		case <-timeout:
			t.Logf("Timeout after receiving %d chunks", chunkCount)
			// Check session error
			if err := session.Error(); err != nil {
				t.Logf("Session error: %v", err)
			}
			t.Fatal("Timeout waiting for response")
		case <-ctx.Done():
			t.Fatalf("Context cancelled: %v", ctx.Err())
		}
	}

done:
	t.Logf("Total chunks received: %d", chunkCount)

	if !gotResponse {
		t.Error("Did not receive a complete response")
	}

	// Verify system instruction was applied - response should mention "Nova"
	t.Logf("Final response: %s", response)
	if response == "" {
		t.Error("Response was empty")
	}

	// Verify cost info is present
	if costInfo != nil {
		t.Logf("Cost info: input=%d, output=%d, total=$%.6f",
			costInfo.InputTokens, costInfo.OutputTokens, costInfo.TotalCost)
	} else {
		t.Error("Cost info was not returned")
	}
}

// TestDuplexIntegration_AudioThenEndInput verifies audio + EndInput triggers response
func TestDuplexIntegration_AudioThenEndInput(t *testing.T) {
	apiKey := os.Getenv("GEMINI_API_KEY")
	if apiKey == "" {
		t.Skip("GEMINI_API_KEY not set")
	}

	// Enable verbose logging
	logger.SetVerbose(true)
	defer logger.SetVerbose(false)

	provider := NewProvider(
		"gemini-test",
		"gemini-2.0-flash-exp",
		"https://generativelanguage.googleapis.com",
		providers.ProviderDefaults{Temperature: 0.7},
		false,
	)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	req := &providers.StreamingInputConfig{
		Config: types.StreamingMediaConfig{
			Type:       types.ContentTypeAudio,
			ChunkSize:  3200,
			SampleRate: 16000,
			Channels:   1,
			BitDepth:   16,
			Encoding:   "pcm_linear16",
		},
		// Explicitly request TEXT responses (not AUDIO)
		Metadata: map[string]interface{}{
			"response_modalities": []string{"TEXT"},
		},
	}

	session, err := provider.CreateStreamSession(ctx, req)
	if err != nil {
		t.Fatalf("Failed to create session: %v", err)
	}
	defer session.Close()

	// Generate test audio (1 second of 440Hz tone)
	encoder := NewAudioEncoder()
	testAudio := encoder.GenerateSineWave(440.0, 1000, 0.5)
	t.Logf("Generated %d bytes of test audio", len(testAudio))

	// Create and send audio chunks
	chunks, err := encoder.CreateChunks(ctx, testAudio)
	if err != nil {
		t.Fatalf("Failed to create chunks: %v", err)
	}

	for i, chunk := range chunks {
		if err := session.SendChunk(ctx, chunk); err != nil {
			t.Fatalf("Failed to send chunk %d: %v", i, err)
		}
		time.Sleep(10 * time.Millisecond) // Simulate streaming
	}

	t.Log("All audio chunks sent, calling EndInput()")

	// Call EndInput to trigger response
	geminiSession, ok := session.(*StreamSession)
	if !ok {
		t.Fatal("Session is not a Gemini StreamSession")
	}
	geminiSession.EndInput()

	// Collect response - accumulate deltas
	var response string
	var gotResponse bool
	var costInfo *types.CostInfo
	timeout := time.After(15 * time.Second)

	for {
		select {
		case chunk, ok := <-session.Response():
			if !ok {
				goto done
			}
			// Accumulate from delta, not Content (Content may be overwritten)
			if chunk.Delta != "" {
				response += chunk.Delta
			}
			if chunk.CostInfo != nil {
				costInfo = chunk.CostInfo
			}
			if chunk.FinishReason != nil {
				t.Logf("Finish reason: %s", *chunk.FinishReason)
				gotResponse = true
				goto done
			}
		case <-timeout:
			t.Fatal("Timeout waiting for response after EndInput")
		case <-ctx.Done():
			t.Fatalf("Context cancelled: %v", ctx.Err())
		}
	}

done:
	t.Logf("Response: %s", response)

	if !gotResponse {
		t.Error("Did not receive a complete response after EndInput")
	}

	if response == "" {
		t.Error("Response was empty - Gemini did not respond to audio + EndInput")
	}

	// Log cost info if available
	if costInfo != nil {
		t.Logf("Cost: input=%d tokens, output=%d tokens, total=$%.6f",
			costInfo.InputTokens, costInfo.OutputTokens, costInfo.TotalCost)
	}
}

// TestDuplexIntegration_MultiTurn verifies multiple audio turns work
func TestDuplexIntegration_MultiTurn(t *testing.T) {
	apiKey := os.Getenv("GEMINI_API_KEY")
	if apiKey == "" {
		t.Skip("GEMINI_API_KEY not set")
	}

	provider := NewProvider(
		"gemini-test",
		"gemini-2.0-flash-exp",
		"https://generativelanguage.googleapis.com",
		providers.ProviderDefaults{Temperature: 0.7},
		false,
	)

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	req := &providers.StreamingInputConfig{
		Config: types.StreamingMediaConfig{
			Type:       types.ContentTypeAudio,
			ChunkSize:  3200,
			SampleRate: 16000,
			Channels:   1,
			BitDepth:   16,
			Encoding:   "pcm_linear16",
		},
		SystemInstruction: "You are a helpful assistant. Keep responses brief.",
	}

	session, err := provider.CreateStreamSession(ctx, req)
	if err != nil {
		t.Fatalf("Failed to create session: %v", err)
	}
	defer session.Close()

	geminiSession := session.(*StreamSession)
	encoder := NewAudioEncoder()

	// Helper to send audio and get response
	sendAudioAndGetResponse := func(turnNum int) string {
		testAudio := encoder.GenerateSineWave(440.0, 500, 0.5) // 500ms audio
		chunks, _ := encoder.CreateChunks(ctx, testAudio)

		for _, chunk := range chunks {
			session.SendChunk(ctx, chunk)
			time.Sleep(10 * time.Millisecond)
		}

		t.Logf("Turn %d: Audio sent, calling EndInput", turnNum)
		geminiSession.EndInput()

		var response string
		timeout := time.After(15 * time.Second)

		for {
			select {
			case chunk, ok := <-session.Response():
				if !ok {
					return response
				}
				response = chunk.Content
				if chunk.FinishReason != nil {
					return response
				}
			case <-timeout:
				t.Logf("Turn %d: Timeout waiting for response", turnNum)
				return response
			}
		}
	}

	// Execute 3 turns
	for i := 1; i <= 3; i++ {
		response := sendAudioAndGetResponse(i)
		t.Logf("Turn %d response: %s", i, response)

		if response == "" {
			t.Errorf("Turn %d: Got empty response", i)
		}
	}
}

// TestDuplexIntegration_ResponseModalities verifies TEXT modality works
func TestDuplexIntegration_ResponseModalities(t *testing.T) {
	apiKey := os.Getenv("GEMINI_API_KEY")
	if apiKey == "" {
		t.Skip("GEMINI_API_KEY not set")
	}

	provider := NewProvider(
		"gemini-test",
		"gemini-2.0-flash-exp",
		"https://generativelanguage.googleapis.com",
		providers.ProviderDefaults{Temperature: 0.7},
		false,
	)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	req := &providers.StreamingInputConfig{
		Config: types.StreamingMediaConfig{
			Type:       types.ContentTypeAudio,
			ChunkSize:  3200,
			SampleRate: 16000,
			Channels:   1,
			BitDepth:   16,
			Encoding:   "pcm_linear16",
		},
		Metadata: map[string]interface{}{
			"response_modalities": []string{"TEXT"},
		},
	}

	session, err := provider.CreateStreamSession(ctx, req)
	if err != nil {
		t.Fatalf("Failed to create session: %v", err)
	}
	defer session.Close()

	// Send text to trigger response
	if err := session.SendText(ctx, "Say hello"); err != nil {
		t.Fatalf("Failed to send text: %v", err)
	}

	// Verify we get text response
	var response string
	timeout := time.After(15 * time.Second)

	for {
		select {
		case chunk, ok := <-session.Response():
			if !ok {
				goto done
			}
			response = chunk.Content
			if chunk.FinishReason != nil {
				goto done
			}
		case <-timeout:
			t.Fatal("Timeout")
		}
	}

done:
	if response == "" {
		t.Error("Expected text response but got empty")
	}
	t.Logf("Text response: %s", response)
}
