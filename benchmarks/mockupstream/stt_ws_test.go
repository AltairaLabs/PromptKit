package main

import (
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"
)

func TestSTTWebSocket_SendsTranscripts(t *testing.T) {
	cfg := STTProfile{
		TranscriptionDelay: 10 * time.Millisecond,
		InterimInterval:    20 * time.Millisecond,
		FinalDelay:         30 * time.Millisecond,
	}

	srv := httptest.NewServer(NewSTTHandler(cfg))
	defer srv.Close()

	url := "ws" + strings.TrimPrefix(srv.URL, "http") + "/v1/listen"
	conn, _, err := websocket.DefaultDialer.Dial(url, nil)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer conn.Close()

	// Send 5 binary audio frames.
	for i := 0; i < 5; i++ {
		if err := conn.WriteMessage(websocket.BinaryMessage, []byte("audio-frame")); err != nil {
			t.Fatalf("write audio frame %d: %v", i, err)
		}
	}

	// Send CloseStream JSON.
	closeMsg, _ := json.Marshal(map[string]string{"type": "CloseStream"})
	if err := conn.WriteMessage(websocket.TextMessage, closeMsg); err != nil {
		t.Fatalf("write CloseStream: %v", err)
	}

	// Read events until the connection closes; collect interim and final transcripts.
	var interimCount, finalCount int
	conn.SetReadDeadline(time.Now().Add(5 * time.Second))
	for {
		_, msg, err := conn.ReadMessage()
		if err != nil {
			// Connection closed — expected after final transcript.
			break
		}
		var event struct {
			Type    string `json:"type"`
			IsFinal bool   `json:"is_final"`
			Channel struct {
				Alternatives []struct {
					Transcript string  `json:"transcript"`
					Confidence float64 `json:"confidence"`
				} `json:"alternatives"`
			} `json:"channel"`
		}
		if err := json.Unmarshal(msg, &event); err != nil {
			t.Logf("non-JSON message (skipping): %s", msg)
			continue
		}
		if event.Type != "Results" {
			continue
		}
		if event.IsFinal {
			finalCount++
		} else {
			interimCount++
		}
	}

	if interimCount == 0 {
		t.Error("expected at least one interim transcript, got 0")
	}
	if finalCount == 0 {
		t.Error("expected at least one final transcript, got 0")
	}
}
