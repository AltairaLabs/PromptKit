// GenKit streaming benchmark endpoint.
//
// Minimal HTTP server wrapping Google GenKit's OpenAI-compatible plugin
// for streaming chat completions. Points at a mock OpenAI upstream.
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"

	"github.com/firebase/genkit/go/ai"
	"github.com/firebase/genkit/go/genkit"
	"github.com/firebase/genkit/go/plugins/compat_oai"
)

var port = flag.Int("port", 8095, "listen port")

type chatMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type chatRequest struct {
	Messages []chatMessage `json:"messages"`
	Stream   bool          `json:"stream"`
}

func main() {
	flag.Parse()

	baseURL := os.Getenv("OPENAI_BASE_URL")
	if baseURL == "" {
		baseURL = "http://localhost:8081/v1"
	}

	ctx := context.Background()

	plugin := &compat_oai.OpenAICompatible{
		Provider: "openai",
		APIKey:   os.Getenv("OPENAI_API_KEY"),
		BaseURL:  baseURL,
	}

	g := genkit.Init(ctx, genkit.WithPlugins(plugin))

	plugin.DefineModel("openai", "gpt-4o", ai.ModelOptions{
		Label: "GPT-4o",
		Supports: &ai.ModelSupports{
			Multiturn: true,
		},
	})

	mux := http.NewServeMux()

	mux.HandleFunc("/health", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	mux.HandleFunc("/v1/chat/completions", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}

		var req chatRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}

		userMsg := ""
		for i := len(req.Messages) - 1; i >= 0; i-- {
			if req.Messages[i].Role == "user" {
				userMsg = req.Messages[i].Content
				break
			}
		}

		if req.Stream {
			handleStream(r.Context(), w, g, userMsg)
		} else {
			handleUnary(r.Context(), w, g, userMsg)
		}
	})

	addr := fmt.Sprintf(":%d", *port)
	log.Printf("genkit-round1 listening on %s (upstream=%s)", addr, baseURL)
	log.Fatal(http.ListenAndServe(addr, mux))
}

func handleStream(ctx context.Context, w http.ResponseWriter, g *genkit.Genkit, userMsg string) {
	flusher, ok := w.(http.Flusher)
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.WriteHeader(http.StatusOK)
	if ok {
		flusher.Flush()
	}

	_, err := genkit.Generate(ctx, g,
		ai.WithModelName("openai/gpt-4o"),
		ai.WithMessages(ai.NewUserTextMessage(userMsg)),
		ai.WithStreaming(func(_ context.Context, chunk *ai.ModelResponseChunk) error {
			text := chunk.Text()
			if text == "" {
				return nil
			}
			data := map[string]any{
				"choices": []map[string]any{{
					"delta":         map[string]any{"content": text},
					"finish_reason": nil,
				}},
			}
			b, _ := json.Marshal(data)
			fmt.Fprintf(w, "data: %s\n\n", b)
			if ok {
				flusher.Flush()
			}
			return nil
		}),
	)
	if err != nil {
		log.Printf("generate error: %v", err)
	}

	// Stop chunk + DONE
	stop, _ := json.Marshal(map[string]any{
		"choices": []map[string]any{{
			"delta":         map[string]any{},
			"finish_reason": "stop",
		}},
	})
	fmt.Fprintf(w, "data: %s\n\n", stop)
	fmt.Fprint(w, "data: [DONE]\n\n")
	if ok {
		flusher.Flush()
	}
}

func handleUnary(ctx context.Context, w http.ResponseWriter, g *genkit.Genkit, userMsg string) {
	resp, err := genkit.Generate(ctx, g,
		ai.WithModelName("openai/gpt-4o"),
		ai.WithMessages(ai.NewUserTextMessage(userMsg)),
	)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{
		"choices": []map[string]any{{
			"message":       map[string]any{"role": "assistant", "content": resp.Text()},
			"finish_reason": "stop",
		}},
	})
}
