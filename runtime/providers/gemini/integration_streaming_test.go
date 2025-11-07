//go:build integration
// +build integration

package gemini

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/AltairaLabs/PromptKit/runtime/providers"
	"github.com/AltairaLabs/PromptKit/runtime/types"
)

// TestStreamingIntegration_EndToEnd tests bidirectional streaming with real Gemini API
// Run with: go test -tags=integration -run TestStreamingIntegration_EndToEnd
// Requires: GEMINI_API_KEY environment variable with Live API access enabled
//
// NOTE: The Gemini Live API is in preview and requires special access.
// If you see "API key not valid" errors, your API key may not have Live API access.
// To enable Live API access, visit: https://ai.google.dev/
func TestStreamingIntegration_EndToEnd(t *testing.T) {
	apiKey := os.Getenv("GEMINI_API_KEY")
	if apiKey == "" {
		t.Skip("Skipping integration test: GEMINI_API_KEY not set")
	}

	// Create provider
	provider := NewGeminiProvider("gemini", "gemini-2.0-flash-exp", "https://generativelanguage.googleapis.com", providers.ProviderDefaults{
		Temperature: 0.7,
	}, false)

	// Verify streaming support
	supported := provider.SupportsStreamInput()
	if len(supported) == 0 {
		t.Fatal("provider should support streaming input")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
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

	// Create stream session
	req := providers.StreamInputRequest{
		Config:    config,
		SystemMsg: "You are a helpful voice assistant. Please respond briefly.",
	}

	session, err := provider.CreateStreamSession(ctx, req)
	if err != nil {
		// Check if this is a Live API access error
		errMsg := err.Error()
		if strings.Contains(errMsg, "API key not valid") || strings.Contains(errMsg, "websocket: close 1007") {
			t.Skipf("Skipping test: API key does not have Gemini Live API access. The Live API is in preview and requires special enablement. Visit https://ai.google.dev/ to request access. Error: %v", err)
		}
		t.Fatalf("failed to create session: %v", err)
	}
	defer session.Close()

	t.Log("Session created successfully")

	// Generate test audio (1 second sine wave at 440Hz)
	encoder := NewAudioEncoder()
	testAudio := encoder.GenerateSineWave(440.0, 1000, 0.5)

	// Create chunks
	chunks, err := encoder.CreateChunks(ctx, testAudio)
	if err != nil {
		t.Fatalf("failed to create chunks: %v", err)
	}

	t.Logf("Created %d audio chunks", len(chunks))

	// Start receiving responses in background
	responseCh := session.Response()
	done := make(chan struct{})
	var responses []string

	go func() {
		defer close(done)
		for chunk := range responseCh {
			t.Logf("Received response chunk: %+v", chunk)
			responses = append(responses, chunk.Delta)
		}
	}()

	// Send audio chunks
	for i, chunk := range chunks {
		if err := session.SendChunk(ctx, chunk); err != nil {
			t.Fatalf("failed to send chunk %d: %v", i, err)
		}
		t.Logf("Sent chunk %d/%d", i+1, len(chunks))

		// Small delay between chunks to avoid overwhelming the API
		time.Sleep(50 * time.Millisecond)
	}

	t.Log("Audio sent, waiting for responses...")

	// Wait for responses or timeout
	select {
	case <-done:
		t.Log("All responses received")
	case <-time.After(10 * time.Second):
		t.Log("Timeout waiting for responses")
	case <-ctx.Done():
		t.Fatalf("context cancelled: %v", ctx.Err())
	}

	// Verify we got responses
	if len(responses) == 0 {
		t.Error("expected to receive responses, got none")
	} else {
		t.Logf("Received %d response chunks", len(responses))
		for i, resp := range responses {
			t.Logf("Response %d: %s", i, resp)
		}
	}

	// Check for errors
	if err := session.Error(); err != nil {
		t.Errorf("session error: %v", err)
	}

	// Close session
	if err := session.Close(); err != nil {
		t.Errorf("failed to close session: %v", err)
	}

	t.Log("Integration test completed successfully")
}

// TestStreamingIntegration_AudioRoundTrip tests sending and receiving audio
func TestStreamingIntegration_AudioRoundTrip(t *testing.T) {
	apiKey := os.Getenv("GEMINI_API_KEY")
	if apiKey == "" {
		t.Skip("Skipping integration test: GEMINI_API_KEY not set")
	}

	provider := NewGeminiProvider("gemini", "gemini-2.0-flash-exp", "https://generativelanguage.googleapis.com", providers.ProviderDefaults{}, false)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	config := types.StreamingMediaConfig{
		Type:       types.ContentTypeAudio,
		ChunkSize:  3200,
		SampleRate: 16000,
		Channels:   1,
		BitDepth:   16,
		Encoding:   "pcm_linear16",
	}

	req := providers.StreamInputRequest{
		Config: config,
	}

	session, err := provider.CreateStreamSession(ctx, req)
	if err != nil {
		// Check if this is a Live API access error
		errMsg := err.Error()
		if strings.Contains(errMsg, "API key not valid") || strings.Contains(errMsg, "websocket: close 1007") {
			t.Skipf("Skipping test: API key does not have Gemini Live API access. Error: %v", err)
		}
		t.Fatalf("failed to create session: %v", err)
	}
	defer session.Close()

	// Generate and send audio
	encoder := NewAudioEncoder()
	testAudio := encoder.GenerateSineWave(440.0, 500, 0.3)
	chunks, _ := encoder.CreateChunks(ctx, testAudio)

	for _, chunk := range chunks {
		if err := session.SendChunk(ctx, chunk); err != nil {
			t.Fatalf("send error: %v", err)
		}
	}

	// Wait briefly for response
	select {
	case <-session.Response():
		t.Log("Received audio response")
	case <-time.After(5 * time.Second):
		t.Log("No audio response within timeout")
	}
}

// TestStreamingIntegration_ErrorHandling tests various error scenarios
func TestStreamingIntegration_ErrorHandling(t *testing.T) {
	tests := []struct {
		name      string
		apiKey    string
		expectErr bool
	}{
		{
			name:      "invalid API key",
			apiKey:    "invalid-key-12345",
			expectErr: true,
		},
		{
			name:      "empty API key",
			apiKey:    "",
			expectErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create provider with test API key
			provider := &GeminiProvider{
				BaseProvider: providers.BaseProvider{},
				Model:        "gemini-2.0-flash-exp",
				BaseURL:      "https://generativelanguage.googleapis.com",
				ApiKey:       tt.apiKey,
			}

			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()

			config := types.StreamingMediaConfig{
				Type:       types.ContentTypeAudio,
				ChunkSize:  3200,
				SampleRate: 16000,
				Channels:   1,
				BitDepth:   16,
				Encoding:   "pcm_linear16",
			}

			req := providers.StreamInputRequest{
				Config: config,
			}

			_, err := provider.CreateStreamSession(ctx, req)

			if tt.expectErr && err == nil {
				t.Error("expected error, got nil")
			}

			if !tt.expectErr && err != nil {
				t.Errorf("unexpected error: %v", err)
			}
		})
	}
}

// TestStreamingIntegration_Performance measures latency
func TestStreamingIntegration_Performance(t *testing.T) {
	apiKey := os.Getenv("GEMINI_API_KEY")
	if apiKey == "" {
		t.Skip("Skipping integration test: GEMINI_API_KEY not set")
	}

	provider := NewGeminiProvider("gemini", "gemini-2.0-flash-exp", "https://generativelanguage.googleapis.com", providers.ProviderDefaults{}, false)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	config := types.StreamingMediaConfig{
		Type:       types.ContentTypeAudio,
		ChunkSize:  3200,
		SampleRate: 16000,
		Channels:   1,
		BitDepth:   16,
		Encoding:   "pcm_linear16",
	}

	req := providers.StreamInputRequest{
		Config: config,
	}

	start := time.Now()
	session, err := provider.CreateStreamSession(ctx, req)
	if err != nil {
		// Check if this is a Live API access error
		errMsg := err.Error()
		if strings.Contains(errMsg, "API key not valid") || strings.Contains(errMsg, "websocket: close 1007") {
			t.Skipf("Skipping test: API key does not have Gemini Live API access. Error: %v", err)
		}
		t.Fatalf("failed to create session: %v", err)
	}
	defer session.Close()

	connectionTime := time.Since(start)
	t.Logf("Connection time: %v", connectionTime)

	// Send audio and measure first response time
	encoder := NewAudioEncoder()
	testAudio := encoder.GenerateSineWave(440.0, 100, 0.5)
	chunks, _ := encoder.CreateChunks(ctx, testAudio)

	sendStart := time.Now()
	for _, chunk := range chunks {
		if err := session.SendChunk(ctx, chunk); err != nil {
			t.Fatalf("send error: %v", err)
		}
	}

	// Send text to trigger response
	if err := session.SendText(ctx, "Respond with OK"); err != nil {
		t.Fatalf("send text error: %v", err)
	}

	// Wait for first response
	select {
	case <-session.Response():
		firstResponseTime := time.Since(sendStart)
		t.Logf("First response time: %v", firstResponseTime)

		// Check if within acceptable latency
		if firstResponseTime > 5*time.Second {
			t.Logf("WARNING: High latency detected: %v (target: <5s)", firstResponseTime)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("timeout waiting for first response")
	}
}
