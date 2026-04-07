package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"
)

// ttsDialer connects to the TTS WebSocket endpoint and returns an open conn.
func ttsDialer(t *testing.T, srv *httptest.Server) *websocket.Conn {
	t.Helper()
	url := "ws" + strings.TrimPrefix(srv.URL, "http") + "/tts/ws"
	conn, _, err := websocket.DefaultDialer.Dial(url, http.Header{})
	if err != nil {
		t.Fatalf("dial TTS WebSocket: %v", err)
	}
	return conn
}

// sendSynthRequest sends a minimal Cartesia-style synthesis request.
func sendSynthRequest(t *testing.T, conn *websocket.Conn, text string) {
	t.Helper()
	req := map[string]string{
		"text":     text,
		"voice_id": "test-voice",
	}
	data, _ := json.Marshal(req)
	if err := conn.WriteMessage(websocket.TextMessage, data); err != nil {
		t.Fatalf("write synthesis request: %v", err)
	}
}

// TestTTSWebSocket_StreamsAudio verifies that the server sends multiple binary
// audio chunks of the configured size followed by a JSON {"type":"done"} message.
func TestTTSWebSocket_StreamsAudio(t *testing.T) {
	cfg := TTSProfile{
		FirstByteDelay:  0,
		ChunkSize:       1024,
		InterChunkDelay: 0,
	}

	srv := httptest.NewServer(NewTTSHandler(cfg))
	defer srv.Close()

	conn := ttsDialer(t, srv)
	defer conn.Close()

	sendSynthRequest(t, conn, "Hello, world!")

	var binaryCount int
	var totalBytes int
	var sawDone bool

	conn.SetReadDeadline(time.Now().Add(5 * time.Second))
	for {
		msgType, msg, err := conn.ReadMessage()
		if err != nil {
			break
		}
		if msgType == websocket.BinaryMessage {
			if len(msg) != cfg.ChunkSize {
				t.Errorf("binary chunk size: got %d, want %d", len(msg), cfg.ChunkSize)
			}
			binaryCount++
			totalBytes += len(msg)
		} else if msgType == websocket.TextMessage {
			var event struct {
				Type string `json:"type"`
			}
			if err := json.Unmarshal(msg, &event); err != nil {
				t.Errorf("failed to parse text message: %v — raw: %s", err, msg)
				continue
			}
			if event.Type == "done" {
				sawDone = true
				break
			}
		}
	}

	if binaryCount < 2 {
		t.Errorf("expected multiple binary chunks, got %d", binaryCount)
	}
	// ~32000 bytes total (1 sec of 16kHz 16-bit mono)
	if totalBytes < 30000 {
		t.Errorf("expected at least 30000 bytes total, got %d", totalBytes)
	}
	if !sawDone {
		t.Error("expected JSON {\"type\":\"done\"} message, not received")
	}
}

// TestTTSWebSocket_FirstByteDelay verifies that the first binary audio chunk
// is delayed by at least FirstByteDelay.
func TestTTSWebSocket_FirstByteDelay(t *testing.T) {
	const delay = 100 * time.Millisecond

	cfg := TTSProfile{
		FirstByteDelay:  delay,
		ChunkSize:       4096,
		InterChunkDelay: 0,
	}

	srv := httptest.NewServer(NewTTSHandler(cfg))
	defer srv.Close()

	conn := ttsDialer(t, srv)
	defer conn.Close()

	sendSynthRequest(t, conn, "Timing test")

	start := time.Now()

	conn.SetReadDeadline(time.Now().Add(5 * time.Second))
	for {
		msgType, _, err := conn.ReadMessage()
		if err != nil {
			t.Fatalf("read error before first binary chunk: %v", err)
		}
		if msgType == websocket.BinaryMessage {
			break
		}
	}
	elapsed := time.Since(start)

	const tolerance = 10 * time.Millisecond
	if elapsed < delay-tolerance {
		t.Errorf("first audio chunk arrived too fast: %v < %v", elapsed, delay-tolerance)
	}
}
