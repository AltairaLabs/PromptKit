package main

import (
	"encoding/base64"
	"encoding/json"
	"net/http"
	"time"

	"github.com/gorilla/websocket"
)

// wsUpgrader is a shared WebSocket upgrader that accepts all origins.
var wsUpgrader = websocket.Upgrader{
	CheckOrigin: func(_ *http.Request) bool { return true },
}

// synthRequest parses synthesis requests from both the simple protocol
// ({"text":"...","voice_id":"..."}) and the Pipecat/Cartesia protocol
// ({"transcript":"...","voice":{"mode":"id","id":"..."},...}).
type synthRequest struct {
	// Simple protocol fields.
	Text    string `json:"text"`
	VoiceID string `json:"voice_id"`

	// Pipecat/Cartesia protocol fields.
	Transcript string `json:"transcript"`
	Voice      struct {
		ID string `json:"id"`
	} `json:"voice"`
}

// isPipecatProtocol returns true when the request uses the Pipecat/Cartesia
// format (transcript field populated rather than text).
func (r *synthRequest) isPipecatProtocol() bool {
	return r.Transcript != "" || (r.Text == "" && r.VoiceID == "")
}

// NewTTSHandler returns an http.Handler that simulates a Cartesia-compatible
// TTS WebSocket endpoint at /tts/ws.
//
// Two wire protocols are supported:
//
// Simple protocol (used by the PromptKit round1 server and test harness):
//   - Request:  {"text":"...","voice_id":"..."}
//   - Response: raw binary audio frames, then JSON {"type":"done"}
//
// Pipecat/Cartesia protocol (used by Pipecat's CartesiaTTSService):
//   - Request:  {"transcript":"...","voice":{"mode":"id","id":"..."},...}
//   - Response: JSON {"type":"chunk","data":"<base64>"}, then {"type":"done"}
//
// The handler auto-detects which protocol is in use from the request shape.
func NewTTSHandler(cfg TTSProfile) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
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

		pipecatMode := req.isPipecatProtocol()

		// Extract context_id for Pipecat protocol (Cartesia sends it, expects it back).
		var contextID string
		if pipecatMode {
			var raw map[string]any
			if json.Unmarshal(msg, &raw) == nil {
				if cid, ok := raw["context_id"].(string); ok {
					contextID = cid
				}
			}
			if contextID == "" {
				contextID = "bench-ctx-1"
			}
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
			if pipecatMode {
				// Pipecat expects JSON {"type":"chunk","data":"<base64>","context_id":"..."}.
				encoded := base64.StdEncoding.EncodeToString(chunk)
				chunkMsg, _ := json.Marshal(map[string]any{
					"type":       "chunk",
					"data":       encoded,
					"context_id": contextID,
				})
				if err := conn.WriteMessage(websocket.TextMessage, chunkMsg); err != nil {
					return
				}
			} else {
				if err := conn.WriteMessage(websocket.BinaryMessage, chunk); err != nil {
					return
				}
			}
			if cfg.InterChunkDelay > 0 && i < numChunks-1 {
				time.Sleep(cfg.InterChunkDelay)
			}
		}

		// Send done message.
		doneMsg, _ := json.Marshal(map[string]any{"type": "done", "context_id": contextID})
		conn.WriteMessage(websocket.TextMessage, doneMsg) //nolint:errcheck
	})
}
