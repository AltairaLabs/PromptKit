package main

import (
	"encoding/json"
	"net/http"
	"time"
)

// NewOpenAISTTHandler returns an http.Handler for POST /v1/audio/transcriptions.
// It accepts multipart form data (OpenAI Whisper API format) and returns a
// JSON transcript after a configurable delay.
func NewOpenAISTTHandler(cfg STTProfile) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}

		// Consume the upload body (multipart audio file) without parsing it.
		// The mock doesn't need the actual audio — just the latency simulation.
		r.Body.Close()

		// Simulate transcription delay.
		if cfg.FinalDelay > 0 {
			time.Sleep(cfg.FinalDelay)
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{
			"text": "hello this is a mock transcript from the benchmark server",
		})
	})
}

// NewOpenAITTSHandler returns an http.Handler for POST /v1/audio/speech.
// It accepts a JSON body with text/voice/model and streams back raw PCM audio.
func NewOpenAITTSHandler(cfg TTSProfile) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}

		// Read request (we don't need the content, just simulate the response).
		r.Body.Close()

		// Simulate first-byte delay.
		if cfg.FirstByteDelay > 0 {
			time.Sleep(cfg.FirstByteDelay)
		}

		// Stream raw PCM audio. Same total bytes as the WS mock: ~32000 bytes
		// (1 second of 16kHz 16-bit mono).
		w.Header().Set("Content-Type", "audio/pcm")
		w.WriteHeader(http.StatusOK)

		flusher, ok := w.(http.Flusher)

		const totalBytes = 32000
		chunk := make([]byte, cfg.ChunkSize)
		sent := 0
		first := true
		for sent < totalBytes {
			remaining := totalBytes - sent
			size := cfg.ChunkSize
			if remaining < size {
				size = remaining
				chunk = chunk[:size]
			}

			if !first && cfg.InterChunkDelay > 0 {
				time.Sleep(cfg.InterChunkDelay)
			}
			first = false

			w.Write(chunk) //nolint:errcheck
			if ok {
				flusher.Flush()
			}
			sent += size
		}
	})
}
