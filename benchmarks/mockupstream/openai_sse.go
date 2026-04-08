package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"
)

// chatRequest is the subset of an OpenAI chat completion request body we care
// about: stream flag and messages (to detect tool-call round-trips).
type chatRequest struct {
	Messages []chatMsg `json:"messages"`
	Stream   bool      `json:"stream"`
}

// chatMsg captures just the role of each message in the request.
type chatMsg struct {
	Role string `json:"role"`
}

// toolCallResponse is a non-streaming response that requests tool execution.
type toolCallResponse struct {
	ID      string           `json:"id"`
	Object  string           `json:"object"`
	Choices []toolCallChoice `json:"choices"`
}

type toolCallChoice struct {
	Index        int         `json:"index"`
	Message      toolCallMsg `json:"message"`
	FinishReason string      `json:"finish_reason"`
}

type toolCallMsg struct {
	Role      string     `json:"role"`
	Content   *string    `json:"content"`
	ToolCalls []toolCall `json:"tool_calls"`
}

type toolCall struct {
	ID       string       `json:"id"`
	Type     string       `json:"type"`
	Function toolCallFunc `json:"function"`
}

type toolCallFunc struct {
	Name      string `json:"name"`
	Arguments string `json:"arguments"`
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

		// Second round (tool result present) → normal completion
		if hasToolResult(req.Messages) {
			if req.Stream {
				handleStream(w, cfg)
			} else {
				handleNonStream(w, cfg)
			}
			return
		}

		// First round, non-streaming → tool_calls response
		if !req.Stream {
			handleToolCallResponse(w)
			return
		}

		// First round, streaming → normal stream (backwards compatible)
		handleStream(w, cfg)
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

// hasToolResult returns true if any message in the request has role "tool",
// indicating the client has already executed a tool call and is sending results.
func hasToolResult(msgs []chatMsg) bool {
	for _, m := range msgs {
		if m.Role == "tool" {
			return true
		}
	}
	return false
}

// handleToolCallResponse writes a non-streaming JSON response that requests
// the client to execute a tool call (lookup_order).
func handleToolCallResponse(w http.ResponseWriter) {
	resp := toolCallResponse{
		ID:     "chatcmpl-bench-tool",
		Object: "chat.completion",
		Choices: []toolCallChoice{{
			Index: 0,
			Message: toolCallMsg{
				Role:    "assistant",
				Content: nil,
				ToolCalls: []toolCall{{
					ID:   "call_bench_1",
					Type: "function",
					Function: toolCallFunc{
						Name:      "lookup_order",
						Arguments: `{"order_id":"12345"}`,
					},
				}},
			},
			FinishReason: "tool_calls",
		}},
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(resp) //nolint:errcheck
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
