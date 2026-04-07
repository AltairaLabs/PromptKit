package main

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

// sseChunk formats a single SSE data line containing a JSON chat completion chunk.
func sseChunk(content string) string {
	return fmt.Sprintf("data: {\"choices\":[{\"delta\":{\"content\":%q}}]}\n\n", content)
}

// sseServer returns an httptest.Server that streams a fixed number of SSE chunks then sends [DONE].
func sseServer(chunks int) *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-cache")
		w.Header().Set("Connection", "keep-alive")
		flusher, ok := w.(http.Flusher)
		if !ok {
			http.Error(w, "streaming not supported", http.StatusInternalServerError)
			return
		}
		for i := 0; i < chunks; i++ {
			fmt.Fprintf(w, "%s", sseChunk(fmt.Sprintf("token%d", i)))
			flusher.Flush()
		}
		fmt.Fprintf(w, "data: [DONE]\n\n")
		flusher.Flush()
	}))
}

func TestStreamingDriver_CollectsMetrics(t *testing.T) {
	srv := sseServer(5)
	defer srv.Close()

	cfg := StreamingConfig{
		TargetURL:   srv.URL,
		Concurrency: 5,
		Requests:    20,
		Timeout:     10 * time.Second,
		Prompt:      "hello",
	}

	agg, err := RunStreamingBenchmark(context.Background(), cfg)
	if err != nil {
		t.Fatalf("RunStreamingBenchmark returned error: %v", err)
	}

	summary := agg.Summarize()

	if summary.Count != 20 {
		t.Errorf("expected Count=20, got %d", summary.Count)
	}
	if summary.Errors != 0 {
		t.Errorf("expected 0 errors, got %d", summary.Errors)
	}
	if summary.FirstByteP50 == 0 {
		t.Error("expected non-zero FirstByteP50")
	}
}

func TestStreamingDriver_HandlesErrors(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "internal server error", http.StatusInternalServerError)
	}))
	defer srv.Close()

	cfg := StreamingConfig{
		TargetURL:   srv.URL,
		Concurrency: 2,
		Requests:    5,
		Timeout:     5 * time.Second,
		Prompt:      "hello",
	}

	agg, err := RunStreamingBenchmark(context.Background(), cfg)
	if err != nil {
		t.Fatalf("RunStreamingBenchmark returned error: %v", err)
	}

	summary := agg.Summarize()

	if summary.Count != 5 {
		t.Errorf("expected Count=5, got %d", summary.Count)
	}
	if summary.Errors != 5 {
		t.Errorf("expected Errors=5, got %d", summary.Errors)
	}
}
