package providers_test

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/AltairaLabs/PromptKit/runtime/v2/providers"
	"github.com/AltairaLabs/PromptKit/runtime/v2/providers/claude"
	"github.com/AltairaLabs/PromptKit/runtime/v2/providers/gemini"
	"github.com/AltairaLabs/PromptKit/runtime/v2/providers/openai"
	"github.com/AltairaLabs/PromptKit/runtime/v2/testutil"
	"github.com/AltairaLabs/PromptKit/runtime/v2/types"
)

func TestOpenAIStreaming(t *testing.T) {
	// Only run live streaming tests when explicitly enabled to avoid CI flakiness
	if os.Getenv("OPENAI_API_KEY") == "" || os.Getenv("RUN_LIVE_STREAMING") != "1" {
		t.Skip("live streaming disabled (set OPENAI_API_KEY and RUN_LIVE_STREAMING=1 to enable)")
	}

	provider := openai.NewProvider(
		"openai-test",
		"gpt-4o-mini",
		"https://api.openai.com/v1",
		providers.ProviderDefaults{
			Temperature: 0.7,
			TopP:        1.0,
			MaxTokens:   100,
		},
		false,
	)

	req := providers.PredictionRequest{
		System:      "You are a helpful assistant.",
		Messages:    []types.Message{{Role: "user", Content: "Say hello!"}},
		Temperature: 0.7,
		MaxTokens:   50,
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	stream, err := provider.PredictStream(ctx, req)
	if err != nil {
		// Gracefully skip quota/rate limit conditions common in CI
		if strings.Contains(err.Error(), "429") || strings.Contains(err.Error(), "rate limit") || strings.Contains(err.Error(), "quota") {
			t.Skipf("skipping due to API limit: %v", err)
			return
		}
		t.Fatalf("PredictStream failed: %v", err)
	}

	chunkCount := 0
	var finalChunk providers.StreamChunk

	for chunk := range stream {
		if chunk.Error != nil {
			t.Fatalf("Stream error: %v", chunk.Error)
		}

		chunkCount++
		finalChunk = chunk

		t.Logf("Chunk %d: delta=%q, tokens=%d", chunkCount, chunk.Delta, chunk.TokenCount)

		if chunk.FinishReason != nil {
			break
		}
	}

	if chunkCount == 0 {
		t.Fatal("No chunks received")
	}

	if finalChunk.FinishReason == nil {
		t.Error("Expected finish reason in final chunk")
	}

	if finalChunk.Content == "" {
		t.Error("Expected accumulated content in final chunk")
	}

	t.Logf("Streaming complete: %d chunks, %d tokens, finish_reason=%s",
		chunkCount, finalChunk.TokenCount, *finalChunk.FinishReason)
}

func TestClaudeStreaming(t *testing.T) {
	if os.Getenv("ANTHROPIC_API_KEY") == "" || os.Getenv("RUN_LIVE_STREAMING") != "1" {
		t.Skip("live streaming disabled (set ANTHROPIC_API_KEY and RUN_LIVE_STREAMING=1 to enable)")
	}

	provider := claude.NewProvider(
		"claude-test",
		"claude-3-5-haiku-20241022",
		"https://api.anthropic.com",
		providers.ProviderDefaults{
			Temperature: 0.7,
			TopP:        1.0,
			MaxTokens:   100,
		},
		false,
	)

	req := providers.PredictionRequest{
		System:      "You are a helpful assistant.",
		Messages:    []types.Message{{Role: "user", Content: "Say hello!"}},
		Temperature: 0.7,
		MaxTokens:   50,
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	stream, err := provider.PredictStream(ctx, req)
	if err != nil {
		if strings.Contains(err.Error(), "429") || strings.Contains(err.Error(), "rate limit") || strings.Contains(err.Error(), "quota") {
			t.Skipf("skipping due to API limit: %v", err)
			return
		}
		t.Fatalf("PredictStream failed: %v", err)
	}

	chunkCount := 0
	var finalChunk providers.StreamChunk

	for chunk := range stream {
		if chunk.Error != nil {
			t.Fatalf("Stream error: %v", chunk.Error)
		}

		chunkCount++
		finalChunk = chunk

		t.Logf("Chunk %d: delta=%q, tokens=%d", chunkCount, chunk.Delta, chunk.TokenCount)

		if chunk.FinishReason != nil {
			break
		}
	}

	if chunkCount == 0 {
		t.Fatal("No chunks received")
	}

	if finalChunk.FinishReason == nil {
		t.Error("Expected finish reason in final chunk")
	}

	if finalChunk.Content == "" {
		t.Error("Expected accumulated content in final chunk")
	}

	t.Logf("Streaming complete: %d chunks, %d tokens, finish_reason=%s",
		chunkCount, finalChunk.TokenCount, *finalChunk.FinishReason)
}

func TestGeminiStreaming(t *testing.T) {
	if os.Getenv("GEMINI_API_KEY") == "" || os.Getenv("RUN_LIVE_STREAMING") != "1" {
		t.Skip("live streaming disabled (set GEMINI_API_KEY and RUN_LIVE_STREAMING=1 to enable)")
	}

	provider := gemini.NewProvider(
		"gemini-test",
		"gemini-2.0-flash-exp",
		"https://generativelanguage.googleapis.com",
		providers.ProviderDefaults{
			Temperature: 0.7,
			TopP:        1.0,
			MaxTokens:   100,
		},
		false,
	)

	req := providers.PredictionRequest{
		System:      "You are a helpful assistant.",
		Messages:    []types.Message{{Role: "user", Content: "Say hello!"}},
		Temperature: 0.7,
		MaxTokens:   50,
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	stream, err := provider.PredictStream(ctx, req)
	if err != nil {
		if strings.Contains(err.Error(), "429") || strings.Contains(err.Error(), "rate limit") || strings.Contains(err.Error(), "quota") {
			t.Skipf("skipping due to API limit: %v", err)
			return
		}
		t.Fatalf("PredictStream failed: %v", err)
	}

	chunkCount := 0
	var finalChunk providers.StreamChunk

	for chunk := range stream {
		if chunk.Error != nil {
			t.Fatalf("Stream error: %v", chunk.Error)
		}

		chunkCount++
		finalChunk = chunk

		t.Logf("Chunk %d: delta=%q, tokens=%d", chunkCount, chunk.Delta, chunk.TokenCount)

		if chunk.FinishReason != nil {
			break
		}
	}

	if chunkCount == 0 {
		t.Fatal("No chunks received")
	}

	if finalChunk.FinishReason == nil {
		t.Error("Expected finish reason in final chunk")
	}

	if finalChunk.Content == "" {
		t.Error("Expected accumulated content in final chunk")
	}

	t.Logf("Streaming complete: %d chunks, %d tokens, finish_reason=%s",
		chunkCount, finalChunk.TokenCount, *finalChunk.FinishReason)
}

func TestSupportsStreaming(t *testing.T) {
	providers := []providers.Provider{
		openai.NewProvider("openai", "gpt-4o-mini", "https://api.openai.com/v1", providers.ProviderDefaults{}, false),
		claude.NewProvider("claude", "claude-3-5-haiku-20241022", "https://api.anthropic.com", providers.ProviderDefaults{}, false),
		gemini.NewProvider("gemini", "gemini-2.0-flash-exp", "https://generativelanguage.googleapis.com", providers.ProviderDefaults{}, false),
	}

	for _, p := range providers {
		if !p.SupportsStreaming() {
			t.Errorf("providers.Provider %s should support streaming", p.ID())
		}
	}
}

func TestValidationAbortError(t *testing.T) {
	chunk := providers.StreamChunk{Content: "bad content"}
	err := &providers.ValidationAbortError{
		Reason: "contains banned word",
		Chunk:  chunk,
	}

	if err.Error() != "validation aborted stream: contains banned word" {
		t.Errorf("Error message: got %q", err.Error())
	}

	if err.Chunk.Content != "bad content" {
		t.Errorf("Chunk content: got %q, want %q", err.Chunk.Content, "bad content")
	}
}

func TestIsValidationAbort(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want bool
	}{
		{
			name: "providers.ValidationAbortError",
			err:  &providers.ValidationAbortError{Reason: "test"},
			want: true,
		},
		{
			name: "generic error",
			err:  context.Canceled,
			want: false,
		},
		{
			name: "nil error",
			err:  nil,
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := providers.IsValidationAbort(tt.err); got != tt.want {
				t.Errorf("providers.IsValidationAbort() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestPtr(t *testing.T) {
	s := "test"
	p := testutil.Ptr(s)

	if p == nil {
		t.Fatal("testutil.Ptr() returned nil")
	}

	if *p != s {
		t.Errorf("testutil.Ptr() = %q, want %q", *p, s)
	}

	// Verify it's a different address
	s2 := "test"
	p2 := testutil.Ptr(s2)

	if p == p2 {
		t.Error("testutil.Ptr() should return different pointers")
	}
}

func TestDefaultStreamBufferSize(t *testing.T) {
	if providers.DefaultStreamBufferSize != 32 {
		t.Errorf("DefaultStreamBufferSize: got %d, want 32", providers.DefaultStreamBufferSize)
	}
}
