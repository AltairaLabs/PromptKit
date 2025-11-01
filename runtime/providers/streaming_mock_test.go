package providers

import (
	"context"
	"encoding/json"
	"github.com/AltairaLabs/PromptKit/runtime/types"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestOpenAIStreamResponse(t *testing.T) {
	// Create a mock SSE stream
	sseData := `data: {"choices":[{"delta":{"content":"Hello"}}]}

data: {"choices":[{"delta":{"content":" world"}}]}

data: {"choices":[{"delta":{"content":"!"}}]}

data: {"choices":[{"delta":{},"finish_reason":"stop"}]}

data: [DONE]

`
	// Create mock server
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(sseData))
	}))
	defer server.Close()

	provider := NewOpenAIProvider(
		"test",
		"gpt-4o-mini",
		server.URL,
		ProviderDefaults{Temperature: 0.7, MaxTokens: 100},
		false,
	)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	stream, err := provider.ChatStream(ctx, ChatRequest{
		Messages: []types.Message{{Role: "user", Content: "test"}},
	})
	if err != nil {
		t.Fatalf("ChatStream failed: %v", err)
	}

	chunks := []StreamChunk{}
	for chunk := range stream {
		if chunk.Error != nil {
			t.Fatalf("Stream error: %v", chunk.Error)
		}
		chunks = append(chunks, chunk)
	}

	if len(chunks) == 0 {
		t.Fatal("Expected chunks")
	}

	// Check final chunk
	final := chunks[len(chunks)-1]
	if final.Content != "Hello world!" {
		t.Errorf("Final content: got %q, want %q", final.Content, "Hello world!")
	}

	if final.FinishReason == nil || *final.FinishReason != "stop" {
		t.Error("Expected finish_reason=stop")
	}
}

func TestClaudeStreamResponse(t *testing.T) {
	// Create a mock SSE stream with Claude's event format
	sseData := `data: {"type":"content_block_start","index":0}

data: {"type":"content_block_delta","delta":{"type":"text_delta","text":"Hello"}}

data: {"type":"content_block_delta","delta":{"type":"text_delta","text":" Claude"}}

data: {"type":"content_block_stop"}

data: {"type":"message_stop","message":{"stop_reason":"end_turn"}}

`
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(sseData))
	}))
	defer server.Close()

	provider := NewClaudeProvider(
		"test",
		"claude-3-5-haiku-20241022",
		server.URL,
		ProviderDefaults{Temperature: 0.7, MaxTokens: 100},
		false,
	)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	stream, err := provider.ChatStream(ctx, ChatRequest{
		Messages: []types.Message{{Role: "user", Content: "test"}},
	})
	if err != nil {
		t.Fatalf("ChatStream failed: %v", err)
	}

	chunks := []StreamChunk{}
	for chunk := range stream {
		if chunk.Error != nil {
			t.Fatalf("Stream error: %v", chunk.Error)
		}
		chunks = append(chunks, chunk)
	}

	if len(chunks) == 0 {
		t.Fatal("Expected chunks")
	}

	// Check final chunk
	final := chunks[len(chunks)-1]
	if final.Content != "Hello Claude" {
		t.Errorf("Final content: got %q, want %q", final.Content, "Hello Claude")
	}

	if final.FinishReason == nil || *final.FinishReason != "end_turn" {
		t.Errorf("Expected finish_reason=end_turn, got %v", final.FinishReason)
	}
}

func TestGeminiStreamResponse(t *testing.T) {
	// Create a mock JSON array stream (Gemini format)
	jsonStream := `[
		{"candidates":[{"content":{"parts":[{"text":"Hello"}]}}]},
		{"candidates":[{"content":{"parts":[{"text":" Gemini"}]}}]},
		{"candidates":[{"content":{"parts":[{"text":"!"}]},"finishReason":"STOP"}]}
	]`
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(jsonStream))
	}))
	defer server.Close()

	provider := NewGeminiProvider(
		"test",
		"gemini-2.0-flash-exp",
		server.URL,
		ProviderDefaults{Temperature: 0.7, MaxTokens: 100},
		false,
	)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	stream, err := provider.ChatStream(ctx, ChatRequest{
		Messages: []types.Message{{Role: "user", Content: "test"}},
	})
	if err != nil {
		t.Fatalf("ChatStream failed: %v", err)
	}

	chunks := []StreamChunk{}
	for chunk := range stream {
		if chunk.Error != nil {
			t.Fatalf("Stream error: %v", chunk.Error)
		}
		chunks = append(chunks, chunk)
	}

	if len(chunks) == 0 {
		t.Fatal("Expected chunks")
	}

	// Check final chunk
	final := chunks[len(chunks)-1]
	if final.Content != "Hello Gemini!" {
		t.Errorf("Final content: got %q, want %q", final.Content, "Hello Gemini!")
	}

	if final.FinishReason == nil || *final.FinishReason != "STOP" {
		t.Errorf("Expected finish_reason=STOP, got %v", final.FinishReason)
	}
}

func TestStreamContextCancellation(t *testing.T) {
	// Create a server that streams slowly
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)

		// Stream a few chunks
		_, _ = w.Write([]byte(`data: {"choices":[{"delta":{"content":"test"}}]}` + "\n\n"))
		if f, ok := w.(http.Flusher); ok {
			f.Flush()
		}

		// Wait longer than context timeout
		time.Sleep(2 * time.Second)
	}))
	defer server.Close()

	provider := NewOpenAIProvider(
		"test",
		"gpt-4o-mini",
		server.URL,
		ProviderDefaults{},
		false,
	)

	// Create context with short timeout
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	stream, err := provider.ChatStream(ctx, ChatRequest{
		Messages: []types.Message{{Role: "user", Content: "test"}},
	})
	if err != nil {
		t.Fatalf("ChatStream failed: %v", err)
	}

	gotCancellation := false
	for chunk := range stream {
		if chunk.Error != nil {
			if chunk.Error == context.DeadlineExceeded || chunk.FinishReason != nil && *chunk.FinishReason == "cancelled" {
				gotCancellation = true
			}
		}
	}

	if !gotCancellation {
		t.Error("Expected context cancellation to be detected")
	}
}

func TestStreamHTTPError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"error": "unauthorized"}`))
	}))
	defer server.Close()

	provider := NewOpenAIProvider(
		"test",
		"gpt-4o-mini",
		server.URL,
		ProviderDefaults{},
		false,
	)

	ctx := context.Background()
	_, err := provider.ChatStream(ctx, ChatRequest{
		Messages: []types.Message{{Role: "user", Content: "test"}},
	})

	if err == nil {
		t.Fatal("Expected error for HTTP 401")
	}

	if !strings.Contains(err.Error(), "401") {
		t.Errorf("Expected 401 in error message, got: %v", err)
	}
}

func TestStreamMalformedJSON(t *testing.T) {
	// Test OpenAI with malformed JSON
	sseData := `data: {"choices":[{"delta":{"content":"ok"}}]}

data: {invalid json}

data: {"choices":[{"delta":{"content":"!"}}]}

data: [DONE]

`
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(sseData))
	}))
	defer server.Close()

	provider := NewOpenAIProvider(
		"test",
		"gpt-4o-mini",
		server.URL,
		ProviderDefaults{},
		false,
	)

	ctx := context.Background()
	stream, err := provider.ChatStream(ctx, ChatRequest{
		Messages: []types.Message{{Role: "user", Content: "test"}},
	})
	if err != nil {
		t.Fatalf("ChatStream failed: %v", err)
	}

	chunks := []StreamChunk{}
	for chunk := range stream {
		if chunk.Error != nil {
			t.Fatalf("Stream error: %v", chunk.Error)
		}
		chunks = append(chunks, chunk)
	}

	// Should skip malformed chunk and continue
	if len(chunks) == 0 {
		t.Fatal("Expected some chunks despite malformed JSON")
	}
}

func TestStreamEmptyResponse(t *testing.T) {
	// Test with empty stream
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("data: [DONE]\n\n"))
	}))
	defer server.Close()

	provider := NewOpenAIProvider(
		"test",
		"gpt-4o-mini",
		server.URL,
		ProviderDefaults{},
		false,
	)

	ctx := context.Background()
	stream, err := provider.ChatStream(ctx, ChatRequest{
		Messages: []types.Message{{Role: "user", Content: "test"}},
	})
	if err != nil {
		t.Fatalf("ChatStream failed: %v", err)
	}

	chunks := []StreamChunk{}
	for chunk := range stream {
		chunks = append(chunks, chunk)
	}

	if len(chunks) != 1 {
		t.Fatalf("Expected 1 final chunk, got %d", len(chunks))
	}

	if chunks[0].Content != "" {
		t.Errorf("Expected empty content, got %q", chunks[0].Content)
	}
}

func TestStreamWithDefaults(t *testing.T) {
	// Test that provider defaults are applied
	sseData := `data: {"choices":[{"delta":{"content":"test"}}]}

data: [DONE]

`
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Verify request has defaults applied
		body, _ := io.ReadAll(r.Body)
		var req map[string]interface{}
		_ = json.Unmarshal(body, &req)

		if temp, ok := req["temperature"].(float64); !ok || temp != 0.8 {
			t.Errorf("Expected temperature 0.8, got %v", req["temperature"])
		}

		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(sseData))
	}))
	defer server.Close()

	provider := NewOpenAIProvider(
		"test",
		"gpt-4o-mini",
		server.URL,
		ProviderDefaults{
			Temperature: 0.8,
			TopP:        0.9,
			MaxTokens:   200,
		},
		false,
	)

	ctx := context.Background()
	stream, err := provider.ChatStream(ctx, ChatRequest{
		Messages: []types.Message{{Role: "user", Content: "test"}},
		// Don't specify temperature - should use default
	})
	if err != nil {
		t.Fatalf("ChatStream failed: %v", err)
	}

	// Consume stream
	for range stream {
	}
}

func TestIsValidationAbortWithNil(t *testing.T) {
	if IsValidationAbort(nil) {
		t.Error("IsValidationAbort(nil) should return false")
	}
}
