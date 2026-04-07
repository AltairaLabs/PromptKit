package main

import (
	"encoding/json"
	"net/http"
	"time"

	"github.com/gorilla/websocket"
)

// wsUpgrader is a shared WebSocket upgrader that accepts all origins.
var wsUpgrader = websocket.Upgrader{
	CheckOrigin: func(_ *http.Request) bool { return true },
}

// synthRequest is the minimal Cartesia-compatible synthesis request.
type synthRequest struct {
	Text    string `json:"text"`
	VoiceID string `json:"voice_id"`
}

// NewTTSHandler returns an http.Handler that simulates a Cartesia-compatible
// TTS WebSocket endpoint at /tts/ws.
//
// Protocol:
//  1. Upgrade HTTP to WebSocket.
//  2. Read one JSON synthesis request.
//  3. Wait cfg.FirstByteDelay.
//  4. Send binary audio chunks of cfg.ChunkSize bytes until ~32000 bytes total,
//     with cfg.InterChunkDelay between each chunk.
//  5. Send JSON {"type":"done"}.
func NewTTSHandler(cfg TTSProfile) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/tts/ws", func(w http.ResponseWriter, r *http.Request) {
		conn, err := wsUpgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer conn.Close()

		// Read synthesis request.
		_, msg, err := conn.ReadMessage()
		if err != nil {
			return
		}
		var req synthRequest
		if err := json.Unmarshal(msg, &req); err != nil {
			return
		}

		// Apply first-byte delay.
		if cfg.FirstByteDelay > 0 {
			time.Sleep(cfg.FirstByteDelay)
		}

		// Send full audio chunks totalling at least 32000 bytes
		// (approx. 1 sec of 16kHz 16-bit mono audio). Each chunk is exactly
		// cfg.ChunkSize bytes; we send ceiling(32000/chunkSize) chunks.
		const targetBytes = 32000
		chunk := make([]byte, cfg.ChunkSize)
		numChunks := (targetBytes + cfg.ChunkSize - 1) / cfg.ChunkSize
		for i := 0; i < numChunks; i++ {
			if err := conn.WriteMessage(websocket.BinaryMessage, chunk); err != nil {
				return
			}
			if cfg.InterChunkDelay > 0 && i < numChunks-1 {
				time.Sleep(cfg.InterChunkDelay)
			}
		}

		// Send done message.
		done, _ := json.Marshal(map[string]string{"type": "done"})
		conn.WriteMessage(websocket.TextMessage, done) //nolint:errcheck
	})
	return mux
}
