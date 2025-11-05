package providers_test

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/AltairaLabs/PromptKit/runtime/providers"
	"github.com/AltairaLabs/PromptKit/runtime/providers/claude"
	"github.com/AltairaLabs/PromptKit/runtime/providers/gemini"
	"github.com/AltairaLabs/PromptKit/runtime/providers/openai"
	"github.com/AltairaLabs/PromptKit/runtime/types"
)

// ptr is a helper function to create a pointer to a string
func ptr(s string) *string {
	return &s
}

func TestOpenAIStreaming(t *testing.T) {
	if os.Getenv("OPENAI_API_KEY") == "" {
		t.Skip("OPENAI_API_KEY not set")
	}

	provider := openai.NewOpenAIProvider(
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

	req := providers.ChatRequest{
		System:      "You are a helpful assistant.",
		Messages:    []types.Message{{Role: "user", Content: "Say hello!"}},
		Temperature: 0.7,
		MaxTokens:   50,
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	stream, err := provider.ChatStream(ctx, req)
	if err != nil {
		t.Fatalf("ChatStream failed: %v", err)
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
	if os.Getenv("ANTHROPIC_API_KEY") == "" {
		t.Skip("ANTHROPIC_API_KEY not set")
	}

	provider := claude.NewClaudeProvider(
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

	req := providers.ChatRequest{
		System:      "You are a helpful assistant.",
		Messages:    []types.Message{{Role: "user", Content: "Say hello!"}},
		Temperature: 0.7,
		MaxTokens:   50,
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	stream, err := provider.ChatStream(ctx, req)
	if err != nil {
		t.Fatalf("ChatStream failed: %v", err)
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
	if os.Getenv("GEMINI_API_KEY") == "" {
		t.Skip("GEMINI_API_KEY not set")
	}

	provider := gemini.NewGeminiProvider(
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

	req := providers.ChatRequest{
		System:      "You are a helpful assistant.",
		Messages:    []types.Message{{Role: "user", Content: "Say hello!"}},
		Temperature: 0.7,
		MaxTokens:   50,
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	stream, err := provider.ChatStream(ctx, req)
	if err != nil {
		t.Fatalf("ChatStream failed: %v", err)
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
		openai.NewOpenAIProvider("openai", "gpt-4o-mini", "https://api.openai.com/v1", providers.ProviderDefaults{}, false),
		claude.NewClaudeProvider("claude", "claude-3-5-haiku-20241022", "https://api.anthropic.com", providers.ProviderDefaults{}, false),
		gemini.NewGeminiProvider("gemini", "gemini-2.0-flash-exp", "https://generativelanguage.googleapis.com", providers.ProviderDefaults{}, false),
	}

	for _, p := range providers {
		if !p.SupportsStreaming() {
			t.Errorf("providers.Provider %s should support streaming", p.ID())
		}
	}
}

func TestStreamChunk_Basic(t *testing.T) {
	chunk := providers.StreamChunk{
		Content:     "Hello world",
		Delta:       " world",
		TokenCount:  5,
		DeltaTokens: 2,
	}

	if chunk.Content != "Hello world" {
		t.Errorf("Content: got %q, want %q", chunk.Content, "Hello world")
	}

	if chunk.Delta != " world" {
		t.Errorf("Delta: got %q, want %q", chunk.Delta, " world")
	}

	if chunk.TokenCount != 5 {
		t.Errorf("TokenCount: got %d, want %d", chunk.TokenCount, 5)
	}

	if chunk.DeltaTokens != 2 {
		t.Errorf("DeltaTokens: got %d, want %d", chunk.DeltaTokens, 2)
	}

	if chunk.FinishReason != nil {
		t.Errorf("FinishReason should be nil, got %v", chunk.FinishReason)
	}

	if chunk.Error != nil {
		t.Errorf("Error should be nil, got %v", chunk.Error)
	}
}

func TestStreamChunk_WithFinishReason(t *testing.T) {
	reason := "stop"
	chunk := providers.StreamChunk{
		Content:      "Complete response",
		TokenCount:   10,
		FinishReason: &reason,
	}

	if chunk.FinishReason == nil {
		t.Fatal("FinishReason should not be nil")
	}

	if *chunk.FinishReason != "stop" {
		t.Errorf("FinishReason: got %q, want %q", *chunk.FinishReason, "stop")
	}
}

func TestStreamChunk_WithError(t *testing.T) {
	testErr := &providers.ValidationAbortError{
		Reason: "banned word detected",
	}

	chunk := providers.StreamChunk{
		Content:      "Partial content",
		Error:        testErr,
		FinishReason: ptr("validation_failed"),
	}

	if chunk.Error == nil {
		t.Fatal("Error should not be nil")
	}

	if !providers.IsValidationAbort(chunk.Error) {
		t.Error("Error should be providers.ValidationAbortError")
	}
}

func TestStreamChunk_WithMetadata(t *testing.T) {
	chunk := providers.StreamChunk{
		Content: "Test",
		Metadata: map[string]interface{}{
			"model":    "gpt-4",
			"provider": "openai",
			"cost":     0.001,
		},
	}

	if chunk.Metadata == nil {
		t.Fatal("Metadata should not be nil")
	}

	if chunk.Metadata["model"] != "gpt-4" {
		t.Errorf("Metadata model: got %v, want %q", chunk.Metadata["model"], "gpt-4")
	}

	if cost, ok := chunk.Metadata["cost"].(float64); !ok || cost != 0.001 {
		t.Errorf("Metadata cost: got %v, want %f", chunk.Metadata["cost"], 0.001)
	}
}

func TestStreamEvent_Basic(t *testing.T) {
	now := time.Now()
	chunk := &providers.StreamChunk{Content: "test", Delta: "test"}

	event := providers.StreamEvent{
		Type:      "chunk",
		Chunk:     chunk,
		Timestamp: now,
	}

	if event.Type != "chunk" {
		t.Errorf("Type: got %q, want %q", event.Type, "chunk")
	}

	if event.Chunk != chunk {
		t.Error("Chunk pointer mismatch")
	}

	if event.Timestamp != now {
		t.Error("Timestamp mismatch")
	}
}

func TestStreamEvent_Complete(t *testing.T) {
	event := providers.StreamEvent{
		Type:      "complete",
		Timestamp: time.Now(),
	}

	if event.Type != "complete" {
		t.Errorf("Type: got %q, want %q", event.Type, "complete")
	}

	if event.Chunk != nil {
		t.Error("Chunk should be nil for complete event")
	}

	if event.Error != nil {
		t.Error("Error should be nil for complete event")
	}
}

func TestStreamEvent_Error(t *testing.T) {
	testErr := &providers.ValidationAbortError{Reason: "test"}

	event := providers.StreamEvent{
		Type:      "error",
		Error:     testErr,
		Timestamp: time.Now(),
	}

	if event.Type != "error" {
		t.Errorf("Type: got %q, want %q", event.Type, "error")
	}

	if event.Error == nil {
		t.Fatal("Error should not be nil")
	}

	if !providers.IsValidationAbort(event.Error) {
		t.Error("Error should be providers.ValidationAbortError")
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
	p := ptr(s)

	if p == nil {
		t.Fatal("ptr() returned nil")
	}

	if *p != s {
		t.Errorf("ptr() = %q, want %q", *p, s)
	}

	// Verify it's a different address
	s2 := "test"
	p2 := ptr(s2)

	if p == p2 {
		t.Error("ptr() should return different pointers")
	}
}

func TestStreamChunk_EmptyStrings(t *testing.T) {
	chunk := providers.StreamChunk{
		Content:     "",
		Delta:       "",
		TokenCount:  0,
		DeltaTokens: 0,
	}

	if chunk.Content != "" {
		t.Errorf("Content should be empty, got %q", chunk.Content)
	}

	if chunk.Delta != "" {
		t.Errorf("Delta should be empty, got %q", chunk.Delta)
	}
}

func TestStreamChunk_ZeroValues(t *testing.T) {
	var chunk providers.StreamChunk

	if chunk.Content != "" {
		t.Error("Zero value Content should be empty")
	}

	if chunk.Delta != "" {
		t.Error("Zero value Delta should be empty")
	}

	if chunk.TokenCount != 0 {
		t.Error("Zero value TokenCount should be 0")
	}

	if chunk.DeltaTokens != 0 {
		t.Error("Zero value DeltaTokens should be 0")
	}

	if chunk.FinishReason != nil {
		t.Error("Zero value FinishReason should be nil")
	}

	if chunk.Error != nil {
		t.Error("Zero value Error should be nil")
	}

	if chunk.Metadata != nil {
		t.Error("Zero value Metadata should be nil")
	}
}

func TestStreamObserver_Interface(t *testing.T) {
	// Verify that our mock observer implements the interface
	var _ providers.StreamObserver = &mockStreamObserver{}
}

type mockStreamObserver struct {
	chunks    []providers.StreamChunk
	completed bool
	errors    []error
	duration  time.Duration
	tokens    int
}

func (m *mockStreamObserver) OnChunk(chunk providers.StreamChunk) {
	m.chunks = append(m.chunks, chunk)
}

func (m *mockStreamObserver) OnComplete(totalTokens int, duration time.Duration) {
	m.completed = true
	m.tokens = totalTokens
	m.duration = duration
}

func (m *mockStreamObserver) OnError(err error) {
	m.errors = append(m.errors, err)
}

func TestMockObserver(t *testing.T) {
	observer := &mockStreamObserver{}

	// Send some chunks
	observer.OnChunk(providers.StreamChunk{Content: "hello", Delta: "hello", TokenCount: 1})
	observer.OnChunk(providers.StreamChunk{Content: "hello world", Delta: " world", TokenCount: 2})

	if len(observer.chunks) != 2 {
		t.Fatalf("Expected 2 chunks, got %d", len(observer.chunks))
	}

	// Complete
	observer.OnComplete(10, 500*time.Millisecond)

	if !observer.completed {
		t.Error("Expected completed flag to be set")
	}

	if observer.tokens != 10 {
		t.Errorf("Expected 10 tokens, got %d", observer.tokens)
	}

	// Error
	observer.OnError(&providers.ValidationAbortError{Reason: "test"})

	if len(observer.errors) != 1 {
		t.Fatalf("Expected 1 error, got %d", len(observer.errors))
	}
}
