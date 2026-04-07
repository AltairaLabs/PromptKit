package main

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"
)

// StreamingConfig holds parameters for a streaming benchmark run.
type StreamingConfig struct {
	TargetURL   string
	Concurrency int
	Requests    int
	Timeout     time.Duration
	Prompt      string
}

// chatRequest is the JSON body sent to the completions endpoint.
type chatRequest struct {
	Model    string        `json:"model"`
	Messages []chatMessage `json:"messages"`
	Stream   bool          `json:"stream"`
}

type chatMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

// RunStreamingBenchmark runs cfg.Requests streaming requests at cfg.Concurrency workers
// and returns an Aggregator with all results recorded.
func RunStreamingBenchmark(ctx context.Context, cfg StreamingConfig) (*Aggregator, error) {
	work := make(chan struct{}, cfg.Requests)
	for i := 0; i < cfg.Requests; i++ {
		work <- struct{}{}
	}
	close(work)

	client := &http.Client{Timeout: cfg.Timeout}
	agg := &Aggregator{}

	var wg sync.WaitGroup
	for i := 0; i < cfg.Concurrency; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for range work {
				result := doStreamRequest(ctx, client, cfg)
				agg.Record(result)
			}
		}()
	}
	wg.Wait()

	return agg, nil
}

// doStreamRequest performs a single streaming POST and returns timing metrics.
func doStreamRequest(ctx context.Context, client *http.Client, cfg StreamingConfig) RequestResult {
	body, err := json.Marshal(chatRequest{
		Model: "gpt-4o",
		Messages: []chatMessage{
			{Role: "user", Content: cfg.Prompt},
		},
		Stream: true,
	})
	if err != nil {
		return RequestResult{Error: fmt.Errorf("marshal: %w", err)}
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		cfg.TargetURL+"/v1/chat/completions", bytes.NewReader(body))
	if err != nil {
		return RequestResult{Error: fmt.Errorf("new request: %w", err)}
	}
	req.Header.Set("Content-Type", "application/json")

	start := time.Now()
	resp, err := client.Do(req)
	if err != nil {
		return RequestResult{Error: fmt.Errorf("do request: %w", err)}
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return RequestResult{Error: fmt.Errorf("unexpected status: %d", resp.StatusCode)}
	}

	var (
		firstByte      time.Duration
		chunkCount     int
		chunkIntervals []time.Duration
		lastChunk      time.Time
	)

	scanner := bufio.NewScanner(resp.Body)
	for scanner.Scan() {
		line := scanner.Text()
		if !strings.HasPrefix(line, "data: ") {
			continue
		}
		payload := strings.TrimPrefix(line, "data: ")
		if payload == "[DONE]" {
			break
		}

		now := time.Now()
		if chunkCount == 0 {
			firstByte = now.Sub(start)
			lastChunk = now
		} else {
			chunkIntervals = append(chunkIntervals, now.Sub(lastChunk))
			lastChunk = now
		}
		chunkCount++
	}

	if err := scanner.Err(); err != nil {
		return RequestResult{Error: fmt.Errorf("scan: %w", err)}
	}

	return RequestResult{
		FirstByteLatency: firstByte,
		TotalDuration:    time.Since(start),
		ChunkCount:       chunkCount,
		ChunkIntervals:   chunkIntervals,
	}
}
