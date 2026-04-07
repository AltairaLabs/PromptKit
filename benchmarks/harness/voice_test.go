package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gorilla/websocket"
)

var wsUpgrader = websocket.Upgrader{
	CheckOrigin: func(r *http.Request) bool { return true },
}

// echoVoiceServer reads frames until a text message, then sleeps 20ms and
// sends 3 binary chunks followed by a done JSON message.
func echoVoiceServer(w http.ResponseWriter, r *http.Request) {
	conn, err := wsUpgrader.Upgrade(w, r, nil)
	if err != nil {
		return
	}
	defer conn.Close()

	// Read until text message signalling end of audio.
	for {
		msgType, _, err := conn.ReadMessage()
		if err != nil {
			return
		}
		if msgType == websocket.TextMessage {
			break
		}
	}

	// Simulate processing delay.
	time.Sleep(20 * time.Millisecond)

	// Send 3 binary audio chunks.
	for i := 0; i < 3; i++ {
		if err := conn.WriteMessage(websocket.BinaryMessage, []byte{0x01, 0x02, 0x03}); err != nil {
			return
		}
	}

	// Send done signal.
	done, _ := json.Marshal(map[string]string{"type": "done"})
	if err := conn.WriteMessage(websocket.TextMessage, done); err != nil {
		return
	}
}

func TestVoiceDriver_MeasuresRoundTrip(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(echoVoiceServer))
	defer srv.Close()

	// Convert http:// URL to ws://.
	wsURL := "ws" + srv.URL[len("http"):]

	cfg := VoiceConfig{
		TargetURL:      wsURL,
		Concurrency:    2,
		Sessions:       4,
		AudioFrames:    3,
		FrameSize:      640,
		FrameInterval:  5 * time.Millisecond,
		SessionTimeout: 5 * time.Second,
	}

	agg, err := RunVoiceBenchmark(context.Background(), cfg)
	if err != nil {
		t.Fatalf("RunVoiceBenchmark: %v", err)
	}

	summary := agg.Summarize()

	if summary.Count != 4 {
		t.Errorf("expected count=4, got %d", summary.Count)
	}
	if summary.Errors != 0 {
		t.Errorf("expected errors=0, got %d", summary.Errors)
	}
	if summary.FirstByteP50 <= 0 {
		t.Errorf("expected non-zero p50 first-byte latency, got %v", summary.FirstByteP50)
	}
}
