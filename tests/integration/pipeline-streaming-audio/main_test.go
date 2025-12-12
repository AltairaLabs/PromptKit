package main

import (
	"context"
	"testing"
	"time"

	"github.com/AltairaLabs/PromptKit/runtime/pipeline"
	"github.com/AltairaLabs/PromptKit/runtime/pipeline/middleware"
	"github.com/AltairaLabs/PromptKit/runtime/providers"
	"github.com/AltairaLabs/PromptKit/runtime/providers/mock"
	"github.com/AltairaLabs/PromptKit/runtime/session"
	"github.com/AltairaLabs/PromptKit/runtime/statestore"
	"github.com/AltairaLabs/PromptKit/runtime/tools"
)

// TestPipelineStreamingWithTTS tests the complete streaming flow:
// Provider generates streaming text → TTS converts to audio → Client receives both
func TestPipelineStreamingWithTTS(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// Setup: Create pipeline with mock provider and TTS
	provider := mock.NewProviderWithRepository("mock", "mock-model", false,
		mock.NewInMemoryMockRepository("Hello world"))

	stateStore := statestore.NewMemoryStore()
	stateStoreConfig := &pipeline.StateStoreConfig{
		Store:          stateStore,
		ConversationID: "test-conversation",
	}

	providerConfig := &middleware.ProviderMiddlewareConfig{
		MaxTokens:   100,
		Temperature: 0.7,
	}
	providerMw := middleware.ProviderMiddleware(provider, tools.NewRegistry(), nil, providerConfig)

	ttsService := &MockTTSService{}
	ttsConfig := middleware.TTSConfig{
		SkipEmpty:     true,
		MinTextLength: 3, // Lower threshold so it processes "Hello world"
	}
	ttsMw := middleware.NewTTSMiddleware(ttsService, ttsConfig)

	p := pipeline.NewPipeline(
		middleware.StateStoreLoadMiddleware(stateStoreConfig),
		providerMw,
		middleware.StateStoreSaveMiddleware(stateStoreConfig),
		ttsMw,
	)

	// Create session
	sess, err := session.NewBidirectionalSession(&session.BidirectionalConfig{
		ConversationID: "test-conversation",
		Pipeline:       p,
	})
	if err != nil {
		t.Fatalf("Failed to create session: %v", err)
	}
	defer sess.Close()

	// Send message to start pipeline
	if err := sess.SendText(ctx, "Say hello"); err != nil {
		t.Fatalf("Failed to send message: %v", err)
	}

	// Collect response chunks
	responseChan := sess.Response()
	var chunks []providers.StreamChunk
	textChunks := 0
	audioChunks := 0
	receivedFinish := false

	timeout := time.After(3 * time.Second)
	for {
		select {
		case chunk, ok := <-responseChan:
			if !ok {
				t.Log("Response channel closed")
				goto Done
			}

			chunks = append(chunks, chunk)

			t.Logf("Received chunk - Delta: %q, MediaDelta: %v, FinishReason: %v",
				chunk.Delta,
				chunk.MediaDelta != nil && chunk.MediaDelta.Data != nil,
				chunk.FinishReason)

			if chunk.Error != nil {
				t.Fatalf("Received error chunk: %v", chunk.Error)
			}

			if chunk.Delta != "" {
				textChunks++
				t.Logf("Received text chunk: %q", chunk.Delta)
			}

			if chunk.MediaDelta != nil && chunk.MediaDelta.Data != nil {
				audioChunks++
				t.Logf("Received audio chunk: %d bytes", len(*chunk.MediaDelta.Data))
			}

			if chunk.FinishReason != nil {
				t.Logf("Received finish: %s", *chunk.FinishReason)
				receivedFinish = true
				goto Done
			}

		case <-timeout:
			t.Fatalf("Timeout waiting for response chunks. Received %d text chunks, %d audio chunks", textChunks, audioChunks)
		}
	}

Done:
	// Assertions
	t.Logf("Total chunks: %d, Text: %d, Audio: %d", len(chunks), textChunks, audioChunks)

	if len(chunks) == 0 {
		t.Fatal("Expected to receive at least one chunk")
	}

	if textChunks == 0 {
		t.Error("Expected to receive text chunks from provider")
	}

	if audioChunks == 0 {
		t.Error("Expected to receive audio chunks from TTS middleware")
	}

	if !receivedFinish {
		t.Error("Expected to receive finish reason")
	}

	// Verify the full text was received
	fullText := ""
	for _, chunk := range chunks {
		fullText += chunk.Delta
	}

	if fullText == "" {
		t.Error("Expected non-empty text response")
	}

	t.Logf("Full response text: %q", fullText)
}
