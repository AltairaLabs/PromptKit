package main

import (
	"encoding/json"
	"net/http"
	"time"

	"github.com/gorilla/websocket"
)

// sttTranscriptEvent mirrors the Deepgram Results message format.
type sttTranscriptEvent struct {
	Type    string     `json:"type"`
	IsFinal bool       `json:"is_final"`
	Channel sttChannel `json:"channel"`
}

type sttChannel struct {
	Alternatives []sttAlternative `json:"alternatives"`
}

type sttAlternative struct {
	Transcript string  `json:"transcript"`
	Confidence float64 `json:"confidence"`
}

func newTranscriptEvent(isFinal bool, text string) sttTranscriptEvent {
	return sttTranscriptEvent{
		Type:    "Results",
		IsFinal: isFinal,
		Channel: sttChannel{
			Alternatives: []sttAlternative{
				{Transcript: text, Confidence: 0.98},
			},
		},
	}
}

// NewSTTHandler returns an http.Handler that implements a Deepgram-compatible
// STT WebSocket server at /v1/listen. It accepts connections with any path
// or query parameters (e.g. auth tokens appended by the Deepgram SDK).
func NewSTTHandler(cfg STTProfile) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := wsUpgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer conn.Close()

		// closeStream signals the read loop received a CloseStream message.
		closeStream := make(chan struct{})
		// finalize signals that a Finalize message was received — send a final
		// transcript immediately without closing the connection.
		finalize := make(chan struct{}, 1)

		// Read loop: consume binary audio frames; watch for control JSON messages.
		//
		// Supported message types:
		//   CloseStream  – end the session (existing behaviour)
		//   KeepAlive    – silently ignored (sent periodically by the Deepgram SDK)
		//   Finalize     – trigger an immediate final transcript without closing
		//   <unknown>    – silently ignored
		go func() {
			defer close(closeStream)
			for {
				msgType, msg, err := conn.ReadMessage()
				if err != nil {
					return
				}
				if msgType == websocket.TextMessage {
					var cmd struct {
						Type string `json:"type"`
					}
					if json.Unmarshal(msg, &cmd) == nil {
						switch cmd.Type {
						case "CloseStream":
							return
						case "Finalize":
							select {
							case finalize <- struct{}{}:
							default:
							}
							// "KeepAlive" and all other types are silently ignored.
						}
					}
				}
				// Binary audio frames are accepted and discarded.
			}
		}()

		// Write loop: send interim transcripts on a ticker; send final on close.
		// We guarantee at least one interim before honoring CloseStream by
		// waiting for the first ticker tick before checking closeStream.
		ticker := time.NewTicker(cfg.InterimInterval)
		defer ticker.Stop()

		streamClosed := false
		for {
			select {
			case <-closeStream:
				// Mark that CloseStream arrived; drain one more ticker tick so
				// the client sees at least one interim before the final.
				streamClosed = true
				closeStream = nil // prevent double-select on closed channel

			case <-finalize:
				// Finalize: send a final transcript immediately but keep the
				// connection open (the client may send more audio afterwards).
				time.Sleep(cfg.FinalDelay)
				finalEvent := newTranscriptEvent(true, "final transcript")
				finalData, _ := json.Marshal(finalEvent)
				if err := conn.WriteMessage(websocket.TextMessage, finalData); err != nil {
					return
				}

			case <-ticker.C:
				// Simulate processing delay before emitting interim result.
				time.Sleep(cfg.TranscriptionDelay)
				event := newTranscriptEvent(false, "interim transcript")
				data, _ := json.Marshal(event)
				if err := conn.WriteMessage(websocket.TextMessage, data); err != nil {
					return
				}
				if streamClosed {
					// We've sent at least one interim after CloseStream; send final.
					time.Sleep(cfg.FinalDelay)
					finalEvent := newTranscriptEvent(true, "final transcript")
					finalData, _ := json.Marshal(finalEvent)
					_ = conn.WriteMessage(websocket.TextMessage, finalData)
					return
				}
			}
		}
	})
}
