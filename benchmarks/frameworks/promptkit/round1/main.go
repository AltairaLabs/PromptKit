// Package main implements an OpenAI-compatible HTTP server that wraps the
// PromptKit SDK for streaming benchmark purposes.
package main

import (
	"bytes"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"

	"github.com/AltairaLabs/PromptKit/runtime/providers"
	"github.com/AltairaLabs/PromptKit/runtime/providers/openai"
	"github.com/AltairaLabs/PromptKit/sdk"
)

var (
	packPath = flag.String("pack", "chat.pack.json", "path to the pack file")
	port     = flag.Int("port", 8090, "HTTP listen port")
)

// chatMessage mirrors the OpenAI chat message structure.
type chatMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

// chatRequest is the JSON body for POST /v1/chat/completions.
type chatRequest struct {
	Messages []chatMessage `json:"messages"`
	Stream   bool          `json:"stream"`
}

// chatResponse is the non-streaming JSON response.
type chatResponse struct {
	Object  string           `json:"object"`
	Choices []responseChoice `json:"choices"`
}

type responseChoice struct {
	Index   int         `json:"index"`
	Message chatMessage `json:"message"`
	Finish  string      `json:"finish_reason"`
}

// sseData is the payload inside each SSE data line.
type sseData struct {
	Object  string      `json:"object"`
	Choices []sseChoice `json:"choices"`
}

type sseChoice struct {
	Index int       `json:"index"`
	Delta sseDeltas `json:"delta"`
}

type sseDeltas struct {
	Content string `json:"content,omitempty"`
	Role    string `json:"role,omitempty"`
}

func main() {
	flag.Parse()

	baseURL := os.Getenv("OPENAI_BASE_URL")
	if baseURL == "" {
		baseURL = "https://api.openai.com/v1"
	}

	toolURL := os.Getenv("TOOL_URL")
	if toolURL == "" {
		toolURL = "http://localhost:8085"
	}

	// Build an OpenAI provider pointing at the mock upstream.
	p := openai.NewProvider(
		"openai",
		"gpt-4o-mini",
		baseURL,
		providers.ProviderDefaults{},
		false,
	)

	mux := http.NewServeMux()

	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	mux.HandleFunc("/v1/chat/completions", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}

		var req chatRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "bad request: "+err.Error(), http.StatusBadRequest)
			return
		}

		// Extract the last user message as the prompt.
		userMsg := ""
		for i := len(req.Messages) - 1; i >= 0; i-- {
			if req.Messages[i].Role == "user" {
				userMsg = req.Messages[i].Content
				break
			}
		}
		if userMsg == "" && len(req.Messages) > 0 {
			userMsg = req.Messages[len(req.Messages)-1].Content
		}

		conv, err := sdk.Open(*packPath, "chat", sdk.WithProvider(p))
		if err != nil {
			http.Error(w, "failed to open conversation: "+err.Error(), http.StatusInternalServerError)
			return
		}
		defer conv.Close()

		ctx := r.Context()

		if req.Stream {
			handleStream(ctx, w, conv, userMsg)
		} else {
			handleUnary(ctx, w, conv, userMsg)
		}
	})

	mux.HandleFunc("/v1/chat/completions/tools", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}

		var req chatRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "bad request: "+err.Error(), http.StatusBadRequest)
			return
		}

		userMsg := ""
		for i := len(req.Messages) - 1; i >= 0; i-- {
			if req.Messages[i].Role == "user" {
				userMsg = req.Messages[i].Content
				break
			}
		}
		if userMsg == "" && len(req.Messages) > 0 {
			userMsg = req.Messages[len(req.Messages)-1].Content
		}

		conv, err := sdk.Open(*packPath, "chat", sdk.WithProvider(p))
		if err != nil {
			http.Error(w, "failed to open conversation: "+err.Error(), http.StatusInternalServerError)
			return
		}
		defer conv.Close()

		conv.OnTool("lookup_order", func(args map[string]any) (any, error) {
			payload, err := json.Marshal(args)
			if err != nil {
				return nil, fmt.Errorf("marshal tool args: %w", err)
			}
			resp, err := http.Post(toolURL+"/tool", "application/json", bytes.NewReader(payload))
			if err != nil {
				return nil, fmt.Errorf("tool call failed: %w", err)
			}
			defer resp.Body.Close()
			body, err := io.ReadAll(resp.Body)
			if err != nil {
				return nil, fmt.Errorf("read tool response: %w", err)
			}
			var result any
			if err := json.Unmarshal(body, &result); err != nil {
				return string(body), nil
			}
			return result, nil
		})

		ctx := r.Context()
		handleStream(ctx, w, conv, userMsg)
	})

	addr := fmt.Sprintf(":%d", *port)
	log.Printf("bench-promptkit-round1 listening on %s (pack=%s, upstream=%s)", addr, *packPath, baseURL)
	if err := http.ListenAndServe(addr, mux); err != nil {
		log.Fatalf("server error: %v", err)
	}
}

// handleStream relays SDK streaming chunks as SSE to the client.
func handleStream(ctx context.Context, w http.ResponseWriter, conv *sdk.Conversation, userMsg string) {
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")

	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming not supported", http.StatusInternalServerError)
		return
	}

	for chunk := range conv.Stream(ctx, userMsg) {
		if chunk.Error != nil {
			log.Printf("stream error: %v", chunk.Error)
			break
		}

		switch chunk.Type {
		case sdk.ChunkText:
			payload := sseData{
				Object: "chat.completion.chunk",
				Choices: []sseChoice{
					{Index: 0, Delta: sseDeltas{Content: chunk.Text}},
				},
			}
			b, err := json.Marshal(payload)
			if err != nil {
				log.Printf("marshal error: %v", err)
				continue
			}
			fmt.Fprintf(w, "data: %s\n\n", b)
			flusher.Flush()

		case sdk.ChunkDone:
			fmt.Fprintf(w, "data: [DONE]\n\n")
			flusher.Flush()
		}
	}
}

// handleUnary sends a single message and returns a JSON response.
func handleUnary(ctx context.Context, w http.ResponseWriter, conv *sdk.Conversation, userMsg string) {
	resp, err := conv.Send(ctx, userMsg)
	if err != nil {
		http.Error(w, "send failed: "+err.Error(), http.StatusInternalServerError)
		return
	}

	out := chatResponse{
		Object: "chat.completion",
		Choices: []responseChoice{
			{
				Index:   0,
				Message: chatMessage{Role: "assistant", Content: resp.Text()},
				Finish:  "stop",
			},
		},
	}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(out); err != nil {
		log.Printf("encode error: %v", err)
	}
}
