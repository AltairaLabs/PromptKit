package main

import (
	"context"
	"testing"

	"github.com/AltairaLabs/PromptKit/runtime/pipeline"
	"github.com/AltairaLabs/PromptKit/runtime/pipeline/middleware"
	"github.com/AltairaLabs/PromptKit/runtime/providers"
)

// TestTTSMiddlewareStreamChunk tests that TTS middleware adds audio to chunks
func TestTTSMiddlewareStreamChunk(t *testing.T) {
	ttsService := &MockTTSService{}
	ttsConfig := middleware.TTSConfig{
		SkipEmpty:     true,
		MinTextLength: 3,
	}
	ttsMw := middleware.NewTTSMiddleware(ttsService, ttsConfig)

	// Create execution context
	execCtx := &pipeline.ExecutionContext{
		Context:    context.Background(),
		StreamMode: true,
	}

	// Create a chunk with text
	chunk := &providers.StreamChunk{
		Delta:   "Hello world",
		Content: "Hello world",
	}

	// Call StreamChunk hook
	err := ttsMw.StreamChunk(execCtx, chunk)
	if err != nil {
		t.Fatalf("StreamChunk returned error: %v", err)
	}

	// Check that audio was added
	if chunk.MediaDelta == nil {
		t.Fatal("Expected MediaDelta to be set")
	}

	if chunk.MediaDelta.Data == nil {
		t.Fatal("Expected MediaDelta.Data to be set")
	}

	t.Logf("Audio added: %d bytes", len(*chunk.MediaDelta.Data))
	t.Logf("MIME type: %s", chunk.MediaDelta.MIMEType)
}
