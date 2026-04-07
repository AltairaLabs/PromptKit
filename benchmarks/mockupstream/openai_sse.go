package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"
)

// chatRequest is the subset of an OpenAI chat completion request body we care
// about: only the stream flag needs to be inspected.
type chatRequest struct {
	Stream bool `json:"stream"`
}

// chunkChoice is a single choice inside a streaming chunk.
type chunkChoice struct {
	Index        int        `json:"index"`
	Delta        chunkDelta `json:"delta"`
	FinishReason *string    `json:"finish_reason"`
}

// chunkDelta carries the incremental content for a streaming chunk.
type chunkDelta struct {
	Content string `json:"content,omitempty"`
}

// streamChunk is one SSE payload in the OpenAI streaming format.
type streamChunk struct {
	ID      string        `json:"id"`
	Object  string        `json:"object"`
	Choices []chunkChoice `json:"choices"`
}

// nonStreamChoice is a single choice inside a non-streaming response.
type nonStreamChoice struct {
	Index        int          `json:"index"`
	Message      nonStreamMsg `json:"message"`
	FinishReason string       `json:"finish_reason"`
}

// nonStreamMsg holds the complete assistant message for non-streaming responses.
type nonStreamMsg struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

// nonStreamResponse is the full non-streaming OpenAI chat completion response.
type nonStreamResponse struct {
	ID      string            `json:"id"`
	Object  string            `json:"object"`
	Choices []nonStreamChoice `json:"choices"`
}

// stopReason is a package-level pointer used for the stop finish_reason.
var stopReason = strPtr("stop")

func strPtr(s string) *string { return &s }

// NewOpenAIHandler returns an http.Handler that simulates the OpenAI
// /v1/chat/completions endpoint using the provided OpenAIProfile.
//
// Streaming requests receive cfg.ChunkCount delta SSE chunks followed by one
// stop chunk and the [DONE] sentinel. Non-streaming requests receive a single
// JSON response whose content is the concatenation of all chunk contents.
func NewOpenAIHandler(cfg OpenAIProfile) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/chat/completions" {
			http.NotFound(w, r)
			return
		}
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}

		var req chatRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "bad request", http.StatusBadRequest)
			return
		}

		if req.Stream {
			handleStream(w, cfg)
		} else {
			handleNonStream(w, cfg)
		}
	})
}

// handleStream writes the SSE streaming response.
func handleStream(w http.ResponseWriter, cfg OpenAIProfile) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming unsupported", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.WriteHeader(http.StatusOK)

	if cfg.FirstChunkDelay > 0 {
		time.Sleep(cfg.FirstChunkDelay)
	}

	// Emit delta chunks.
	for i := 1; i <= cfg.ChunkCount; i++ {
		chunk := streamChunk{
			ID:     "chatcmpl-bench",
			Object: "chat.completion.chunk",
			Choices: []chunkChoice{
				{
					Index:        0,
					Delta:        chunkDelta{Content: fmt.Sprintf("chunk-%d ", i)},
					FinishReason: nil,
				},
			},
		}
		writeSSEChunk(w, chunk)
		flusher.Flush()

		if i < cfg.ChunkCount && cfg.InterChunkDelay > 0 {
			time.Sleep(cfg.InterChunkDelay)
		}
	}

	// Emit stop chunk.
	stopChunk := streamChunk{
		ID:     "chatcmpl-bench",
		Object: "chat.completion.chunk",
		Choices: []chunkChoice{
			{
				Index:        0,
				Delta:        chunkDelta{},
				FinishReason: stopReason,
			},
		},
	}
	writeSSEChunk(w, stopChunk)
	flusher.Flush()

	// Emit [DONE] sentinel.
	fmt.Fprint(w, "data: [DONE]\n\n")
	flusher.Flush()
}

// writeSSEChunk marshals v and writes it as a single SSE data line.
func writeSSEChunk(w http.ResponseWriter, v any) {
	data, _ := json.Marshal(v)
	fmt.Fprintf(w, "data: %s\n\n", data)
}

// handleNonStream writes a single JSON response whose content is the
// concatenation of all chunk texts (matching what streaming would emit).
func handleNonStream(w http.ResponseWriter, cfg OpenAIProfile) {
	var sb strings.Builder
	for i := 1; i <= cfg.ChunkCount; i++ {
		sb.WriteString(fmt.Sprintf("chunk-%d ", i))
	}

	resp := nonStreamResponse{
		ID:     "chatcmpl-bench",
		Object: "chat.completion",
		Choices: []nonStreamChoice{
			{
				Index: 0,
				Message: nonStreamMsg{
					Role:    "assistant",
					Content: sb.String(),
				},
				FinishReason: "stop",
			},
		},
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(resp) //nolint:errcheck
}
