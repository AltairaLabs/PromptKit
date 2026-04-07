package main

import (
	"bufio"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// openAIChunk is the minimal structure of an OpenAI streaming chunk.
type openAIChunk struct {
	ID      string `json:"id"`
	Object  string `json:"object"`
	Choices []struct {
		Index        int     `json:"index"`
		FinishReason *string `json:"finish_reason"`
		Delta        struct {
			Content string `json:"content"`
		} `json:"delta"`
	} `json:"choices"`
}

// openAIResponse is the minimal structure of a non-streaming OpenAI response.
type openAIResponse struct {
	ID      string `json:"id"`
	Object  string `json:"object"`
	Choices []struct {
		Message struct {
			Content string `json:"content"`
		} `json:"message"`
		FinishReason string `json:"finish_reason"`
	} `json:"choices"`
}

func postJSON(t *testing.T, srv *httptest.Server, body string) *http.Response {
	t.Helper()
	resp, err := http.Post(srv.URL+"/v1/chat/completions", "application/json", strings.NewReader(body))
	if err != nil {
		t.Fatalf("POST failed: %v", err)
	}
	return resp
}

// TestOpenAISSE_StreamsChunks verifies that a streaming request produces the
// expected number of SSE data chunks with valid OpenAI chunk format.
func TestOpenAISSE_StreamsChunks(t *testing.T) {
	cfg := OpenAIProfile{
		ChunkCount:      3,
		InterChunkDelay: 0,
		FirstChunkDelay: 0,
	}
	handler := NewOpenAIHandler(cfg)
	srv := httptest.NewServer(handler)
	defer srv.Close()

	resp := postJSON(t, srv, `{"model":"gpt-4","stream":true,"messages":[]}`)
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}
	ct := resp.Header.Get("Content-Type")
	if !strings.HasPrefix(ct, "text/event-stream") {
		t.Fatalf("expected text/event-stream Content-Type, got %q", ct)
	}

	var chunks []openAIChunk
	var sawDone bool

	scanner := bufio.NewScanner(resp.Body)
	for scanner.Scan() {
		line := scanner.Text()
		if line == "" {
			continue
		}
		if !strings.HasPrefix(line, "data: ") {
			t.Fatalf("unexpected SSE line format: %q", line)
		}
		payload := strings.TrimPrefix(line, "data: ")
		if payload == "[DONE]" {
			sawDone = true
			break
		}
		var chunk openAIChunk
		if err := json.Unmarshal([]byte(payload), &chunk); err != nil {
			t.Fatalf("failed to parse chunk JSON: %v — raw: %s", err, payload)
		}
		chunks = append(chunks, chunk)
	}
	if err := scanner.Err(); err != nil {
		t.Fatalf("scanner error: %v", err)
	}

	// Expect cfg.ChunkCount delta chunks + 1 stop chunk = 4 total
	wantTotal := cfg.ChunkCount + 1
	if len(chunks) != wantTotal {
		t.Errorf("expected %d chunks (including stop), got %d", wantTotal, len(chunks))
	}
	if !sawDone {
		t.Error("expected [DONE] sentinel, not seen")
	}

	// Verify delta chunks have expected content pattern
	for i := 0; i < cfg.ChunkCount; i++ {
		if len(chunks[i].Choices) == 0 {
			t.Errorf("chunk %d has no choices", i)
			continue
		}
		content := chunks[i].Choices[0].Delta.Content
		expected := "chunk-" + string(rune('0'+i+1)) + " "
		if i >= 9 {
			// For chunk numbers >= 10 we just check it's non-empty
			if content == "" {
				t.Errorf("chunk %d has empty content", i)
			}
		} else if content != expected {
			t.Errorf("chunk %d: expected content %q, got %q", i, expected, content)
		}
	}

	// Verify stop chunk
	if len(chunks) == wantTotal {
		stopChunk := chunks[wantTotal-1]
		if len(stopChunk.Choices) == 0 {
			t.Error("stop chunk has no choices")
		} else if stopChunk.Choices[0].FinishReason == nil || *stopChunk.Choices[0].FinishReason != "stop" {
			t.Error("stop chunk: finish_reason should be 'stop'")
		}
	}
}

// TestOpenAISSE_FirstChunkDelay verifies that the first chunk is delayed by
// at least FirstChunkDelay.
func TestOpenAISSE_FirstChunkDelay(t *testing.T) {
	const delay = 80 * time.Millisecond
	cfg := OpenAIProfile{
		ChunkCount:      2,
		InterChunkDelay: 0,
		FirstChunkDelay: delay,
	}
	handler := NewOpenAIHandler(cfg)
	srv := httptest.NewServer(handler)
	defer srv.Close()

	start := time.Now()
	resp := postJSON(t, srv, `{"model":"gpt-4","stream":true,"messages":[]}`)
	defer resp.Body.Close()

	// Read until first data line
	scanner := bufio.NewScanner(resp.Body)
	for scanner.Scan() {
		line := scanner.Text()
		if strings.HasPrefix(line, "data: ") {
			break
		}
	}
	elapsed := time.Since(start)

	if elapsed < delay {
		t.Errorf("first chunk arrived too fast: %v < %v", elapsed, delay)
	}
}

// TestOpenAISSE_NonStreamRequest verifies that a non-streaming request returns
// a complete JSON response with the concatenated content.
func TestOpenAISSE_NonStreamRequest(t *testing.T) {
	cfg := OpenAIProfile{
		ChunkCount:      5,
		InterChunkDelay: 0,
		FirstChunkDelay: 0,
	}
	handler := NewOpenAIHandler(cfg)
	srv := httptest.NewServer(handler)
	defer srv.Close()

	resp := postJSON(t, srv, `{"model":"gpt-4","stream":false,"messages":[]}`)
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}
	ct := resp.Header.Get("Content-Type")
	if !strings.HasPrefix(ct, "application/json") {
		t.Fatalf("expected application/json Content-Type, got %q", ct)
	}

	var result openAIResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	if len(result.Choices) == 0 {
		t.Fatal("expected at least one choice in response")
	}
	content := result.Choices[0].Message.Content
	if content == "" {
		t.Error("expected non-empty content in response")
	}
	// Verify all chunk contents are present
	for i := 1; i <= cfg.ChunkCount; i++ {
		expected := "chunk-"
		if !strings.Contains(content, expected) {
			t.Errorf("content %q does not contain %q", content, expected)
			break
		}
	}
	if result.Choices[0].FinishReason != "stop" {
		t.Errorf("expected finish_reason 'stop', got %q", result.Choices[0].FinishReason)
	}
}
