// Package tts provides text-to-speech services.
// This file contains WebSocket streaming implementation for Cartesia TTS.
// It is excluded from coverage testing due to the difficulty of mocking WebSocket connections.
package tts

import (
	"context"
	"fmt"
	"time"

	"github.com/gorilla/websocket"
)

// SynthesizeStream converts text to audio with streaming output via WebSocket.
// This provides ultra-low latency (<100ms first-byte) for real-time applications.
//
//nolint:gocritic // hugeParam: SynthesisConfig passed by value to satisfy StreamingService interface
func (s *CartesiaService) SynthesizeStream(
	ctx context.Context, text string, config SynthesisConfig,
) (<-chan AudioChunk, error) {
	if text == "" {
		return nil, ErrEmptyText
	}

	// Use config voice or default
	voice := config.Voice
	if voice == "" {
		voice = cartesiaDefaultVoice
	}

	// Use config model or service default
	model := config.Model
	if model == "" {
		model = s.model
	}

	// Connect to WebSocket
	wsURL := fmt.Sprintf("%s?api_key=%s&cartesia_version=2024-06-10", s.wsURL, s.apiKey)

	dialer := websocket.DefaultDialer
	conn, resp, err := dialer.DialContext(ctx, wsURL, nil)
	if resp != nil && resp.Body != nil {
		_ = resp.Body.Close()
	}
	if err != nil {
		return nil, NewSynthesisError("cartesia", "", "websocket connection failed", err, true)
	}

	outputFormat := s.mapFormat(config.Format)

	// Send synthesis request
	reqBody := map[string]interface{}{
		"model_id":   model,
		"transcript": text,
		"voice": map[string]string{
			"mode": "id",
			"id":   voice,
		},
		"output_format": outputFormat,
		"context_id":    fmt.Sprintf("ctx_%d", time.Now().UnixNano()),
	}

	if err := conn.WriteJSON(reqBody); err != nil {
		_ = conn.Close()
		return nil, NewSynthesisError("cartesia", "", "failed to send request", err, true)
	}

	// Create output channel
	chunks := make(chan AudioChunk, streamChannelBuffer)

	// Start goroutine to read responses
	go s.readStreamResponses(ctx, conn, chunks)

	return chunks, nil
}

// readStreamResponses reads audio chunks from the WebSocket connection.
func (s *CartesiaService) readStreamResponses(
	ctx context.Context, conn *websocket.Conn, chunks chan<- AudioChunk,
) {
	defer close(chunks)
	defer conn.Close()

	index := 0

	for {
		if ctx.Err() != nil {
			chunks <- AudioChunk{Error: ctx.Err()}
			return
		}

		var resp cartesiaWSResponse
		if err := conn.ReadJSON(&resp); err != nil {
			if !websocket.IsCloseError(err, websocket.CloseNormalClosure) {
				chunks <- AudioChunk{Error: err}
			}
			return
		}

		chunk, err := s.processWSResponse(&resp, index)
		if err != nil {
			chunks <- AudioChunk{Error: err}
			return
		}

		if chunk != nil {
			index++
			chunks <- *chunk
		}

		if resp.Done {
			return
		}
	}
}
