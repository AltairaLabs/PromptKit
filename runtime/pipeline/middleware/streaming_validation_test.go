package middleware

import (
	"context"
	"strings"
	"testing"

	"github.com/AltairaLabs/PromptKit/runtime/pipeline"
	"github.com/AltairaLabs/PromptKit/runtime/providers"
	"github.com/AltairaLabs/PromptKit/runtime/types"
	"github.com/AltairaLabs/PromptKit/runtime/validators"
)

// TestDynamicValidatorMiddleware_StreamChunk_LengthExceeded tests length validator aborts when limit exceeded
func TestDynamicValidatorMiddleware_StreamChunk_LengthExceeded(t *testing.T) {
	registry := validators.NewRegistry()
	middleware := DynamicValidatorMiddleware(registry)

	// Create execution context
	execCtx := &pipeline.ExecutionContext{
		Context:  context.Background(),
		Messages: []types.Message{},
		Metadata: make(map[string]interface{}),
	}

	// Set up validator configs (as prompt assembly would do)
	execCtx.Metadata["validator_configs"] = []validators.ValidatorConfig{
		{
			Type: "max_length",
			Params: map[string]interface{}{
				"max_characters": 100,
				"max_tokens":     25,
			},
		},
	}

	// Run Process() to initialize validators
	err := middleware.Process(execCtx, func() error {
		return nil // No-op next
	})

	if err != nil {
		t.Fatalf("Process() failed: %v", err)
	}

	// Now test StreamChunk with chunks
	// Chunk 1 - under limit, should pass
	chunk1 := &providers.StreamChunk{
		Content:    "Short content that is under the limit.",
		TokenCount: 10,
	}

	err = middleware.StreamChunk(execCtx, chunk1)
	if err != nil {
		t.Fatalf("Chunk 1 should pass, got error: %v", err)
	}

	// Chunk 2 - over limit, should fail
	chunk2 := &providers.StreamChunk{
		Content:    "This is a much longer piece of content that definitely exceeds the 100 character limit that we set for the validator.",
		TokenCount: 30,
	}

	err = middleware.StreamChunk(execCtx, chunk2)
	if err == nil {
		t.Fatal("Chunk 2 should fail validation, got nil error")
	}

	// Verify stream was interrupted
	if !execCtx.StreamInterrupted {
		t.Error("Expected StreamInterrupted to be true")
	}

	t.Logf("✓ Length validator correctly aborted stream: %v", err)
}

// TestDynamicValidatorMiddleware_StreamChunk_BannedWords tests banned words detection
func TestDynamicValidatorMiddleware_StreamChunk_BannedWords(t *testing.T) {
	registry := validators.NewRegistry()
	middleware := DynamicValidatorMiddleware(registry)

	execCtx := &pipeline.ExecutionContext{
		Context:  context.Background(),
		Messages: []types.Message{},
		Metadata: make(map[string]interface{}),
	}

	execCtx.Metadata["validator_configs"] = []validators.ValidatorConfig{
		{
			Type: "banned_words",
			Params: map[string]interface{}{
				"words": []interface{}{"forbidden", "banned"},
			},
		},
	}

	err := middleware.Process(execCtx, func() error {
		return nil
	})

	if err != nil {
		t.Fatalf("Process() failed: %v", err)
	}

	// Clean chunk - should pass
	chunk1 := &providers.StreamChunk{
		Content: "This is acceptable content.",
	}

	err = middleware.StreamChunk(execCtx, chunk1)
	if err != nil {
		t.Fatalf("Clean chunk should pass, got error: %v", err)
	}

	// Chunk with banned word - should fail
	chunk2 := &providers.StreamChunk{
		Content: "This is acceptable content. But this word is forbidden!",
	}

	err = middleware.StreamChunk(execCtx, chunk2)
	if err == nil {
		t.Fatal("Chunk with banned word should fail, got nil error")
	}

	if !execCtx.StreamInterrupted {
		t.Error("Expected StreamInterrupted to be true")
	}

	t.Logf("✓ Banned words validator correctly detected violation: %v", err)
}

// TestDynamicValidatorMiddleware_StreamChunk_NoValidators tests that no validators means no errors
func TestDynamicValidatorMiddleware_StreamChunk_NoValidators(t *testing.T) {
	registry := validators.NewRegistry()
	middleware := DynamicValidatorMiddleware(registry)

	execCtx := &pipeline.ExecutionContext{
		Context:  context.Background(),
		Messages: []types.Message{},
		Metadata: make(map[string]interface{}),
	}

	// No validator configs - should pass everything

	err := middleware.Process(execCtx, func() error {
		return nil
	})

	if err != nil {
		t.Fatalf("Process() failed: %v", err)
	}

	// Any chunk should pass
	chunk := &providers.StreamChunk{
		Content: "Any content is fine without validators, even very long content repeated many times. " +
			strings.Repeat("More content. ", 100),
	}

	err = middleware.StreamChunk(execCtx, chunk)
	if err != nil {
		t.Fatalf("Should not error without validators, got: %v", err)
	}

	t.Log("✓ No validators case works correctly")
}

// TestDynamicValidatorMiddleware_StreamChunk_MultipleValidators tests multiple validators work together
func TestDynamicValidatorMiddleware_StreamChunk_MultipleValidators(t *testing.T) {
	registry := validators.NewRegistry()
	middleware := DynamicValidatorMiddleware(registry)

	execCtx := &pipeline.ExecutionContext{
		Context:  context.Background(),
		Messages: []types.Message{},
		Metadata: make(map[string]interface{}),
	}

	execCtx.Metadata["validator_configs"] = []validators.ValidatorConfig{
		{
			Type: "max_length",
			Params: map[string]interface{}{
				"max_characters": 200,
			},
		},
		{
			Type: "banned_words",
			Params: map[string]interface{}{
				"words": []interface{}{"forbidden"},
			},
		},
	}

	err := middleware.Process(execCtx, func() error {
		return nil
	})

	if err != nil {
		t.Fatalf("Process() failed: %v", err)
	}

	// Chunk that passes both
	chunk1 := &providers.StreamChunk{
		Content: "Short and clean.",
	}

	err = middleware.StreamChunk(execCtx, chunk1)
	if err != nil {
		t.Fatalf("Clean chunk should pass both validators, got: %v", err)
	}

	// Reset interrupt flag for next test
	execCtx.StreamInterrupted = false

	// Chunk that fails banned words (should catch before length)
	chunk2 := &providers.StreamChunk{
		Content: "This contains a forbidden word.",
	}

	err = middleware.StreamChunk(execCtx, chunk2)
	if err == nil {
		t.Fatal("Should fail on banned word")
	}

	t.Logf("✓ Multiple validators work correctly: %v", err)
}

// TestDynamicValidatorMiddleware_StreamChunk_WithSuppression tests streaming validation with suppression enabled
func TestDynamicValidatorMiddleware_StreamChunk_WithSuppression(t *testing.T) {
	registry := validators.NewRegistry()
	middleware := DynamicValidatorMiddlewareWithSuppression(registry, true)

	execCtx := &pipeline.ExecutionContext{
		Context:  context.Background(),
		Messages: []types.Message{},
		Metadata: make(map[string]interface{}),
	}

	execCtx.Metadata["validator_configs"] = []validators.ValidatorConfig{
		{
			Type: "banned_words",
			Params: map[string]interface{}{
				"words": []interface{}{"forbidden", "banned"},
			},
		},
	}

	err := middleware.Process(execCtx, func() error {
		return nil
	})

	if err != nil {
		t.Fatalf("Process() failed: %v", err)
	}

	// Clean chunk - should pass
	chunk1 := &providers.StreamChunk{
		Content: "This is acceptable content.",
	}

	err = middleware.StreamChunk(execCtx, chunk1)
	if err != nil {
		t.Fatalf("Clean chunk should pass, got error: %v", err)
	}

	// Chunk with banned word - should NOT throw error but SHOULD interrupt stream
	chunk2 := &providers.StreamChunk{
		Content: "This is acceptable content. But this word is forbidden!",
	}

	err = middleware.StreamChunk(execCtx, chunk2)
	if err != nil {
		t.Fatalf("With suppression enabled, chunk should not throw error, got: %v", err)
	}

	// Stream SHOULD be interrupted (validation failed) but no error thrown
	if !execCtx.StreamInterrupted {
		t.Error("Expected StreamInterrupted to be true (validation failed but suppressed)")
	}

	// Verify failure was recorded in metadata
	failed, _ := execCtx.Metadata["_streaming_validation_failed"].(bool)
	if !failed {
		t.Error("Expected _streaming_validation_failed to be true")
	}

	t.Log("✓ Streaming validation with suppression works correctly - stream interrupted, failure recorded, no error thrown")
}

// TestDynamicValidatorMiddleware_StreamChunk_WithoutSuppression tests streaming validation without suppression
func TestDynamicValidatorMiddleware_StreamChunk_WithoutSuppression(t *testing.T) {
	registry := validators.NewRegistry()
	middleware := DynamicValidatorMiddlewareWithSuppression(registry, false)

	execCtx := &pipeline.ExecutionContext{
		Context:  context.Background(),
		Messages: []types.Message{},
		Metadata: make(map[string]interface{}),
	}

	execCtx.Metadata["validator_configs"] = []validators.ValidatorConfig{
		{
			Type: "banned_words",
			Params: map[string]interface{}{
				"words": []interface{}{"forbidden"},
			},
		},
	}

	err := middleware.Process(execCtx, func() error {
		return nil
	})

	if err != nil {
		t.Fatalf("Process() failed: %v", err)
	}

	// Chunk with banned word - should fail because suppression is disabled
	chunk := &providers.StreamChunk{
		Content: "This word is forbidden!",
	}

	err = middleware.StreamChunk(execCtx, chunk)
	if err == nil {
		t.Fatal("Without suppression, chunk should fail validation, got nil error")
	}

	// Stream should be interrupted without suppression
	if !execCtx.StreamInterrupted {
		t.Error("Expected StreamInterrupted to be true without suppression")
	}

	t.Logf("✓ Streaming validation without suppression correctly throws error: %v", err)
}
